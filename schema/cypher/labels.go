package cypher

import (
	"github.com/simon-lentz/yammm/internal/mapping"
	"github.com/simon-lentz/yammm/schema"
)

// IsIdentifier reports whether name is a Neo4j identifier the generated Cypher
// can write unquoted: an ASCII letter or underscore, then ASCII letters, digits
// and underscores, and, in any letter case, none of the Cypher keywords the
// module reserves (the package doc's Cypher Reserved Words section).
func IsIdentifier(name string) bool {
	return mapping.IsIdentifier(name)
}

// Label returns the Neo4j node label of t: LabelPrefix + the name of the
// schema that declares t + LabelSeparator ("__" when empty) + t's name, each
// as written, as in "book_catalog__Publisher". [Config.CheckLabels] judges the
// result; an abstract type gets the string too, though no node carries it.
func (c Config) Label(t schema.Type) string {
	return c.labels().Label(t)
}

// CheckLabels judges the label of every non-abstract type across s's IMPORT
// CLOSURE under c, one [E_NEO4J_INVALID_IDENTIFIER] per break of the label
// rule; a break of c's settings is reported alone, and a schema name's break
// leaves its types' labels unjudged (the package doc's Labels section). A label
// that keeps the rule decodes to one (schema, type) pair, so no two types share
// one. The breaks are returned as one *[diag.ResultError], nil when there is
// none; every generator and the neo4j write path call it and refuse the schema
// on its error.
func (c Config) CheckLabels(s schema.Schema) error {
	return c.labels().Check(s)
}
