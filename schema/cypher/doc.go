// Package cypher maps a yammm schema to a Neo4j 5 graph: the label each type is
// written under, and the constraint and index DDL that holds the graph to the
// schema. It also judges Neo4j identifiers and builds the DROP statements a
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
//   - [Config.LabelPrefix]: a prefix for every label
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
// # Errors and Warnings
//
// A method that refuses a schema returns a *[diag.ResultError] holding every
// failure as an issue with a stable code: read it with errors.AsType, and test
// it for a code with errors.Is. A Config is the setting every call shares, and
// an [Option] is a knob of one call: [Config.Constraints], the one method that
// can draw a warning, takes [WithWarnings], which receives each warning as it is
// found and never sees a failure.
//
// # Labels
//
// [Config.Label] composes a type's label as LabelPrefix, the name of the
// schema that declares the type, LabelSeparator and the type name, each as
// written: "book_catalog__Publisher". An imported type keeps the label its own
// schema gives it, which is the label its nodes are written under. Nothing is
// rewritten; a label that breaks the rule below is refused, never repaired.
//
// [Config.CheckLabels] judges the label of every non-abstract type across the
// schema's import closure, as its doc states, and every generator and the neo4j write path refuse the schema on its error,
// each break one [E_NEO4J_INVALID_IDENTIFIER]. The rule:
//
//   - LabelPrefix is empty or starts an identifier, and LabelSeparator is ASCII
//     letters, digits and underscores; a setting that breaks this is reported
//     once.
//   - The schema name and the type name are each an ASCII letter or
//     underscore, then ASCII letters, digits and underscores; a load holds
//     both to the stricter DSL rule.
//   - The separator neither occurs in the schema name nor runs into it from
//     its end, so a label decodes to one (schema, type) pair: schema "a" with
//     type B__C writes a__B__C, so schema "a__B" is refused under "__", and so
//     is "geo_". A schema name that breaks this is reported once.
//   - The composed label passes [IsIdentifier].
//
// A load holds one schema per name and one type per name in a schema, so two
// types whose labels keep the rule never share one.
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
// Generation is ALL-OR-NOTHING: any validation error returns (nil, err), so
// one property that cannot be emitted withholds every statement. That is
// deliberate — a partial script looks complete and silently drops the
// guarantees it omits — but it means the diagnostic is all the user gets, so
// each one names what it refuses and why: a property; for a label, the schema,
// the type and the label; for a schema name, the schema; for a prefix or
// separator, the setting, in its message.
//
// Beside a name Neo4j refuses as an identifier (Cypher Reserved Words, below)
// and a label that breaks the label rule (Labels, above), the one shape a valid schema can hit
// here is a list whose ELEMENT resolves to a collection: a List or a Vector,
// written directly or through an alias. Those are legal yammm and validate
// normally, but Neo4j has no nested collection property type, so the property is
// refused with [E_NEO4J_UNSUPPORTED_TYPE] under every Config whose labels keep
// the label rule, [NoProperties] included. The Config decides whether a
// property's name is checked as an identifier, since a name is checked only
// where a returned statement writes it. Beyond its label settings, it never
// decides whether a nested collection is refused.
// The pass judges such a property on every type it emits constraints for, the
// types and part types that inherit it included. It judges none when a label
// breaks the label rule, since the pass then returns the label rule's
// [E_NEO4J_INVALID_IDENTIFIER] alone, and none on an abstract type, which no
// node carries. A model that must export to Neo4j
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
// labels — are judged by [IsIdentifier] during constraint and index generation
// and refused as [E_NEO4J_INVALID_IDENTIFIER], whose detail says why: "the name
// is a Cypher reserved keyword" for a reserved property name and "the label is
// a Cypher reserved keyword" for a reserved label (the check is
// case-insensitive); a constraint or index name is built from those checked
// parts. A label usually absorbs a reserved type name — the label "app__MATCH"
// is not a keyword — but a reserved property name fails wherever a generated
// constraint or index statement names it. No generated DDL names an edge
// property, so no pass checks an edge property's name.
//
// Under the default Config the constraint pass already checks every node
// property it emits a constraint for, which is each but an optional list whose
// element is a collection, refused above, and reports a declared property's name
// once, naming the type that declares it, however many heirs and statements
// write it, as the index pass does. Under [RequiredProperties], [NoProperties]
// or [Community] it checks fewer, and the index pass then covers properties
// named only by an index annotation. For export-compatibility feedback before
// write time, run [Config.Constraints] AND [Config.Indexes], or call
// [IsIdentifier] on names directly.
//
// The DROP builders take the opposite path, because they take a name the
// database already holds rather than one this package generated: every name,
// a reserved word or one [IsIdentifier] refuses included, is backtick-quoted —
// see [DropConstraintStatement].
//
// # Neo4j Version
//
// The supported floor is Neo4j 5.26, the oldest server the integration suite
// runs. Every statement a Config emits runs on it in the edition the Config
// names: the PROPERTY_TYPE constraints for scalar, List and Vector properties,
// NODE KEY, and the CREATE VECTOR INDEX ... OPTIONS form. A Community server
// refuses the existence, type and NODE KEY constraints an Enterprise Config
// emits; [Community] emits none of them.
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
//	schema/cypher  ──imports──▶  schema, diag, internal/mapping, internal/parse
package cypher
