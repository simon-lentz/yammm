package json

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
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

	output := a.buildOutput(result)

	var data []byte
	var err error
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
func (a *Adapter) buildOutput(result *graph.Snapshot) map[string]any {
	output := make(map[string]any)
	s := result.Schema()

	// Iterate types in sorted order for deterministic output
	for _, typeID := range result.Types() {
		typeName := schema.TagForm(s, typeID)
		instances := result.InstancesOf(typeID)
		serialized := make([]map[string]any, 0, len(instances))

		for _, inst := range instances {
			obj := serializeInstance(inst, result, s)
			serialized = append(serialized, obj)
		}

		output[typeName] = serialized
	}

	return output
}

// lookupType resolves a TypeID to its schema.Type by checking local types and imports.
func lookupType(s *schema.Schema, id schema.TypeID) (*schema.Type, bool) {
	if s == nil {
		return nil, false
	}
	// TypeByID indexes the whole import closure. Walking direct imports found
	// no transitively imported type, which the v3 wire preserves through Load.
	return s.TypeByID(id)
}

// serializeInstance converts a graph.Instance to a JSON-serializable map.
// Uses schema to determine cardinality (scalar vs array) and field names.
// Edge lookup uses snap.EdgesFrom for O(1) per-instance access.
func serializeInstance(inst *graph.Instance, snap *graph.Snapshot, s *schema.Schema) map[string]any {
	obj := make(map[string]any)

	// Lookup the type for schema-based serialization
	schemaType, hasType := lookupType(s, inst.TypeID())

	// 1. Add properties in sorted order for deterministic output
	for name, val := range inst.Properties().SortedRange() {
		obj[name] = unwrapValue(val)
	}

	// 2. Add FK references for associations using the snapshot edge index.
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

			// Determine field name and cardinality from schema
			fieldName := relName // fallback
			isMany := len(edges) > 1
			if hasType {
				if rel, ok := schemaType.Relation(relName); ok {
					fieldName = rel.FieldName()
					isMany = rel.IsMany()
				}
			}

			if isMany {
				// Many cardinality: array of FK arrays
				fks := make([]any, len(edges))
				for i, e := range edges {
					fks[i] = e.Target().PrimaryKey().Clone()
				}
				obj[fieldName] = fks
			} else if len(edges) > 0 {
				// One cardinality: FK as array of key components
				obj[fieldName] = edges[0].Target().PrimaryKey().Clone()
			}
		}
	}

	// 3. Add composed children in sorted order.
	// ComposedRelations returns sorted relation names; Composed returns a defensive copy.
	for _, relName := range inst.ComposedRelations() {
		children := inst.Composed(relName)

		// Determine field name and cardinality from schema
		fieldName := relName // fallback
		isMany := len(children) > 1
		if hasType {
			if rel, ok := schemaType.Relation(relName); ok {
				fieldName = rel.FieldName()
				isMany = rel.IsMany()
			}
		}

		if isMany {
			// Many cardinality: array of objects
			arr := make([]map[string]any, len(children))
			for i, child := range children {
				arr[i] = serializeInstance(child, snap, s)
			}
			obj[fieldName] = arr
		} else if len(children) > 0 {
			// One cardinality: inline object
			obj[fieldName] = serializeInstance(children[0], snap, s)
		}
	}

	return obj
}

// unwrapValue recursively converts an immutable.Value to a JSON-compatible any.
func unwrapValue(v immutable.Value) any {
	if v.IsNil() {
		return nil
	}

	// Check for wrapped collections
	if m, ok := v.Map(); ok {
		result := make(map[string]any, m.Len())
		for k, val := range m.Range() {
			result[k] = unwrapValue(val)
		}
		return result
	}
	if s, ok := v.Slice(); ok {
		result := make([]any, s.Len())
		for i, val := range s.Iter2() {
			result[i] = unwrapValue(val)
		}
		return result
	}

	// Primitives: return directly
	return v.Unwrap()
}
