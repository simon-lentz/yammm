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
	indent             string
	includeDiagnostics bool
}

// WithIndent sets the indentation string for pretty-printing.
// Use "" for compact output (default), "\t" for tab indentation,
// or "  " (two spaces) for space indentation.
func WithIndent(indent string) WriteOption {
	return func(c *writeConfig) {
		c.indent = indent
	}
}

// WithDiagnostics includes unresolved edges and duplicates in the output
// under a "$diagnostics" key. This is disabled by default and intended
// for debugging or diagnostic purposes.
func WithDiagnostics(include bool) WriteOption {
	return func(c *writeConfig) {
		c.includeDiagnostics = include
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
// references for resolved associations. Use WithDiagnostics(true) to include
// unresolved edges and duplicates in a "$diagnostics" section.
//
// Returns ErrNilResult if result is nil.
//
//nolint:revive // ctx reserved for future use (cancellation, tracing)
func (a *Adapter) MarshalObject(ctx context.Context, result *graph.Snapshot, opts ...WriteOption) ([]byte, error) {
	if result == nil {
		return nil, ErrNilResult
	}

	cfg := &writeConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	output := a.buildOutput(result, cfg)

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

// MarshalInstance serializes a single ValidInstance to JSON bytes.
//
// Unlike [Adapter.MarshalObject] (which operates on a complete [*graph.Snapshot]),
// this method works directly with a [*instance.ValidInstance]. It serializes:
//   - Properties from [instance.ValidInstance.Properties]
//   - FK references from [instance.ValidInstance.Edges] (as inline key arrays)
//   - Composed children from [instance.ValidInstance.Compositions] (recursively)
//
// The schemaType determines field names and cardinality for edges and compositions.
// If schemaType is nil, relation names are used as field names and all edge
// collections are serialized as arrays.
//
// Returns [ErrNilInstance] if inst is nil.
//
//nolint:revive // ctx reserved for future use (cancellation, tracing)
func (a *Adapter) MarshalInstance(
	ctx context.Context,
	inst *instance.ValidInstance,
	schemaType *schema.Type,
	opts ...WriteOption,
) ([]byte, error) {
	if inst == nil {
		return nil, ErrNilInstance
	}

	cfg := &writeConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	obj := serializeValidInstance(inst, schemaType)

	var data []byte
	var err error
	if cfg.indent != "" {
		data, err = json.MarshalIndent(obj, "", cfg.indent)
	} else {
		data, err = json.Marshal(obj)
	}
	if err != nil {
		return nil, fmt.Errorf("json marshal: %w", err)
	}
	return data, nil
}

// serializeValidInstance converts a ValidInstance to a JSON-serializable map.
// This parallels [serializeInstance] but operates on ValidInstance directly,
// reading edges and compositions from the instance's own data rather than
// from a resolved graph snapshot.
func serializeValidInstance(inst *instance.ValidInstance, schemaType *schema.Type) map[string]any {
	obj := make(map[string]any)

	// 1. Properties in sorted order for deterministic output.
	for name, val := range inst.Properties().SortedRange() {
		obj[name] = unwrapValue(val)
	}

	// 2. FK references from edges.
	for relationName, edgeData := range inst.Edges() {
		fieldName := relationName
		isMany := true // default to array when schema unavailable
		if schemaType != nil {
			rel, ok := schemaType.Relation(relationName)
			if !ok || rel.Kind() != schema.RelationAssociation {
				continue
			}
			fieldName = rel.FieldName()
			isMany = rel.IsMany()
		}

		targets := edgeData.Targets()
		if len(targets) == 0 {
			continue
		}

		if isMany {
			fks := make([]any, len(targets))
			for i := range targets {
				fks[i] = targets[i].TargetKey().Clone()
			}
			obj[fieldName] = fks
		} else {
			// Single cardinality: inline the key directly.
			obj[fieldName] = targets[0].TargetKey().Clone()
		}
	}

	// 3. Composed children.
	for relationName, composedValue := range inst.Compositions() {
		fieldName := relationName
		isMany := true // default to array when schema unavailable
		if schemaType != nil {
			rel, ok := schemaType.Relation(relationName)
			if !ok {
				continue
			}
			fieldName = rel.FieldName()
			isMany = rel.IsMany()
		}

		// Compositions wrap *instance.ValidInstance children inside immutable.Slice.
		// Child schema type resolution is not available from the parent's
		// *schema.Type alone, so children serialize with nil schemaType
		// (properties still serialize; nested relation field names use raw names).
		unwrapped := composedValue.Unwrap()
		if slice, ok := unwrapped.(immutable.Slice); ok {
			if isMany {
				children := make([]map[string]any, 0, slice.Len())
				for i := range slice.Len() {
					childValid, ok := slice.Get(i).Unwrap().(*instance.ValidInstance)
					if !ok {
						continue
					}
					children = append(children, serializeValidInstance(childValid, nil))
				}
				obj[fieldName] = children
			} else if slice.Len() > 0 {
				if childValid, ok := slice.Get(0).Unwrap().(*instance.ValidInstance); ok {
					obj[fieldName] = serializeValidInstance(childValid, nil)
				}
			}
			continue
		}

		// Defensive: bare *ValidInstance (not wrapped in slice).
		if childValid, ok := unwrapped.(*instance.ValidInstance); ok {
			obj[fieldName] = serializeValidInstance(childValid, nil)
		}
	}

	return obj
}

// buildOutput constructs the JSON-serializable output map from a graph snapshot.
func (a *Adapter) buildOutput(result *graph.Snapshot, cfg *writeConfig) map[string]any {
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

	// Optionally include diagnostics
	if cfg.includeDiagnostics {
		diag := serializeDiagnostics(result, s)
		if len(diag) > 0 {
			output["$diagnostics"] = diag
		}
	}

	return output
}

// lookupType resolves a TypeID to its schema.Type by checking local types and imports.
func lookupType(s *schema.Schema, id schema.TypeID) (*schema.Type, bool) {
	if s == nil {
		return nil, false
	}

	// Check local types
	if id.SchemaPath() == s.SourceID() {
		return s.Type(id.Name())
	}

	// Check imported schemas
	for imp := range s.Imports() {
		if imp.Schema() != nil && imp.Schema().SourceID() == id.SchemaPath() {
			return imp.Schema().Type(id.Name())
		}
	}

	return nil, false
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

// serializeDiagnostics creates the $diagnostics section with unresolved edges and duplicates.
func serializeDiagnostics(result *graph.Snapshot, s *schema.Schema) map[string]any {
	diag := make(map[string]any)

	// Serialize unresolved edges
	unresolved := result.Unresolved()
	if len(unresolved) > 0 {
		unresolvedArr := make([]map[string]any, len(unresolved))
		for i, u := range unresolved {
			// Get field name from schema if available
			fieldName := u.Relation // fallback to relation name
			if schemaType, ok := lookupType(s, u.Source.TypeID()); ok {
				if rel, ok := schemaType.Relation(u.Relation); ok {
					fieldName = rel.FieldName()
				}
			}
			unresolvedArr[i] = map[string]any{
				"source_type": u.Source.TypeName(),
				"source_key":  u.Source.PrimaryKey().Clone(),
				"relation":    fieldName,
				"target_type": schema.TagForm(result.Schema(), u.TargetType),
				"target_key":  parseKeyString(u.TargetKey),
			}
		}
		diag["unresolved"] = unresolvedArr
	}

	// Serialize duplicates
	duplicates := result.Duplicates()
	if len(duplicates) > 0 {
		duplicatesArr := make([]map[string]any, len(duplicates))
		for i, d := range duplicates {
			duplicatesArr[i] = map[string]any{
				"type":         d.Instance.TypeName(),
				"key":          d.Instance.PrimaryKey().Clone(),
				"conflict_key": d.Conflict.PrimaryKey().Clone(),
			}
		}
		diag["duplicates"] = duplicatesArr
	}

	return diag
}

// parseKeyString parses a canonical key string back to []any.
// Returns the original string wrapped in a slice if parsing fails.
// Uses UseNumber to preserve integer types in diagnostic keys.
func parseKeyString(s string) []any {
	var result []any
	dec := json.NewDecoder(bytes.NewReader([]byte(s)))
	dec.UseNumber()
	if err := dec.Decode(&result); err != nil {
		// Fallback: return string as single-element slice
		return []any{s}
	}
	// Normalize json.Number to int64/float64
	for i, v := range result {
		if num, ok := v.(json.Number); ok {
			result[i] = immutable.NormalizeNumber(num)
		}
	}
	return result
}
