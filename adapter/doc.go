// Package adapter holds the platform adapters: packages that turn a yammm
// schema and a [graph.Snapshot] into what one database platform stores. Its one
// subpackage, [neo4j], targets Neo4j 5. The generators that convert a schema to
// an artifact (Go source, Markdown) are packages under schema/: gogen and
// markdown.
//
// # Architectural Boundary
//
// Adapters are the outermost layer of the library; only the command-line
// programs under cmd/ sit above them. This design provides:
//
//   - Dependency hygiene via import granularity: Go packages are granular at the
//     import level. Consumers who import only schema and instance depend on no
//     adapter's third-party module: adapter/neo4j pulls the Neo4j driver's
//     dbtype package only when it is imported.
//
//   - Clear library/consumer boundary: An adapter subpackage imports the
//     library to use it, as a downstream consumer's own adapter does.
//
//   - Extensibility signal: Users see adapter/neo4j and understand they can
//     create adapter/myplatform using the same pattern.
//
// # Snapshots Out
//
// The Neo4j adapter's batch node and edge queries take a complete
// [graph.Snapshot]; its other functions take no snapshot. No adapter takes an
// [instance.ValidInstance].
//
// # Dependency Direction
//
// Adapters depend on library packages; library packages never depend on adapters:
//
//	adapter/neo4j  ──imports──▶  schema, graph, instance, immutable, diag,
//	                             github.com/neo4j/neo4j-go-driver/v6/neo4j/dbtype
//
// # Layering Discipline
//
// The production code of every adapter imports no internal/* package, keeping a
// clean separation between core library internals and the adapter layer: neo4j
// reaches only for the public API. The rule binds production code alone: an
// adapter test can import a core internal package, as test support or as the
// internals the test checks against.
//
// # Subpackages
//
//   - [neo4j]: Neo4j constraint and index DDL, label mapping, write query generation, and drift diffs against a live database
package adapter
