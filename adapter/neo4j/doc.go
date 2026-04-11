// Package neo4j generates Neo4j 5 constraint statements, label mappings,
// graph shape metadata, parameterized write queries, and schema inference
// from yammm schemas and live Neo4j databases.
//
// # Architectural Position
//
// The neo4j adapter lives alongside [github.com/simon-lentz/yammm/adapter/json]
// in the adapter layer. It depends on library packages (schema, graph, instance,
// immutable, diag); library packages never depend on adapters.
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
// # Constraint Generation
//
// [Adapter.ConstraintsForSchema] returns constraint statements as raw Cypher
// strings. [Adapter.ConstraintsStructured] returns [Constraint] values with
// parsed metadata (kind, label, properties, type expression):
//
//	adapter := neo4j.New()
//	constraints, result := adapter.ConstraintsStructured(ctx, s)
//
// Constraint kinds include [ConstraintUnique], [ConstraintNotNull],
// [ConstraintType], and [ConstraintNodeKey]. Generation is controlled by
// adapter options (see Configuration below).
//
// # Label Mapping
//
// [Adapter.Label] generates namespaced Neo4j labels from schema and type names.
// [Adapter.DetectLabelCollisions] checks for collisions after sanitization.
// [SanitizeIdentifier] and [ValidateIdentifier] apply Neo4j naming rules,
// and [CypherReservedKeywords] returns the set of reserved keywords.
//
// # Graph Shape
//
// [Adapter.ShapeForSchema] converts a schema into a [GraphShape] describing
// the Neo4j node structure (labels, primary keys, required fields per type).
// The shape is required input for write query generation.
//
// # Dual-Mode Write Surface
//
// Write query generation supports two operational modes:
//
//   - Graph mode: [Adapter.BatchNodeQueries] and [Adapter.BatchEdgeQueries]
//     operate on a complete [graph.Snapshot] for high-throughput batch writes.
//
//   - Instance mode: [Adapter.NodeQueryFor] accepts any [NodeSource] (including
//     [*instance.ValidInstance]), and [Adapter.EdgeQueriesFor] generates edge
//     queries directly from a validated instance's edge data — no graph needed.
//
// Single-instance queries ([Adapter.NodeQueryFor], [Adapter.EdgeQueryFor])
// complement the batch entry points for streaming pipelines.
//
// # Introspection
//
// The package generates Cypher queries for inspecting a live Neo4j database:
//
//   - [IntrospectConstraintsQuery]: fetches all constraints
//   - [IntrospectIndexesQuery]: fetches non-constraint-backing indexes
//   - [IntrospectRelationshipsQuery] / [Adapter.IntrospectRelationshipsQueryFor]: discovers relationship signatures
//
// Parse the results with [ParseRemoteConstraints], [ParseRemoteIndexes],
// and [ParseRemoteRelationships].
//
// # Schema Inference
//
// [Adapter.InferSchema] generates a .yammm DSL scaffold from remote
// constraints and relationships discovered via introspection.
//
// # Constraint Diffing
//
// [Adapter.DiffConstraints] produces a [ConstraintDiffResult] classifying
// desired vs. actual constraints into four categories: matched, drifted
// (same target but different definition), to-create, and to-drop.
//
// # Configuration
//
// Adapter options control constraint generation behavior:
//
//   - [WithEdition]: target Neo4j edition ([Enterprise] or [Community])
//   - [WithNodeKeyConstraints]: use NODE KEY instead of separate UNIQUE + NOT NULL
//   - [WithScalarTypeConstraints]: emit PROPERTY_TYPE constraints for scalar properties
//   - [WithRequiredOnlyTypeConstraints]: restrict type constraints to required properties
//   - [WithNamedConstraints]: include explicit constraint names
//   - [WithLabelSeparator]: separator between schema and type in labels (default "__")
//   - [WithLabelPrefix]: global prefix for all labels
//
// Write options control query generation:
//
//   - [WithImmutableKeys]: properties set only on node creation
//   - [WithNodeChunkSize] / [WithEdgeChunkSize]: max rows per UNWIND batch
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
// All schemas passed to the adapter must be sealed, which is always
// the case for schemas returned by [github.com/simon-lentz/yammm/schema.Load]
// or [github.com/simon-lentz/yammm/schema.Builder.Build]. Sealed schemas
// guarantee that alias chains are fully resolved, inheritance is
// linearized, and type identities are assigned.
//
// # Thread Safety
//
// An [Adapter] is safe for concurrent use after construction.
// Configuration is immutable after [New] returns. All methods use only
// local allocations and read-only access to the frozen config.
//
// # Dependencies
//
//	adapter/neo4j  --imports-->  schema, graph, instance, immutable, diag, neo4j-go-driver/v6 (dbtype only)
package neo4j
