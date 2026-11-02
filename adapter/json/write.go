package json

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"slices"

	"github.com/simon-lentz/yammm/adapter/internal/constraintof"
	"github.com/simon-lentz/yammm/adapter/internal/refusal"
	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
)

// WriteOption configures serialization behavior for MarshalObject and WriteObject.
type WriteOption func(*writeConfig)

// writeConfig holds configuration for JSON serialization.
type writeConfig struct {
	indent string
}

// WithIndent sets the indentation string for pretty-printing.
// Use "" for compact output (default), "\t" for tab indentation,
// or "  " (two spaces) for space indentation.
func WithIndent(indent string) WriteOption {
	return func(c *writeConfig) {
		c.indent = indent
	}
}

// MarshalObject serializes a graph snapshot to JSON bytes in object-keyed format.
//
// The output format groups instances by type name:
//
//	{
//	  "Person": [{"id": "p1", "name": "Alice"}, ...],
//	  "Company": [{"id": "c1", "name": "Acme"}, ...]
//	}
//
// Instances include their properties, composed children (inline), and foreign key
// references for resolved associations.
//
// Returns ErrZeroSnapshot if result is zero. MarshalObject checks ctx once per type
// group and returns an error wrapping ctx.Err(), and nothing else.
func (a *Adapter) MarshalObject(ctx context.Context, result graph.Snapshot, opts ...WriteOption) ([]byte, error) {
	if result.IsZero() {
		return nil, ErrZeroSnapshot
	}
	cfg := &writeConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	output, err := a.buildOutput(ctx, result)
	if err != nil {
		return nil, err
	}

	// The document is a data file people read and edit, and no HTML embeds it,
	// so "<", ">" and "&" are written as themselves; a JSON reader, this
	// package's own among them, reads either spelling as the same text.
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", cfg.indent)
	if err := enc.Encode(output); err != nil {
		return nil, fmt.Errorf("json marshal: %w", err)
	}
	// Encode ends the value with a newline that json.Marshal does not write.
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

// WriteObject writes a graph snapshot to an io.Writer in JSON object-keyed format.
//
// See MarshalObject for output format details.
//
// Returns the number of bytes written and ErrZeroSnapshot if result is zero.
// Returns io.ErrShortWrite if the writer reports no error and accepts fewer
// bytes than provided.
func (a *Adapter) WriteObject(ctx context.Context, w io.Writer, result graph.Snapshot, opts ...WriteOption) (int64, error) {
	data, err := a.MarshalObject(ctx, result, opts...)
	if err != nil {
		return 0, err
	}
	// [Adapter.buildOutput] returns a cancellation it sees before a type group.
	// This check covers what follows the last one — that group's build and the
	// encode — so a run cancelled there writes nothing rather than a whole
	// document nobody waited for.
	if err := ctx.Err(); err != nil {
		return 0, fmt.Errorf("json write object: %w", err)
	}

	n, err := w.Write(data)
	if err == nil && n < len(data) {
		return int64(n), io.ErrShortWrite
	}
	return int64(n), err
}

// buildOutput constructs the JSON-serializable output map from a graph snapshot.
// Cancellation is checked per type group, the unit of work the loop walks, as
// [snapshot.Marshal] checks per group during emission. The CSV writer is finer:
// it checks per instance, so a single very large type group stops sooner there
// than here.
func (a *Adapter) buildOutput(ctx context.Context, result graph.Snapshot) (map[string]any, error) {
	output := make(map[string]any)
	s := result.Schema()

	// encoding/json sorts an object's keys, so the order the types are walked
	// in does not reach the output.
	for _, typeID := range result.Types() {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("json marshal object: %w", err)
		}

		typeName, ok := schema.AddressableTag(s, typeID)
		if !ok {
			return nil, fmt.Errorf("json adapter: snapshot denotes type %s, which the entry schema cannot name, so the output object has no key for it; no constructor builds such a snapshot, so an invariant is broken", typeID)
		}
		instances := slices.Collect(result.InstancesOf(typeID))
		serialized := make([]map[string]any, 0, len(instances))

		for _, inst := range instances {
			obj, err := serializeInstance(inst, result, s)
			if err != nil {
				return nil, err
			}
			serialized = append(serialized, obj)
		}

		output[typeName] = serialized
	}

	return output, nil
}

// serializeInstance converts a graph.Instance to a JSON-serializable map,
// in the shapes the adapter's own parser accepts: an association target is
// a _target_-keyed object with its edge properties beside the components,
// and a composition is an array for every multiplicity.
// Edge lookup uses snap.EdgesFrom for O(1) per-instance access.
func serializeInstance(inst graph.Instance, snap graph.Snapshot, s schema.Schema) (map[string]any, error) {
	obj := make(map[string]any)

	// The field names every relation renders under come from the type.
	schemaType, ok := s.TypeByID(inst.TypeID())
	if !ok {
		return nil, fmt.Errorf("json adapter: instance %s: type %s does not resolve, so its relations' field names are unknowable; no constructor builds such a snapshot, so an invariant is broken",
			diag.ValueText(inst.PrimaryKey().String()), inst.TypeID())
	}

	// 1. Add properties. encoding/json sorts an object's keys, so the order
	// they are added in does not reach the output.
	for name, val := range inst.Properties().SortedRange() {
		v, ok := unwrapValue(val, constraintof.Property(schemaType, name))
		if !ok {
			return nil, refusal.New(ErrUnrepresentable, "json adapter: instance %s: property %q holds %s",
				diag.ValueText(inst.PrimaryKey().String()), name, unwritable)
		}
		obj[name] = v
	}

	// 2. Add association targets using the snapshot edge index.
	// EdgesFrom returns edges already sorted by (relation, targetType, targetKey).
	// Group edges by relation name, preserving sorted order within each group.
	var byRel map[string][]graph.Edge
	var relOrder []string
	for e := range snap.EdgesFrom(inst) {
		if byRel == nil {
			byRel = make(map[string][]graph.Edge)
		}
		rel := e.Relation()
		if _, seen := byRel[rel]; !seen {
			relOrder = append(relOrder, rel)
		}
		byRel[rel] = append(byRel[rel], e)
	}

	for _, relName := range relOrder {
		edges := byRel[relName]

		// Every constructor holds an edge to an association the type
		// declares, at its declared target, and a (one) to one record.
		rel, _ := schemaType.Relation(relName)
		fieldName := rel.FieldName()

		objs := make([]any, len(edges))
		for i, e := range edges {
			target, err := edgeTargetObject(s, rel, e)
			if err != nil {
				return nil, err
			}
			objs[i] = target
		}

		// A (one) association emits the single object the parser expects,
		// and a (many) the array.
		if !rel.IsMany() {
			obj[fieldName] = objs[0]
		} else {
			obj[fieldName] = objs
		}
	}

	// 3. Add composed children, always as an array — the
	// parser requires one for every composition, (one) included.
	// ComposedRelations returns sorted relation names.
	for _, relName := range inst.ComposedRelations() {
		// Every constructor holds a slot to a composition the type declares.
		rel, _ := schemaType.Relation(relName)
		fieldName := rel.FieldName()

		arr := make([]map[string]any, 0, inst.ComposedCount(relName))
		for child := range inst.Composed(relName) {
			m, err := serializeInstance(child, snap, s)
			if err != nil {
				return nil, err
			}
			arr = append(arr, m)
		}
		obj[fieldName] = arr
	}

	return obj, nil
}

// edgeTargetObject renders one resolved edge as the object the parser
// accepts: _target_<pk> components in the target's canonical stored form,
// with the edge properties beside them. Every constructor holds the target to
// the association's declared type and its stored key to that type's arity,
// so an unresolvable target is a broken invariant and carries no refusal class.
func edgeTargetObject(s schema.Schema, rel schema.Relation, e graph.Edge) (map[string]any, error) {
	target, ok := s.TypeByID(e.Target().TypeID())
	if !ok {
		return nil, fmt.Errorf("json adapter: cannot render edge %q: target type %s does not resolve, so its _target_ field names are unknowable; no constructor builds such a snapshot, so an invariant is broken",
			e.Relation(), e.Target().TypeID())
	}
	pks := slices.Collect(target.PrimaryKeys())
	key := e.Target().PrimaryKey()

	out := make(map[string]any, len(pks))
	for i, pk := range pks {
		// "_target_" is the parser's reserved prefix; no declared property
		// can begin with an underscore, so the namespaces cannot collide. A
		// stored key is in the form its constraint stores where it renders one.
		out["_target_"+pk.Name()] = key.Get(i).Unwrap()
	}
	for name, val := range e.Properties().SortedRange() {
		var c schema.Constraint
		if p, ok := rel.Property(name); ok {
			c = p.Constraint()
		}
		v, ok := unwrapValue(val, c)
		if !ok {
			return nil, refusal.New(ErrUnrepresentable, "json adapter: edge %q to %s: edge property %q holds %s",
				e.Relation(), diag.ValueText(e.Target().PrimaryKey().String()), name, unwritable)
		}
		out[name] = v
	}
	return out, nil
}

// unwritable names what [unwrapValue] refuses, for the refusal that names the
// property holding it.
const unwritable = "a number literal this adapter does not read back as a number"

// unwrapValue recursively converts an immutable.Value to a JSON-compatible any,
// rendering each scalar in the form its constraint stores. A collection
// descends with its element constraint, so a List<Timestamp> canonicalizes at
// its elements and not only at its outer level. It reports false when
// [canonicalOrRaw] refuses a value anywhere in v.
func unwrapValue(v immutable.Value, c schema.Constraint) (any, bool) {
	if m, ok := v.Map(); ok {
		result := make(map[string]any, m.Len())
		for k, val := range m.Range() {
			u, ok := unwrapValue(val, nil)
			if !ok {
				return nil, false
			}
			result[k] = u
		}
		return result, true
	}
	if s, ok := v.Slice(); ok {
		elem := constraintof.Element(c)
		result := make([]any, s.Len())
		for i, val := range s.Iter2() {
			u, ok := unwrapValue(val, elem)
			if !ok {
				return nil, false
			}
			result[i] = u
		}
		return result, true
	}

	return canonicalOrRaw(v.Unwrap(), c)
}

// canonicalOrRaw renders raw in the form its constraint stores, or as it stands
// when the constraint cannot: MarshalObject returns an error rather than a
// diag.Result, so failing here would fail a whole export over one malformed
// value. A float returns as a json.RawMessage with a float indicator (".", "e" or
// "E"), the set [immutable.IsIntegerLiteral] reads as a float: without it a whole
// float emits int-shaped and reads back as an integer wherever a reader classifies
// a number by its spelling, a negative zero without its sign. The indicator
// follows the Go type the value holds, not its constraint: a float held under an
// Integer is written as the float it is, so the document states what the snapshot
// holds and the validator refuses it on the way back. It reports false for a
// json.Number [readsBack] refuses.
func canonicalOrRaw(raw any, c schema.Constraint) (any, bool) {
	held := raw
	if canonical, err := instance.CanonicalValue(raw, c); err == nil {
		held = canonical
	}
	switch x := held.(type) {
	case float64:
		// The digits come from encoding/json, so this cannot drift from the encoder
		// that writes every other number in the document.
		b, _ := json.Marshal(x) //nolint:errchkjson // a held float is finite
		if !bytes.ContainsAny(b, ".eE") {
			b = append(b, '.', '0')
		}
		return json.RawMessage(b), true
	case json.Number:
		return x, readsBack(x)
	}
	return held, true
}

// readsBack reports whether the literal encoding/json writes for n, a JSON
// number literal as every json.Number a validator or a document holds is, is read
// back as the number n spells. encoding/json writes the literal as it stands.
// [Adapter.ParseObject] hands the validator the literal's own text or the
// number it spells, and a Float, which reads every number either numeric kind
// reads, judges both alike: a literal no finite float64 holds is a number of
// neither kind.
func readsBack(n json.Number) bool {
	return instance.CheckValue(n, schema.NewFloatConstraint()) == nil
}
