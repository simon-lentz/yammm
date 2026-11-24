package cypher

import "strings"

// DropConstraintStatement returns the Cypher that removes the constraint named
// name, which introspection or a diff result reports. The name is always
// backtick-quoted, a backtick in it doubled, so a name a person or another tool
// chose cannot alter the statement's shape, and a reserved word or a name
// [IsIdentifier] refuses is dropped like any other.
//
// The caller owns the choice of verb: a
// [github.com/simon-lentz/yammm/neo4j.Blockers] lists constraints and
// indexes apart, and an index with an OwningConstraint is dropped by dropping
// that constraint. This builds a statement. It does not execute one.
func DropConstraintStatement(name string) string {
	return dropStatement("CONSTRAINT", name)
}

// DropIndexStatement returns the Cypher that removes the index named name,
// quoted as [DropConstraintStatement] quotes. This builds a statement. It does
// not execute one.
func DropIndexStatement(name string) string {
	return dropStatement("INDEX", name)
}

// DropStatement returns the Cypher that removes this constraint.
func (c Constraint) DropStatement() string {
	return DropConstraintStatement(c.Name)
}

// DropStatement returns the Cypher that removes this index.
func (i Index) DropStatement() string {
	return DropIndexStatement(i.Name)
}

func dropStatement(kind, name string) string {
	return "DROP " + kind + " " + quoteIdentifier(name) + " IF EXISTS"
}

// quoteIdentifier backtick-quotes a name for use in Cypher, doubling any
// backtick it contains so the quoting cannot be escaped from.
func quoteIdentifier(name string) string {
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}
