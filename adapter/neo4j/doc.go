// Package neo4j generates Neo4j 5 constraint and index DDL, label mappings,
// graph shape metadata, parameterized write queries, schema inference, and
// constraint/index drift diffs from yammm schemas and live Neo4j databases.
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
// (directly, via neo4j-migrations, via a consumer's apply step, etc.).
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
// Generation is ALL-OR-NOTHING: any validation error returns (nil, result), so
// one unemittable property withholds the whole schema's DDL. That is deliberate
// — a partial script looks complete and silently drops the guarantees it omits —
// but it means the diagnostic is all the user gets, so each one names the
// property and the reason.
//
// The one shape a valid schema can hit here is a list whose ELEMENT is itself a
// collection: List<List<T>>, List<Vector[N]>, or a list of a list-typed alias.
// Those are legal yammm and validate normally, but Neo4j has no nested
// collection property type, so [ErrUnsupportedListElem] surfaces as
// [E_NEO4J_UNSUPPORTED_TYPE]. A model that must export to Neo4j represents the
// inner collection as a part type reached by a composition instead. (A bare
// Vector is fine — it maps to LIST<FLOAT NOT NULL>; it is only a Vector nested
// INSIDE a list that has no expression.)
//
// # Index Generation
//
// [Adapter.IndexesForSchema] returns index statements as raw Cypher strings
// derived from a schema's @index, @@index, @vector, @fulltext, and @@fulltext
// annotations.
// [Adapter.IndexesStructured] returns [Index] values with parsed metadata
// (kind, label, properties, vector configuration):
//
//	indexes, result := adapter.IndexesStructured(ctx, s)
//
// A property-level @index yields a single-property [IndexRange]; a type-level
// @@index yields a composite range index (declared property order is
// significant); a property-level @vector yields an [IndexVector] ANN index
// whose dimension comes from the property's Vector[N] constraint. Load-time
// validation guarantees eligibility for every schema a public entry point
// returns — dangling references are rejected at construction — and the adapter
// still re-checks as defense in depth, reporting [E_NEO4J_UNKNOWN_PROPERTY]
// for a property that does not exist and [E_NEO4J_INVALID_INDEX_TARGET] for
// one whose type cannot carry the index, rather than trusting a model that
// may have been assembled outside those guarantees.
//
// Index names are always emitted — {label}_{props}_idx for range,
// {label}_{prop}_vector_idx for vector — because diff and DROP tooling need
// stable names and this new surface has no unnamed back-compat to preserve.
// Because statements carry IF NOT EXISTS, two indexes sharing an emitted name
// would make the database silently skip the second. The readable name is not
// injective — property names may contain the underscore it joins on — so names
// that would collide are disambiguated with a short deterministic digest rather
// than reported as an error — index emission is all-or-nothing, so an error
// would suppress every index in the schema. Disambiguation is index-set-internal;
// it does not cross-check emitted constraint names, whose disjoint suffixes make
// an index-vs-constraint collision negligible.
//
// Unlike constraints, indexes are emitted for every edition: range and vector
// indexes are core query features on both Community and Enterprise. Abstract
// types are skipped; part types are not (they receive a label and constraints,
// so they receive index DDL too). The emitted CREATE VECTOR INDEX ... OPTIONS
// statement form requires Neo4j 5.15+.
//
// # Label Mapping
//
// [Adapter.Label] generates namespaced Neo4j labels from schema and type names.
// [Adapter.DetectLabelCollisions] checks for collisions after sanitization.
// [SanitizeIdentifier] and [ValidateIdentifier] apply Neo4j naming rules,
// rejecting reserved keywords with [ErrReservedKeyword].
//
// # Cypher Reserved Words
//
// The DSL does not reserve Cypher keywords: a property named "match" or a
// type named "MATCH" is valid yammm and exports cleanly through the JSON
// and CSV adapters, but can fail Neo4j export. Identifiers that appear
// unquoted in generated Cypher — property names, primary keys, and the
// assembled labels — are checked with [ValidateIdentifier] during constraint and
// index generation and rejected with [ErrReservedKeyword]
// (the check is case-insensitive). Namespaced labels usually absorb
// reserved type names — the label "app__MATCH" is not a keyword — but a
// reserved property name always fails, and a reserved type name fails in
// unscoped (empty schema name) label mode. For export-compatibility
// feedback before write time, run [Adapter.ConstraintsForSchema] AND
// [Adapter.IndexesForSchema], or call [ValidateIdentifier] on names directly.
// The DROP builders take the opposite path, because they take a name the
// database already holds rather than one this package generated: a name that
// fails validation, reserved word included, is backtick-quoted rather than
// rejected — see [DropConstraintStatement].
// Under the default configuration the constraint pass already checks every
// property it emits a constraint for, which is all of them; under
// [WithScalarTypeConstraints](false) or [WithRequiredOnlyTypeConstraints](true)
// it checks fewer, and the index pass then covers properties named only by an
// @index / @@index / @vector annotation. Note that [Adapter.ShapeForSchema]
// validates the label only — it is not a property-name gate.
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
// [Adapter.BatchNodeQueries] and [Adapter.BatchEdgeQueries] operate on a
// complete [graph.Snapshot] for high-throughput batch writes. Both refuse a
// snapshot in which two type identities render one type name, rather than
// writing two types under one label.
//
// # Composition Ownership
//
// [Adapter.BatchNodeQueries] returns a phased slice, ordered by
// [NodeQueryKind]: every node merge precedes every composition replace,
// which precedes every composition create (parent-first by depth). The
// ordering is a documented guarantee — executing the slice in order is
// correct.
//
// A parent write replaces its composed subtree. The replace phase deletes
// every part reachable from each written root through the schema's
// composition closure — whether or not the snapshot carries children, so a
// zero-child write still removes stale ones — and the create phase rebuilds
// the tree from the snapshot's instances. Parts are created fresh after the
// delete (SET c = ..., never SET c +=), so a stale property cannot survive
// on a part the way it can on a merged root node.
//
// Part nodes carry their identity in the _composed_key property (see
// [graph.FormatComposedKey]); for a keyless (many) part the key is
// positional and NOT a stable identity across writes — safe under replace
// semantics, which never match an existing part node by key.
//
// # Write-Once Derivation
//
// [ImmutableKeysFor] returns a type's @writeOnce-annotated properties (own and
// inherited). [Adapter.ShapeForSchema] records them on each [NodeShape], and the
// node write path selects the immutable-key shape per type from that set,
// so a @writeOnce property is set on node creation and never rewritten on a
// subsequent MERGE.
//
// The guarantee does not depend on the caller holding a [schema.Type]: the keys
// travel on the shape, which every write entry point already receives. Nothing
// in the write surface removes a key from that set, so there is no way to opt a
// @writeOnce property back into being rewritten.
//
// # Cypher Builders
//
// [BuildBatchRelationshipMergeQuery] produces the UNWIND-batched relationship
// MERGE template the write surface uses internally. It is exported for
// consumers — a link engine resolving edges across datasets, for instance —
// that want the template without the surrounding param-and-chunk plumbing.
//
// It is a pure function: no execution, no driver dependency, no side effects.
// Callers pair the returned string with their own parameter map and feed both
// to a driver session at the call site. The template always ends with
// `RETURN count(*) AS matched_rows`, so a consumer implementing
// silent-failure detection has a stable column to sum; aggregation across
// chunks is the consumer's.
//
// The node-merge and single-relationship templates are internal. Node
// templates stay RETURN-free — a constraint violation on a node surfaces as a
// driver error, not a silent zero-match.
//
// # Introspection
//
// The package generates Cypher queries for inspecting a live Neo4j database:
//
//   - [IntrospectConstraintsQuery]: fetches all constraints
//   - [IntrospectIndexesQuery]: fetches every non-LOOKUP index, including the
//     index backing a constraint — [RemoteIndex.OwningConstraint] identifies
//     those, and the diff needs them because they block a CREATE INDEX by name
//     and by definition
//   - [IntrospectRelationshipsQuery] / [Adapter.IntrospectRelationshipsQueryFor]: discovers relationship signatures
//
// Parse the results with [ParseRemoteConstraints], [ParseRemoteIndexes],
// and [ParseRemoteRelationships]. [ParseRemoteIndexes] reads the index options
// map, so [RemoteIndex.VectorDimensions] and [RemoteIndex.VectorSimilarity]
// expose a vector index's configuration for drift detection.
//
// # Schema Inference
//
// [Adapter.InferSchema] generates a .yammm DSL scaffold from remote
// constraints and relationships discovered via introspection.
//
// # Diff Scope and Name Blocking
//
// Both diff entry points take an [*OwnedLabels] — the exact set of labels this
// adapter emits for a schema, built once with [Adapter.OwnedLabels] and passed
// to both halves:
//
//	owned := adapter.OwnedLabels(ctx, s)
//	cDiff := adapter.DiffConstraints(desiredConstraints, actualConstraints, owned, actualIndexes...)
//	iDiff := adapter.DiffIndexes(desiredIndexes, actualIndexes, owned, actualConstraints...)
//
// Ownership is set membership, not a rule applied to a remote object's label
// string. [Adapter.Label] composes a label from a caller-configurable prefix and
// separator around two sanitized free-form names and is not invertible, so for
// any rule that reads a schema back out of a label there is a configuration, or
// a sibling schema name, that satisfies the rule without belonging to the
// schema. See [OwnedLabels] for what the set covers and what it cannot see.
//
// Both results carry an Excluded count — the remote objects that entered NO
// bucket, because the comparison had nothing to say about them: the schema owns
// no label they carry, or they are of a kind this configuration cannot declare.
// It is deliberately not drift; in a database shared with other applications a
// non-zero count is the normal state. It exists so that "0 to drop" cannot be
// read as "the database is accounted for": ownership is derived from the schema
// in hand, so objects left behind by a type deleted or renamed since the last
// apply sit on a label no current type declares and nothing can name them.
//
// The trailing variadic argument is the OTHER side's remote objects. Index and
// constraint names share ONE Neo4j namespace, and every emitted statement
// carries IF NOT EXISTS, so a declaration whose name the database already holds
// is a silent no-op rather than a create. Passing both sides lets each diff
// report such a declaration as drift naming the holder. A constraint backed by
// an index appears in SHOW INDEXES under the constraint's name and is seen
// automatically; NOT NULL and TYPE constraints have no backing index and reach
// [Adapter.DiffIndexes] only this way. Omitting the argument is safe but weaker
// — the blocked declaration then reports as a create the server ignores on
// every run.
//
// # Constraint Diffing
//
// [Adapter.DiffConstraints] produces a [ConstraintDiffResult] classifying
// desired vs. actual constraints into FIVE categories: matched, drifted (a
// different definition under the same identity, or a name the database already
// holds), to-create, to-drop, and unverified (present, but the server did not
// report what was needed to compare it). A caller deciding "is this in sync?"
// must consult Unverified as well: treating it as matched reports an unchecked
// constraint as verified.
//
// Unverified is reachable when the records came from a query that does not yield
// propertyType — a caller introspecting with its own Cypher rather than
// [IntrospectConstraintsQuery]. A server too old to report that column is also
// too old to hold a TYPE constraint, so such a constraint simply lands in
// to-create there.
//
// # Index Diffing
//
// [Adapter.DiffIndexes] produces an [IndexDiffResult] classifying desired vs.
// actual indexes into FIVE categories: matched, drifted, to-create, to-drop, and
// unverified (present, but its configuration could not be read, or it is still
// populating). As with constraints, Unverified is neither in sync nor drifted and
// must be consulted separately.
//
// Drift has four producers, so [IndexDrift.Actual] is not always an index kind
// the schema could declare: a vector index whose dimension or similarity
// differs; a definition change under a name the database already holds; an index
// in a state that serves no queries; and a desired index the server would refuse
// to create, because its name or its whole definition is already taken — there
// the Actual is the blocker, which may be a TEXT, POINT, or multi-label
// FULLTEXT index, an
// index on a label this schema does not own, or a constraint's backing index. Composite property order is
// significant, a deliberate divergence from [Adapter.DiffConstraints]: a
// same-set/different-order remote index is a distinct index — create + drop when
// its name differs too, and drift when it holds the desired index's name. A
// schema-owned remote index with no declaration is reported as a drop — the
// drift the index feature exists to surface. Drops are reported, never applied.
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
//   - [WithNodeChunkSize] / [WithEdgeChunkSize]: max rows per UNWIND batch
//
// # Edition Gating
//
// [WithEdition] controls which constraint types are emitted. Enterprise
// edition supports all constraint types (UNIQUE, NOT NULL, NODE KEY,
// PROPERTY_TYPE). Community edition supports UNIQUE constraints only; NOT
// NULL and PROPERTY_TYPE are omitted, having no Community equivalent, and
// [W_NEO4J_EDITION_CONSTRAINT_OMITTED] reports once per call how many of each
// were dropped, so the guarantees the database will not hold are named
// rather than silently absent from the script.
//
// NODE KEY is not omitted but DEGRADED. It is an encoding of UNIQUE + NOT NULL
// rather than a guarantee of its own, so dropping it whole would discard the
// UNIQUE half Community supports — which is what once left primary keys with no
// constraint at all under [WithNodeKeyConstraints](true) plus [Community], since
// the primary key's NOT NULL is skipped whenever a NODE KEY is meant to cover
// it. Under Community the kind is chosen as UNIQUE up front and
// [W_NEO4J_NODE_KEY_UNSUPPORTED] reports the substitution, so the flag cannot
// change Community output.
//
// # Neo4j Version
//
// The default configuration targets Neo4j 5.0+ Enterprise.
// [WithNodeKeyConstraints](true) requires Neo4j 5.7+.
// Minimum recommended version: Neo4j 5.13+ (fixes the orphaned index
// bug where interrupted constraint creation leaves an orphaned backing
// index that blocks subsequent IF NOT EXISTS retries).
// The emitted CREATE VECTOR INDEX ... OPTIONS statement form (from @vector
// annotations) requires Neo4j 5.15+.
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
// local allocations and read-only access to the frozen config. The one
// package-level mutable value is a concurrency-safe memo of which zone names
// the host's tz database resolves, consulted by [Coerce]; it caches a host
// fact fixed for the process lifetime and holds no adapter state.
//
// # Dependencies
//
//	adapter/neo4j  --imports-->  schema, graph, instance, immutable, diag, neo4j-go-driver/v6 (dbtype only)
package neo4j
