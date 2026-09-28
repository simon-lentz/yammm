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
// Sections with nothing to say are omitted entirely, with two exceptions: an
// imported schema's heading is written even when the schema declares nothing,
// and the class diagram is written whenever it is on, even with no class.
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
// with a CommonMark parser, renders it to HTML, applies GitHub's tag filter
// and builds the tree an HTML5 parser builds. It allocates over every heading
// element of that tree, as GitHub anchors every h1 to h6 element it renders:
// the Markdown headings, those inside doc comments included, and each heading
// a doc comment writes as raw HTML, whose anchor is the slug of its text
// content, a heading nested in it included. So each link
// lands on the heading it names: two headings that slug alike each keep a
// working link, and a type whose heading slugs like a section heading — a
// type named Types — links to its own heading.
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
// diagrams). A class id is the display sanitized to ASCII letters, digits
// and underscores, with every "direction" in it written "direc_tion": Mermaid
// reads a line that ends in "direction" before a line that starts with a
// direction keyword as one direction statement. Two displays that sanitize to
// one id stay two classes: the later takes the id suffixed _2, _3, and so on.
// A class whose id differs from its display takes the display as its label.
// That labelled form needs Mermaid 10.1.0 or later. An imported type always
// takes it, and an entry type only when its name holds "direction", so an
// import-free schema without such a name renders on Mermaid 9. When the
// diagram holds a labelled class the document says so in one sentence under
// the "## Class Diagram" heading, before the fence.
//
// Relations render in the type sections as DSL-notation bullets under a bold
// label, with the target linked to its section:
//
//	**Associations**
//
//	-   `--> OWNER (one)` [Person](#person)
//
// An inherited relation carries a "— from <Owner>" marker naming its declaring
// ancestor, the same provenance the property table gives inherited rows. A
// relation's edge properties nest as a sub-table under its bullet, and its
// doc-comment as an indented block. A bullet is "-" and three spaces, so what
// nests under it starts at column 4, a tab stop: a doc comment there reads as
// it reads at column 0, a tab in it included.
//
// # Invariants
//
// Each invariant renders its failure message as a bullet, written as a quoted
// Go string literal and escaped for Markdown — with a "— from <Owner>" marker
// when inherited — its doc-comment beneath, then the declaration source
// ("! \"message\" expression") in a yammm code fence, extracted from the
// schema source via the invariant's span. The source is laid out as written:
// the first line starts at the declaration's "!", and each continuation line
// loses as much of the white space up to the declaration's column as it
// starts with and keeps the rest, a CR LF or
// a lone CR is a line end, and the comments the span starts with, the doc
// comment first, are stripped whatever they hold, since the doc renders apart. When no
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
// GitHub's extensions, and verifies the structure it wrote. No block is left
// open at the end. Every heading it wrote is a top-level heading of its level
// whose text reads as the text it meant, holding the anchor the document's
// heading elements allocate it. Outside doc comments, the links the parser
// reads are exactly the internal links the generator wrote, each to such a
// heading: no other link, autolink or image. Every table it wrote reads with
// its columns and rows. The class diagram reads as a fenced code block each of
// whose lines is one of the forms the emitter writes, a subset of Mermaid's
// class-diagram grammar and stricter than it, read with each entity code
// replaced as Mermaid's render replaces it. Its own fences are sized past any
// backtick run in their body. A failure there is a generator bug surfaced as
// an error, never emitted output.
//
// Doc-comment text is written as the Markdown its author wrote, so a link, a
// table or a heading inside it is the author's own and is not checked; that
// holds in a description cell too. Its line ends are written as LF, a CR LF
// or a lone CR each being a line end to CommonMark. A block a doc comment
// leaves open — a fenced code block, or an HTML block that a blank line does
// not end — is closed at the end of that comment's block with the line the
// parser reads as its end, and a raw-text element such as <textarea> with
// </pre>, which ends its HTML block and which GitHub's tag filter leaves
// alone, so it cannot swallow the rest of the document. Raw HTML a doc
// comment leaves open for an HTML parser — a comment, a CDATA section, a tag,
// a quoted attribute value, a noscript element's text — is closed after it by
// one line that reads as nothing from a closed state (<!-- -->, <![CDATA[ ]]>,
// <wbr x='"'> or </noscript>), kept only when an HTML5
// parser then reads an element written after the comment; a description cell
// takes its closer on the cell's line, after a space. A Markdown heading
// inside the comment that its own open HTML takes is no heading, as on GitHub.
// An element the comment leaves open, such as a <div>, is the author's, and
// the generator's text after it sits inside it.
//
// Every other text the schema supplies renders literally under GitHub-flavored
// Markdown. GitHub's emoji filter runs after it, and outside code it can still
// read a :shortcode:, as it did in a heading when measured. A code cell — a property's name and Type, a DataType's name and
// Definition — shows its text byte for byte: a code span with each pipe
// escaped, or, for text holding a backtick, a backslash before a pipe or a
// line break, a <code> element whose Markdown-significant characters are
// entities. A description cell holds the doc comment's Markdown on one line.
// A line break inside a code span folds to the space Markdown reads it as.
// One inside raw HTML or an HTML block folds to a space, which an HTML parser
// reads as white space, except in a pre or listing element's text, where it is
// a <br>, and the one right after that element's start tag, which an HTML
// parser drops, folds to nothing.
// Any other folds to <br> in place of the backslash that makes it a hard
// break. A pipe after an even run of backslashes gains one, so the row keeps
// its cells and the pipe renders as the doc comment's does. A
// schema name and an invariant message are escaped for Markdown, and a control
// character in a schema name is written as its Go escape (\n). GitHub reads a
// www host, a URL and an email address as a link even through an escape, so an
// empty HTML comment, which a browser does not show, splits each colon, each
// at sign and each dot after "www" in a schema name, an invariant message and
// a <code> cell.
//
// A Mermaid class label writes every character but an ASCII letter or digit,
// a dot, and an underscore between two ASCII letters or digits as an entity
// code (#32; for a space), which Mermaid decodes when it renders; Mermaid 11 reads a label as
// Markdown, and Mermaid's render rewrites the sequences it uses as entity
// placeholders. An edge label and a member line hold schema identifiers and
// the generator's words: each writes a double quote as #quot;, and the number
// sign, colon, semicolon, percent sign, <, > and & as #35;, #58;, #59;, #37;,
// #60;, #62; and #38;, so the multiplicity one:many labels an edge as
// one#58;many.
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
// [Marshal] returns an error, and no output, only when it cannot read back the
// document it emitted, or when that document fails the structural self-check
// Output Guarantees describes. Each is a generator bug: the escapes above keep every
// schema-supplied text in a form the check accepts, and doc-comment text is
// not checked, so no schema is refused for its names or its doc-comment text.
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
//	adapter/markdown  ──imports──▶  location, schema, github.com/yuin/goldmark,
//	                                github.com/yuin/goldmark/ast,
//	                                github.com/yuin/goldmark/extension,
//	                                github.com/yuin/goldmark/extension/ast,
//	                                github.com/yuin/goldmark/renderer,
//	                                github.com/yuin/goldmark/renderer/html,
//	                                github.com/yuin/goldmark/text,
//	                                github.com/yuin/goldmark/util,
//	                                golang.org/x/net/html
//
// markdown imports public yammm packages, the standard library, goldmark, a
// CommonMark parser with GitHub's extensions and no dependencies of its own,
// and golang.org/x/net/html, the Go team's HTML5 parser, to read the HTML a
// document renders to as a browser does.
// No internal/* (the adapter-layer carve-out documented in adapter/doc.go
// stays gogen-only), no instance/graph, no diag, and no Mermaid tooling: the
// golden corpus plus the parser-backed self-check carry output verification,
// and a test behind the mermaid build tag parses every golden's diagram with
// Mermaid itself.
package markdown
