package json

import (
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
// Returns ErrNilResult if result is nil. When two types in the snapshot
// render the same output name, returns an error naming both identities — a
// name-keyed object cannot separate them.
//
//nolint:revive // ctx reserved for future use (cancellation, tracing)
func (a *Adapter) MarshalObject(ctx context.Context, result *graph.Snapshot, opts ...WriteOption) ([]byte, error) {
	if result == nil {
		return nil, ErrNilResult
	}
	if err := renderedNameCollision(result); err != nil {
		return nil, err
	}

	cfg := &writeConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	output, err := a.buildOutput(result)
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

	n, err := w.Write(data)
	if err == nil && n < len(data) {
		return int64(n), io.ErrShortWrite
	}
	return int64(n), err
}

// renderedNameCollision reports an error when two type identities in the
// snapshot render one output name. The rendering is lossy where the snapshot
// is not, so the writer refuses rather than silently merging the pair.
func renderedNameCollision(snap *graph.Snapshot) error {
	s := snap.Schema()
	seen := make(map[string]schema.TypeID)
	for _, id := range snap.Types() {
		name := schema.TagForm(s, id)
		if first, ok := seen[name]; ok {
			return fmt.Errorf("json adapter: type %s and type %s both render object key %q, so the output object cannot separate them",
				first, id, name)
		}
		seen[name] = id
	}
	return nil
}

// buildOutput constructs the JSON-serializable output map from a graph snapshot.
func (a *Adapter) buildOutput(result *graph.Snapshot) (map[string]any, error) {
	output := make(map[string]any)
	s := result.Schema()

	// Iterate types in sorted order for deterministic output
	for _, typeID := range result.Types() {
		typeName := schema.TagForm(s, typeID)
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
func edgeTargetObject(s *schema.Schema, rel *schema.Relation, e *graph.Edge) (map[string]any, error) {
	target, ok := lookupType(s, e.Target().TypeID())
	if !ok {
		return nil, fmt.Errorf("json adapter: cannot render edge %q: target type %s does not resolve, so its _target_ field names are unknowable",
			e.Relation(), e.Target().TypeID())
	}
	pks := target.PrimaryKeysSlice()
	key := e.Target().PrimaryKey()
	if key.Len() != len(pks) {
		return nil, fmt.Errorf("json adapter: edge %q target key has %d components; type %s declares %d",
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

// elementConstraint returns a list constraint's element constraint, or nil for
// any other constraint.
func elementConstraint(c schema.Constraint) schema.Constraint {
	if c == nil {
		return nil
	}
	lc, ok := schema.ResolveAlias(c).(schema.ListConstraint)
	if !ok {
		return nil
	}
	return lc.Element()
}

// canonicalOrRaw renders raw in the form its constraint stores, and returns it
// untouched when the constraint cannot render it. MarshalObject returns an
// error rather than a diag.Result, so failing here would fail a whole export
// over one malformed value.
func canonicalOrRaw(raw any, c schema.Constraint) any {
	canonical, err := instance.CanonicalValue(raw, c)
	if err != nil {
		return raw
	}
	return canonical
}
