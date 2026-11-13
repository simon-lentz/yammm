package markdown

import (
	"bytes"
	"iter"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
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
// character, marks, decimal digits, connector punctuation and the two join
// controls U+200C and U+200D, read from the Go release's Unicode tables;
// GitHub keeps both join controls. Lowercasing maps U+0130 to "i" and a
// combining dot, as GitHub's full case mapping does.
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
	return r == '\u200c' || r == '\u200d' || unicode.In(r, unicode.L, unicode.M, unicode.Nd, unicode.Nl, unicode.Pc, unicode.Other_Alphabetic)
}

// descriptionCell writes a doc comment as a table cell, which GitHub reads as
// inline Markdown on one line: the comment's lines fold into one (foldLines),
// and a pipe after an even backslash run gains one backslash, so the row keeps
// its cells. The table then drops one backslash before a pipe, in a code span
// too.
func descriptionCell(s string) string {
	if s = lfLineEndings(s); strings.Contains(s, "\n") {
		s = foldLines(s)
	}
	pipes := strings.Count(s, "|")
	if pipes == 0 {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + pipes)
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

// foldLines joins a doc comment's lines into one cell line, reading the text
// as the cell is read: as inline Markdown, never as a document of blocks. A
// line break inside a code span, an HTML tag or an inline link's tail
// (writeLinkTail) folds to a space, with the white space that starts the next
// line dropped. In a pre or listing element's text it is a <br> and the line
// is kept whole, a backslash that ends it included, save the one right after
// the element's start tag, which an HTML parser drops and which folds to
// nothing. Any other folds to <br> with the white space around it dropped,
// save a space lineBreak keeps. A www link or URL is written as it is
// (extendedAutolinkLength). The "(" after a link text whose tail does not
// form is escaped (closeBracket).
// An unescaped backslash that ends a line before a line of text is the hard
// break it writes and goes; one that ends a line before a blank line, or
// inside a pre element's text, is doubled before the <br>, so it stays a
// backslash. The "<" of a declaration with no end is
// escaped, so no <br> ends it.
func foldLines(s string) string {
	// A line break adds at most five bytes, and a declaration's "<" and a
	// failed tail's "(" one each.
	grow := 5*strings.Count(s, "\n") + strings.Count(s, "<!") + strings.Count(s, "](")
	f := lineFolder{inlineScanner: inlineScanner{src: s}, out: make([]byte, 0, len(s)+grow)}
	f.fold()
	return string(bytes.TrimSpace(f.out))
}

// lineFolder is foldLines' state over one doc comment.
type lineFolder struct {
	inlineScanner
	out []byte
	// pre holds the pre and listing elements open at this point, innermost
	// last, true for a listing element, and preStarted is set when the last
	// thing read is such an element's start tag.
	pre        []bool
	preStarted bool
	// brackets holds the link and image openers, "[" and "![", open at this
	// point, innermost last, true for an image. linkFormed is set when a link
	// formed after the last "[" was opened: cmark-gfm then reads no "[" before
	// it as a link's start.
	brackets   []bool
	linkFormed bool
	// autolinkEnd is the length of out after the last extended autolink: a
	// backslash that ends one is the link's, never an escape or a hard break.
	autolinkEnd int
	parens      parenRun
	// The title and end of the tail read after the destination that ends at
	// lastDestTo.
	lastDestTo, lastTitleFrom, lastTitleTo, lastEnd int
}

func (f *lineFolder) fold() {
	s := f.src
	for i := 0; i < len(s); {
		afterPreStart := f.preStarted
		f.preStarted = false
		switch c := s[i]; {
		case c == '\\' && i+1 < len(s) && isASCIIPunct(s[i+1]):
			f.out = append(f.out, c, s[i+1])
			i += 2
		case c == '`':
			n := runLength(s, i, '`')
			end := f.codeSpanEnd(i+n, n)
			if end < 0 {
				end = i + n
			}
			f.writeSpan(s[i:end])
			i = end
		case c == '<':
			end, tag := f.htmlEnd(i)
			if end < 0 {
				if declarationNameLength(s[i+1:]) > 0 {
					// The ">" of a <br> written after it would end the declaration.
					f.out = append(f.out, '\\')
				}
				f.out = append(f.out, c)
				i++
				break
			}
			f.writeSpan(s[i:end])
			f.countPre(tag)
			i = end
		case c == '\n':
			i = f.lineBreak(i, afterPreStart)
		case c == '[':
			f.openBracket(i, false)
			f.linkFormed = false
			f.out = append(f.out, c)
			i++
		case c == '!' && strings.HasPrefix(s[i+1:], "[") && !strings.HasPrefix(s[i+1:], "[^"):
			f.openBracket(i+1, true)
			f.out = append(f.out, "!["...)
			i += 2
		case c == ']' && len(f.brackets) > 0:
			i = f.closeBracket(i)
		case (c == 'w' || c == ':') && len(f.brackets) == 0:
			if n := extendedAutolinkLength(s, i); n > 0 {
				f.out = append(f.out, s[i:i+n]...)
				f.autolinkEnd = len(f.out)
				i += n
				break
			}
			f.out = append(f.out, c)
			i++
		default:
			f.out = append(f.out, c)
			i++
		}
	}
}

// openBracket opens the link or image opener whose "[" is src[i]. The stack
// is made once, to hold every "[" from the first.
func (f *lineFolder) openBracket(i int, image bool) {
	if f.brackets == nil {
		f.brackets = make([]bool, 0, strings.Count(f.src[i:], "["))
	}
	f.brackets = append(f.brackets, image)
}

// closeBracket reads the "]" at src[i] that closes the innermost opener, and
// returns where the text after it starts. A link or an image ends with the
// inline tail after the "]" when cmark-gfm reads one there; when it reads
// none, the "(" is written escaped.
func (f *lineFolder) closeBracket(i int) int {
	image := f.brackets[len(f.brackets)-1]
	f.brackets = f.brackets[:len(f.brackets)-1]
	f.out = append(f.out, ']')
	i++
	if i == len(f.src) || f.src[i] != '(' || !image && f.linkFormed {
		return i
	}
	t, ok := f.linkTailAt(i)
	if !ok {
		// The fold can join what cmark-gfm reads apart, so the cell would
		// read a tail here that the doc comment does not hold.
		f.out = append(f.out, '\\', '(')
		return i + 1
	}
	f.writeLinkTail(i, t)
	if !image {
		f.linkFormed = true
	}
	return t.end
}

// writeSpan writes a code span or a tag on one line.
func (f *lineFolder) writeSpan(span string) {
	for {
		line, rest, more := strings.Cut(span, "\n")
		f.out = append(f.out, line...)
		if !more {
			return
		}
		f.out = append(f.out, ' ')
		span = strings.TrimLeft(rest, " \t")
	}
}

// countPre reads a pre or listing element's start or end tag as an HTML
// parser reads it in a body: a start tag opens the element, self-closing too,
// and an end tag closes the innermost open one of its name and all inside it,
// or is ignored when none is open. No other element is tracked, so an end tag
// a scope boundary hides closes its element here all the same, and the end
// tag of an element that holds one, such as </div>, leaves it open here.
func (f *lineFolder) countPre(tag htmlTag) {
	listing := strings.EqualFold(tag.name, "listing")
	if !listing && !strings.EqualFold(tag.name, "pre") {
		return
	}
	if !tag.end {
		f.pre = append(f.pre, listing)
		f.preStarted = true
		return
	}
	for i, l := range slices.Backward(f.pre) {
		if l == listing {
			f.pre = f.pre[:i]
			return
		}
	}
}

// lineBreak folds the line break at src[i] that no code span and no tag holds,
// and returns where the next line's text starts. A <br> is punctuation where
// the line break was white space, so a space beside it keeps an emphasis
// delimiter's flanking and a www link's start as the doc comment reads them.
func (f *lineFolder) lineBreak(i int, afterPreStart bool) int {
	if len(f.pre) > 0 {
		if afterPreStart {
			return i + 1
		}
		if endsInOddBackslashRun(f.out) && len(f.out) != f.autolinkEnd {
			f.out = append(f.out, '\\')
		}
		f.spaceAfterDelimiter()
		f.out = append(f.out, "<br>"...)
		return i + 1
	}
	next := i + 1
	for next < len(f.src) && (f.src[next] == ' ' || f.src[next] == '\t') {
		next++
	}
	untrimmed := len(f.out)
	f.out = bytes.TrimRight(f.out, " \t")
	switch {
	case !endsInOddBackslashRun(f.out) || len(f.out) == f.autolinkEnd:
		f.spaceAfterDelimiter()
	case len(f.out) == untrimmed && next < len(f.src) && f.src[next] != '\n':
		// Markdown reads this backslash as the hard break itself.
		f.out = f.out[:len(f.out)-1]
	default:
		// A lone backslash would escape the "<" of the <br>.
		f.out = append(f.out, '\\')
	}
	f.out = append(f.out, "<br>"...)
	if next < len(f.src) && (isEmphasisDelimiter(f.src[next]) || strings.HasPrefix(f.src[next:], "www.")) {
		f.out = append(f.out, ' ')
	}
	return next
}

// spaceAfterDelimiter writes a space when the text written so far ends in an
// emphasis delimiter no backslash escapes.
func (f *lineFolder) spaceAfterDelimiter() {
	if n := len(f.out); n > 0 && isEmphasisDelimiter(f.out[n-1]) && !endsInOddBackslashRun(f.out[:n-1]) {
		f.out = append(f.out, ' ')
	}
}

func isEmphasisDelimiter(c byte) bool { return c == '*' || c == '_' || c == '~' }

// endsInOddBackslashRun reports whether b ends in a backslash that no
// backslash before it escapes.
func endsInOddBackslashRun(b []byte) bool {
	return (len(b)-len(bytes.TrimRight(b, `\`)))%2 == 1
}

// lfLineEndings makes every CR LF and every lone CR of s an LF. CommonMark
// ends a line at each, and the parser the self-check reads with ends one at
// LF alone. Text with no CR is returned as it is, with no allocation.
func lfLineEndings(s string) string {
	if !strings.Contains(s, "\r") {
		return s
	}
	return lineEndings.Replace(s)
}

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
	rs := []rune(lfLineEndings(s))
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

// dollarSpan writes a dollar outside code. GitHub reads a pair of dollars as
// math there, and a backslash does not stop it; its documentation gives a span
// round the dollar as the escape.
const dollarSpan = "<span>$</span>"

// escapeInline backslash-escapes the ASCII punctuation Markdown reads as
// inline syntax, a table cell's pipe included, writes a linkBreak where GitHub
// would read an autolink and a dollarSpan for each dollar, so text such as a
// schema name renders literally in a heading, a link or a table cell. An
// underscore between two letters or digits opens no emphasis in GFM and is
// left as is.
func escapeInline(s string) string {
	rs := []rune(s)
	var b strings.Builder
	for i, r := range rs {
		if breaksLinkAt(rs, i) {
			b.WriteString(linkBreak)
		}
		if r == '$' {
			b.WriteString(dollarSpan)
			continue
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
func (g *generator) emitTypeSection(t schema.Type) {
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
	if props := slices.Collect(t.AllProperties()); len(props) > 0 {
		block()
		g.writePropertyTable(t, props)
	}
	// Associations and compositions share one relation namespace, so one
	// resolver marks inherited relations of either kind.
	relFrom := inheritedFrom(g, t, func(st schema.Type) iter.Seq[schema.Relation] {
		return slices.Values(slices.Concat(slices.Collect(st.Associations()), slices.Collect(st.Compositions())))
	})
	for _, list := range []struct {
		label string
		rels  []schema.Relation
	}{
		{"**Associations**", slices.Collect(t.AllAssociations())},
		{"**Compositions**", slices.Collect(t.AllCompositions())},
	} {
		if len(list.rels) > 0 {
			block()
			g.writeRelationList(list.label, list.rels, relFrom)
		}
	}
	if invs := slices.Collect(t.AllInvariants()); len(invs) > 0 {
		block()
		g.writeInvariantList(t, invs)
	}
}

// inheritedOwners maps every member a type inherits — keyed by record — to the
// Markdown display name of the ancestor that declares it, for the "from <Owner>"
// provenance marker. own extracts a type's own members of the relevant kind;
// members the type declares itself belong to no ancestor and are absent from
// the result. Merge only pulls inherited members from resolved, closure-present
// ancestors, so every inherited member resolves to a display name here, and
// each member has one declaring ancestor, which the linearization lists once.
func inheritedOwners[T comparable](g *generator, t schema.Type, own func(schema.Type) iter.Seq[T]) map[T]string {
	owners := make(map[T]string)
	for super := range t.SuperTypes() {
		se, ok := g.types[super.ID()]
		if !ok {
			continue
		}
		for m := range own(se.typ) {
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
func inheritedFrom[T comparable](g *generator, t schema.Type, own func(schema.Type) iter.Seq[T]) func(T) string {
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
func (g *generator) typeBadges(t schema.Type) string {
	var parts []string
	if t.IsAbstract() {
		parts = append(parts, "*Abstract type* — no direct instances.")
	}
	if t.IsPart() {
		parts = append(parts, "*Part type* — instances exist only as composed children.")
	}
	if inherits := slices.Collect(t.Inherits()); len(inherits) > 0 {
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
func (g *generator) superLink(t schema.Type, ref schema.TypeRef) string {
	if e, ok := g.resolveSuper(t, ref); ok {
		return g.link(e)
	}
	return escapeInline(ref.String())
}

// writePropertyTable writes the flattened property table over all, the type's
// full property set, own rows first. Inherited rows carry a "from <Owner>"
// modifier naming the declaring ancestor as displayed in this document.
func (g *generator) writePropertyTable(t schema.Type, all []schema.Property) {
	ownProps := slices.Collect(t.Properties())
	own := make(map[schema.Property]bool, len(ownProps))
	for _, p := range ownProps {
		own[p] = true
	}
	// Inherited rows reuse the declaring ancestor's own Property records, so
	// the map keys each row to its declarer. A row whose annotations were
	// merged across ancestors is a synthesized copy absent from every own slice,
	// so an inherited row goes through Origin() to reach the declared property.
	ownerOf := inheritedOwners(g, t, schema.Type.Properties)

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
func (g *generator) writeEdgePropertyTable(props []schema.Property) {
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
func writePropertyRow(tw *tableWriter, p schema.Property, mods string) {
	tw.row(codeCellOf(p.Name()), codeCellOf(p.Constraint().String()), tableCell{md: mods}, tw.g.descriptionCellOf(p.Documentation()))
}

// writeRelationList writes a labeled bullet list of relations in DSL notation
// with linked targets. An inherited relation carries a " — from <Owner>"
// marker via origin, mirroring the property table. A relation's documentation
// and edge-property sub-table nest under its bullet.
func (g *generator) writeRelationList(label string, rels []schema.Relation, origin func(schema.Relation) string) {
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
		if props := slices.Collect(rel.Properties()); len(props) > 0 {
			g.buf.WriteString("\n")
			g.writeEdgePropertyTable(props)
		}
	}
}

// relationTarget links the relation's target type; when the target is absent
// from this document's type map it degrades to the reference's own spelling,
// unlinked and escaped as schema text.
func (g *generator) relationTarget(rel schema.Relation) string {
	if e, ok := g.types[rel.TargetID()]; ok {
		return g.link(e)
	}
	return escapeInline(rel.Target().String())
}

// writeInvariantList writes the type's invariants: the failure message as the
// bullet (an inherited invariant carries a " — from <Owner>" marker, mirroring
// the property table), the documentation indented beneath it, then the
// declaration source in a yammm fence.
func (g *generator) writeInvariantList(t schema.Type, invs []schema.Invariant) {
	from := inheritedFrom(g, t, schema.Type.Invariants)
	g.buf.WriteString("**Invariants**\n")
	for _, inv := range invs {
		g.buf.WriteString("\n" + bulletMarker + escapeInline(strconv.Quote(inv.Name())) + from(inv) + "\n")
		if doc := inv.Documentation(); doc != "" {
			g.buf.WriteString("\n")
			g.writeAuthorText(doc, true)
			g.buf.WriteString("\n")
		}
		var fence bytes.Buffer
		writeFence(&fence, "yammm", g.invariantSource(inv))
		g.buf.WriteString("\n" + indentUnderBullet(fence.String()))
	}
}

// invariantSource extracts the invariant's declaration text from its source.
// The comments the span starts with — the doc comment, which renders separately,
// and any other before the "!" — are stripped, whatever they hold, so the fence
// starts at the declaration.
func (g *generator) invariantSource(inv schema.Invariant) string {
	span := inv.Span()
	content := g.sourceContent(span.Source)
	end := span.End.Byte
	text := withoutLeadingComments(string(content[span.Start.Byte:end]))
	at := end - len(text)
	return dedent(lfLineEndings(text), lineIndent(content, at))
}

// withoutLeadingComments returns text from its first character that is neither
// white space nor inside a comment. text ends at a declaration's last token, so
// every comment in it ends.
func withoutLeadingComments(text string) string {
	for {
		text = strings.TrimLeft(text, " \t\r\n")
		if rest, ok := strings.CutPrefix(text, "/*"); ok {
			_, text, _ = strings.Cut(rest, "*/")
			continue
		}
		if rest, ok := strings.CutPrefix(text, "//"); ok {
			text = rest[strings.IndexAny(rest, "\r\n"):]
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
func (g *generator) sourceContent(id location.SourceID) []byte {
	if content, ok := g.contents[id]; ok {
		return content
	}
	content, _ := g.sources.ContentBySource(id)
	g.contents[id] = content
	return content
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
