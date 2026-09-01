package neo4j

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/location"
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
	// Unexported, so it can only come from [Adapter.ShapeForSchema]. The write
	// path refuses a shape without it — see [GraphShape.schemaID] — because keys
	// that pass through uncoerced disagree in type with the properties they
	// match against.
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
// Construct via [Adapter.ShapeForSchema]. The write path enforces that rather
// than asking for it: a shape it did not build is refused.
type GraphShape struct {
	Types map[schema.TypeID]NodeShape

	// schemaID is the identity of the schema [Adapter.ShapeForSchema] built this
	// shape from. Its zero value means the shape came from somewhere else.
	//
	// The write path requires it, and requires it to match the snapshot's own
	// schema. A shape the adapter did not build carries no key constraints, so
	// merge keys pass through [coerceValue] uncoerced while the SAME properties
	// are coerced from the schema type — a Date primary key then reaches the
	// driver as a string in the merge key and as a native date in the
	// properties. The MERGE matches its key against the stored property, so it
	// matches nothing and creates a duplicate node on every re-ingestion. A
	// shape built from a DIFFERENT schema reaches the same place through a
	// legitimate constructor, which is why the identity is compared and not
	// merely present.
	//
	// Unexported, so nothing outside this package can set it: the guarantee is
	// structural rather than a request in a doc comment.
	schemaID location.SourceID
}

// requireShapeFor refuses a [GraphShape] this adapter did not build, and one
// built from a schema other than the snapshot's.
func requireShapeFor(shapes *GraphShape, s *schema.Schema) error {
	switch {
	case shapes == nil:
		return errors.New("neo4j adapter: nil GraphShape; build one with Adapter.ShapeForSchema")
	case shapes.schemaID.IsZero():
		return errors.New("neo4j adapter: GraphShape was not built by Adapter.ShapeForSchema, so it carries no key constraints; " +
			"merge keys would pass through uncoerced while their properties are coerced from the schema, and every re-ingestion would duplicate instead of match")
	case s == nil:
		return errors.New("neo4j adapter: snapshot carries no schema to check the GraphShape against")
	case shapes.schemaID != s.SourceID():
		return fmt.Errorf("neo4j adapter: GraphShape was built from schema %s but the snapshot carries schema %s",
			shapes.schemaID, s.SourceID())
	}
	return nil
}

// ShapeForSchema converts a yammm schema into a [GraphShape] describing
// the Neo4j node structure for each non-abstract type across the WHOLE
// import closure — each member's types are labelled under that member's
// own schema name, so an association or composition reaching an imported
// type finds its shape without a hand-built merge.
//
// This is the metadata needed to ensure write-time label and key consistency.
// Consumers use [NodeShape.Label] for MERGE patterns and [NodeShape.PrimaryKeys]
// for MERGE key properties.
//
// If validation errors are found, returns (nil, result) where result contains
// [E_NEO4J_INVALID_IDENTIFIER] or [E_NEO4J_LABEL_COLLISION] issues — two
// closure members sharing a schema name render colliding labels, and a
// TypeID-keyed map would silently hold two shapes under one label.
func (a *Adapter) ShapeForSchema(ctx context.Context, s *schema.Schema) (*GraphShape, diag.Result) {
	collector := diag.NewCollector(0)
	shape := &GraphShape{
		Types:    make(map[schema.TypeID]NodeShape),
		schemaID: s.SourceID(),
	}

	// Defense-in-depth, like [Adapter.DetectLabelCollisions]: the source
	// registry rejects a duplicated schema name (E_DUPLICATE_TYPE), and
	// [SanitizeIdentifier] is the identity on front-door names, so no
	// loader-built closure can reach this refusal. It guards construction
	// paths that bypass the loader.
	seenLabels := make(map[string]schema.TypeID)
	for t, label := range a.emittableTypes(ctx, s, collector) {
		if first, dup := seenLabels[label]; dup {
			collector.Collect(labelCollisionIssue(label, []schema.TypeID{first, t.ID()}))
			continue
		}
		seenLabels[label] = t.ID()
		a.recordShape(shape, t, label)
	}

	result := collector.Result()
	if !result.OK() {
		return nil, result
	}
	return shape, result
}

// recordShape computes and stores one type's NodeShape.
func (a *Adapter) recordShape(shape *GraphShape, t *schema.Type, label string) {
	{
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
}
