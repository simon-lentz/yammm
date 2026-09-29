package markdown

import (
	"bytes"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"golang.org/x/net/html"
)

// multiplicity renders the DSL multiplicity vocabulary for a relation's
// forward direction: required-single "one", required-many "one:many",
// optional-many "many", optional-single "_" (also the omitted-multiplicity
// default). The accepted long spellings ("_:one", "one:one") normalize to
// the same four canonical short forms.
func multiplicity(optional, many bool) string {
	switch {
	case optional && many:
		return "many"
	case many:
		return "one:many"
	case optional:
		return "_"
	default:
		return "one"
	}
}

// slug derives a heading's anchor as GitHub does: the text lowercased, every
// character outside \p{Word}, the hyphen and the space removed, and each space
// made a hyphen. \p{Word} here is letters and every other alphabetic
// character, marks, decimal digits and connector punctuation, read from the Go
// release's Unicode tables; the join controls are removed, as GitHub removes
// them. Lowercasing maps U+0130 to "i" and a combining dot, as GitHub's full
// case mapping does.
func slug(heading string) string {
	var b strings.Builder
	b.Grow(len(heading))
	for _, r := range strings.ToLower(strings.ReplaceAll(heading, "\u0130", "i\u0307")) {
		switch {
		case r == ' ':
			b.WriteByte('-')
		case r == '-' || isWordRune(r):
			b.WriteRune(r)
		}
	}
	return b.String()
}

// isWordRune reports whether r is a character GitHub keeps in an anchor.
func isWordRune(r rune) bool {
	return unicode.In(r, unicode.L, unicode.M, unicode.Nd, unicode.Nl, unicode.Pc, unicode.Other_Alphabetic)
}

// descriptionCell writes a doc comment into a table cell, changing only what
// the table layer reads: its lines fold into one (foldLines), and a pipe after
// an even backslash run gains one backslash, so the row keeps its cells and
// the pipe renders as in the doc comment.
func descriptionCell(md goldmark.Markdown, s string) string {
	if s = lineEndings.Replace(s); strings.Contains(s, "\n") {
		s = foldLines(md, s)
	}
	var b strings.Builder
	run := 0
	for i := range len(s) {
		c := s[i]
		if c == '|' && run%2 == 0 {
			b.WriteByte('\\')
		}
		if c == '\\' {
			run++
		} else {
			run = 0
		}
		b.WriteByte(c)
	}
	return b.String()
}

// foldLines joins a doc comment's lines into one cell line. A line break
// inside a code span, raw HTML or an HTML block folds to a space, which a code
// span and an HTML parser read it as. In a pre or listing element's text it is
// a <br>, the line kept whole, save the one an HTML parser drops after the
// element's start tag, which folds to nothing. Any other folds to <br>, the
// white space around it dropped, and so does the backslash that made it a
// hard break.
func foldLines(md goldmark.Markdown, s string) string {
	spans, kept, dropped := inlineSpans(md, s)
	inside := func(pos int) bool {
		return slices.ContainsFunc(spans, func(sp span) bool { return sp.start <= pos && pos < sp.end })
	}
	var out string
	at := 0
	for i, line := range strings.Split(s, "\n") {
		switch {
		case i == 0:
			out = line
		case slices.Contains(kept, at-1):
			out += "<br>" + line
		case slices.Contains(dropped, at-1):
			out += line
		case inside(at - 1):
			out += " " + strings.TrimLeft(line, " \t")
		default:
			out = withoutHardBreak(strings.TrimRight(out, " \t")) + "<br>" + strings.TrimLeft(line, " \t")
		}
		at += len(line) + 1
	}
	return strings.TrimSpace(out)
}

// inlineSpans returns the byte ranges of s that Markdown reads as a code span,
// raw HTML or an HTML block, and, from preformattedBreaks, the line breaks an
// HTML block holds in pre or listing text and the ones a parser drops there.
func inlineSpans(md goldmark.Markdown, s string) (spans []span, kept, dropped []int) {
	if err := ast.Walk(parse(md, []byte(s)), func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n := n.(type) {
		case *ast.CodeSpan:
			if first, ok := n.FirstChild().(*ast.Text); ok {
				if last, ok := n.LastChild().(*ast.Text); ok {
					spans = append(spans, span{start: first.Segment.Start, end: last.Segment.Stop})
				}
			}
		case *ast.RawHTML:
			if l := n.Segments.Len(); l > 0 {
				spans = append(spans, span{start: n.Segments.At(0).Start, end: n.Segments.At(l - 1).Stop})
			}
		case *ast.HTMLBlock:
			if l := n.Lines().Len(); l > 0 {
				sp := span{start: n.Lines().At(0).Start, end: n.Lines().At(l - 1).Stop}
				spans = append(spans, sp)
				k, d := preformattedBreaks(s[sp.start:sp.end], sp.start)
				kept, dropped = append(kept, k...), append(dropped, d...)
			}
		}
		return ast.WalkContinue, nil
	}); err != nil {
		return nil, nil, nil
	}
	return spans, kept, dropped
}

// preformattedBreaks returns where block, which starts at base, holds a line
// break in a pre or listing element's text, and where it holds the one line
// break right after such an element's start tag, which an HTML parser drops.
func preformattedBreaks(block string, base int) (kept, dropped []int) {
	z := html.NewTokenizer(strings.NewReader(block))
	depth, off, afterStart := 0, 0, false
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			return kept, dropped
		}
		raw := string(z.Raw())
		opens := false
		switch tt {
		case html.TextToken:
			for i := range len(raw) {
				switch {
				case raw[i] != '\n' || depth == 0:
				case afterStart && i == 0:
					dropped = append(dropped, base+off+i)
				default:
					kept = append(kept, base+off+i)
				}
			}
		case html.StartTagToken, html.EndTagToken:
			if name, _ := z.TagName(); string(name) == "pre" || string(name) == "listing" {
				if tt == html.StartTagToken {
					depth++
					opens = true
				} else {
					depth = max(depth-1, 0)
				}
			}
		}
		afterStart = opens
		off += len(raw)
	}
}

// withoutHardBreak drops the backslash that ends s when it ends an odd run:
// Markdown reads that backslash before a line break as the break itself.
func withoutHardBreak(s string) string {
	trimmed := strings.TrimRight(s, "\\")
	if (len(s)-len(trimmed))%2 == 1 {
		return s[:len(s)-1]
	}
	return s
}

// lineEndings makes every CR LF and every lone CR an LF. CommonMark ends a
// line at each, and the parser the self-check reads with ends one at LF alone.
var lineEndings = strings.NewReplacer("\r\n", "\n", "\r", "\n")

// codeCell renders s as code inside a Markdown table cell, byte for byte.
// A code span processes no backslash escape, so its content is written as is
// with one exception: the table layer splits on every pipe a backslash does
// not precede and then drops that backslash, so each pipe is written \|.
// Three inputs a code span cannot carry exactly — a backtick, a backslash
// before a pipe, a line break — take a <code> element instead, whose content
// Markdown does parse, so codeTagText neutralizes every character that
// parse or the table layer reads. Empty input yields an empty cell, since a
// code span with no content is not valid Markdown.
func codeCell(s string) string {
	if s == "" {
		return ""
	}
	if !strings.ContainsAny(s, "`\r\n") && !strings.Contains(s, `\|`) {
		return "`" + strings.ReplaceAll(s, "|", `\|`) + "`"
	}
	return "<code>" + codeTagText(s) + "</code>"
}

// codeTagText makes text literal inside a raw HTML <code> element in a table
// cell: HTML metacharacters and inline syntax become entities, a pipe the
// table layer's \|, a line break <br>, and a linkBreak stops each autolink.
func codeTagText(s string) string {
	rs := []rune(lineEndings.Replace(s))
	var b strings.Builder
	for i, r := range rs {
		if breaksLinkAt(rs, i) {
			b.WriteString(linkBreak)
		}
		if e, ok := codeTagEntities[r]; ok {
			b.WriteString(e)
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

var codeTagEntities = map[rune]string{
	'&':  "&amp;",
	'<':  "&lt;",
	'>':  "&gt;",
	'`':  "&#96;",
	'\\': "&#92;",
	'*':  "&#42;",
	'_':  "&#95;",
	'[':  "&#91;",
	']':  "&#93;",
	'~':  "&#126;",
	'!':  "&#33;",
	'|':  `\|`,
	'\n': "<br>",
}

// linkBreak is an empty HTML comment. Inside a word it ends the text GitHub's
// autolink extension scans, and a browser shows nothing for it.
const linkBreak = "<!---->"

// breaksLinkAt reports whether a linkBreak goes before rs[i], where GitHub
// would read an autolink: a colon ends a URL scheme, an at sign joins an email
// address, and a dot after "www", in any case, starts a www link.
func breaksLinkAt(rs []rune, i int) bool {
	switch rs[i] {
	case ':', '@':
		return true
	case '.':
		return i >= 3 && strings.EqualFold(string(rs[i-3:i]), "www")
	}
	return false
}

// escapeInline backslash-escapes the ASCII punctuation Markdown reads as
// inline syntax, a table cell's pipe included, and writes a linkBreak where
// GitHub would read an autolink, so text such as a schema name renders
// literally in a heading, a link or a table cell. An underscore between two
// letters or digits opens no emphasis in GFM and is left as is.
func escapeInline(s string) string {
	rs := []rune(s)
	var b strings.Builder
	for i, r := range rs {
		if breaksLinkAt(rs, i) {
			b.WriteString(linkBreak)
		}
		escape := strings.ContainsRune("\\`*[]<>&~|!#", r)
		if r == '_' {
			escape = i == 0 || i == len(rs)-1 || !isAlnum(rs[i-1]) || !isAlnum(rs[i+1])
		}
		if escape {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

func isAlnum(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }

// keepTrailingSpaces writes a heading's trailing spaces as character
// references. An ATX heading drops trailing spaces from its text, so a schema
// name that ends in one would otherwise not read as written.
func keepTrailingSpaces(md string) string {
	trimmed := strings.TrimRight(md, " ")
	return trimmed + strings.Repeat("&#32;", len(md)-len(trimmed))
}

// printableName writes each control character of a schema name as its Go
// escape (\n, \t, \x00), so a name holding a line break stays on one line in
// a heading, a link, a table cell or a diagram label. The escape is text, so
// the heading's anchor and the rendered heading slug alike.
func printableName(s string) string {
	if !strings.ContainsFunc(s, unicode.IsControl) {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		if unicode.IsControl(r) {
			q := strconv.QuoteRune(r)
			b.WriteString(q[1 : len(q)-1])
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// mermaidID sanitizes a display name into a Mermaid class identifier: every
// character outside ASCII letters, digits and underscores (notably the dots in
// qualified type names) becomes an underscore. Mermaid's class-diagram lexer
// reads an id as \w+, which is ASCII, and its table of other letters is partial,
// so a non-ASCII letter or digit can fail to lex. Every "direction" becomes
// "direc_tion": Mermaid reads a line ending in "direction", white space and a
// direction keyword starting the next line as one direction statement, so an
// id ending in "direction" would swallow the lines around it. uniqueMermaidID
// makes the result unique. When the id differs from the display name, the
// diagram emits the display-label form `class id["display"]` so the rendered
// name keeps its original spelling.
func mermaidID(name string) string {
	id := strings.Map(func(r rune) rune {
		if r == '_' || isASCIIAlnum(r) {
			return r
		}
		return '_'
	}, name)
	return strings.ReplaceAll(id, "direction", "direc_tion")
}

func isASCIIAlnum(r rune) bool {
	return 'a' <= r && r <= 'z' || 'A' <= r && r <= 'Z' || '0' <= r && r <= '9'
}

// mermaidLabel writes a class label with every character an entity code but
// an ASCII letter or digit, a dot, and an underscore between two ASCII letters
// or digits. Mermaid decodes each code when it renders, and Mermaid 11 reads a
// label as Markdown, so no other character reaches a label as itself.
func mermaidLabel(s string) string {
	rs := []rune(s)
	var b strings.Builder
	for i, r := range rs {
		keep := isASCIIAlnum(r) || r == '.'
		if r == '_' {
			keep = i > 0 && i < len(rs)-1 && isASCIIAlnum(rs[i-1]) && isASCIIAlnum(rs[i+1])
		}
		if keep {
			b.WriteRune(r)
			continue
		}
		b.WriteString("#" + strconv.Itoa(int(r)) + ";")
	}
	return b.String()
}

// mermaidChars writes each character Mermaid reads as syntax in an edge label
// or a member line as an entity code, which Mermaid decodes when it renders:
// the quote, the colon and semicolon that end an edge label, the percent sign
// that starts a comment or a directive, the number sign that starts an entity
// code, and the characters a rendered label reads as HTML. Both hold schema
// identifiers and the generator's own words, which carry no other syntax.
func mermaidChars(s string) string {
	return mermaidTextEscaper.Replace(s)
}

var mermaidTextEscaper = strings.NewReplacer(
	`"`, "#quot;",
	"#", "#35;",
	":", "#58;",
	";", "#59;",
	"%", "#37;",
	"<", "#60;",
	">", "#62;",
	"&", "#38;",
)

// bulletMarker starts a list item whose content sits at column 4, a tab stop,
// so a doc comment written there reads as at column 0, where closeAuthorText
// judges it, and a tab in it never lands partly inside the indent.
const bulletMarker = "-   "

// bulletIndent is the continuation indent of a bulletMarker item: text
// indented by it sits inside the item.
const bulletIndent = len(bulletMarker)

// indentUnderBullet indents every non-empty line of s by the list-item
// continuation indent. Blank lines stay blank so nested blocks introduce no
// trailing whitespace.
func indentUnderBullet(s string) string {
	prefix := strings.Repeat(" ", bulletIndent)
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if line != "" {
			lines[i] = prefix + line
		}
	}
	return strings.Join(lines, "\n")
}

// emitTypeSection writes one type's reference section: heading, badges
// line, documentation, flattened property table, association and
// composition lists, and invariants. Blocks are separated by single blank
// lines; empty blocks are omitted entirely. The blocks are written straight
// to the document so each doc comment's place in it is recorded.
func (g *generator) emitTypeSection(t *schema.Type) {
	e := g.types[t.ID()]

	g.buf.WriteString("### " + e.displayMD + "\n")
	block := func() { g.buf.WriteString("\n") }
	if b := g.typeBadges(t); b != "" {
		block()
		g.buf.WriteString(b + "\n")
	}
	if doc := t.Documentation(); doc != "" {
		block()
		g.writeAuthorText(doc, false)
		g.buf.WriteString("\n")
	}
	if props := t.AllPropertiesSlice(); len(props) > 0 {
		block()
		g.writePropertyTable(t, props)
	}
	// Associations and compositions share one relation namespace, so one
	// resolver marks inherited relations of either kind.
	relFrom := inheritedFrom(g, t, func(st *schema.Type) []*schema.Relation {
		return slices.Concat(st.AssociationsSlice(), st.CompositionsSlice())
	})
	for _, list := range []struct {
		label string
		rels  []*schema.Relation
	}{
		{"**Associations**", t.AllAssociationsSlice()},
		{"**Compositions**", t.AllCompositionsSlice()},
	} {
		if len(list.rels) > 0 {
			block()
			g.writeRelationList(list.label, list.rels, relFrom)
		}
	}
	if invs := t.AllInvariantsSlice(); len(invs) > 0 {
		block()
		g.writeInvariantList(t, invs)
	}
}

// inheritedOwners maps every member a type inherits — keyed by pointer — to the
// Markdown display name of the ancestor that declares it, for the "from <Owner>"
// provenance marker. own extracts a type's own members of the relevant kind;
// members the type declares itself belong to no ancestor and are absent from
// the result. Merge only pulls inherited members from resolved, closure-present
// ancestors, so every inherited member resolves to a display name here, and
// each member has one declaring ancestor, which the linearization lists once.
func inheritedOwners[T comparable](g *generator, t *schema.Type, own func(*schema.Type) []T) map[T]string {
	owners := make(map[T]string)
	for _, super := range t.SuperTypesSlice() {
		se, ok := g.types[super.ID()]
		if !ok {
			continue
		}
		for _, m := range own(se.typ) {
			owners[m] = se.displayMD
		}
	}
	return owners
}

// inheritedFrom builds the provenance-suffix resolver for one kind of type
// member: it returns " — from <Owner>" for a member the type inherits, naming
// the declaring ancestor by its Markdown display name (displayMD), and "" for a
// member the type declares itself. This gives
// relation bullets and invariant bullets the same provenance the property
// table's "from <Owner>" modifier gives inherited rows.
func inheritedFrom[T comparable](g *generator, t *schema.Type, own func(*schema.Type) []T) func(T) string {
	owners := inheritedOwners(g, t, own)
	return func(m T) string {
		if owner, ok := owners[m]; ok {
			return " — from " + owner
		}
		return ""
	}
}

// typeBadges renders the badges paragraph: the abstract or part marker and
// the Extends links resolved from the declared inherits clauses. Returns
// "" when the type has none.
func (g *generator) typeBadges(t *schema.Type) string {
	var parts []string
	if t.IsAbstract() {
		parts = append(parts, "*Abstract type* — no direct instances.")
	}
	if t.IsPart() {
		parts = append(parts, "*Part type* — instances exist only as composed children.")
	}
	if inherits := t.InheritsSlice(); len(inherits) > 0 {
		links := make([]string, len(inherits))
		for i, ref := range inherits {
			links[i] = g.superLink(t, ref)
		}
		parts = append(parts, "Extends: "+strings.Join(links, ", ")+".")
	}
	return strings.Join(parts, " ")
}

// superLink resolves a declared extends reference to a link on the parent
// type's section, falling back to the reference's own spelling, escaped as
// schema text, when the parent is absent from this document's type map.
func (g *generator) superLink(t *schema.Type, ref schema.TypeRef) string {
	if e, ok := g.resolveSuper(t, ref); ok {
		return g.link(e)
	}
	return escapeInline(ref.String())
}

// writePropertyTable writes the flattened property table over all, the type's
// full property set, own rows first. Inherited rows carry a "from <Owner>"
// modifier naming the declaring ancestor as displayed in this document.
func (g *generator) writePropertyTable(t *schema.Type, all []*schema.Property) {
	// One PropertiesSlice call: it clones the type's property slice, so calling
	// it again just to size the map allocates and discards a whole slice per
	// rendered type.
	ownProps := t.PropertiesSlice()
	own := make(map[*schema.Property]bool, len(ownProps))
	for _, p := range ownProps {
		own[p] = true
	}
	// Inherited rows reuse the declaring ancestor's own *Property values, so
	// the pointer map keys each row to its declarer. A row whose annotations were
	// merged across ancestors is a synthesized copy absent from every own slice,
	// so both lookups go through Origin() to reach the declared property.
	ownerOf := inheritedOwners(g, t, (*schema.Type).PropertiesSlice)

	tw := g.newTable(0, "Property", "Type", "Modifiers", "Description")
	for _, p := range all {
		var mods []string
		switch {
		case p.IsPrimaryKey():
			mods = append(mods, "primary")
		case p.IsRequired():
			mods = append(mods, "required")
		}
		if !own[p.Origin()] {
			mods = append(mods, "from "+ownerOf[p.Origin()])
		}
		writePropertyRow(tw, p, strings.Join(mods, ", "))
	}
}

// writeEdgePropertyTable writes the sub-table for a relation's edge properties
// under its bullet, with the same columns as the type property table.
func (g *generator) writeEdgePropertyTable(props []*schema.Property) {
	tw := g.newTable(bulletIndent, "Property", "Type", "Modifiers", "Description")
	for _, p := range props {
		mods := ""
		if p.IsRequired() {
			mods = "required"
		}
		writePropertyRow(tw, p, mods)
	}
}

// writePropertyRow writes one property-table row; the Type cell renders
// the constraint's DSL form (named DataTypes display their name). mods is
// Markdown already, its owner names escaped by escapeInline.
func writePropertyRow(tw *tableWriter, p *schema.Property, mods string) {
	tw.row(codeCellOf(p.Name()), codeCellOf(p.Constraint().String()), tableCell{md: mods}, tw.g.descriptionCellOf(p.Documentation()))
}

// writeRelationList writes a labeled bullet list of relations in DSL notation
// with linked targets. An inherited relation carries a " — from <Owner>"
// marker via origin, mirroring the property table. A relation's documentation
// and edge-property sub-table nest under its bullet.
func (g *generator) writeRelationList(label string, rels []*schema.Relation, origin func(*schema.Relation) string) {
	g.buf.WriteString(label + "\n")
	for _, rel := range rels {
		g.buf.WriteString("\n")
		arrow := "-->"
		if rel.IsComposition() {
			arrow = "*->"
		}
		mult := multiplicity(rel.IsOptional(), rel.IsMany())
		g.buf.WriteString(bulletMarker + "`" + arrow + " " + rel.Name() + " (" + mult + ")` " + g.relationTarget(rel) + origin(rel) + "\n")
		if doc := rel.Documentation(); doc != "" {
			g.buf.WriteString("\n")
			g.writeAuthorText(doc, true)
			g.buf.WriteString("\n")
		}
		if props := rel.PropertiesSlice(); len(props) > 0 {
			g.buf.WriteString("\n")
			g.writeEdgePropertyTable(props)
		}
	}
}

// relationTarget links the relation's target type; when the target is absent
// from this document's type map it degrades to the reference's own spelling,
// unlinked and escaped as schema text.
func (g *generator) relationTarget(rel *schema.Relation) string {
	if e, ok := g.types[rel.TargetID()]; ok {
		return g.link(e)
	}
	return escapeInline(rel.Target().String())
}

// writeInvariantList writes the type's invariants: the failure message as the
// bullet (an inherited invariant carries a " — from <Owner>" marker, mirroring
// the property table), the documentation indented beneath it, then the
// declaration source in a yammm fence when the span and source content allow
// extraction.
func (g *generator) writeInvariantList(t *schema.Type, invs []*schema.Invariant) {
	from := inheritedFrom(g, t, (*schema.Type).InvariantsSlice)
	g.buf.WriteString("**Invariants**\n")
	for _, inv := range invs {
		g.buf.WriteString("\n" + bulletMarker + escapeInline(strconv.Quote(inv.Name())) + from(inv) + "\n")
		if doc := inv.Documentation(); doc != "" {
			g.buf.WriteString("\n")
			g.writeAuthorText(doc, true)
			g.buf.WriteString("\n")
		}
		if src, ok := g.invariantSource(inv); ok {
			var fence bytes.Buffer
			writeFence(&fence, "yammm", src)
			g.buf.WriteString("\n" + indentUnderBullet(fence.String()))
		}
	}
}

// invariantSource extracts the invariant's declaration text from its source,
// and returns ok=false — degrading the invariant to message-only — when byte
// offsets are unknown or the schema carries no source content (a
// Builder-built schema). The comments the span starts with — the doc comment,
// which renders separately, and any other before the "!" — are stripped,
// whatever they hold, so the fence starts at the declaration.
func (g *generator) invariantSource(inv *schema.Invariant) (string, bool) {
	span := inv.Span()
	if g.sources == nil || !span.Start.HasByte() || !span.End.HasByte() {
		return "", false
	}
	content, ok := g.sourceContent(span.Source)
	if !ok {
		return "", false
	}
	start, end := span.Start.Byte, span.End.Byte
	if start < 0 || end > len(content) || start >= end {
		return "", false
	}
	text := withoutLeadingComments(string(content[start:end]))
	at := end - len(text)
	text = strings.TrimRight(dedent(lineEndings.Replace(text), lineIndent(content, at)), " \t\n")
	return text, text != ""
}

// withoutLeadingComments returns text from its first character that is
// neither white space nor inside a comment; when a comment does not end, it
// returns text from that comment on.
func withoutLeadingComments(text string) string {
	for {
		text = strings.TrimLeft(text, " \t\r\n")
		if rest, ok := strings.CutPrefix(text, "/*"); ok {
			_, after, closed := strings.Cut(rest, "*/")
			if !closed {
				return text
			}
			text = after
			continue
		}
		if rest, ok := strings.CutPrefix(text, "//"); ok {
			i := strings.IndexAny(rest, "\r\n")
			if i < 0 {
				return text
			}
			text = rest[i:]
			continue
		}
		return text
	}
}

// lineIndent returns the source line holding pos up to pos, with every
// character but a tab written as a space: the indent that holds the
// declaration's column, whatever precedes it on its line.
func lineIndent(content []byte, pos int) string {
	begin := bytes.LastIndexAny(content[:pos], "\r\n") + 1
	return strings.Map(func(r rune) rune {
		if r == '\t' {
			return r
		}
		return ' '
	}, string(content[begin:pos]))
}

// sourceContent returns a source's content, read from the schema once per
// source: each read copies the whole source, and a source holds many
// invariants.
func (g *generator) sourceContent(id location.SourceID) ([]byte, bool) {
	if content, ok := g.contents[id]; ok {
		return content, true
	}
	content, ok := g.sources.ContentBySource(id)
	if ok {
		g.contents[id] = content
	}
	return content, ok
}

// dedent removes from each continuation line the part of indent, the
// declaration line's own indentation, that the line starts with. The first
// line starts at the declaration's token, so the fence shows the declaration
// laid out as in its source.
func dedent(s, indent string) string {
	if indent == "" {
		return s
	}
	lines := strings.Split(s, "\n")
	for i, line := range lines[1:] {
		lead := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
		lines[i+1] = line[len(commonPrefix(indent, lead)):]
	}
	return strings.Join(lines, "\n")
}

// commonPrefix returns the longest common prefix of a and b.
func commonPrefix(a, b string) string {
	n := min(len(a), len(b))
	for i := range n {
		if a[i] != b[i] {
			return a[:i]
		}
	}
	return a[:n]
}

// writeFence writes a fenced code block, sizing the fence one backtick
// longer than the longest backtick run in body (minimum three) so embedded
// backtick runs cannot terminate the block early. The body always ends
// with exactly one trailing newline before the closing fence.
func writeFence(b *bytes.Buffer, lang, body string) {
	fenceLen := 3
	run := 0
	for _, r := range body {
		if r == '`' {
			run++
			if run >= fenceLen {
				fenceLen = run + 1
			}
		} else {
			run = 0
		}
	}
	fence := strings.Repeat("`", fenceLen)
	b.WriteString(fence)
	b.WriteString(lang)
	b.WriteByte('\n')
	b.WriteString(body)
	if !strings.HasSuffix(body, "\n") {
		b.WriteByte('\n')
	}
	b.WriteString(fence)
	b.WriteByte('\n')
}
