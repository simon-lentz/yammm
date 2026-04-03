// Package neo4j generates Neo4j 5 constraint statements, label mappings,
// graph shape metadata, and parameterized write queries from yammm schemas.
//
// # Architectural Position
//
// The neo4j adapter lives at the outermost tier of the yammm module,
// alongside [github.com/simon-lentz/yammm/adapter/json]. It depends on
// library packages (schema, graph, immutable, diag, location); library
// packages never depend on adapters.
//
// # Neo4j Driver Dependency (Type-Only)
//
// The adapter imports [github.com/neo4j/neo4j-go-driver/v6/neo4j/dbtype]
// for temporal type definitions (dbtype.Date). It does not import
// connection, session, or transaction packages — no code in this package
// opens a connection or executes queries against a database. Consumers
// supply their own driver and choose how to apply the generated output
// (directly, via neo4j-migrations, via rdata's Apply(), etc.).
//
// This mirrors the JSON adapter principle: adapter/json does not import
// HTTP libraries — it produces bytes. adapter/neo4j does not import the
// Neo4j session API — it produces Cypher and driver-compatible parameters.
//
// # Thread Safety
//
// An [Adapter] is safe for concurrent use after construction.
// Configuration is immutable after [New] returns. All methods use only
// local allocations and read-only access to the frozen config.
//
// # External Dependencies
//
// The package depends on the Go standard library, yammm library packages,
// and [github.com/neo4j/neo4j-go-driver/v6/neo4j/dbtype] (type definitions
// only, zero transitive dependencies). No test frameworks beyond [testing].
//
// # Edition Gating
//
// [WithEdition] controls which constraint types are emitted. Enterprise
// edition supports all constraint types (UNIQUE, NOT NULL, NODE KEY,
// PROPERTY_TYPE). Community edition supports UNIQUE constraints only;
// all other constraint types are silently omitted.
//
// # Neo4j Version
//
// The default configuration targets Neo4j 5.0+ Enterprise.
// [WithNodeKeyConstraints](true) requires Neo4j 5.7+.
// Minimum recommended version: Neo4j 5.13+ (fixes the orphaned index
// bug where interrupted constraint creation leaves an orphaned backing
// index that blocks subsequent IF NOT EXISTS retries).
//
// # Sealed Schemas
//
// All schemas passed to the adapter should be sealed
// (schema.IsSealed() == true), which is always the case after
// [github.com/simon-lentz/yammm/schema/load.Load] completes. Sealed
// schemas guarantee that alias chains are fully resolved, inheritance
// is linearized, and type identities are assigned.
//
// # Dependencies
//
//	adapter/neo4j  --imports-->  schema, graph, diag, neo4j-go-driver/v6 (dbtype only)
package neo4j
