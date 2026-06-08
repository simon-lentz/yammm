// Package gogen generates Go source from a yammm schema: one struct per type,
// named Enum/DataType types, EDGE_ association structs, a Graph aggregate, and an
// embedded SerializedModel. Output is stdlib-only (it imports at most "time").
//
// gogen handles the full range of yammm schemas, including schemas with imports:
// the full import closure is flattened into one self-contained package, with
// schema-qualified names where cross-schema identifiers would otherwise collide.
// A loaded, resolved schema already guarantees what faithful emission needs —
// every association targets a concrete type (E_INVALID_ASSOCIATION_TARGET) and
// every concrete type carries a primary key (E_NO_PRIMARY_KEY) — so an EDGE_
// struct's Where block can always identify its target. For the narrow cases gogen
// still cannot render in Go — a schema with no retained source, or a name clash
// schema-qualification cannot resolve — it returns an error rather than emitting
// broken or misleading Go.
//
// Unlike the data adapters (adapter/json, adapter/csv, adapter/neo4j), gogen has
// no instance-data path: it is schema-in, bytes-out. Call [Marshal] with a loaded,
// resolved schema.
package gogen
