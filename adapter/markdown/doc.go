// Package markdown generates a Markdown reference document, including a
// Mermaid class diagram, from a yammm schema. Call [Marshal] with a loaded,
// resolved schema; it returns one deterministic, self-contained document
// covering the schema and its whole import closure — a canonical,
// regenerable documentation artifact that renders anywhere GitHub-flavored
// Markdown does (GitHub file views and READMEs, editor previews, static
// site generators).
//
// # Schema-In, Bytes-Out
//
// Like adapter/gogen and adapter/jschema — and unlike the data adapters —
// markdown has no instance-data path: it never parses, validates, or
// serializes instances, and it imports neither instance nor graph. It maps
// a completed schema to one Markdown document, nothing more, and returns a
// plain error rather than the [github.com/simon-lentz/yammm/diag.Result]
// the rest of the library threads through, because its only failures are
// generator-internal (see Error Conditions), not data diagnostics with
// source locations.
//
// # Document Structure
//
// One document per invocation, entry schema first:
//
//   - "# Schema <Name>" — the title, followed by the schema's own
//     doc-comment when present.
//   - "## Class Diagram" — one Mermaid classDiagram fence covering every
//     type in the import closure (omitted under [WithClassDiagram] false).
//   - "## Types" — one "### <TypeName>" section per entry-schema type in
//     declaration order: badge line (abstract / part markers, Extends
//     links), doc-comment, property table, association and composition
//     lists, invariants.
//   - "## Data Types" — a Name | Definition | Description table over the
//     schema's named DataTypes.
//   - One "## Schema <Name> (imported as <alias>)" section per imported
//     schema in closure order — a transitively imported schema (one the
//     entry does not import directly) has no alias and heads as plain
//     "## Schema <Name>". Its types render as "### <schemaName>.<TypeName>"
//     (collision-proof headings and anchors), its DataTypes under a
//     "### Data Types" table.
//
// Sections with nothing to say are omitted entirely — no empty headings.
//
// # Tables Own Detail; the Diagram Owns Shape
//
// The class diagram deliberately keeps constraint detail out: class members
// are the property name plus a bare kind label (String, Enum, List, …; a
// named DataType shows its name), because full constraint forms overflow
// diagram boxes. The per-type tables carry the detail: the Type column
// renders each constraint's DSL form (String[1, 100], Enum["a", "b"],
// List<FipsCode>) and the Modifiers column carries primary / required plus
// a "from <Owner>" marker on inherited rows — the property table is
// flattened over the full inheritance chain so a type's complete shape
// reads in one place, with provenance preserved.
//
// The diagram draws each type's OWN members and relation edges only;
// inheritance edges (Parent <|-- Child) convey the rest. Abstract and part
// types carry <<Abstract>> / <<Part>> stereotype annotations. Edge labels
// reuse the DSL vocabulary — NAME plus parenthesized multiplicity (one,
// many, one:many, _) — rather than Mermaid cardinality notation, so the
// whole document speaks one vocabulary. Mermaid namespaces are deliberately
// not used (some Markdown renderers do not support them in class
// diagrams); qualified type names render as sanitized class ids
// with a display label instead. That labelled form needs Mermaid 10.1.0 or
// later, and only an imported type takes it, so a schema with imports is in
// scope and an import-free one renders on Mermaid 9. When the diagram holds
// a labelled class the document says so in one sentence under the "## Class
// Diagram" heading, before the fence.
//
// Relations render in the type sections as DSL-notation bullets with the
// target linked to its section:
//
//	--> OWNER (one) [Person](#person)
//
// An inherited relation carries a "— from <Owner>" marker naming its declaring
// ancestor, the same provenance the property table gives inherited rows. A
// relation's edge properties nest as a sub-table under its bullet, and its
// doc-comment as an indented line.
//
// # Invariants
//
// Each invariant renders its failure message as a bullet — with a
// "— from <Owner>" marker when inherited — its doc-comment beneath, then the
// declaration source ("! \"message\" expression",
// exactly as written, doc comment stripped) in a yammm code fence,
// extracted from the schema source via the invariant's span. When no
// source text is available — a Builder-built schema, or a span without
// byte offsets — the fence is omitted and the message stands alone:
// degrading one fence beats rejecting the schema, so [Marshal] does NOT
// require a source-backed schema.
//
// # Output Guarantees
//
// Output is deterministic — byte-identical across runs, machines, and
// checkouts (no absolute paths, all walks ordered) — so generated
// documents can be committed and drift-checked in CI by regenerating and
// diffing. Before returning, [Marshal] structurally verifies its own
// output: every code fence closes (fences are sized past any backtick run
// in their body, so embedded backticks cannot terminate them early), every
// internal link resolves to a heading emitted in this document, and every
// table separator row matches its header's column count. A failure there
// is a generator bug surfaced as an error, never emitted output. Table
// cells escape backslashes and pipes and fold newlines so arbitrary
// doc-comment and enum text cannot break table structure; a value
// containing a backtick renders through an entity-escaped <code> element
// instead of a code span.
//
// # Preconditions
//
// The schema must be completed (aliases resolved, inheritance linearized) —
// always true for a schema returned by
// [github.com/simon-lentz/yammm/schema.Load],
// [github.com/simon-lentz/yammm/schema.LoadString],
// [github.com/simon-lentz/yammm/schema.LoadSourcesWithEntry], or
// [github.com/simon-lentz/yammm/schema.Builder.Build]. Source backing is
// optional (see Invariants).
//
// # Configuration
//
//   - [WithClassDiagram]: include or omit the Mermaid class-diagram
//     section (default true).
//
// # Error Conditions
//
// [Marshal] returns an error (never partial or broken output) when two type
// headings slug to the same anchor — a rename-able input collision (slug
// normalization strips the dots that qualify an imported name), since a link
// to one type would otherwise resolve to the other's section — or when the
// emitted document fails the structural self-check (an unclosed fence, an
// unresolvable internal link, or a malformed table, each a generator bug).
//
// # Thread Safety
//
// [Marshal] is safe for concurrent use. It allocates fresh state per call
// and shares no mutable package state.
//
// # Annotations
//
// Schema annotations do not appear in the emitted document — not in the
// property tables, not in the class diagram. A schema that gains its first
// annotation renders byte-identical Markdown. The generated reference describes
// the data model; where a schema's annotations land as store DDL is
// [github.com/simon-lentz/yammm/adapter/neo4j]'s output, and `yammm neo4j
// indexes` renders it.
//
// They do reach the generator indirectly: a property inherited from several
// ancestors carries the union of their annotations, so the merged view a
// property table iterates yields a synthesized copy rather than the declared
// *Property. The own-versus-inherited lookups therefore key through
// [github.com/simon-lentz/yammm/schema.Property.Origin]; without it such a
// row's provenance silently degrades from the ancestor's display name to the
// declaring scope's bare name — "from A" where "from base.A" is meant, which
// names a different type in any document with two schemas.
//
// # Dependencies
//
//	adapter/markdown  ──imports──▶  schema, location
//
// markdown imports only public yammm packages and the standard library —
// no internal/* (the adapter-layer carve-out documented in adapter/doc.go
// stays gogen-only), no instance/graph, no diag, and no third-party
// modules or Mermaid tooling: the golden corpus plus the structural
// self-check carry output verification.
package markdown
