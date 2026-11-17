package neo4j

import (
	"fmt"
	"strings"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/internal/mapping"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/schema/cypher"
)

// nodeShape describes the Neo4j representation of a single yammm type.
type nodeShape struct {
	label       string   // Neo4j label (prefix + schema name + separator + type name)
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
// import closure, each labeled from its own member's schema name by
// [cypher.Config.Label]. A result with errors is [cypher.Config.CheckLabels]'
// refusal, and then no shape is derived.
func shapeFor(c cypher.Config, s schema.Schema) (*graphShape, diag.Result) {
	if res := c.CheckLabels(s); res.HasErrors() {
		return nil, res
	}
	shape := &graphShape{types: make(map[schema.TypeID]nodeShape)}
	labels := mapping.Labels{Prefix: c.LabelPrefix, Separator: c.LabelSeparator}
	for t, label := range labels.LabeledTypes(s) {
		recordShape(shape, t, label)
	}
	return shape, diag.OK()
}

// writeShape refuses a zero snapshot with [ErrZeroSnapshot], whose schema no
// shape can be derived from, and otherwise returns its schema's shape under c.
func writeShape(c cypher.Config, snap graph.Snapshot) (*graphShape, error) {
	if snap.IsZero() {
		return nil, ErrZeroSnapshot
	}
	shapes, res := shapeFor(c, snap.Schema())
	if res.HasErrors() {
		return nil, fmt.Errorf("label the schema's types: %w", res.Err())
	}
	return shapes, nil
}

// recordShape computes and stores one type's [nodeShape].
func recordShape(shape *graphShape, t schema.Type, label string) {
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
		immutableKeys:  immutableKeysFor(t),
		keyConstraints: keyConstraints,
	}
}
