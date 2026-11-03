// Package adapter provides format-specific adapters that parse data into
// [instance.RawInstance] values and write a [graph.Snapshot]. Each adapter
// subpackage targets one data format (JSON, Neo4j Cypher) and may have its own
// external dependencies. The generators that convert a schema to an artifact
// (Go source, JSON Schema, Markdown) are packages under schema/: gogen,
// jschema and markdown.
//
// # Architectural Boundary
//
// Adapters are the outermost layer of the library; only the command-line
// programs under cmd/ sit above them. This design provides:
//
//   - Dependency hygiene via import granularity: Go packages are granular at the
//     import level. Consumers who import only schema and instance depend on no
//     adapter's third-party module: adapter/json pulls tidwall/jsonc and
//     adapter/neo4j the Neo4j driver's dbtype package, each only when that
//     adapter is imported.
//
//   - Clear library/consumer boundary: Each adapter subpackage imports the
//     library to use it, as a downstream consumer's own adapter does.
//
//   - Extensibility signal: Users see adapter/json and understand they can
//     create adapter/myformat using the same pattern.
//
// # Data In, Snapshots Out
//
// The data adapters read and write one way. The JSON parser returns
// [instance.RawInstance] values for the validator. The JSON serializer and the
// Neo4j adapter's batch node and edge queries take a complete [graph.Snapshot];
// the Neo4j adapter's other functions take no snapshot. No adapter takes an
// [instance.ValidInstance].
//
// # Dependency Direction
//
// Adapters depend on library packages; library packages never depend on adapters:
//
//	adapter/json      ──imports──▶  instance, diag, location, location/path, graph,
//	                                immutable, schema, adapter/internal/constraintof,
//	                                adapter/internal/refusal, github.com/tidwall/jsonc
//	adapter/neo4j     ──imports──▶  schema, graph, instance, immutable, diag,
//	                                adapter/internal/constraintof, adapter/internal/refusal,
//	                                github.com/neo4j/neo4j-go-driver/v6/neo4j/dbtype
//
// # Layering Discipline
//
// The production code of every adapter imports no core internal/* package,
// keeping a clean separation between core library internals and the adapter
// layer: json and neo4j reach only for the public API and adapter/internal.
// The rule binds production code alone: an adapter test can import a core
// internal package, as test support or as the internals the test checks
// against.
//
// An internal package under adapter/ is not an exception to this rule: it is the
// adapters', not the core's. adapter/internal holds the two the adapters share:
// refusal for a refusal's class and constraintof for the constraint a value renders
// through, which the JSON and Neo4j adapters read.
//
// # Subpackages
//
//   - [json]: JSON adapter that reads JSONC and records a location for every instance it parses
//   - [neo4j]: Neo4j constraint and index DDL, label mapping, write query generation, and drift diffs against a live database
package adapter
