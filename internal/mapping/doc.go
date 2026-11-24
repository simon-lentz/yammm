// Package mapping holds the parts of the Neo4j storage mapping that
// schema/cypher and neo4j both read: the identifier rule, the label
// composition and the walk that labels a schema's types, the diagnostic code
// those raise, the identity keys a declaration and a database object are
// paired by, and the names the DDL and the write path must agree on.
//
// # One Walk
//
// INVARIANT: the set [Labels.LabeledTypes] yields is both the EMIT set and the
// OWN set. The constraint and index generators, the label check, the
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
// Every generator and write path judges the closure with [Labels.Check]
// before it walks, and refuses the whole schema on an error, so no walk skips
// a type; schema/cypher's OwnedLabels lists labels without judging them. The
// type name is used as written; a load rejects a name that is not an
// identifier, and the write path keys its shape by [schema.TypeID].
//
// # Label Rule
//
// A label is Prefix + S + separator + T, where S is the declaring schema's name
// and T the type's. Nothing is rewritten: a label that breaks the rule is
// refused, never repaired. The rule has four parts:
//
//   - The prefix is empty or starts a Neo4j identifier, and the separator is
//     ASCII letters, digits and underscores; an empty separator is "__".
//   - S and T are each an ASCII letter or underscore, then ASCII letters,
//     digits and underscores. A load holds every schema and type name to the
//     DSL's stricter rule, and a schema comes only from a load, so
//     [Labels.Check] does not judge this part again.
//   - strings.Index(S+separator, separator) == len(S): the separator neither
//     occurs in S nor runs into it from its end, so a label decodes to one
//     (S, T) pair. Schema "a" with type B__C and schema "a__B" with type C
//     would both write a__B__C; the rule refuses the second, and "geo_" under
//     "__" too, since geo_ + __ first holds __ inside the name.
//   - The composed label is no Cypher reserved keyword ([IsIdentifier]).
//
// A load holds one schema per name (E_DUPLICATE_SCHEMA) and one type per name
// within a schema, so (S, T) is unique across a closure; with the decoding
// part, two types never share a label, and no collision check is needed.
//
// # Dependencies
//
//	internal/mapping  ──imports──▶  diag, schema
package mapping
