package neo4j

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema"
)

// nodeShape describes the Neo4j representation of a single yammm type.
type nodeShape struct {
	label       string   // Sanitized Neo4j label (namespace + separator + type)
	primaryKeys []string // Property names forming the uniqueness constraint

	// immutableKeys are the type's @writeOnce properties (own and inherited),
	// sorted and deduplicated, recorded so the write path derives them once per type.
	immutableKeys []string

	// keyConstraints maps each name in primaryKeys to the schema constraint its
	// value must satisfy, so every write path coerces merge keys to the same
	// driver-native types [propsToParamMap] produces for $props. A merge key
	// bound raw where its property is coerced matches nothing: a Date primary
	// key MERGEs on a string against DATE-valued nodes.
	keyConstraints map[string]schema.Constraint
}

// GraphShape is the input [Adapter.BatchNodeQueries] and
// [Adapter.BatchEdgeQueries] take for a schema: each non-abstract type's label,
// primary keys and their constraints, and @writeOnce keys, with the schema object
// they came from. [Adapter.ShapeForSchema] builds it. No field is exported,
// so a caller passes it on and cannot change what it holds.
type GraphShape struct {
	// types is keyed by [schema.TypeID] rather than by rendered name: a name is a
	// rendering under one schema, and a snapshot's instances carry the identity.
	types map[schema.TypeID]nodeShape

	// schema is the object [Adapter.ShapeForSchema] built this shape from. Its
	// zero value means the shape came from somewhere else.
	//
	// The write path requires it to be the snapshot's own schema object, because
	// another load, even of one source name, can declare a key another way. A
	// merge key would then reach the driver as a string while the SAME property
	// is coerced from the snapshot's schema to a native date. The MERGE matches its
	// key against the stored property, so it matches nothing: each re-ingestion
	// creates a duplicate node, or fails where a uniqueness constraint holds the
	// key. So the object is compared, and not merely present.
	schema schema.Schema
}

// requireShapeFor refuses a nil [GraphShape], one [Adapter.ShapeForSchema] did not
// build, and one built from any object but the snapshot's schema. Another load of
// one source is another object.
func requireShapeFor(shapes *GraphShape, s schema.Schema) error {
	switch {
	case shapes == nil:
		return errors.New("neo4j adapter: nil GraphShape; build one with Adapter.ShapeForSchema")
	case shapes.schema.IsZero():
		return errors.New("neo4j adapter: GraphShape was not built by Adapter.ShapeForSchema; build one with Adapter.ShapeForSchema")
	case s.IsZero():
		return errors.New("neo4j adapter: snapshot carries no schema to check the GraphShape against")
	case shapes.schema != s:
		return fmt.Errorf("neo4j adapter: GraphShape was built from schema %s but the snapshot carries schema %s, and another load of one source is another schema",
			shapes.schema.SourceID(), s.SourceID())
	}
	return nil
}

// ShapeForSchema converts a yammm schema into a [GraphShape] describing
// the Neo4j node structure for each non-abstract type across the WHOLE
// import closure — [Adapter.Label] labels each member's types from that
// member's own schema name, so an association or composition reaching an imported
// type finds its shape without a hand-built merge.
//
// If validation errors are found, returns (nil, result) where result contains
// [E_NEO4J_INVALID_IDENTIFIER] or [E_NEO4J_LABEL_COLLISION] issues. Two types of the
// closure that render one label draw the collision when the label is valid, and the
// identifier issue when it is not.
func (a *Adapter) ShapeForSchema(ctx context.Context, s schema.Schema) (*GraphShape, diag.Result) {
	collector := diag.NewCollector(0)
	shape := &GraphShape{
		types:  make(map[schema.TypeID]nodeShape),
		schema: s,
	}

	// A load refuses a repeated schema name (E_DUPLICATE_SCHEMA), but
	// [SanitizeIdentifier] maps "geo-regions" and "geo_regions" to one label
	// prefix, so two names in one closure reach this refusal.
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

// recordShape computes and stores one type's [nodeShape].
func (a *Adapter) recordShape(shape *GraphShape, t schema.Type, label string) {
	// Extract primary keys, keeping each one's constraint so the write path
	// can coerce the value the MERGE matches on.
	var primary []string
	keyConstraints := make(map[string]schema.Constraint)
	for pk := range t.PrimaryKeys() {
		if pkName := strings.TrimSpace(pk.Name()); pkName != "" {
			primary = append(primary, pkName)
			keyConstraints[pkName] = pk.Constraint()
		}
	}

	shape.types[t.ID()] = nodeShape{
		label:          label,
		primaryKeys:    primary,
		immutableKeys:  ImmutableKeysFor(t),
		keyConstraints: keyConstraints,
	}
}
