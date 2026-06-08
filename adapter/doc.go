// Package adapter provides format-specific adapters for parsing, serializing,
// and exporting [instance.RawInstance] and [instance.ValidInstance] values, plus a
// schema-to-Go code generator. Each adapter subpackage targets a specific output —
// a data format (JSON, CSV, Neo4j Cypher) or generated Go source — and may have its
// own external dependencies.
//
// # Architectural Boundary
//
// Adapters are the outermost layer of the module. This design provides:
//
//   - Dependency hygiene via import granularity: Go packages are granular at the
//     import level. Consumers who import only schema and instance do not
//     transitively depend on tidwall/jsonc. Adapter dependencies are pulled only
//     when adapter/json is imported.
//
//   - Clear library/consumer boundary: The adapter package explicitly imports
//     the library to use it, mirroring how downstream consumers structure their
//     own adapters.
//
//   - Extensibility signal: Users see adapter/json and understand they can
//     create adapter/myformat using the same pattern.
//
// # Operational Modes
//
// The data adapters support two operational modes:
//
//   - Graph mode: operates on [graph.Snapshot] for batch operations after
//     building a complete in-memory graph. Used by batch query generators
//     and full-snapshot serializers.
//
//   - Instance mode: operates on individual [instance.ValidInstance] values
//     for streaming pipelines where instances are processed one at a time
//     without building a complete graph. Used by per-instance query generators
//     and single-instance serializers.
//
// Parse methods return [instance.RawInstance]. Write methods accept either
// [instance.ValidInstance] (streaming) or [graph.Snapshot] (batch).
//
// The gogen adapter belongs to neither mode: it is schema-in, bytes-out. It
// consumes a completed schema and emits Go source, with no instance-data path and
// no dependency on instance or graph.
//
// # Dependency Direction
//
// Adapters depend on library packages; library packages never depend on adapters:
//
//	adapter/csv    ──imports──▶  instance, diag, location, graph, immutable, schema
//	adapter/gogen  ──imports──▶  schema, location, internal/ident
//	adapter/json   ──imports──▶  instance, diag, location, location/path, graph, immutable, schema
//	adapter/neo4j  ──imports──▶  schema, graph, instance, immutable, diag
//
// # Layering Discipline
//
// The data adapters (csv, json, neo4j) import no internal/* packages, keeping a
// clean separation between core library internals and the adapter layer. The gogen
// adapter is the sole exception: it imports internal/ident to reuse the canonical
// identifier-casing transform the library applies elsewhere (e.g. JSON field
// names), so generated Go identifiers stay consistent with the rest of yammm
// instead of duplicating that logic.
//
// # Subpackages
//
//   - [csv]: CSV/TSV adapter with schema-driven type coercion
//   - [gogen]: Go source generator (structs, enums, EDGE_ associations, a Graph aggregate, and an embedded SerializedModel) from a schema and its import closure
//   - [json]: JSON adapter with optional location tracking and JSONC support
//   - [neo4j]: Neo4j 5 constraint generation, label mapping, and write query generation
package adapter
