package cypher

import "github.com/simon-lentz/yammm/internal/mapping"

// Config is the storage mapping from a schema to a Neo4j graph: how a type's
// label is composed and which constraints its properties get. Its zero value is
// the default mapping. The DDL, the ownership set, the diff and the write path
// each take a Config, so a caller that passes one value to each diffs a graph
// under the mapping it was written with.
type Config struct {
	// LabelPrefix is prepended to every label. Default: "" (no prefix).
	LabelPrefix string

	// LabelSeparator joins the schema name and the type name in a label. ""
	// means "__", which gives labels like "book_catalog__Publisher".
	LabelSeparator string

	// Edition is the target Neo4j edition. Default: Enterprise.
	Edition Edition

	// NodeKeys emits one NODE KEY in place of UNIQUE and NOT NULL for the
	// primary key of a type that is not a part, on Enterprise. Default: false.
	NodeKeys bool

	// TypeConstraints decides which properties get a property type constraint.
	// Default: AllProperties.
	TypeConstraints TypeConstraints
}

// Edition represents a Neo4j deployment edition.
type Edition uint8

const (
	// Enterprise supports all constraint types.
	Enterprise Edition = iota
	// Community supports UNIQUE constraints only.
	// EXISTS (NOT NULL), KEY, and PROPERTY_TYPE constraints are Enterprise-only.
	Community
)

// TypeConstraints decides which properties get a property type constraint,
// `REQUIRE n.prop IS :: T`, and with it whether an optional property's name is
// checked as an identifier. The package doc's Property Type Constraints
// section states what each value emits.
type TypeConstraints uint8

const (
	// AllProperties gives every scalar, List and Vector property a type
	// constraint.
	AllProperties TypeConstraints = iota
	// RequiredProperties gives a type constraint to required properties alone.
	RequiredProperties
	// NoProperties emits no type constraint.
	NoProperties
)

// labels returns the label composition c configures.
func (c Config) labels() mapping.Labels {
	return mapping.Labels{Prefix: c.LabelPrefix, Separator: c.LabelSeparator}
}

// community reports whether c targets a Community server.
func (c Config) community() bool {
	return c.Edition == Community
}

// typeConstraintsFor reports whether a property whose requiredness is required
// gets a type constraint under c.
func (c Config) typeConstraintsFor(required bool) bool {
	switch c.TypeConstraints {
	case AllProperties:
		return true
	case RequiredProperties:
		return required
	default:
		return false
	}
}
