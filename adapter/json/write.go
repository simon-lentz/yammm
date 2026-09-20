package json

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"

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
// Returns ErrNilResult if result is nil.
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

	var data []byte
	if cfg.indent != "" {
		data, err = json.MarshalIndent(output, "", cfg.indent)
	} else {
		data, err = json.Marshal(output)
	}
	if err != nil {
		return nil, fmt.Errorf("json marshal: %w", err)
	}
	return data, nil
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

	// Iterate types in sorted order for deterministic output
	for _, typeID := range result.Types() {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("json marshal object: %w", err)
		}

		typeName, ok := schema.AddressableTag(s, typeID)
		if !ok {
			return nil, fmt.Errorf("json adapter: snapshot denotes type %s, which the entry schema cannot name, so the output object has no key for it; every constructor refuses such a snapshot, so this is an invariant violation", typeID)
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

	// Lookup the type for schema-based serialization
	schemaType, hasType := lookupType(s, inst.TypeID())

	// 1. Add properties in sorted order for deterministic output
	for name, val := range inst.Properties().SortedRange() {
		obj[name] = unwrapValue(val, propertyConstraint(schemaType, name))
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

			fieldName := relName // fallback
			var rel *schema.Relation
			if hasType {
				if r, ok := schemaType.Relation(relName); ok {
					rel = r
					fieldName = r.FieldName()
				}
			}

			objs := make([]any, len(edges))
			for i, e := range edges {
				target, err := edgeTargetObject(s, rel, e)
				if err != nil {
					return nil, err
				}
				objs[i] = target
			}

			// A resolvable (one) relation carrying one edge emits the single
			// object the parser expects. Everything else — (many), an
			// unresolvable relation name, or a (one) carrying several edges
			// (a bypass-built graph) — emits the array, the shape that does
			// not invent a multiplicity the schema cannot confirm.
			if rel != nil && !rel.IsMany() && len(objs) == 1 {
				obj[fieldName] = objs[0]
			} else {
				obj[fieldName] = objs
			}
		}
	}

	// 3. Add composed children in sorted order, always as an array — the
	// parser requires one for every composition, (one) included.
	// ComposedRelations returns sorted relation names; Composed returns a defensive copy.
	for _, relName := range inst.ComposedRelations() {
		children := inst.Composed(relName)

		fieldName := relName // fallback
		if hasType {
			if rel, ok := schemaType.Relation(relName); ok {
				fieldName = rel.FieldName()
			}
		}

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
// with the edge properties beside them. The target type supplies the field
// names, so an unresolvable target is an error rather than a shape this
// adapter's own parser rejects.
//
// The two arms differ in what a caller can do about them. An unresolvable
// target type cannot be reached: every type a snapshot denotes is one the
// entry schema can name, which every constructor holds, so that arm is an
// invariant violation and carries no refusal class. A target key of the wrong
// arity IS reachable — [graph.RebuildSnapshot] checks identity, denoted types,
// root types and cardinality, and no key arity — so that arm carries
// [ErrUnrepresentable]. Any differing arity reaches it, not only a short key.
func edgeTargetObject(s *schema.Schema, rel *schema.Relation, e *graph.Edge) (map[string]any, error) {
	target, ok := lookupType(s, e.Target().TypeID())
	if !ok {
		return nil, fmt.Errorf("json adapter: cannot render edge %q: target type %s does not resolve, so its _target_ field names are unknowable; every constructor refuses such a snapshot, so this is an invariant violation",
			e.Relation(), e.Target().TypeID())
	}
	pks := target.PrimaryKeysSlice()
	key := e.Target().PrimaryKey()
	if key.Len() != len(pks) {
		return nil, refuse(ErrUnrepresentable, "json adapter: edge %q target key has %d components; type %s declares %d",
			e.Relation(), key.Len(), e.Target().TypeID(), len(pks))
	}

	out := make(map[string]any, len(pks))
	for i, pk := range pks {
		// "_target_" is the parser's reserved prefix; no declared property
		// can begin with an underscore, so the namespaces cannot collide.
		out["_target_"+pk.Name()] = canonicalOrRaw(key.Get(i).Unwrap(), pk.Constraint())
	}
	for name, val := range e.Properties().SortedRange() {
		var c schema.Constraint
		if rel != nil {
			if p, ok := rel.Property(name); ok {
				c = p.Constraint()
			}
		}
		out[name] = unwrapValue(val, c)
	}
	return out, nil
}

// unwrapValue recursively converts an immutable.Value to a JSON-compatible any,
// rendering each scalar in the form its constraint stores. A collection
// descends with its element constraint, so a List<Timestamp> canonicalizes at
// its elements and not only at its outer level.
func unwrapValue(v immutable.Value, c schema.Constraint) any {
	if v.IsNil() {
		return nil
	}

	// Check for wrapped collections
	if m, ok := v.Map(); ok {
		result := make(map[string]any, m.Len())
		for k, val := range m.Range() {
			result[k] = unwrapValue(val, nil)
		}
		return result
	}
	if s, ok := v.Slice(); ok {
		elem := elementConstraint(c)
		result := make([]any, s.Len())
		for i, val := range s.Iter2() {
			result[i] = unwrapValue(val, elem)
		}
		return result
	}

	// Primitives: rendered through the constraint.
	return canonicalOrRaw(v.Unwrap(), c)
}

// propertyConstraint returns a declared property's constraint, or nil for a
// name the type does not declare or a type that did not resolve.
func propertyConstraint(t *schema.Type, name string) schema.Constraint {
	if t == nil {
		return nil
	}
	p, ok := t.Property(name)
	if !ok {
		return nil
	}
	return p.Constraint()
}

// elementConstraint returns the constraint each element of a collection
// renders through: a List's element constraint, and Float for a Vector, whose
// elements are floats the constraint does not name one by one.
func elementConstraint(c schema.Constraint) schema.Constraint {
	if c == nil {
		return nil
	}
	switch rc := schema.ResolveAlias(c).(type) {
	case schema.ListConstraint:
		return rc.Element()
	case schema.VectorConstraint:
		return schema.NewFloatConstraint()
	}
	return nil
}

// canonicalOrRaw renders raw in the form its constraint stores, then marks a
// float-bearing value through [withFloatIndicator], so a Float returns as a
// json.RawMessage rather than as the float64 it is stored as. It returns raw
// untouched when the constraint cannot render it: MarshalObject returns an
// error rather than a diag.Result, so failing here would fail a whole export
// over one malformed value.
func canonicalOrRaw(raw any, c schema.Constraint) any {
	canonical, err := instance.CanonicalValue(raw, c)
	if err != nil {
		return raw
	}
	return withFloatIndicator(canonical, c)
}

// withFloatIndicator renders a value the schema declares float-bearing with a
// float indicator (".", "e" or "E") — the set [immutable.NormalizeNumber]
// reads as float64. Without it a whole float emits int-shaped and narrows to
// int64 across this package's own round trip, and the sign of a negative zero
// is lost with it.
//
// The digits come from encoding/json itself, so this cannot drift from the
// encoder that writes every other number in the document. A value under any
// other constraint passes through.
//
// A non-finite float never arrives: [instance.CanonicalValue] refuses one, so
// [canonicalOrRaw] returns it raw and the document's own Marshal fails the
// export. The Marshal error below is therefore unreachable through that
// caller, and returns the value to the path it would have taken anyway.
//
// A Vector's elements arrive under a Float constraint that [elementConstraint]
// supplies, so they are rendered by this same rule.
func withFloatIndicator(v any, c schema.Constraint) any {
	if c == nil || schema.ResolveAlias(c).Kind() != schema.KindFloat {
		return v
	}
	f, ok := v.(float64)
	if !ok {
		return v
	}
	b, err := json.Marshal(f)
	if err != nil {
		return v
	}
	if !bytes.ContainsAny(b, ".eE") {
		b = append(b, '.', '0')
	}
	return json.RawMessage(b)
}
