package json

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"reflect"

	"github.com/simon-lentz/yammm/adapter/internal/constraintof"
	"github.com/simon-lentz/yammm/adapter/internal/refusal"
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
// Returns ErrNilResult if result is nil. MarshalObject checks ctx once per type
// group and returns an error wrapping ctx.Err(), and nothing else.
func (a *Adapter) MarshalObject(ctx context.Context, result *graph.Snapshot, opts ...WriteOption) ([]byte, error) {
	if result == nil {
		return nil, ErrNilResult
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
// Returns the number of bytes written and ErrNilResult if result is nil.
// Returns io.ErrShortWrite if the writer accepts fewer bytes than provided.
func (a *Adapter) WriteObject(ctx context.Context, w io.Writer, result *graph.Snapshot, opts ...WriteOption) (int64, error) {
	data, err := a.MarshalObject(ctx, result, opts...)
	if err != nil {
		return 0, err
	}
	// A cancellation during the build already returned above, from
	// [Adapter.buildOutput]. This covers the remaining window — the encode
	// itself — so a run cancelled there writes nothing rather than a whole
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
// [snapshot.Marshal] checks per group during emission. The CSV writers are
// finer: writeSnapshotTypeTo checks per instance, so a single very large type
// group stops sooner there than here.
func (a *Adapter) buildOutput(ctx context.Context, result *graph.Snapshot) (map[string]any, error) {
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
		instances := result.InstancesOf(typeID)
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

// lookupType resolves a TypeID to its schema.Type across the entry schema's
// whole import closure. Identity, not name: a transitively imported type has
// no alias to qualify with, so a name-keyed walk of local types and direct
// imports cannot reach it.
func lookupType(s *schema.Schema, id schema.TypeID) (*schema.Type, bool) {
	if s == nil {
		return nil, false
	}
	// TypeByID indexes the whole import closure. Walking direct imports found
	// no transitively imported type, which the v3 wire preserves through Load.
	return s.TypeByID(id)
}

// serializeInstance converts a graph.Instance to a JSON-serializable map,
// in the shapes the adapter's own parser accepts: an association target is
// a _target_-keyed object with its edge properties beside the components,
// and a composition is an array for every multiplicity.
// Edge lookup uses snap.EdgesFrom for O(1) per-instance access.
func serializeInstance(inst *graph.Instance, snap *graph.Snapshot, s *schema.Schema) (map[string]any, error) {
	obj := make(map[string]any)

	// The field names every relation renders under come from the type.
	schemaType, ok := lookupType(s, inst.TypeID())
	if !ok {
		return nil, fmt.Errorf("json adapter: instance %s: type %s does not resolve, so its relations' field names are unknowable; no constructor builds such a snapshot, so an invariant is broken",
			inst.PrimaryKey(), inst.TypeID())
	}

	// 1. Add properties. encoding/json sorts an object's keys, so the order
	// they are added in does not reach the output.
	for name, val := range inst.Properties().SortedRange() {
		v, ok := unwrapValue(val, constraintof.Property(schemaType, name))
		if !ok {
			return nil, refusal.New(ErrUnrepresentable, "json adapter: instance %s: property %q holds a value JSON cannot write: a non-finite float, or a Go value encoding/json refuses",
				inst.PrimaryKey(), name)
		}
		obj[name] = v
	}

	// 2. Add association targets using the snapshot edge index.
	// EdgesFrom returns edges already sorted by (relation, targetType, targetKey).
	allEdges := snap.EdgesFrom(inst)
	if len(allEdges) > 0 {
		// Group edges by relation name, preserving sorted order within each group.
		byRel := make(map[string][]*graph.Edge)
		var relOrder []string
		for _, e := range allEdges {
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
	}

	// 3. Add composed children, always as an array — the
	// parser requires one for every composition, (one) included.
	// ComposedRelations returns sorted relation names; Composed returns a defensive copy.
	for _, relName := range inst.ComposedRelations() {
		children := inst.Composed(relName)

		// Every constructor holds a slot to a composition the type declares.
		rel, _ := schemaType.Relation(relName)
		fieldName := rel.FieldName()

		arr := make([]map[string]any, len(children))
		for i, child := range children {
			m, err := serializeInstance(child, snap, s)
			if err != nil {
				return nil, err
			}
			arr[i] = m
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
func edgeTargetObject(s *schema.Schema, rel *schema.Relation, e *graph.Edge) (map[string]any, error) {
	target, ok := lookupType(s, e.Target().TypeID())
	if !ok {
		return nil, fmt.Errorf("json adapter: cannot render edge %q: target type %s does not resolve, so its _target_ field names are unknowable; no constructor builds such a snapshot, so an invariant is broken",
			e.Relation(), e.Target().TypeID())
	}
	pks := target.PrimaryKeysSlice()
	key := e.Target().PrimaryKey()

	out := make(map[string]any, len(pks))
	for i, pk := range pks {
		// "_target_" is the parser's reserved prefix; no declared property
		// can begin with an underscore, so the namespaces cannot collide. An
		// immutable.Key holds no component JSON cannot write.
		v, _ := canonicalOrRaw(key.Get(i).Unwrap(), pk.Constraint())
		out["_target_"+pk.Name()] = v
	}
	for name, val := range e.Properties().SortedRange() {
		var c schema.Constraint
		if p, ok := rel.Property(name); ok {
			c = p.Constraint()
		}
		v, ok := unwrapValue(val, c)
		if !ok {
			return nil, refusal.New(ErrUnrepresentable, "json adapter: edge %q to %s: edge property %q holds a value JSON cannot write: a non-finite float, or a Go value encoding/json refuses",
				e.Relation(), e.Target().PrimaryKey(), name)
		}
		out[name] = v
	}
	return out, nil
}

// unwrapValue recursively converts an immutable.Value to a JSON-compatible any,
// rendering each scalar in the form its constraint stores. A collection
// descends with its element constraint, so a List<Timestamp> canonicalizes at
// its elements and not only at its outer level. It reports false when a
// non-finite float stands anywhere in v.
func unwrapValue(v immutable.Value, c schema.Constraint) (any, bool) {
	if v.IsNil() {
		return nil, true
	}

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

// canonicalOrRaw renders raw in the form its constraint stores, then marks a
// float through [withFloatIndicator], so a float returns as a json.RawMessage
// rather than as the float64 it is stored as. It returns raw untouched, marked
// the same way, when the constraint cannot render it: MarshalObject returns an
// error rather than a diag.Result, so failing here would fail a whole export
// over one malformed value.
func canonicalOrRaw(raw any, c schema.Constraint) (any, bool) {
	canonical, err := instance.CanonicalValue(raw, c)
	if err != nil {
		return withFloatIndicator(raw)
	}
	return withFloatIndicator(canonical)
}

// withFloatIndicator renders a float with a float indicator (".", "e" or "E"),
// the set [immutable.IsIntegerLiteral] reads as a float. Without it a whole
// float emits int-shaped and reads back as an integer wherever a reader
// classifies a number by its spelling, a negative zero without its sign. The
// indicator follows the Go type the value holds, not its constraint:
// a float held under an Integer is written as the float it is, so the document
// states what the snapshot holds and the validator refuses it on the way back.
//
// The digits come from encoding/json itself, so this cannot drift from the
// encoder that writes every other number in the document. A float32, and a
// named type over one, keeps its 32-bit shortest form. Every other value
// passes through. A non-finite float reports false, JSON having no number for
// it, and so does a Go value encoding/json refuses.
func withFloatIndicator(v any) (any, bool) {
	var b []byte
	var err error
	switch rv := reflect.ValueOf(v); rv.Kind() {
	case reflect.Float32:
		b, err = json.Marshal(float32(rv.Float()))
	case reflect.Float64:
		b, err = json.Marshal(rv.Float())
	case reflect.Map, reflect.Array, reflect.Slice, reflect.Pointer, reflect.Struct, reflect.Interface,
		reflect.Chan, reflect.Func, reflect.Complex64, reflect.Complex128, reflect.UnsafePointer:
		// A Go value the immutable layer holds unwrapped: encoding/json
		// decides whether it has a spelling, a NaN inside one included.
		if _, err := json.Marshal(v); err != nil {
			return nil, false
		}
		return v, true
	default:
		return v, true
	}
	// encoding/json refuses a float only when it is NaN or an infinity.
	if err != nil {
		return nil, false
	}
	if !bytes.ContainsAny(b, ".eE") {
		b = append(b, '.', '0')
	}
	return json.RawMessage(b), true
}
