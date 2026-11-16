// Package cypher maps a yammm schema to a Neo4j 5 graph: the label each type is
// written under, and the constraint and index DDL that holds the graph to the
// schema. It also validates Neo4j identifiers and builds the DROP statements a
// diff's plan applies.
//
// # Architectural Position
//
// Package cypher is a generator: a schema goes in and Cypher text comes out. It
// reads no instance data, opens no connection and imports no Neo4j driver
// package. The write path, the introspection queries and the drift diff are
// [github.com/simon-lentz/yammm/neo4j]'s. Its write calls and its diff take the
// [Config] this package defines, so a caller that passes one value to both diffs
// a graph under the mapping it was written with.
//
// # Configuration
//
// A [Config] is the storage mapping, and its zero value is the default mapping:
//
//   - [Config.LabelPrefix]: a prefix for every label composed with a schema name
//   - [Config.LabelSeparator]: the separator between the schema name and the
//     type name in a label; "" means "__"
//   - [Config.Edition]: the target Neo4j edition, [Enterprise] or [Community]
//   - [Config.NodeKeys]: one NODE KEY in place of UNIQUE and NOT NULL for a
//     primary key
//   - [Config.TypeConstraints]: which properties get a property type
//     constraint
//
// A Config is a value. Every method reads it and writes only to values it
// allocates, so one Config is safe for concurrent use.
//
// # Labels
//
// [Config.Label] composes a type's label as LabelPrefix, the sanitized name of
// the schema that declares the type, LabelSeparator and the sanitized type
// name: "book_catalog__Publisher". An imported type keeps the label its own
// schema gives it, which is the label its nodes are written under. A type whose
// schema name is empty or white space is labelled with its sanitized name alone,
// with no prefix and no separator. A type whose name is empty or white space has
// no label.
//
// Sanitizing trims the name, replaces a space, '-', '.', '/' and '\' with an
// underscore, puts an underscore before a leading digit, and then drops every
// character but an ASCII letter, a digit and an underscore. The prefix and the
// separator are not sanitized; an empty separator means "__". A composed label is
// then checked with [ValidateIdentifier]: [Config.Constraints], [Config.Indexes]
// and the write path refuse a schema whose labels are not all valid identifiers
// with [E_NEO4J_INVALID_IDENTIFIER].
//
// Two types can compose one label: schema "a" with type B__C and schema "a__B"
// with type C both give "a__B__C". [Config.CheckLabels] reports each such label
// across the schema's import closure with [E_NEO4J_LABEL_COLLISION], and every
// generator and writer refuses the schema on it.
//
// [Config.OwnedLabels] returns the set of labels a schema owns, which a diff
// scopes itself by. Its doc states what the set covers and why ownership is set
// membership and never a rule read from a label string.
//
// # Constraint Generation
//
// [Config.Constraints] returns [Constraint] values: name, kind, label,
// properties, type expression and the CREATE CONSTRAINT IF NOT EXISTS
// statement. Constraint kinds are [ConstraintUnique], [ConstraintNotNull],
// [ConstraintType] and [ConstraintNodeKey]; the zero ConstraintKind names no
// kind. Every constraint carries a name, {label}_{props}_{kind}, and two
// constraints whose readable names collide are told apart with a short
// deterministic digest.
//
// Generation is ALL-OR-NOTHING: any validation error returns (nil, result), so
// one property that cannot be emitted withholds every statement. That is
// deliberate — a partial script looks complete and silently drops the
// guarantees it omits — but it means the diagnostic is all the user gets, so
// each one names what it refuses and why: a property, or for a label collision
// or an invalid label the type and the label.
//
// Beside a name Neo4j refuses as an identifier (Cypher Reserved Words, below)
// and two types that compose one label, the one shape a valid schema can hit
// here is a list whose ELEMENT resolves to a collection: a List or a Vector,
// written directly or through an alias. Those are legal yammm and validate
// normally, but Neo4j has no nested collection property type, so the property is
// refused with [E_NEO4J_UNSUPPORTED_TYPE] under every Config,
// [NoProperties] included. The Config decides whether a property's name is
// checked as an identifier, since a name is checked only where a returned
// statement writes it. It never decides whether a nested collection is refused.
// The pass judges such a property on every type it emits constraints for, the
// types and part types that inherit it included. It judges none on a type whose
// label is invalid, which is refused with [E_NEO4J_INVALID_IDENTIFIER] instead,
// or on an abstract type, which no node carries. A model that must export to Neo4j
// represents the inner collection as a part type reached by a composition
// instead. (A bare Vector is fine — it maps to LIST<FLOAT NOT NULL>; it is only
// a Vector nested INSIDE a list that has no expression.)
//
// # Property Type Constraints
//
// A property type constraint, REQUIRE n.prop IS :: T, holds a property's stored
// values to the Neo4j type its yammm constraint maps to. [Config.TypeConstraints]
// selects the properties that get one:
//
//   - [AllProperties], the default: every scalar, List and Vector property
//   - [RequiredProperties]: required properties alone
//   - [NoProperties]: none, list-shaped and scalar alike
//
// A property that gets no type constraint and no other constraint is written by
// no returned statement, so its name is not checked as an identifier here; the
// index pass checks a name an index annotation writes.
//
// # Edition Gating
//
// [Config.Edition] decides which constraint kinds are emitted. Enterprise holds
// every kind (UNIQUE, NOT NULL, NODE KEY, PROPERTY_TYPE). Community holds UNIQUE
// constraints only; NOT NULL and PROPERTY_TYPE are omitted, having no Community
// equivalent, and [W_NEO4J_EDITION_CONSTRAINT_OMITTED] reports once per call how
// many of each were dropped, so the guarantees the database will not hold are
// named rather than silently absent from the script.
//
// NODE KEY is not omitted but DEGRADED. It is an encoding of UNIQUE + NOT NULL
// rather than a guarantee of its own, so dropping it whole would discard the
// UNIQUE half Community supports — which is what once left primary keys with no
// constraint at all under [Config.NodeKeys] plus [Community], since the primary
// key's NOT NULL is skipped whenever a NODE KEY is meant to cover it. Under
// Community the kind is chosen as UNIQUE up front and
// [W_NEO4J_NODE_KEY_UNSUPPORTED] reports the substitution, so NodeKeys cannot
// change Community output.
//
// # Index Generation
//
// [Config.Indexes] returns [Index] values derived from a schema's @index,
// @@index, @vector, @fulltext and @@fulltext annotations: name, kind, label,
// properties, vector configuration and the statement.
//
// A property-level @index yields a single-property [IndexRange]; a type-level
// @@index yields a composite range index (declared property order is
// significant); a property-level @vector yields an [IndexVector] ANN index whose
// dimension comes from the property's Vector[N] constraint; @fulltext and
// @@fulltext yield an [IndexFulltext]. Load-time validation guarantees
// eligibility for every schema a public entry point returns — dangling
// references are rejected at construction — and the generator still re-checks,
// with [E_NEO4J_UNKNOWN_PROPERTY] for a property that does not exist and
// [E_NEO4J_INVALID_INDEX_TARGET] for one whose type cannot carry the index. No
// schema those entry points return draws either code.
//
// Index names are always emitted — {label}_{props}_idx for range,
// {label}_{prop}_vector_idx for vector, {label}_{props}_fulltext_idx for
// fulltext — because diff and DROP tooling need stable names. Because
// statements carry IF NOT EXISTS, two indexes sharing an emitted name would make
// the database silently skip the second. The readable name is not injective —
// property names may contain the underscore it joins on — so names that would
// collide are disambiguated with a short deterministic digest rather than
// reported as an error, since index emission is all-or-nothing and an error
// would suppress every index in the schema. Disambiguation is
// index-set-internal; it does not cross-check emitted constraint names, whose
// disjoint suffixes make an index-vs-constraint collision negligible.
//
// Unlike constraints, indexes are emitted for every edition: range, vector and
// fulltext indexes are core query features on both Community and Enterprise.
// Abstract types are skipped; part types are not (they receive a label and
// constraints, so they receive index DDL too).
//
// Apply every constraint statement before every index statement. A range index
// over a primary key's properties in their order is the index a UNIQUE or NODE
// KEY constraint creates. Applied after the constraint, its statement creates
// nothing and the constraint's backing index serves it; applied first, the index
// makes the server refuse the constraint with IndexAlreadyExists.
//
// # Cypher Reserved Words
//
// The DSL reserves no Cypher keyword as one. Of the six spellings its own
// grammar refuses as a property name, four are Cypher keywords too ("as", "in",
// "true" and "false"). A property named "match" or a type named "MATCH" is
// valid yammm, but can fail Neo4j export. The names a schema writes into
// generated DDL unquoted — property names, primary keys, and the assembled
// labels — are checked with [ValidateIdentifier] during constraint and index
// generation and rejected as [E_NEO4J_INVALID_IDENTIFIER], whose detail begins
// with the refusing error's text, [ErrReservedKeyword]'s for a reserved word
// (the check is case-insensitive); a constraint or index name is built from
// those checked parts. Namespaced labels usually absorb reserved type names —
// the label "app__MATCH" is not a keyword — but a reserved property name fails
// wherever a generated constraint or index statement names it. No generated DDL
// names an edge property, so no pass checks an edge property's name.
//
// Under the default Config the constraint pass already checks every node
// property it emits a constraint for, which is each but an optional list whose
// element is a collection, refused above, and reports a declared property's name
// once, naming the type that declares it, however many heirs and statements
// write it, as the index pass does. Under [RequiredProperties], [NoProperties]
// or [Community] it checks fewer, and the index pass then covers properties
// named only by an index annotation. For export-compatibility feedback before
// write time, run [Config.Constraints] AND [Config.Indexes], or call
// [ValidateIdentifier] on names directly.
//
// The DROP builders take the opposite path, because they take a name the
// database already holds rather than one this package generated: a name that
// fails validation, reserved word included, is backtick-quoted rather than
// rejected, save a blank one, which is refused with [ErrEmptyIdentifier] — see
// [DropConstraintStatement].
//
// # Neo4j Version
//
// The default Config targets Neo4j 5.9+ Enterprise, and 5.10+ for a schema with
// a List or Vector property: it emits PROPERTY_TYPE constraints, which Neo4j 5.9
// added for scalar types and 5.10 for LIST types. Under [NoProperties] it
// targets Neo4j 5.0+. [Config.NodeKeys] needs no newer release than the
// default. The emitted CREATE VECTOR INDEX ... OPTIONS statement form (from
// @vector annotations) requires Neo4j 5.15+.
//
// # Sealed Schemas
//
// A schema comes only from [github.com/simon-lentz/yammm/schema.Load],
// [github.com/simon-lentz/yammm/schema.LoadString] or
// [github.com/simon-lentz/yammm/schema.LoadSourcesWithEntry], and each returns
// it sealed. Sealed schemas guarantee that alias chains are fully resolved,
// inheritance is linearized, and type identities are assigned.
//
// # Dependencies
//
//	schema/cypher  ──imports──▶  schema, diag, internal/mapping
package cypher
