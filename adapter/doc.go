// Package adapter provides format-specific adapters for parsing, serializing,
// and exporting [instance.RawInstance] and [instance.ValidInstance] values.
// Each adapter subpackage handles a specific data format (JSON, Neo4j, etc.)
// and may have its own external dependencies.
//
// # Architectural Boundary
//
// Adapters are the outermost layer of the module. This design provides:
//
//   - Dependency hygiene via import granularity: Go modules are granular at the
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
// # Dual-Mode Surface
//
// Adapters support two operational modes:
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
// # Dependency Direction
//
// Adapters depend on library packages; library packages never depend on adapters:
//
//	adapter/csv    ──imports──▶  instance, diag, location, graph, immutable, schema
//	adapter/json   ──imports──▶  instance, diag, location, graph, immutable, schema
//	adapter/neo4j  ──imports──▶  schema, graph, instance, immutable, diag
//
// # Layering Discipline
//
// The adapter package does not import internal/* packages. This maintains a
// clean separation between core library internals and the adapter layer.
//
// # Subpackages
//
//   - [csv]: CSV/TSV adapter with schema-driven type coercion (FK columns empty in snapshot mode; see package csv doc)
//   - [json]: JSON adapter with optional location tracking and JSONC support
//   - [neo4j]: Neo4j 5 constraint generation, label mapping, and write query generation
package adapter
