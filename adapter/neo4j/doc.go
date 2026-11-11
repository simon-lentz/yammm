// Package neo4j generates Neo4j 5 constraint and index DDL, label mappings,
// graph shape metadata, parameterized write queries and their coerced
// parameters, introspection queries, and constraint/index drift diffs from
// yammm schemas and live Neo4j databases, and it sanitizes and validates Neo4j
// identifiers.
//
// # Architectural Position
//
// Package neo4j is the module's one adapter. It depends on library packages
// (schema, graph, immutable, diag) and on internal/value, the rule the
// validator, the graph and the snapshot reader read a value by; library packages
// never depend on adapters.
//
// # Neo4j Driver Dependency (Type-Only)
//
// The adapter imports [github.com/neo4j/neo4j-go-driver/v6/neo4j/dbtype]
// for temporal type definitions (dbtype.Date). It does not import
// connection, session, or transaction packages — no code in this package
// opens a connection or executes queries against a database. Consumers
// supply their own driver and choose how to apply the generated output
// (directly, via neo4j-migrations, via a consumer's apply step, etc.): the
// package produces Cypher and driver-compatible parameters.
//
// # Constraint Generation
//
// [Adapter.ConstraintsForSchema] returns constraint statements as raw Cypher
// strings. [Adapter.ConstraintsStructured] returns [Constraint] values with
// parsed metadata (name, kind, label, properties, type expression, statement):
//
//	adapter := neo4j.New()
//	constraints, result := adapter.ConstraintsStructured(ctx, s)
//
// Constraint kinds include [ConstraintUnique], [ConstraintNotNull],
// [ConstraintType], and [ConstraintNodeKey]. Generation is controlled by
// adapter options (see Configuration below).
//
// Generation is ALL-OR-NOTHING: any validation error returns (nil, result), so
// one unemittable property withholds every statement the call would return.
// That is deliberate — a partial script looks complete and silently drops the
// guarantees it omits — but it means the diagnostic is all the user gets, so
// each one names what it refuses and why: a property, or for a label collision
// or an invalid label the type and the label.
//
// Beside a name Neo4j refuses as an identifier (Cypher Reserved Words, below)
// and two types that compose one label ([Adapter.DetectLabelCollisions]), the one
// shape a valid schema can hit here is a list whose ELEMENT resolves to a
// collection: a List or a Vector, written directly or through an alias.
// Those are legal yammm and validate normally, but Neo4j has no nested
// collection property type, so [ErrUnsupportedListElem] surfaces as
// [E_NEO4J_UNSUPPORTED_TYPE] under every option that decides which constraints
// are emitted, [WithScalarTypeConstraints](false) included. Such an option
// decides whether a property's name is checked as an identifier, since a name is
// checked only where a returned statement writes it. No such option decides
// whether a nested collection is refused. The pass judges such a property on every type
// it emits constraints for, the types and part types that inherit it included.
// It judges none on a type whose label is invalid, which is refused with
// [E_NEO4J_INVALID_IDENTIFIER] instead, or on an abstract type, which has no
// label. A model that must export to Neo4j represents the
// inner collection as a part type reached by a composition instead. (A bare
// Vector is fine — it maps to LIST<FLOAT NOT NULL>; it is only a Vector nested
// INSIDE a list that has no expression.) The write path refuses a value of
// such a property by its type, before any element is read, with
// [ErrUnsupportedListElem]: [Adapter.BatchNodeQueries]
// and [CoerceParams] return an error for a value that holds one, an
// empty list included, and read a value that holds none as nil, which
// BatchNodeQueries writes as null.
//
// # Index Generation
//
// [Adapter.IndexesForSchema] returns index statements as raw Cypher strings
// derived from a schema's @index, @@index, @vector, @fulltext, and @@fulltext
// annotations.
// [Adapter.IndexesStructured] returns [Index] values with parsed metadata
// (name, kind, label, properties, vector configuration and the statement):
//
//	indexes, result := adapter.IndexesStructured(ctx, s)
//
// A property-level @index yields a single-property [IndexRange]; a type-level
// @@index yields a composite range index (declared property order is
// significant); a property-level @vector yields an [IndexVector] ANN index
// whose dimension comes from the property's Vector[N] constraint. Load-time
// validation guarantees eligibility for every schema a public entry point
// returns — dangling references are rejected at construction — and the adapter
// still re-checks, with [E_NEO4J_UNKNOWN_PROPERTY] for a property that does not
// exist and [E_NEO4J_INVALID_INDEX_TARGET] for one whose type cannot carry the
// index. No schema those entry points return draws either code.
//
// Index names are always emitted — {label}_{props}_idx for range,
// {label}_{prop}_vector_idx for vector, {label}_{props}_fulltext_idx for
// fulltext — because diff and DROP tooling need
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
// Unlike constraints, indexes are emitted for every edition: range, vector and
// fulltext indexes are core query features on both Community and Enterprise. Abstract
// types are skipped; part types are not (they receive a label and constraints,
// so they receive index DDL too). The emitted CREATE VECTOR INDEX ... OPTIONS
// statement form requires Neo4j 5.15+.
//
// Apply every constraint statement before every index statement. A range index
// over a primary key's properties in their order is the index a UNIQUE or NODE
// KEY constraint creates. Applied after the constraint, its statement creates
// nothing and the constraint's backing index serves it; applied first, the index
// makes the server refuse the constraint with IndexAlreadyExists.
//
// # Label Mapping
//
// [Adapter.Label] generates namespaced Neo4j labels from schema and type names.
// [Adapter.DetectLabelCollisions] checks for collisions after sanitization.
// [SanitizeIdentifier] rewrites a name into the characters a Neo4j identifier
// takes, and [ValidateIdentifier] applies Neo4j's naming rules, rejecting
// reserved keywords with [ErrReservedKeyword].
//
// # Cypher Reserved Words
//
// The DSL reserves no Cypher keyword as one. Of the six spellings its own
// grammar refuses as a property name, four are Cypher keywords too ("as", "in",
// "true" and "false"). A property named "match" or a type named "MATCH" is
// valid yammm, but can fail Neo4j export. The names a schema writes into
// generated DDL unquoted — property names, primary keys, and the assembled
// labels — are checked with [ValidateIdentifier] during constraint and index
// generation and rejected as E_NEO4J_INVALID_IDENTIFIER, whose detail begins
// with the refusing error's text, [ErrReservedKeyword]'s for a reserved word (the check is case-insensitive); a constraint or index name is built from those checked
// parts. Namespaced labels usually absorb
// reserved type names — the label "app__MATCH" is not a keyword — but a
// reserved property name fails wherever a generated constraint or index
// statement names it, and a reserved type name fails in unscoped (empty schema
// name) label mode. No generated DDL names an edge property, so no pass checks
// an edge property's name. The write queries write relationship types unquoted
// too and check only an edge's ([Adapter.BatchEdgeQueries]): a composition's
// relation name, which the composition statements write, is checked nowhere.
// For export-compatibility
// feedback before write time, run [Adapter.ConstraintsForSchema] AND
// [Adapter.IndexesForSchema], or call [ValidateIdentifier] on names directly.
// The DROP builders take the opposite path, because they take a name the
// database already holds rather than one this package generated: a name that
// fails validation, reserved word included, is backtick-quoted rather than
// rejected, save a blank one, which is refused with [ErrEmptyIdentifier] — see
// [DropConstraintStatement].
// Under the default configuration the constraint pass already checks every
// node property it emits a constraint for, which is each but an optional list
// whose element is a collection, refused above, and reports a
// declared property's name once, naming the type that declares it, however many
// heirs and statements write it, as the index pass does; under
// [WithScalarTypeConstraints](false), [WithRequiredOnlyTypeConstraints](true)
// or [WithEdition]([Community]) it checks fewer, and the index pass then covers properties named only by an
// @index / @@index / @vector / @fulltext / @@fulltext annotation. Note that [Adapter.ShapeForSchema]
// validates the label only — it is not a property-name gate.
//
// # Graph Shape
//
// [Adapter.ShapeForSchema] converts a schema into a [GraphShape] describing
// the Neo4j node structure (labels, primary keys and their constraints, and
// @writeOnce keys per type) and holding the schema it came from. The shape is required input for write query generation, and exports no field.
//
// # Write Surface
//
// [Adapter.BatchNodeQueries] and [Adapter.BatchEdgeQueries] operate on a
// complete [graph.Snapshot] for high-throughput batch writes. They are the
// snapshot's write surface, and the package has no single-item write. Hand-built
// Cypher can use [BuildBatchRelationshipMergeQuery] and [CoerceParams].
//
// Both require a [GraphShape] that [Adapter.ShapeForSchema] built from the
// snapshot's schema object, and refuse any other: another load can declare a key
// another way, so a merge key would reach the driver under one type while the
// same property is coerced from this schema to another. A shape is keyed by
// [schema.TypeID], so two closure types sharing one bare name each get their own
// label. Two root types render
// one label only where their sanitized schema and type names compose one
// label, as two SCHEMA names that sanitize alike do, or schema "a" with type B__C
// and schema "a__B" with type C; [Adapter.ShapeForSchema] refuses that closure.
//
// An error marked [ErrUnrepresentable] refuses these values: one its constraint's coercion refuses, a nested collection's, a map at a
// property the schema declares, a list or a map a document nests more than 256
// levels deep, a [encoding/json.Number] no driver number holds, or a primary key
// that is absent or holds no value. An integer literal a finite float64 holds
// passes them at a Float position and rounds to its nearest float64. No snapshot these writers accept holds a property the schema does not
// declare: the validator keeps one out of a graph, and
// [github.com/simon-lentz/yammm/snapshot.Load] and [graph.NewFromSnapshot] refuse
// one. Of the values the mark refuses, a validated snapshot holds only a nested
// collection's, which the validator accepts; any other is held only by a
// snapshot whose values no validator refused at Error. [CoerceParams] marks its
// refusals of a value so too. No other error
// carries the class: a caller's own, such as a constraint
// [schema.CheckConstraint] refuses, a [GraphShape] the adapter did not build
// for the snapshot's schema or a relationship type that is no valid identifier,
// and a cancelled context.
//
// # Parameter Coercion
//
// One rule converts a value in the form the validator stores to the
// driver-native type its [schema.Constraint] requires, so the value satisfies
// Neo4j TYPE constraints. The write path applies it once it has read each
// json.Number as the driver's number. [CoerceParams] applies it to the
// parameters of hand-built Cypher: it first reads each value at its constraint
// as [github.com/simon-lentz/yammm/instance.Validator] reads a property value,
// so a parameter reaches the driver as the write path sends that value from a
// validated instance, and a value the validator refuses (a wrong kind, a bound,
// a member, a pattern) is an error. CoerceParams reads one flat map and walks
// no value: to coerce the fields of a nested map or of a row, call it on that
// map. The rule takes the full constraint, not just its kind, so a Timestamp's
// custom layout is honored; a DataType reference is resolved internally.
//
//   - Float: a float64. Integer: an int64. Boolean: a bool. String, Enum and
//     Pattern: a string. Each passes through, and a value of any other Go type
//     is a coercion failure and returns an error.
//   - UUID: a string -> its canonical text, lower case with hyphens. A string
//     that is no UUID is a coercion failure.
//   - Timestamp: a string -> time.Time (Neo4j ZONED DATETIME), parsed against the
//     constraint's custom Go layout when it declares one (Timestamp["…"]) and
//     against RFC3339 otherwise. A string that does not parse, or any other
//     value, is a coercion failure. The text is read in UTC, so the host's
//     zone decides nothing, and the instant goes as its offset and never under
//     a zone name: a UTC instant goes as offset zero. The server holds one
//     instant under a zone name and under that zone's offset as two unequal
//     values, and Cypher's datetime() of RFC 3339 text gives the offset form.
//   - Date: a "2006-01-02" string -> dbtype.Date (Neo4j DATE). A string that
//     does not parse, or any other value, is a coercion failure. The date is
//     midnight UTC on the day the text shows, since a date carries no time of
//     day. [CoerceParams] also passes a dbtype.Date at a Date, read the same
//     way; in a list it is refused, as the validator refuses it.
//   - Vector: a []float64 passes through. At a List and at a Vector a []any is
//     element-coerced by the rules above into a typed slice. A []any at any
//     other kind is refused, since a scalar cannot hold a list.
//
// A map is refused under every constraint, whatever its key and element types:
// Neo4j stores no map as a property value. A nil map holds no value and returns
// nil.
//
// [CoerceParams] reads a value as the validator does, so a value in a form the
// validator reads is read the same way. A Go integer of any width, a named type
// and a time.Time at a Timestamp are three such forms. It refuses one value
// the validator takes: a value at a List whose element is a collection. A
// time.Time goes through the text the validator stores it as. At a Timestamp
// that text is in the constraint's layout, so the instant reaches the server at
// the precision that text keeps and, with one exception, at its offset. Text
// under a layout that holds no offset is read as UTC. The exception: under a
// layout that holds a zone abbreviation beside an offset, text whose
// abbreviation is UTC is read as UTC whatever offset it keeps. From a key that
// holds a constraint no zone name reaches the server, a location's such as
// Europe/Berlin included, and there a pointer is refused, a nil one too.
//
// On the write path the rule refuses only a value no validator refused at
// Error: one snapshot.Load read without revalidating at Error, or one
// graph.NewFromSnapshot imported under another schema, which holds no value to
// its constraint. There it judges a value's form and never its bounds, members
// or pattern, nor a Vector's dimension. A Timestamp, a Date and a UUID are
// forms, so their text is parsed.
//
// The write path refuses a value at a List whose element is a List or a Vector
// by its type first: Neo4j has no nested collection property type, so a value
// there, an empty list included, is an error wrapping [ErrUnsupportedListElem],
// and [CoerceParams] refuses one so too. The write path reads every JSON number
// literal as the driver's number before the rule: at a Float its nearest
// float64, elsewhere an integer literal as the int64 it spells and a float
// literal as its nearest float64, which an Integer refuses as a float. A
// literal no finite float64 holds, or no int64 holds outside a Float and a
// Vector element, where the .ys reader left it a json.Number, is refused before
// the rule.
//
// A value that holds none ([github.com/simon-lentz/yammm/instance.IsAbsent]),
// nil, a nil slice, a nil map or a nil immutable container, is returned as nil,
// which the driver writes as null, and a nil constraint ("no type to coerce against") passes any other
// value through unchanged. A kind added to [schema.ConstraintKind] after the rule's switch was
// written surfaces as a lint failure via its //exhaustive:enforce directive, rather
// than as a silent driver-side PROPERTY_TYPE rejection in production.
//
// # Composition Ownership
//
// [Adapter.BatchNodeQueries] returns a phased slice, ordered by
// [NodeQueryKind]: every node merge precedes every composition replace,
// which precedes every composition create (parent-first by depth). The
// ordering is a documented guarantee — executing the slice in order is
// correct.
//
// The guarantee is about ORDER, not atomicity. Each query is handed to the
// caller separately, so a caller that runs each in its own transaction and
// fails partway leaves a subtree deleted by the replace phase and not yet
// rebuilt by the create phase. A caller that needs the replace and create of
// one root to be all-or-nothing runs the slice inside a single transaction.
//
// A parent write replaces its composed subtree. The replace phase deletes
// every part reachable from each written root through the schema's
// composition closure — whether or not the snapshot carries children, so a
// zero-child write still removes stale ones — and the create phase rebuilds
// the tree from the snapshot's instances. Parts are created fresh after the
// delete (SET c = ..., never SET c +=), so a stale property cannot survive
// on a part the way it can on a merged root node.
//
// Part nodes carry their identity in the _composed_key property, which this
// adapter mints because a part has none of its own: the library identifies one
// through its parent composition, and only a store that must address each node
// separately needs an address. For a keyless (many) part the segment is
// positional and NOT a stable identity across writes — safe under replace
// semantics, which delete a root's earlier parts first, so a key matches only a
// part node the same write created.
//
// The address is a flat JSON array: the owning root's LABEL, the root's key
// values, then one two-element segment per composition hop, except a (one)
// composition, whose segment names the relation alone.
//
//	["shop__Order",["o1"],["SECTIONS",["s1"]],["NOTES",0]]
//
// Three properties earn that shape. It is FLAT, so the address grows linearly
// with depth; nesting each level's rendering inside the next escapes the level
// above on every hop and the escapes compound. It leads with the root's LABEL,
// because the depth-2 parent MATCH is scoped by the PART label and not by the
// root, so two root types sharing a key value and a relation name would
// otherwise mint one address for children under one part label —
// [Adapter.ShapeForSchema] refuses a closure whose labels collide, which is
// what makes the label exact. And it carries NO source path: a type identity
// renders a file-backed schema's absolute path, which would put the writing
// machine's directory layout on every part node and change every address when
// the schema file moves.
//
// # Write-Once Derivation
//
// [ImmutableKeysFor] returns a type's @writeOnce-annotated properties (own and
// inherited). [Adapter.ShapeForSchema] records them on the shape, and the
// node write path selects the immutable-key shape per type from that set,
// so a @writeOnce property is set on node creation and never rewritten on a
// subsequent MERGE.
//
// The guarantee does not depend on the caller holding a [schema.Type]: the keys
// travel on the shape, which every write entry point already receives, and a
// caller cannot change them.
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
// The node-merge and composition templates are internal. A node merge ends
// with no RETURN clause: a MERGE creates the node it does not find, so it has
// no missed match to count. The composition templates are MATCH-anchored, so
// each ends with the same `RETURN count(*) AS matched_rows`.
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
//
// Parse the results with [ParseRemoteConstraints] and [ParseRemoteIndexes].
// [ParseRemoteIndexes] reads the index options map, so
// [RemoteIndex.VectorDimensions] and [RemoteIndex.VectorSimilarity] expose a
// vector index's configuration for drift detection.
//
// # Diff Scope and Name Blocking
//
// Both diff entry points take an [*OwnedLabels] — the labels a schema owns,
// every named, non-abstract member type's label, a superset of the labels
// this adapter emits
// ([Adapter.OwnedLabels] says which the emitters leave out), built once with [Adapter.OwnedLabels] and passed
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
// schema. See [Adapter.OwnedLabels] for what the set covers and what it cannot
// see.
//
// Both results carry an Excluded count — the remote objects the comparison
// set aside because the schema owns no label they carry, or could not declare
// them as they stand under this configuration ([RemoteIndex.Declarable] states
// the index side's rule). One can still stand as the Actual of a Drift, where
// it holds the name of a declaration nothing realises, already serves the
// definition of an index a declaration asks for, or is a UNIQUE or NODE KEY
// over the key a declaration of the other kind needs: a NODE KEY under
// [WithEdition](Community), or one on a declared label the owned set leaves out.
// It is deliberately not drift; in a database shared with other applications a
// non-zero count is the normal state. It exists so that "0 to drop" cannot be
// read as "the database is accounted for": ownership is derived from the schema
// in hand, so objects left behind by a type deleted or renamed since the last
// apply sit on a label no current type declares and nothing can name them.
//
// The trailing variadic argument is the OTHER side's remote objects. Index and
// constraint names share ONE Neo4j namespace, and every emitted statement
// carries IF NOT EXISTS, so a declaration whose name the database already holds
// creates nothing. Over a name of its own kind the statement is a silent no-op,
// and a constraint's backing index counts as an index. Over the other kind's name
// the server refuses it: a CREATE INDEX over a NOT NULL or TYPE constraint's name,
// and a CREATE CONSTRAINT over a plain index's name. Passing both sides lets each diff
// report such a declaration as drift naming the holder. A constraint backed by
// an index appears in SHOW INDEXES under the constraint's name and is seen
// automatically; NOT NULL and TYPE constraints have no backing index and reach
// [Adapter.DiffIndexes] only this way. Omitting the argument is weaker: the
// blocked declaration then reports as a create on every run, which the server
// refuses.
//
// # Constraint Diffing
//
// [Adapter.DiffConstraints] produces a [ConstraintDiffResult] classifying
// desired vs. actual constraints into FIVE categories: matched, drifted (a
// different definition under the same identity, a name the database already
// holds, a UNIQUE or NODE KEY declaration whose label and properties, in its
// order, a RANGE index already serves under another name, or one beside a constraint of the
// other of those two kinds over the same label and properties in the same
// order), to-create, to-drop, and unverified (present, but the server did not
// report what was needed to compare it). The server refuses a UNIQUE and a NODE
// KEY constraint over one key, so the other kind's constraint is that drift's
// Actual and is not also a to-drop: drop it, then create the declaration. A
// NODE KEY also requires every node to hold each key property, and the server
// refuses it while a node lacks one. Applied after the drop, that refusal leaves
// the key with no uniqueness constraint, so check the data before the drop.
// A caller deciding "is this in sync?"
// must consult Unverified as well: treating it as matched reports an unchecked
// constraint as verified.
//
// Unverified is reachable when the records came from a query that does not yield
// propertyType — a caller introspecting with its own Cypher rather than
// [IntrospectConstraintsQuery]. A server too old to report that column is also
// too old to hold a TYPE constraint, so such a constraint lands in to-create
// there, or in drift where an object holding its name blocks the create.
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
// in a state that serves no queries and will not recover on its own (FAILED; a
// POPULATING index is unverified); and a desired index the server will not
// create, because its name or its whole definition is already taken. There
// the Actual is the blocker: an index of any kind or on any label, a
// constraint's backing index, an index that realises another declaration of
// this schema, or, when the caller passes constraints as alsoBlocking, a
// constraint with no backing index, whose Actual carries its name, labels and
// properties and no index type. Composite property order is
// significant, a deliberate divergence from [Adapter.DiffConstraints]: a
// same-set/different-order remote index is a distinct index — create + drop when
// its name differs too and [RemoteIndex.Declarable] accepts it, create alone when
// that refuses it, and drift when it holds the desired index's name. A
// schema-owned remote index that [RemoteIndex.Declarable] accepts and no
// declaration matches is reported as a drop — the drift the index feature
// exists to surface. An owned index it refuses is never a drop: it is counted in [IndexDiffResult.Excluded], a
// blocker included, unless it is a constraint's backing index that serves a
// declaration. That row is the declaration's Actual and is not counted: a match
// where it serves queries ([RemoteIndex.IsOnline]), drift where it is FAILED,
// and unverified where it is POPULATING. Drops are reported, never applied.
//
// # Configuration
//
// Adapter options control constraint generation behavior:
//
//   - [WithEdition]: target Neo4j edition ([Enterprise] or [Community])
//   - [WithNodeKeyConstraints]: use NODE KEY instead of separate UNIQUE + NOT NULL
//   - [WithScalarTypeConstraints]: emit PROPERTY_TYPE constraints for scalar,
//     List and Vector properties
//   - [WithRequiredOnlyTypeConstraints]: restrict type constraints to required properties
//   - [WithNamedConstraints]: include explicit constraint names
//   - [WithLabelSeparator]: separator between schema and type in labels (default "__")
//   - [WithLabelPrefix]: prefix for every label rendered with a schema name
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
// The default configuration targets Neo4j 5.9+ Enterprise, and 5.10+ for a
// schema with a List or Vector property: it emits PROPERTY_TYPE constraints,
// which Neo4j 5.9 added for scalar types and 5.10 for LIST types. With
// [WithScalarTypeConstraints](false) it targets Neo4j 5.0+, and 5.9+ for a schema
// with a composition, whose replace query uses a quantified path pattern.
// [WithNodeKeyConstraints](true) needs no newer release than the default.
// The emitted CREATE VECTOR INDEX ... OPTIONS statement form (from @vector
// annotations) requires Neo4j 5.15+.
//
// # Sealed Schemas
//
// A schema comes only from [github.com/simon-lentz/yammm/schema.Load],
// [github.com/simon-lentz/yammm/schema.LoadString] or
// [github.com/simon-lentz/yammm/schema.LoadSourcesWithEntry], and each returns
// it sealed. Sealed schemas
// guarantee that alias chains are fully resolved, inheritance is
// linearized, and type identities are assigned.
//
// # Thread Safety
//
// An [Adapter] is safe for concurrent use after construction.
// Configuration is immutable after [New] returns. Every method reads the
// frozen config and writes only to values it allocates.
//
// # Dependencies
//
//	adapter/neo4j  ──imports──▶  schema, graph, immutable, diag, internal/value,
//	                             github.com/neo4j/neo4j-go-driver/v6/neo4j/dbtype
package neo4j
