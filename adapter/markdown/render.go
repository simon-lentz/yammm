package markdown

import (
	"bytes"
	"slices"
	"strconv"
	"strings"
	"unicode"

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

// slug derives the GitHub-style anchor for a heading: lowercase, spaces
// become hyphens, and every other character outside letters, digits,
// hyphens, and underscores is stripped (dots in qualified type names
// disappear entirely). The self-check resolves emitted internal links
// against anchors computed by this function.
func slug(heading string) string {
	var b strings.Builder
	b.Grow(len(heading))
	for _, r := range strings.ToLower(heading) {
		switch {
		case r == ' ':
			b.WriteByte('-')
		case r == '-' || r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		}
	}
	return b.String()
}

// escapeCell returns s made safe for use inside a Markdown table cell:
// backslashes and pipes are escaped so they cannot split the row, and
// newlines fold to <br> so the cell stays on one line. Whitespace around
// each folded line break is dropped — it is source-comment layout, not
// content, and a cell cannot render it anyway.
func escapeCell(s string) string {
	if strings.ContainsAny(s, "\r\n") {
		s = strings.ReplaceAll(s, "\r\n", "\n")
		s = strings.ReplaceAll(s, "\r", "\n")
		lines := strings.Split(s, "\n")
		for i, line := range lines {
			lines[i] = strings.TrimSpace(line)
		}
		s = strings.Join(lines, "<br>")
	}
	// Escape backslashes before pipes: a literal "\|" in content must become
	// "\\\|" (odd backslash run, a genuinely escaped pipe) rather than "\\|"
	// (even run), which a GFM table parser reads as an escaped backslash plus
	// a live column delimiter — splitting the row. The <br> markers inserted
	// above carry no backslash or pipe, so folding is unaffected.
	s = strings.ReplaceAll(s, `\`, `\\`)
	return strings.ReplaceAll(s, "|", `\|`)
}

// codeTagEscaper makes text safe inside a raw HTML <code> element:
// HTML metacharacters are entity-escaped, and backticks become &#96; so
// they cannot open a code span within the surrounding Markdown.
var codeTagEscaper = strings.NewReplacer(
	"&", "&amp;",
	"<", "&lt;",
	">", "&gt;",
	"`", "&#96;",
)

// codeCell renders s as code inside a Markdown table cell. The normal form
// is a backtick code span with table-cell escaping applied; when s itself
// contains a backtick the code span cannot represent it, so the cell
// switches to an entity-escaped <code> element. Empty input yields an
// empty cell, since a code span with no content is not valid Markdown.
func codeCell(s string) string {
	if s == "" {
		return ""
	}
	if !strings.Contains(s, "`") {
		return "`" + escapeCell(s) + "`"
	}
	return "<code>" + escapeCell(codeTagEscaper.Replace(s)) + "</code>"
}

// mermaidID sanitizes a display name into a valid Mermaid class
// identifier: characters outside letters, digits, and underscores
// (notably the dots in qualified type names) become underscores. When the
// result differs from the input, the diagram emits the display-label form
// `class id["display"]` so the rendered name keeps its original spelling.
func mermaidID(name string) string {
	return strings.Map(func(r rune) rune {
		if r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) {
			return r
		}
		return '_'
	}, name)
}

// writeTableRow writes one table row; cells must already be escaped (via
// escapeCell or codeCell). An empty cell renders as the standard empty
// Markdown cell.
func writeTableRow(b *bytes.Buffer, cells ...string) {
	b.WriteByte('|')
	for _, cell := range cells {
		b.WriteByte(' ')
		b.WriteString(cell)
		b.WriteString(" |")
	}
	b.WriteByte('\n')
}

// writeTableHeader writes a table's header row followed by the separator
// row the self-check verifies for column-count agreement.
func writeTableHeader(b *bytes.Buffer, cols ...string) {
	writeTableRow(b, cols...)
	sep := make([]string, len(cols))
	for i := range sep {
		sep[i] = "---"
	}
	writeTableRow(b, sep...)
}

// indentUnderBullet indents every non-empty line of s by the two-space
// list-item continuation indent. Blank lines stay blank so nested blocks
// introduce no trailing whitespace.
func indentUnderBullet(s string) string {
	const prefix = "  "
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
// lines; empty blocks are omitted entirely. The heading's anchor is
// registered for the self-check.
func (g *generator) emitTypeSection(t *schema.Type) {
	e := g.types[t.ID()]
	g.anchors[e.anchor] = true

	blocks := []string{"### " + e.display + "\n"}
	if b := g.typeBadges(t); b != "" {
		blocks = append(blocks, b+"\n")
	}
	if doc := t.Documentation(); doc != "" {
		blocks = append(blocks, doc+"\n")
	}
	if tbl := g.propertyTable(t); tbl != "" {
		blocks = append(blocks, tbl)
	}
	// Associations and compositions share one relation namespace, so one
	// resolver marks inherited relations of either kind.
	relFrom := inheritedFrom(g, t, func(st *schema.Type) []*schema.Relation {
		return slices.Concat(st.AssociationsSlice(), st.CompositionsSlice())
	})
	if list := g.relationList("**Associations**", t.AllAssociationsSlice(), relFrom); list != "" {
		blocks = append(blocks, list)
	}
	if list := g.relationList("**Compositions**", t.AllCompositionsSlice(), relFrom); list != "" {
		blocks = append(blocks, list)
	}
	if list := g.invariantList(t); list != "" {
		blocks = append(blocks, list)
	}
	g.buf.WriteString(strings.Join(blocks, "\n"))
}

// inheritedOwners maps every member a type inherits — keyed by pointer — to the
// display name of the ancestor that declares it, for the "from <Owner>"
// provenance marker. own extracts a type's own members of the relevant kind;
// members the type declares itself belong to no ancestor and are absent from
// the result. Merge only pulls inherited members from resolved, closure-present
// ancestors, so every inherited member resolves to a display name here.
func inheritedOwners[T comparable](g *generator, t *schema.Type, own func(*schema.Type) []T) map[T]string {
	owners := make(map[T]string)
	for _, super := range t.SuperTypesSlice() {
		se, ok := g.types[super.ID()]
		if !ok {
			continue
		}
		for _, m := range own(se.typ) {
			if _, exists := owners[m]; !exists {
				owners[m] = se.display
			}
		}
	}
	return owners
}

// inheritedFrom builds the provenance-suffix resolver for one kind of type
// member: it returns " — from <Owner>" for a member the type inherits, naming
// the declaring ancestor as displayed in this document (schema-qualified for an
// imported ancestor), and "" for a member the type declares itself. This gives
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
// type's section, falling back to the reference's own spelling when the
// parent did not resolve (a deferred import).
func (g *generator) superLink(t *schema.Type, ref schema.TypeRef) string {
	if e, ok := g.resolveSuper(t, ref); ok {
		return "[" + e.display + "](#" + e.anchor + ")"
	}
	return ref.String()
}

// propertyTable renders the flattened property table over the type's full
// property set, own rows first. Inherited rows carry a "from <Owner>"
// modifier naming the declaring ancestor as displayed in this document.
func (g *generator) propertyTable(t *schema.Type) string {
	all := t.AllPropertiesSlice()
	if len(all) == 0 {
		return ""
	}

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

	var b bytes.Buffer
	writeTableHeader(&b, "Property", "Type", "Modifiers", "Description")
	for _, p := range all {
		var mods []string
		switch {
		case p.IsPrimaryKey():
			mods = append(mods, "primary")
		case p.IsRequired():
			mods = append(mods, "required")
		}
		if !own[p.Origin()] {
			owner, ok := ownerOf[p.Origin()]
			if !ok {
				owner = p.DeclaringScope().String()
			}
			mods = append(mods, "from "+owner)
		}
		writePropertyRow(&b, p, strings.Join(mods, ", "))
	}
	return b.String()
}

// edgePropertyTable renders the sub-table for a relation's edge
// properties, using the same columns as the type property table.
func edgePropertyTable(props []*schema.Property) string {
	var b bytes.Buffer
	writeTableHeader(&b, "Property", "Type", "Modifiers", "Description")
	for _, p := range props {
		mods := ""
		if p.IsRequired() {
			mods = "required"
		}
		writePropertyRow(&b, p, mods)
	}
	return b.String()
}

// writePropertyRow writes one property-table row; the Type cell renders
// the constraint's DSL form (named DataTypes display their name).
func writePropertyRow(b *bytes.Buffer, p *schema.Property, mods string) {
	typeCell := ""
	if c := p.Constraint(); c != nil {
		typeCell = c.String()
	}
	writeTableRow(b, codeCell(p.Name()), codeCell(typeCell), escapeCell(mods), escapeCell(p.Documentation()))
}

// relationList renders a labeled bullet list of relations in DSL notation
// with linked targets. An inherited relation carries a " — from <Owner>"
// marker via origin, mirroring the property table. A relation's documentation
// and edge-property sub-table nest under its bullet. Returns "" for an empty
// list.
func (g *generator) relationList(label string, rels []*schema.Relation, origin func(*schema.Relation) string) string {
	if len(rels) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(label + "\n")
	for _, rel := range rels {
		b.WriteString("\n")
		arrow := "-->"
		if rel.IsComposition() {
			arrow = "*->"
		}
		mult := multiplicity(rel.IsOptional(), rel.IsMany())
		b.WriteString("- `" + arrow + " " + rel.Name() + " (" + mult + ")` " + g.relationTarget(rel) + origin(rel) + "\n")

		var nested []string
		if doc := rel.Documentation(); doc != "" {
			nested = append(nested, indentUnderBullet(doc)+"\n")
		}
		if props := rel.PropertiesSlice(); len(props) > 0 {
			nested = append(nested, indentUnderBullet(edgePropertyTable(props)))
		}
		for _, block := range nested {
			b.WriteString("\n" + block)
		}
	}
	return b.String()
}

// relationTarget links the relation's resolved target type; when the
// target never resolved (a deferred import on a Builder-built schema) it
// degrades to the reference's own spelling, unlinked.
func (g *generator) relationTarget(rel *schema.Relation) string {
	if e, ok := g.types[rel.TargetID()]; ok {
		return "[" + e.display + "](#" + e.anchor + ")"
	}
	return rel.Target().String()
}

// invariantList renders the type's invariants: the failure message as the
// bullet (an inherited invariant carries a " — from <Owner>" marker, mirroring
// the property table), the documentation indented beneath it, then the
// declaration source in a yammm fence when the span and source content allow
// extraction. Returns "" when the type has no invariants.
func (g *generator) invariantList(t *schema.Type) string {
	invs := t.AllInvariantsSlice()
	if len(invs) == 0 {
		return ""
	}
	from := inheritedFrom(g, t, (*schema.Type).InvariantsSlice)
	var b strings.Builder
	b.WriteString("**Invariants**\n")
	for _, inv := range invs {
		b.WriteString("\n- " + strconv.Quote(inv.Name()) + from(inv) + "\n")
		if doc := inv.Documentation(); doc != "" {
			b.WriteString("\n" + indentUnderBullet(doc) + "\n")
		}
		if src, ok := g.invariantSource(inv); ok {
			var fence bytes.Buffer
			writeFence(&fence, "yammm", src)
			b.WriteString("\n" + indentUnderBullet(fence.String()))
		}
	}
	return b.String()
}

// invariantSource extracts the invariant's declaration text from its
// source. It returns ok=false — degrading the invariant to message-only —
// when byte offsets are unknown or the schema carries no source content
// (a Builder-built schema). The declaration span includes a leading doc
// comment when one is present; since the documentation renders separately,
// it is stripped from the fenced text.
func (g *generator) invariantSource(inv *schema.Invariant) (string, bool) {
	span := inv.Span()
	if g.sources == nil || !span.Start.HasByte() || !span.End.HasByte() {
		return "", false
	}
	content, ok := g.sources.ContentBySource(span.Source)
	if !ok {
		return "", false
	}
	start, end := span.Start.Byte, span.End.Byte
	if start < 0 || end > len(content) || start >= end {
		return "", false
	}
	text := string(content[start:end])
	if inv.Documentation() != "" {
		if i := strings.Index(text, "*/"); i >= 0 {
			text = strings.TrimLeft(text[i+2:], " \t\r\n")
		}
	}
	text = strings.TrimRight(dedent(text), " \t\r\n")
	return text, text != ""
}

// dedent strips the longest common leading-whitespace prefix from every
// continuation line. The first line starts at the declaration's own token,
// so it carries no indentation to strip; continuation lines carry the
// source file's, which would otherwise leak into the fence.
func dedent(s string) string {
	lines := strings.Split(s, "\n")
	if len(lines) < 2 {
		return s
	}
	prefix := ""
	first := true
	for _, line := range lines[1:] {
		if strings.TrimSpace(line) == "" {
			continue
		}
		indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
		if first {
			prefix = indent
			first = false
			continue
		}
		prefix = commonPrefix(prefix, indent)
	}
	if prefix == "" {
		return s
	}
	for i, line := range lines[1:] {
		lines[i+1] = strings.TrimPrefix(line, prefix)
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
