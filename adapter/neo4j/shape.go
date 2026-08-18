package neo4j

import (
	"context"
	"strings"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema"
)

// NodeShape describes the Neo4j representation of a single yammm type.
//
// Construct via [Adapter.ShapeForSchema]; do not create directly.
type NodeShape struct {
	Type           string   // Original yammm type name
	Label          string   // Sanitized Neo4j label (namespace + separator + type)
	PrimaryKeys    []string // Property names forming the uniqueness constraint
	RequiredFields []string // All required properties (including primary keys)

	// ImmutableKeys are the type's @writeOnce properties (own and inherited),
	// sorted and deduplicated — see [ImmutableKeysFor].
	//
	// Non-nil when [Adapter.ShapeForSchema] computed the set, including when the
	// set is empty; nil marks a shape that never computed one. The write path
	// honors @writeOnce from this field, so the guarantee holds for a caller
	// passing a nil *schema.Type — the documented streaming call shape — and
	// costs no per-node derivation.
	ImmutableKeys []string

	// keyConstraints maps each name in PrimaryKeys to the schema constraint its
	// value must satisfy, so every write path coerces merge keys to the same
	// driver-native types [propsToParamMap] produces for $props. A merge key
	// bound raw where its property is coerced matches nothing: a Date primary
	// key MERGEs on a string against DATE-valued nodes.
	//
	// Unexported, so it can only come from [Adapter.ShapeForSchema] — a shape
	// built by hand, or restored from a serialization that cannot carry a
	// [schema.Constraint], has none and its keys pass through unchanged, which
	// is the behaviour of a shape that never declared their types.
	keyConstraints map[string]schema.Constraint
}

// GraphShape maps yammm type identities to their Neo4j node representations.
//
// Keyed by [schema.TypeID] rather than by rendered name: a name is a
// rendering, and a transitively imported type renders to a bare name that a
// same-named local type also renders to, so a name-keyed index binds one
// type's instances to another type's label and keys. [NodeShape.Type] carries
// the rendered name for display.
//
// Construct via [Adapter.ShapeForSchema]; do not create directly.
type GraphShape struct {
	Types map[schema.TypeID]NodeShape
}

// ShapeForSchema converts a yammm schema into a [GraphShape] describing
// the Neo4j node structure for each non-abstract type.
//
// This is the metadata needed to ensure write-time label and key consistency.
// Consumers use [NodeShape.Label] for MERGE patterns and [NodeShape.PrimaryKeys]
// for MERGE key properties.
//
// If validation errors are found, returns (nil, result) where result contains
// [E_NEO4J_INVALID_IDENTIFIER] issues.
func (a *Adapter) ShapeForSchema(ctx context.Context, s *schema.Schema) (*GraphShape, diag.Result) {
	collector := diag.NewCollector(0)
	shape := &GraphShape{
		Types: make(map[schema.TypeID]NodeShape),
	}

	for t, label := range a.emittableTypes(ctx, s, collector) {
		// Trimmed to match the label, which is built from the same form. The
		// map is keyed by identity, so this is the display name alone.
		name := strings.TrimSpace(t.Name())

		immutable := ImmutableKeysFor(t)
		if immutable == nil {
			immutable = []string{}
		}

		// Extract primary keys, keeping each one's constraint so the write path
		// can coerce the value the MERGE matches on.
		var primary []string
		keyConstraints := make(map[string]schema.Constraint)
		for _, pk := range t.PrimaryKeysSlice() {
			if pkName := strings.TrimSpace(pk.Name()); pkName != "" {
				primary = append(primary, pkName)
				keyConstraints[pkName] = pk.Constraint()
			}
		}

		// Extract required fields (non-optional properties) with dedup.
		var required []string
		seen := make(map[string]struct{})
		for _, prop := range t.AllPropertiesSlice() {
			propName := strings.TrimSpace(prop.Name())
			if propName == "" {
				continue
			}
			if _, exists := seen[propName]; exists {
				continue
			}
			if prop.IsRequired() {
				seen[propName] = struct{}{}
				required = append(required, propName)
			}
		}

		// Merge primary keys into required (avoiding duplicates).
		for _, key := range primary {
			if _, exists := seen[key]; !exists {
				required = append(required, key)
			}
		}

		shape.Types[t.ID()] = NodeShape{
			Type:           name,
			Label:          label,
			PrimaryKeys:    primary,
			RequiredFields: required,
			// Non-nil even when empty, so the write path can tell a shape that
			// computed no @writeOnce keys from one that never computed any (a
			// hand-built or pre-upgrade shape) and only falls back for the latter.
			ImmutableKeys:  immutable,
			keyConstraints: keyConstraints,
		}
	}

	result := collector.Result()
	if !result.OK() {
		return nil, result
	}
	return shape, result
}
