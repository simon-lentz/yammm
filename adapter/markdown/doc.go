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
//     "## Schema <Name>". Its types render under their display names (below),
//     its DataTypes under a "### Data Types" table.
//
// Sections with nothing to say are omitted entirely — no empty headings.
//
// A type is named the way the entry schema names it, in its heading, in every
// link to it, in "from <Owner>" markers and in the diagram: the bare name for a
// type the entry declares, the entry's own tag "<alias>.<TypeName>" for one it
// imports directly, and "<TypeName> (<schemaName>)" for one it reaches only
// through another import, which has no tag. The tag is the one
// [github.com/simon-lentz/yammm/schema.AddressableTag] returns, which is how a
// data file names the type.
//
// Every heading takes the anchor GitHub gives it: the heading's slug — its
// text lowercased, every character but letters and other alphabetic
// characters, marks, decimal digits, connector punctuation and hyphens
// removed, spaces made hyphens — suffixed -1, -2, …
// when an earlier heading took it. The generator reads the emitted document
// with a CommonMark parser and allocates over every heading the parser finds,
// the headings inside doc comments included, so each link lands on the heading
// it names: two headings that slug alike each keep a working link, and a type
// whose heading slugs like a section heading — a type named Types — links to
// its own heading.
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
// reads in one place, with provenance preserved. When the diagram is still
// too large, [WithClassMembers] with false drops the member lines and keeps
// every class, stereotype and edge.
//
// The diagram draws each type's OWN members and relation edges only;
// inheritance edges (Parent <|-- Child) convey the rest. Abstract and part
// types carry <<Abstract>> / <<Part>> stereotype annotations. Edge labels
// reuse the DSL vocabulary — NAME plus parenthesized multiplicity (one,
// many, one:many, _) — rather than Mermaid cardinality notation, so the
// whole document speaks one vocabulary. Mermaid namespaces are deliberately
// not used (some Markdown renderers do not support them in class
// diagrams); an imported type's display renders as a sanitized class id
// with the display as its label instead. Two displays that sanitize to one id
// stay two classes: the later takes the id suffixed _2, _3, and so on. That
// labelled form needs Mermaid 10.1.0 or
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
// Each invariant renders its failure message as a bullet, written as a quoted
// Go string literal and escaped for Markdown — with a "— from <Owner>" marker
// when inherited —
// its doc-comment beneath, then the
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
// diffing. The document is a versioned surface; docs/VERSIONING.md states
// its tiers.
//
// Before returning, [Marshal] reads the document with a CommonMark parser, with
// GitHub's extensions, and verifies the structure it wrote: no block is left
// open at the end; every heading it wrote is a top-level heading of its level
// whose text reads as the text it meant, holding the anchor the parser's
// headings allocate it; outside doc comments, the internal links the parser
// reads are exactly the links the generator wrote, each to such a heading; and
// every table it wrote reads with its columns and rows. Its own
// fences are sized past any backtick run in their body. A failure there is a
// generator bug surfaced as an error, never emitted output.
//
// Doc-comment text is written as the Markdown its author wrote, so a link, a
// table or a heading inside it is the author's own and is not checked. A block
// a doc comment leaves open — a fenced code block, or an HTML block that a
// blank line does not end — is closed at the end of that comment's block with
// the line the parser reads as its end, so it cannot swallow the rest of the
// document. Every other text the schema supplies renders literally. A code cell — a property's name and
// Type, a DataType's name and Definition — shows its text byte for byte: a
// code span with each pipe escaped, or, for text holding a backtick, a
// backslash before a pipe or a line break, a <code> element whose
// Markdown-significant characters are entities. A description cell escapes
// backslashes and pipes and folds newlines to <br>. A schema name and an
// invariant message are escaped for Markdown, a control character in a schema
// name is written as its Go escape (\n), and a double quote in a Mermaid class
// label is written #quot;.
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
//   - [WithClassMembers]: include or omit the member lines inside each
//     diagram class (default true). With false, the classes keep their
//     stereotypes and edges, and the per-type tables still carry every
//     property.
//
// # Error Conditions
//
// [Marshal] returns an error, and no output, only when the emitted document
// fails the structural self-check: a block left open at the end, a heading the
// parser does not read as the generator wrote it or with the anchor the
// generator linked, internal links that do not read as the generator wrote them
// or name no heading, or a table that does not read with the columns and rows
// written. Each is a generator bug, so no schema is refused for its names
// or its doc-comment text.
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
//	adapter/markdown  ──imports──▶  schema, github.com/yuin/goldmark,
//	                                github.com/yuin/goldmark/ast,
//	                                github.com/yuin/goldmark/extension,
//	                                github.com/yuin/goldmark/extension/ast,
//	                                github.com/yuin/goldmark/text
//
// markdown imports public yammm packages, the standard library and goldmark,
// a CommonMark parser with GitHub's extensions and no dependencies of its own.
// No internal/* (the adapter-layer carve-out documented in adapter/doc.go
// stays gogen-only), no instance/graph, no diag, and no Mermaid tooling: the
// golden corpus plus the parser-backed self-check carry output verification.
package markdown
