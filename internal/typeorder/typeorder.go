// Package typeorder orders type identities by schema name, then type name.
//
// A load refuses two schemas of one name, and a schema declares one type per
// name, so the pair is unique over an import closure and the same on every
// host. [schema.TypeID.String] holds the schema's source path instead, so an
// order by it moves when one schema text is loaded from another directory.
// The graph's snapshot orders read it, and so do the Neo4j writer's edge
// statement order and the part labels of a composition replace statement.
package typeorder

import (
	"cmp"

	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// Order holds the schema name of each member of an import closure, by the
// member's source.
type Order map[location.SourceID]string

// New indexes the schema name of each member of s's import closure.
func New(s schema.Schema) Order {
	o := make(Order)
	for member := range s.Closure() {
		o[member.SourceID()] = member.Name()
	}
	return o
}

// Compare orders a and b by schema name, then type name. The order is total
// over the identities of the closure o indexes.
func (o Order) Compare(a, b schema.TypeID) int {
	return cmp.Or(
		cmp.Compare(o[a.SchemaPath()], o[b.SchemaPath()]),
		cmp.Compare(a.Name(), b.Name()),
	)
}
