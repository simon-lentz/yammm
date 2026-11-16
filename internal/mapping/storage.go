package mapping

import (
	"errors"
	"fmt"

	"github.com/simon-lentz/yammm/schema"
)

// ComposedKeyProp is the property carrying a part node's identity. Sibling
// uniqueness is not a Neo4j constraint, so a part's declared primary key
// cannot key its node; this composed path string can. No declared property
// can begin with an underscore, so the name cannot collide.
const ComposedKeyProp = "_composed_key"

// ErrUnsupportedListElem is the error [ListElementType] wraps for an element
// kind Neo4j cannot store in a list.
var ErrUnsupportedListElem = errors.New("neo4j: unsupported list element type")

// ListElementType maps a yammm list element constraint to a Neo4j type string.
// Returns the element type name used inside LIST<...> syntax (e.g., "STRING NOT NULL").
func ListElementType(c schema.Constraint) (string, error) {
	c = schema.ResolveAlias(c)
	//exhaustive:enforce
	switch c.Kind() {
	case schema.KindString, schema.KindUUID, schema.KindEnum, schema.KindPattern:
		return "STRING NOT NULL", nil
	case schema.KindInteger:
		return "INTEGER NOT NULL", nil
	case schema.KindFloat:
		return "FLOAT NOT NULL", nil
	case schema.KindBoolean:
		return "BOOLEAN NOT NULL", nil
	case schema.KindTimestamp:
		return "ZONED DATETIME NOT NULL", nil
	case schema.KindDate:
		return "DATE NOT NULL", nil
	case schema.KindVector, schema.KindList, schema.KindAlias:
		return "", fmt.Errorf("%w: list element kind %v", ErrUnsupportedListElem, c.Kind())
	default:
		return "", fmt.Errorf("%w: unknown list element kind %v", ErrUnsupportedListElem, c.Kind())
	}
}

// The remote type strings SHOW CONSTRAINTS and SHOW INDEXES report for the
// kinds the DSL declares, and the one a kind outside its enum renders as.
const (
	Uniqueness            = "UNIQUENESS"
	NodePropertyExistence = "NODE_PROPERTY_EXISTENCE"
	NodePropertyType      = "NODE_PROPERTY_TYPE"
	NodeKey               = "NODE_KEY"

	Range    = "RANGE"
	Vector   = "VECTOR"
	Fulltext = "FULLTEXT"

	UnknownRemoteType = "UNKNOWN"
)

// DeclarableConstraintTypes returns the remote types of the constraint kinds an
// edition can hold, which are the kinds the generator emits and the kinds a
// diff owns. A Community server holds UNIQUE alone. The generator's edition
// filter and the diff's declarability gate both read this, so the desired side
// and the owned actual side cannot disagree.
func DeclarableConstraintTypes(community bool) []string {
	if community {
		return []string{Uniqueness}
	}
	return []string{Uniqueness, NodePropertyExistence, NodePropertyType, NodeKey}
}

// IndexTypes lists the remote types of the index kinds the DSL declares.
func IndexTypes() []string {
	return []string{Range, Vector, Fulltext}
}
