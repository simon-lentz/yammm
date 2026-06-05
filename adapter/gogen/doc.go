// Package gogen generates Go source from a yammm schema: one struct per type,
// named Enum/DataType types, EDGE_ association structs, a Graph aggregate, and an
// embedded SerializedModel. Output is stdlib-only (it imports at most "time").
//
// gogen supports every yammm-valid schema, including schemas with imports: the
// full import closure is flattened into one self-contained package, with
// schema-qualified names where cross-schema identifiers would otherwise collide.
//
// Unlike the data adapters (adapter/json, adapter/csv, adapter/neo4j), gogen has
// no instance-data path: it is schema-in, bytes-out. Call [Marshal] with a loaded,
// resolved schema.
package gogen
