package neo4j

import (
	"fmt"
	"strings"
)

// DropConstraintStatement returns the Cypher that removes a constraint by name.
//
// The name comes from introspection or from a diff result, so it is whatever a
// person or another tool created, not something this package generated. **Every
// non-empty name is backtick-quoted**, so it cannot alter the statement's
// shape and no name is undroppable — including a Cypher reserved word, where
// refusing would be the opposite of what a DROP builder is for. An empty or
// all-space name is an error wrapping [ErrEmptyIdentifier] — there is no object
// it could name.
//
// Quoting unconditionally rather than only when [ValidateIdentifier] objects
// removes this path's dependence on that validator's reserved-word table: the
// table does not match the Neo4j documentation it cites, and a DROP builder
// that had to know which words are reserved would need it to.
//
// The caller owns the choice of verb. Index and constraint names share one
// namespace, so the object blocking a desired constraint may be an index; see
// [ConstraintDrift.Actual], whose empty Type with a non-empty Name is the
// discriminator that selects [DropIndexStatement] instead.
//
// This builds a statement. It does not execute one.
func DropConstraintStatement(name string) (string, error) {
	return dropStatement("CONSTRAINT", name)
}

// DropIndexStatement returns the Cypher that removes an index by name.
//
// Quoting and error semantics match [DropConstraintStatement], and so does the
// caller's ownership of the verb.
//
// This builds a statement. It does not execute one.
func DropIndexStatement(name string) (string, error) {
	return dropStatement("INDEX", name)
}

// DropStatement returns the Cypher that removes this constraint.
//
// It is an error when Name is empty, which is the state
// [WithNamedConstraints](false) leaves every constraint in: an anonymous
// constraint has no portable name to drop it by.
func (c Constraint) DropStatement() (string, error) {
	return DropConstraintStatement(c.Name)
}

// DropStatement returns the Cypher that removes this index.
//
// [Adapter.IndexesStructured] always emits a name, so this fails only on a
// hand-built Index.
func (i Index) DropStatement() (string, error) {
	return DropIndexStatement(i.Name)
}

// dropStatement is the one DROP renderer, so the two verbs cannot drift on
// quoting or on the empty-name rule.
func dropStatement(kind, name string) (string, error) {
	if strings.TrimSpace(name) == "" {
		return "", fmt.Errorf("%w: drop %s name", ErrEmptyIdentifier, strings.ToLower(kind))
	}
	return "DROP " + kind + " " + quoteIdentifier(name) + " IF EXISTS", nil
}

// quoteIdentifier backtick-quotes a name for use in Cypher, doubling any
// backtick it contains so the quoting cannot be escaped from.
//
// Nothing else in this package quotes: every identifier it generates is
// validated at emission, so CREATE-side names never need it. Remote names are
// the exception — they are arbitrary, and quoting every one of them is why this
// path needs no opinion about which words Cypher reserves.
func quoteIdentifier(name string) string {
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}
