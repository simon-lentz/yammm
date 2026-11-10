// Package markdown generates a Markdown reference document, including a
// Mermaid class diagram, from a yammm schema. Call [Marshal] with a loaded,
// resolved schema; it returns one deterministic, self-contained document
// covering the schema and its whole import closure — a canonical,
// regenerable documentation artifact in GitHub-flavored Markdown. Its internal
// links follow GitHub's anchors, and its diagram is a Mermaid fence, which a
// renderer without Mermaid shows as code.
//
// # Schema-In, Bytes-Out
//
// Like schema/gogen — and unlike adapter/neo4j —
// markdown has no instance-data path: it never parses, validates, or
// serializes instances, and it imports neither instance nor graph. It maps
// a completed schema to one Markdown document, nothing more, and returns a
// plain error rather than a [github.com/simon-lentz/yammm/diag.Result],
// because its only failures are
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
// Every heading GitHub anchors takes the anchor GitHub gives it, within the
// two limits this paragraph ends with: the heading's slug — its
// text lowercased, every character but letters and other alphabetic
// characters, marks, decimal digits, connector punctuation, hyphens and the
// two join controls U+200C and U+200D removed, spaces made hyphens — suffixed
// -1, -2, … when an earlier heading took it. GitHub anchors every h1 to h6
// element it renders that is not inside another, so a heading inside a doc
// comment takes an anchor too: a Markdown heading, and each heading a doc
// comment writes as raw HTML, whose anchor is the slug of its text content,
// the text of a heading nested in it included. A heading nested in another
// takes no anchor and no slug. When a doc comment holds what such a heading
// needs (an h1 to h6 tag or, where it is written as a block, a "#" or a line
// of "=" or "-" alone), the generator reads the emitted document with a
// CommonMark parser, renders it to HTML, applies GitHub's tag filter, builds
// the tree an HTML5 parser builds, and allocates over every heading element
// of that tree that is not inside another or below a select element that no
// keygen element in it closed first, which GitHub's parser loses. GitHub's
// parser reads a template's content apart: a heading in it is not lost, and a
// keygen in it closes no select outside it. It reads the document too when a
// doc comment written as a block leaves open HTML that no closer (below)
// closes. The generator's own headings take their anchors in their own order:
// one the tree holds at its place among the tree's headings, and one the tree
// loses after every heading the tree holds, so a link to it leads nowhere
// rather than to another heading. So each link lands on the heading it names,
// save one to a heading the tree loses, which leads nowhere: two headings that
// slug alike each keep a working link, and a type whose heading slugs like a
// section heading — a type named Types — links to its own heading. The two
// limits: the document is read with goldmark and golang.org/x/net/html, not
// with GitHub's cmark-gfm and HTML parser, so where they read a doc comment's
// text in two ways, a link can land on another heading; and x/net/html
// rejects HTML nested deeper than 512 elements, so a document a doc comment
// nests that deep keeps the anchors of the generator's headings alone, and a
// link can land on a doc comment's heading that slugs alike.
//
// # Tables Own Detail; the Diagram Owns Shape
//
// The class diagram deliberately keeps constraint detail out: class members
// are the property name plus a bare kind label (String, Enum, List, …; a
// named DataType shows its name). The per-type tables carry the detail: the Type column
// renders each constraint's DSL form (String[1, 100], Enum["a", "b"],
// List<FipsCode>) and the Modifiers column carries primary / required plus
// a "from <Owner>" marker on inherited rows — the property table is
// flattened over the full inheritance chain so a type's complete shape
// reads in one place, with provenance preserved. The generator writes a
// diagram of any length and splits none. Mermaid's default maxTextSize is
// 50,000 characters, and Mermaid shows its size message in place of a longer
// diagram. When the diagram is still too large, [WithClassMembers] with false
// drops the member lines and keeps every class, stereotype and edge.
//
// The diagram draws each type's OWN members and relation edges only;
// inheritance edges (Parent <|-- Child) convey the rest. An abstract type
// carries an <<Abstract>> stereotype annotation, and a part type that is not
// abstract a <<Part>> one. Edge labels
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
// That labelled form needs Mermaid 10.1.0 or later, the oldest release the oracle
// pins; 9.4.3 and 10.0.0 refuse it. An imported type always
// takes it, and an entry type only when its id differs from its name, so an
// import-free schema in which no id differs renders on Mermaid 9. When the
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
// comment first, are stripped whatever they hold, since the doc renders apart.
//
// # Output Guarantees
//
// Output is deterministic: the same schema, the same options, the same yammm
// version and the same build — the Go release that builds the generator and
// the module versions the build selects — give the same bytes, across runs
// and checkouts (no absolute paths, all walks ordered), so generated
// documents can be committed and drift-checked in CI by regenerating and
// diffing. The document is a versioned surface; docs/API.md's Versioning
// Policy states its tiers.
//
// Before returning, [Marshal] verifies the structure it wrote. It reads the
// document with each doc comment replaced, a block by a one-word paragraph at
// the block's column and a description cell by an empty cell, with a
// CommonMark parser with GitHub's extensions, so the verdict is about the
// generator's own text and no doc comment decides it. No block is left open at
// the end. The headings read are the ones it wrote and no other, each a
// top-level heading of its level whose text reads as the text it meant and
// whose anchor is its slug or a numbered form of it, held by no other heading.
// The links read are exactly the internal links the generator wrote, each to
// such a heading: no other link, autolink or image, and no raw HTML but the
// tags the generator writes (<code>, </code>, <br>, the empty comment, and
// <span> and </span> round a dollar). Every table it wrote reads with its
// columns and rows. The class diagram reads as a fenced code block each of
// whose lines is one of the forms the emitter writes, a subset of Mermaid's
// class-diagram grammar and stricter than it, read with each entity code
// replaced as Mermaid's render replaces it. Its own fences are sized past any
// backtick run in their body. A failure there is a generator bug surfaced as
// an error, never emitted output.
//
// Doc-comment text is written as the Markdown its author wrote, so a link, a
// table or a heading inside it is the author's own and is not checked. A
// description cell holds it as inline Markdown (below) and is checked only
// for a line break, which the fold never writes. The text is the schema's documentation,
// whose continuation lines lost the indentation they all share, byte for byte,
// when the parser read the comment, so layout
// that indents every continuation line alike makes no indented code block;
// lines indented by different bytes, such as a tab on one and spaces on
// another, share none and keep theirs. Its line ends are written as LF, a CR LF or a lone CR each being a
// line end to CommonMark. A block a doc comment
// leaves open — a fenced code block, or an HTML block that a blank line does
// not end — is closed at the end of that comment's block with the line the
// parser reads as its end, and a raw-text element such as <textarea> with
// </pre>, which ends its HTML block and which GitHub's tag filter leaves
// alone, so it cannot swallow the rest of the document. An HTML block that
// opens with <!-- or <![CDATA[ is closed this way, by --> or ]]>. In a
// paragraph a comment, a tag or an attribute value left open is text. Raw HTML
// a doc comment leaves open for an HTML parser inside an HTML block — a
// comment, a CDATA section, a tag, a quoted attribute value or a select
// element, as in <div><!-- x — is closed after it by one line that reads as
// nothing from a closed state (<!-- -->, <![CDATA[ ]]>, <wbr x='"'> or
// </select>), kept only when an HTML5 parser, reading the rendered HTML with
// GitHub's tag filter applied and scripting off, as GitHub parses it, and
// counting an element under a select element that no keygen element closed
// first as lost, as GitHub's parser loses it (a template's content read
// apart), then reads an element written after the comment. The closer is a
// courtesy and never a condition: a comment that none of the four lines closes
// is written as it is, and [Marshal] refuses nothing for it. The parser is
// golang.org/x/net/html. A description cell takes no closer: in a cell's
// inline Markdown a comment, a tag or an attribute value left open is text. A
// Markdown heading inside the comment that its own open HTML takes is no heading, as on GitHub.
// An element the comment leaves open, such as a <div>, is the author's, and
// an HTML parser can place the generator's text after it inside it.
//
// Every other text the schema supplies renders literally under GitHub-flavored
// Markdown. Outside a code span, the empty comment that splits each colon
// (below) splits a :shortcode: too. A code cell — a property's name and Type, a
// DataType's name and Definition — shows its text byte for byte: a code span
// with each pipe escaped, or, for text holding a backtick, a backslash before a
// pipe or a line break, a <code> element whose Markdown-significant characters
// are entities, each pipe written \| and each line break <br>. A description cell holds the doc comment's text on one line,
// and GitHub reads a cell as inline Markdown. No block of the comment is a
// block there: a heading, a list, a quote or a table shows as its characters,
// a fence of backticks reads as a code span, and Markdown between the tags of
// an HTML block is live. Text of more than one line folds to one line of
// inline text, read as cmark-gfm, GitHub's parser, reads it; text of one line
// is written as it is, save the pipe rule below. A line break inside a code span or inside a tag
// (a comment, a declaration, a processing instruction and a CDATA section are
// tags here) folds to a space, and the white space that starts the next line
// goes. So does one in an inline link's "(destination title)" tail, save one
// inside the destination or the title, whose value holds it: that one is
// written as the character reference &#10;. In a pre or listing element's
// text it is a <br> and the line is kept whole, save the one right after that
// element's start tag, which an HTML parser drops and which folds to nothing.
// The fold tracks pre and listing elements alone: the end tag of an element
// that holds one, such as </div>, leaves it open, and an end tag that an
// element such as <object> hides from GitHub's parser closes it all the same.
// Any other line break folds to <br>, the white space around it dropped. A
// <br> is punctuation where the line break was white space, so a space stays
// beside it after a "*", "_" or "~" no backslash escapes and, outside pre and
// listing text, before a line that starts with one or with "www.". A www link
// or a URL that GitHub's autolink extension reads where no link's text is open
// is written as it is, its brackets and a backslash that ends it included. In
// the fold, the "(" after a link's text whose tail does not form is written
// "\(", so the cell reads no tail there either. An unescaped backslash that
// ends a line before a line of text is the hard break it writes and gives way
// to the <br>; one that ends a line before a blank line, one that white space
// follows at the line's end, and one inside a pre or listing element's text
// are doubled before the <br>, so each shows as one backslash. In the fold,
// the "<" of a declaration that nothing ends (<!DOCTYPE html with no ">" after
// it) is written "\<", so the ">" of a <br> does not end it. A pipe after an even run of backslashes
// gains one, so the row keeps its cells; the table then drops one backslash
// before a pipe, inside a code span too, so a code span that holds a
// backslash before a pipe shows the pipe alone. A
// schema name, an invariant message, the name or import tag the entry gives a
// type, and a type reference are escaped for Markdown, and a control
// character in a schema name is written as its Go escape (\n). GitHub reads a
// www host, a URL and an email address as a link even through an escape, so an
// empty HTML comment, which a browser does not show, splits each colon, each
// at sign and each dot that directly follows "www", in any case, in text
// escaped this way and in a <code> cell. GitHub reads a pair of dollars
// outside code as math, and a backslash does not stop it, so each dollar in
// text escaped this way is written inside a <span>. A <code> cell keeps its
// dollars: GitHub reads no math inside a <code> element.
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
// [github.com/simon-lentz/yammm/schema.LoadString], or
// [github.com/simon-lentz/yammm/schema.LoadSourcesWithEntry].
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
// [Marshal] returns an error, and no output, only when the text it wrote fails
// the structural self-check Output Guarantees describes. That is a generator
// bug: the escapes above keep every schema-supplied text in a form the check
// accepts, and the check reads no doc comment's Markdown: of a description
// cell it reads only that the cell holds no line break, which the fold never
// writes. So no schema is refused for its names or its doc-comment text.
//
// # Thread Safety
//
// [Marshal] is safe for concurrent use. It allocates fresh state per call
// and shares no mutable package state.
//
// # Annotations
//
// Schema annotations do not appear in the emitted document — not in the
// property tables, not in the class diagram — so adding one changes no output,
// save where its text stands before an invariant on the invariant's source
// line: the invariant's fence dedents its continuation lines by the column the
// invariant starts at. The generated reference describes
// the data model; where a schema's annotations land as store DDL is
// [github.com/simon-lentz/yammm/adapter/neo4j]'s output, and `yammm neo4j
// indexes` renders it.
//
// They do reach the generator indirectly: a property inherited from several
// direct supertypes carries the union of their merged views' annotations. When
// that union differs from the surviving property's own set, the merged view a
// property table iterates yields a synthesized copy rather than the declared
// property. The own-versus-inherited lookups therefore key through
// [github.com/simon-lentz/yammm/schema.Property.Origin]; without it the owner
// lookup misses such a row, which would read "from " with no owner where
// "from base.A" is meant.
//
// # Dependencies
//
//	schema/markdown  ──imports──▶  location, schema, github.com/yuin/goldmark,
//	                               github.com/yuin/goldmark/ast,
//	                               github.com/yuin/goldmark/extension,
//	                               github.com/yuin/goldmark/extension/ast,
//	                               github.com/yuin/goldmark/renderer,
//	                               github.com/yuin/goldmark/renderer/html,
//	                               github.com/yuin/goldmark/text,
//	                               github.com/yuin/goldmark/util,
//	                               golang.org/x/net/html
//
// markdown imports public yammm packages, the standard library, goldmark, a
// CommonMark parser with GitHub's extensions and no dependencies of its own,
// and golang.org/x/net/html, the Go team's HTML5 parser, to read the HTML a
// doc comment renders to, for its closer, and the HTML the document renders
// to, for the anchors of its headings, and to decode the entities in the text
// of a heading the self-check reads.
// No internal/*, no instance/graph, no diag, and no Mermaid tooling: the
// golden corpus plus the parser-backed self-check carry output verification,
// and a test behind the mermaid build tag parses every golden's diagram with
// Mermaid itself.
package markdown
