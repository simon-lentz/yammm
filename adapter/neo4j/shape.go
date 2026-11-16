package neo4j

import (
	"context"
	"fmt"
	"strings"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
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

// graphShape is each non-abstract closure type's [nodeShape], keyed by
// [schema.TypeID]: a name is a rendering under one schema, and a snapshot's
// instances carry the identity.
type graphShape struct {
	types map[schema.TypeID]nodeShape
}

// shapeFor derives the [graphShape] of every non-abstract type across s's whole
// import closure, each labeled from its own member's schema name by [Adapter.Label].
// A result with errors holds [E_NEO4J_INVALID_IDENTIFIER] or
// [E_NEO4J_LABEL_COLLISION] issues: two closure types that render one label draw
// the collision when the label is valid, and the identifier issue when it is not.
func (a *Adapter) shapeFor(ctx context.Context, s schema.Schema) (*graphShape, diag.Result) {
	collector := diag.NewCollector(0)
	shape := &graphShape{types: make(map[schema.TypeID]nodeShape)}

	// A load refuses a repeated schema name (E_DUPLICATE_SCHEMA), but a name
	// holding the separator composes one label from two pairs, as schema "a"
	// with B__C and schema "a__B" with C do, so a closure reaches this refusal.
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

// writeShape refuses a zero snapshot with [ErrZeroSnapshot], whose schema no
// shape can be derived from, and otherwise returns its schema's shape under a.
func (a *Adapter) writeShape(ctx context.Context, snap graph.Snapshot) (*graphShape, error) {
	if snap.IsZero() {
		return nil, ErrZeroSnapshot
	}
	shapes, res := a.shapeFor(ctx, snap.Schema())
	if res.HasErrors() {
		return nil, fmt.Errorf("label the schema's types: %w", res.Err())
	}
	return shapes, nil
}

// recordShape computes and stores one type's [nodeShape].
func (a *Adapter) recordShape(shape *graphShape, t schema.Type, label string) {
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
