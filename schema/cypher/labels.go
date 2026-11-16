package cypher

import (
	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/internal/mapping"
	"github.com/simon-lentz/yammm/schema"
)

// ValidateIdentifier checks whether a name is a valid Neo4j identifier.
// Returns an error wrapping [ErrEmptyIdentifier] if the name is empty,
// [ErrInvalidIdentifier] if it does not match ^[A-Za-z_][A-Za-z0-9_]*$, and
// [ErrReservedKeyword] if it is a Cypher reserved keyword (case-insensitive).
// The context parameter is included in error messages (e.g., "property 'run_id'").
func ValidateIdentifier(name, context string) error {
	return mapping.ValidateIdentifier(name, context) //nolint:wrapcheck // the write path shares the rule through internal/mapping; its error is this function's
}

// Label returns the Neo4j node label of t: LabelPrefix + the sanitized name of
// the schema that declares t + LabelSeparator ("__" when empty) + t's sanitized
// name, as in "book_catalog__Publisher". The package doc's Labels section states the
// sanitizing rule and the cases that give another form. An abstract type gets
// the string too, though no node carries it: the generators, the write path and
// [Config.OwnedLabels] skip abstract types.
func (c Config) Label(t schema.Type) string {
	return c.labels().Label(t)
}

// CheckLabels reports every label two or more non-abstract types across s's
// IMPORT CLOSURE share, one [E_NEO4J_LABEL_COLLISION] per label. Its type
// detail carries full type IDENTITIES, not display names, because the names
// are what collided:
//
//	types file:///a.yammm:B__C and file:///b.yammm:C all render label "a__B__C"
func (c Config) CheckLabels(s schema.Schema) diag.Result {
	collector := diag.NewCollector(0)
	labelToTypes := make(map[string][]schema.TypeID)

	for t, label := range c.labels().LabeledTypes(s) {
		labelToTypes[label] = append(labelToTypes[label], t.ID())
	}

	for label, ids := range labelToTypes {
		if len(ids) > 1 {
			collector.Collect(mapping.LabelCollisionIssue(label, ids))
		}
	}

	return collector.Result()
}
