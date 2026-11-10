// Package adapter holds the platform adapters: packages that turn a yammm
// schema and a [graph.Snapshot] into what one database platform stores. Its one
// subpackage, [neo4j], targets Neo4j 5. The generators that convert a schema to
// an artifact (Go source, Markdown) are packages under schema/: gogen and
// markdown.
//
// # Architectural Boundary
//
// No library package imports an adapter outside its tests. Of the command-line
// programs under cmd/, yammm does. A consumer that imports schema and instance
// alone does not depend on the Neo4j driver: adapter/neo4j pulls the driver's
// dbtype package only when it is imported.
//
// An adapter is a package of this module. It reads a value by the module's own
// rule, internal/value, so its write path and the validator agree on what a
// value is.
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
//	adapter/neo4j  ──imports──▶  schema, graph, immutable, diag, internal/value,
//	                             github.com/google/uuid,
//	                             github.com/neo4j/neo4j-go-driver/v6/neo4j/dbtype
//
// # Subpackages
//
//   - [neo4j]: Neo4j constraint and index DDL, label mapping, write query generation, and drift diffs against a live database
package adapter
