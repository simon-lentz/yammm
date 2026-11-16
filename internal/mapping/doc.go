// Package mapping holds the parts of the Neo4j storage mapping that
// schema/cypher and neo4j both read: the identifier rules, the label
// composition and the walk that labels a schema's types, the two diagnostic
// codes those raise, the identity keys a declaration and a database object are
// paired by, and the names the DDL and the write path must agree on.
//
// # One Walk
//
// INVARIANT: the set [Labels.LabeledTypes] yields is both the EMIT set and the
// OWN set. The constraint and index generators, the label collision check, the
// ownership set and the write path's shape all reach their types through it, so
// what is created, what is claimed and what is written cannot diverge. That is
// what keeps the diff safe: a label is owned exactly when it is desired, so
// widening the walk can never make a plan DROP a label the emitter still
// writes. Narrowing one caller without the others would break that.
//
// The closure is the scope because the write path gives every closure member's
// types a label and writes nodes under it. A walk over the entry schema's own
// types alone left an imported type with a label and node writes, and with no
// constraint, no index and no ownership.
//
// [Labels.EmittableTypes] skips a label that is not a valid identifier, and
// the collision check walks [Labels.LabeledTypes] instead because it must not:
// two types colliding on a label that is also an invalid identifier are still a
// collision. A caller that runs both walks the type list twice, which costs less
// than coupling a diagnostic to an emitter. The type name is trimmed before the
// label is built; a load rejects a name with surrounding white space, so the
// trim changes no name, and the write path keys its shape by [schema.TypeID].
//
// # Dependencies
//
//	internal/mapping  ──imports──▶  diag, schema
package mapping
