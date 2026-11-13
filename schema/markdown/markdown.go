package markdown

import (
	"bytes"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
	"github.com/yuin/goldmark"
)

// Option configures Marshal.
type Option func(*config)

type config struct {
	classDiagram bool
	classMembers bool
}

// WithClassDiagram toggles the Mermaid class-diagram section (default true).
func WithClassDiagram(include bool) Option {
	return func(c *config) {
		c.classDiagram = include
	}
}

// WithClassMembers toggles the member lines inside each diagram class
// (default true). With false, classes keep their stereotype and edges and
// list no properties; the per-type tables still carry every property.
func WithClassMembers(include bool) Option {
	return func(c *config) {
		c.classMembers = include
	}
}

// Marshal renders a schema and its transitive import closure as one
// self-contained Markdown document, laid out as the package documentation's
// Document Structure states. Output is deterministic — byte-identical across runs for
// the same schema, options and build of the generator — and the text the
// generator wrote is structurally verified before return; an error reports a
// generator bug, and no doc comment's text draws one.
func Marshal(s schema.Schema, opts ...Option) ([]byte, error) {
	cfg := config{classDiagram: true, classMembers: true}
	for _, o := range opts {
		o(&cfg)
	}
	return newGenerator(s, cfg).marshal()
}

// marshal emits the document, emits it again when the read of its heading
// elements moved an anchor of the outline, and returns it checked.
func (g *generator) marshal() ([]byte, error) {
	g.emitDocument()
	if g.authorHeadings && g.reanchor() {
		g.reset()
		g.emitDocument()
	}
	return g.finish()
}

// reanchor allocates every anchor over the heading elements read in the
// emitted document, doc-comment headings included, as GitHub allocates them,
// and reports whether an outline anchor moved. An outline heading the read
// loses, which GitHub anchors not at all, takes an anchor after every heading
// the read finds, so it moves none of theirs and its links lead nowhere
// rather than to another heading. A document that cannot be read keeps the
// outline's anchors. Heading text does not depend on anchors, so a second
// emission allocates the same anchors.
func (g *generator) reanchor() bool {
	src := g.buf.Bytes()
	read, err := g.headings.headings(parse(g.md, src), src)
	if err != nil {
		return false
	}
	at := make(map[int]int, len(g.outline))
	for i, h := range g.outline {
		at[h.offset] = i
	}
	var alloc anchorAllocator
	anchors := make([]string, len(g.outline))
	found := make([]bool, len(g.outline))
	for _, p := range read {
		if i, own := at[p.offset]; own {
			anchors[i], found[i] = alloc.allocate(g.outline[i].text), true
		} else {
			alloc.allocate(p.text)
		}
	}
	for i, h := range g.outline {
		if !found[i] {
			anchors[i] = alloc.allocate(h.text)
		}
	}
	moved := false
	for i, anchor := range anchors {
		if g.outline[i].anchor != anchor {
			g.outline[i].anchor = anchor
			moved = true
		}
	}
	for _, h := range g.outline {
		if h.kind == kindTypeSection {
			g.types[h.typ.ID()].anchor = h.anchor
		}
	}
	return moved
}

// reset discards the emitted document and the state emitting it recorded.
func (g *generator) reset() {
	g.authored = nil
	g.buf.Reset()
	g.links = nil
	g.tables = nil
	g.labelled = false
	g.diagramAt = -1
}

// finish runs the self-check over the emitted document and returns it. Marshal
// returns only what finish returns, so the check cannot be bypassed without
// losing the output.
func (g *generator) finish() ([]byte, error) {
	if err := g.selfCheck(); err != nil {
		return nil, err
	}
	return g.buf.Bytes(), nil
}

// outlineKind names what an outline entry's section holds.
type outlineKind int

const (
	kindTitle outlineKind = iota
	kindClassDiagram
	kindTypes
	kindTypeSection
	kindDataTypes
	kindImportedSchema
)

// outlineEntry is one heading of the document, in document order. The
// outline is built once, allocates every anchor, and is what emitDocument
// walks, so the headings written and the anchors linked to are one list.
type outlineEntry struct {
	kind   outlineKind
	level  int
	text   string        // the heading's text as rendered, which the anchor is derived from
	md     string        // the heading's Markdown source, with inline syntax escaped
	anchor string        // allocated by anchorAllocator in document order
	offset int           // where emitDocument wrote the heading line, for the self-check
	schema schema.Schema // kindDataTypes, kindImportedSchema
	typ    schema.Type   // kindTypeSection
}

// emitDocument writes the document by walking the outline: title, schema
// documentation, class diagram, the entry schema's type sections and
// data-type table, then one section per imported schema in closure order.
func (g *generator) emitDocument() {
	for i, h := range g.outline {
		if i > 0 {
			g.buf.WriteString("\n")
		}
		g.outline[i].offset = g.buf.Len() // every case below writes its heading first
		switch h.kind {
		case kindTitle:
			g.buf.WriteString("# " + h.md + "\n")
			if doc := g.entry.Documentation(); doc != "" {
				g.buf.WriteString("\n")
				g.writeAuthorText(doc, false)
				g.buf.WriteString("\n")
			}
		case kindClassDiagram:
			g.emitClassDiagram(h)
		case kindTypes:
			g.buf.WriteString("## " + h.md + "\n")
		case kindTypeSection:
			g.emitTypeSection(h.typ)
		case kindDataTypes:
			hashes := "##"
			if h.level == 3 {
				hashes = "###"
			}
			g.buf.WriteString(hashes + " " + h.md + "\n\n")
			g.writeDataTypeTable(slices.Collect(h.schema.DataTypes()))
		case kindImportedSchema:
			g.buf.WriteString("## " + h.md + "\n")
			if doc := h.schema.Documentation(); doc != "" {
				g.buf.WriteString("\n")
				g.writeAuthorText(doc, false)
				g.buf.WriteString("\n")
			}
		}
	}
}

// writeDataTypeTable writes the Name | Definition | Description table over a
// schema's named DataTypes.
func (g *generator) writeDataTypeTable(dts []schema.DataType) {
	tw := g.newTable(0, "Name", "Definition", "Description")
	for _, dt := range dts {
		tw.row(codeCellOf(dt.Name()), codeCellOf(dt.Constraint().String()), g.descriptionCellOf(dt.Documentation()))
	}
}

// selfCheck verifies the structure the generator wrote. It reads ownText, the
// emitted document with each doc comment replaced, with a CommonMark parser,
// so the verdict is about the generator's text alone and no doc comment
// decides it. Nothing is left open at the end. The headings read are the
// outline's and no other: each a top-level heading of its level whose text is
// the text the generator meant, holding an anchor of its own that its slug
// gives. The links read are the internal links the generator wrote, each to an
// outline heading, and the raw HTML read is the tags the generator writes.
// Every table it wrote is read with its columns and rows. The class diagram is
// read as a fenced code block whose every line is a form the emitter writes. A
// failure is a generator bug.
func (g *generator) selfCheck() error {
	own, at := g.ownText()
	doc := probeText(g.md, own)
	if _, open := doc.open(); open {
		return errors.New("markdown: self-check: the document ends inside an open block")
	}
	root, out := doc.root, doc.src
	read, err := markdownHeadings(root, out)
	if err != nil {
		return fmt.Errorf("markdown: self-check: %w", err)
	}
	anchors := make(map[string]bool, len(g.outline))
	for _, h := range g.outline {
		got, ok := read[at(h.offset)]
		if !ok || got.Parent() != root {
			return fmt.Errorf("markdown: self-check: heading %q is not read as a top-level heading", h.text)
		}
		text, err := textContent(g.md, got, out)
		switch {
		case err != nil:
			return fmt.Errorf("markdown: self-check: %w", err)
		case got.Level != h.level || text != h.text:
			return fmt.Errorf("markdown: self-check: heading %q is read as level %d %q", h.text, got.Level, text)
		case !isAnchorOf(h.anchor, slug(h.text)):
			return fmt.Errorf("markdown: self-check: heading %q takes anchor #%s, which is neither its slug #%s nor a numbered form of it", h.text, h.anchor, slug(h.text))
		case anchors[h.anchor]:
			return fmt.Errorf("markdown: self-check: two headings take anchor #%s", h.anchor)
		}
		anchors[h.anchor] = true
		delete(read, at(h.offset))
	}
	if len(read) > 0 {
		text, err := textContent(g.md, read[slices.Min(slices.Collect(maps.Keys(read)))], out)
		if err != nil {
			return fmt.Errorf("markdown: self-check: %w", err)
		}
		return fmt.Errorf("markdown: self-check: text outside doc comments is read as the heading %q", text)
	}
	links := map[string]int{}
	for _, a := range g.links {
		links[a]++
	}
	linked, err := ownInlines(root, out)
	if err != nil {
		return fmt.Errorf("markdown: self-check: %w", err)
	}
	for _, a := range slices.Sorted(maps.Keys(linked)) {
		if links[a] == 0 {
			return fmt.Errorf("markdown: self-check: internal link #%s is read %d times outside doc comments and written by no emitter", a, linked[a])
		}
	}
	for _, a := range slices.Sorted(maps.Keys(links)) {
		n := links[a]
		if !anchors[a] {
			return fmt.Errorf("markdown: self-check: internal link #%s resolves to no emitted heading", a)
		}
		if linked[a] != n {
			return fmt.Errorf("markdown: self-check: internal link #%s is written %d times and read %d", a, n, linked[a])
		}
	}
	shapes, err := tables(root, out)
	if err != nil {
		return fmt.Errorf("markdown: self-check: %w", err)
	}
	for _, tw := range g.tables {
		if tw.bad != "" {
			return fmt.Errorf("markdown: self-check: %s", tw.bad)
		}
		got, ok := shapes[at(tw.offset)]
		if !ok {
			return errors.New("markdown: self-check: a table the generator wrote is not read as a table")
		}
		if got.cols != tw.cols || got.rows != tw.rows {
			return fmt.Errorf("markdown: self-check: a table written with %d columns and %d rows is read with %d and %d", tw.cols, tw.rows, got.cols, got.rows)
		}
	}
	if g.diagramAt >= 0 {
		body, ok := fencedCodeAt(root, out, at(g.diagramAt))
		if !ok {
			return errors.New("markdown: self-check: the class diagram is not read as a fenced code block")
		}
		if err := checkDiagram(body); err != nil {
			return fmt.Errorf("markdown: self-check: %w", err)
		}
	}
	return nil
}

// isAnchorOf reports whether anchor is one anchorAllocator gives a heading
// that slugs to base: base, or base suffixed "-" and a number.
func isAnchorOf(anchor, base string) bool {
	suffix, ok := strings.CutPrefix(anchor, base)
	if !ok || suffix == "" {
		return ok
	}
	n, err := strconv.Atoi(strings.TrimPrefix(suffix, "-"))
	return err == nil && n > 0 && suffix == "-"+strconv.Itoa(n)
}

// docStandIn is the paragraph the self-check reads where a doc comment's block
// is written.
const docStandIn = "doc"

// ownText returns the emitted document with each doc comment replaced, a
// block by docStandIn at the block's column and a description cell by an empty
// cell, and the function that maps an offset of the generator's own text in
// the document to its offset there.
func (g *generator) ownText() ([]byte, func(int) int) {
	doc := g.buf.Bytes()
	if len(g.authored) == 0 {
		return doc, func(offset int) int { return offset }
	}
	own := make([]byte, 0, len(doc))
	// shorter[i] is how much shorter the text before g.authored[i] is there.
	shorter := make([]int, len(g.authored)+1)
	from := 0
	for i, a := range g.authored {
		own = append(append(own, doc[from:a.start]...), a.standIn...)
		from = a.end
		shorter[i+1] = shorter[i] + (a.end - a.start) - len(a.standIn)
	}
	own = append(own, doc[from:]...)
	return own, func(offset int) int {
		i, _ := slices.BinarySearchFunc(g.authored, offset, func(a authorText, offset int) int { return a.end - 1 - offset })
		return offset - shorter[i]
	}
}

// leavesNothingOpen reports whether doc holds none of what an open block or an
// open HTML construct needs: a run of three backticks or tildes, which starts
// a fenced code block, and a "<", which starts every HTML construct.
func leavesNothingOpen(doc string) bool {
	return !strings.Contains(doc, "<") && !strings.Contains(doc, "```") && !strings.Contains(doc, "~~~")
}

// closed returns doc with the Markdown block and the HTML construct it leaves
// open closed, read once per distinct text, and whether an HTML5 parser then
// reads what follows it. When it does not, the doc comment's HTML takes the
// generator's headings after it, and GitHub anchors none of them.
func (g *generator) closed(doc string) (text string, readsPast bool) {
	if leavesNothingOpen(doc) {
		return doc, true
	}
	if c, ok := g.closedDocs[doc]; ok {
		return c.text, c.readsPast
	}
	text, readsPast = closeAuthorText(g.md, doc), true
	if strings.Contains(doc, "<") {
		text, readsPast = g.headings.closeAuthorHTML(g.md, text)
	}
	g.closedDocs[doc] = closedDoc{text: text, readsPast: readsPast}
	return text, readsPast
}

// closedDoc is a doc comment closed, as closed returns it.
type closedDoc struct {
	text      string
	readsPast bool
}

// writeAuthorText writes a doc comment as a block, indented under its bullet
// when underBullet is set, with its line endings made LF and the Markdown
// block and the HTML construct it leaves open closed.
func (g *generator) writeAuthorText(doc string, underBullet bool) {
	doc = lfLineEndings(doc)
	text, readsPast := g.closed(doc)
	standIn := docStandIn
	if underBullet {
		text, standIn = indentUnderBullet(text), indentUnderBullet(standIn)
	}
	start := g.buf.Len()
	g.buf.WriteString(text)
	g.authored = append(g.authored, authorText{start: start, end: g.buf.Len(), standIn: standIn})
	g.authorHeadings = g.authorHeadings || !readsPast || holdsHeadingTag(doc) || mayHoldMarkdownHeading(doc)
}

// authorText is a doc comment's byte range in the emitted document, and the
// text the self-check reads in its place.
type authorText struct {
	start, end int
	standIn    string
}

// holdsHeadingTag reports whether s holds the start of an h1 to h6 tag, which
// a heading element written as raw HTML needs.
func holdsHeadingTag(s string) bool {
	for {
		i := strings.IndexByte(s, '<')
		if i < 0 || i+2 >= len(s) {
			return false
		}
		if (s[i+1] == 'h' || s[i+1] == 'H') && '1' <= s[i+2] && s[i+2] <= '6' {
			return true
		}
		s = s[i+1:]
	}
}

// mayHoldMarkdownHeading reports whether a doc comment written as a block
// holds what a Markdown heading needs: a "#", or a line of "=" or of "-" alone
// under its block quote markers and indentation.
func mayHoldMarkdownHeading(doc string) bool {
	if strings.Contains(doc, "#") {
		return true
	}
	for line := range strings.SplitSeq(doc, "\n") {
		line = strings.Trim(line, " \t>")
		if line != "" && (strings.Trim(line, "=") == "" || strings.Trim(line, "-") == "") {
			return true
		}
	}
	return false
}

// tableWriter writes one table into the document and records its shape: its
// column and row counts, where its header line starts, and a row the
// self-check reports because the parser cannot see it — a cell count other
// than the header's, which GitHub's tables pad or trim, or a line break.
type tableWriter struct {
	g      *generator
	indent string
	cols   int
	rows   int
	offset int
	bad    string
}

// tableCell is one cell's Markdown, and whether it holds a doc comment, which
// the self-check reads as an empty cell.
type tableCell struct {
	md     string
	author bool
}

func codeCellOf(s string) tableCell { return tableCell{md: codeCell(s)} }

// descriptionCellOf writes a doc comment as a table cell. The cell is inline
// Markdown, where nothing a doc comment leaves open as a block is open, so it
// takes no closer and is never read alone.
func (g *generator) descriptionCellOf(doc string) tableCell {
	g.authorHeadings = g.authorHeadings || holdsHeadingTag(doc)
	return tableCell{md: descriptionCell(doc), author: true}
}

// newTable writes a table's header and separator rows, indented by indent,
// and returns the writer for its body rows.
func (g *generator) newTable(indent int, cols ...string) *tableWriter {
	tw := &tableWriter{g: g, indent: strings.Repeat(" ", indent), cols: len(cols), offset: g.buf.Len()}
	g.tables = append(g.tables, tw)
	header := make([]tableCell, len(cols))
	sep := make([]tableCell, len(cols))
	for i, c := range cols {
		header[i], sep[i] = tableCell{md: c}, tableCell{md: "---"}
	}
	tw.write(header)
	tw.write(sep)
	return tw
}

func (tw *tableWriter) row(cells ...tableCell) {
	switch {
	case tw.bad != "":
	case len(cells) != tw.cols:
		tw.bad = fmt.Sprintf("table row has %d cells, its header %d", len(cells), tw.cols)
	case slices.ContainsFunc(cells, func(c tableCell) bool { return strings.ContainsAny(c.md, "\r\n") }):
		tw.bad = "a table cell holds a line break"
	}
	tw.rows++
	tw.write(cells)
}

// write writes one row of cells, which are Markdown already, and records each
// doc comment's place in the document. An empty one has no place to record.
func (tw *tableWriter) write(cells []tableCell) {
	b := &tw.g.buf
	b.WriteString(tw.indent + "|")
	for _, c := range cells {
		b.WriteByte(' ')
		start := b.Len()
		b.WriteString(c.md)
		if c.author && c.md != "" {
			tw.g.authored = append(tw.g.authored, authorText{start: start, end: b.Len()})
		}
		b.WriteString(" |")
	}
	b.WriteByte('\n')
}

// typeEntry records how one type in the closure is addressed in the emitted
// document: its display text, the anchor its heading takes, and its Mermaid
// class id.
type typeEntry struct {
	typ       schema.Type
	display   string // what a reader sees: see displayName
	displayMD string // display with inline Markdown syntax escaped
	anchor    string
	mermaidID string
}

// generator accumulates the emitted document and the cross-referencing
// state the emitters and the self-check share.
type generator struct {
	buf        bytes.Buffer
	entry      schema.Schema
	closure    []schema.Schema
	declaredIn map[schema.TypeID]schema.Schema // each type's declaring schema
	types      map[schema.TypeID]*typeEntry
	outline    []outlineEntry
	sources    schema.Sources
	contents   map[location.SourceID][]byte // each source's content, read once
	cfg        config

	md         goldmark.Markdown    // the parser the self-check and the closing of doc comments read with
	headings   headingReader        // reads the heading elements of what md parses
	links      []string             // the anchor of every internal link emitted
	authored   []authorText         // every doc comment written, in document order
	tables     []*tableWriter       // every table emitted
	closedDocs map[string]closedDoc // each doc comment closed reads, by its text

	// authorHeadings records that a doc comment written may hold a heading
	// element, which takes an anchor the outline did not allocate, or leaves
	// open HTML that takes the outline's headings after it.
	authorHeadings bool

	// labelled records that writeClass emitted a labelled class, which is
	// what the floor sentence is about.
	labelled bool
	// diagramAt is where the class diagram's fence starts, or -1 when the
	// document has none.
	diagramAt int
}

// newGenerator builds the generator for a schema and its import closure: each
// type's display name and Mermaid id, and the outline with every heading's
// anchor. An anchor is allocated as GitHub allocates it — the heading's slug,
// suffixed -1, -2, … when an earlier heading took it — so two headings that
// slug alike each keep a working link and no schema is refused for its names.
func newGenerator(s schema.Schema, cfg config) *generator {
	g := &generator{
		entry:      s,
		closure:    slices.Collect(s.Closure()),
		declaredIn: map[schema.TypeID]schema.Schema{},
		types:      make(map[schema.TypeID]*typeEntry),
		sources:    s.Sources(),
		contents:   map[location.SourceID][]byte{},
		cfg:        cfg,
		md:         newParser(),
		headings:   newHeadingReader(),
		closedDocs: map[string]closedDoc{},
		diagramAt:  -1,
	}
	mermaidIDs := map[string]bool{}
	for _, sch := range g.closure {
		for t := range sch.Types() {
			g.declaredIn[t.ID()] = sch
			display, displayMD := g.displayName(sch, t)
			g.types[t.ID()] = &typeEntry{
				typ:       t,
				display:   display,
				displayMD: displayMD,
				mermaidID: uniqueMermaidID(mermaidID(display), mermaidIDs),
			}
		}
	}
	g.buildOutline()
	return g
}

// displayName returns how the document names t, as text and as Markdown: the
// bare name for a type the entry schema declares, the entry's own tag
// ("alias.Name") for one it imports directly — the name a data file uses —
// and "Name (schema)" for one it reaches only through another import, which
// has no name a data file can use and so is never written as a tag.
func (g *generator) displayName(sch schema.Schema, t schema.Type) (string, string) {
	if tag, ok := schema.AddressableTag(g.entry, t.ID()); ok {
		return tag, escapeInline(tag)
	}
	name := sch.Name()
	return t.Name() + " (" + name + ")", t.Name() + " (" + escapeInline(name) + ")"
}

// buildOutline lists the document's headings in the order emitDocument writes
// them and allocates each anchor. A section with nothing to say has no entry.
func (g *generator) buildOutline() {
	var alloc anchorAllocator
	add := func(e outlineEntry) {
		e.anchor = alloc.allocate(e.text)
		g.outline = append(g.outline, e)
	}
	entryName := g.entry.Name()
	add(outlineEntry{kind: kindTitle, level: 1, text: "Schema " + entryName, md: "Schema " + escapeInline(entryName)})
	if g.cfg.classDiagram {
		add(outlineEntry{kind: kindClassDiagram, level: 2, text: "Class Diagram", md: "Class Diagram"})
	}
	g.addSchemaTypes(add, g.entry, true)
	for _, sch := range g.closure[1:] {
		name := sch.Name()
		text, md := "Schema "+name, "Schema "+escapeInline(name)
		if alias, ok := g.entry.FindImportAlias(sch.SourceID()); ok {
			text += " (imported as " + alias + ")"
			md += " (imported as " + alias + ")"
		}
		add(outlineEntry{kind: kindImportedSchema, level: 2, text: text, md: md, schema: sch})
		g.addSchemaTypes(add, sch, false)
	}
	for i, h := range g.outline {
		if h.kind == kindTypeSection {
			g.types[h.typ.ID()].anchor = g.outline[i].anchor
		}
	}
}

// addSchemaTypes adds one schema's type sections and its data-type table. The
// entry schema's types sit under a "## Types" heading and its table under
// "## Data Types"; an imported schema's sit directly under its own heading.
func (g *generator) addSchemaTypes(add func(outlineEntry), sch schema.Schema, entry bool) {
	types := slices.Collect(sch.Types())
	if entry && len(types) > 0 {
		add(outlineEntry{kind: kindTypes, level: 2, text: "Types", md: "Types"})
	}
	for _, t := range types {
		e := g.types[t.ID()]
		add(outlineEntry{kind: kindTypeSection, level: 3, text: e.display, md: e.displayMD, typ: t})
	}
	if len(slices.Collect(sch.DataTypes())) > 0 {
		level := 3
		if entry {
			level = 2
		}
		add(outlineEntry{kind: kindDataTypes, level: level, text: "Data Types", md: "Data Types", schema: sch})
	}
}

// anchorAllocator hands out heading anchors the way GitHub does: the slug of
// the heading text, and on a repeat the slug suffixed -1, -2, … — skipping any
// suffixed form an earlier heading already holds.
type anchorAllocator struct {
	occurrences map[string]int
}

func (a *anchorAllocator) allocate(text string) string {
	if a.occurrences == nil {
		a.occurrences = map[string]int{}
	}
	base := slug(text)
	result := base
	for {
		if _, taken := a.occurrences[result]; !taken {
			break
		}
		a.occurrences[base]++
		result = base + "-" + strconv.Itoa(a.occurrences[base])
	}
	a.occurrences[result] = 0
	return result
}

// uniqueMermaidID returns base, or base suffixed _2, _3, … when an earlier
// class took it, and records the result. Class ids are internal to the
// diagram, so two types whose display names sanitize alike stay two classes.
func uniqueMermaidID(base string, taken map[string]bool) string {
	id := base
	for i := 2; taken[id]; i++ {
		id = base + "_" + strconv.Itoa(i)
	}
	taken[id] = true
	return id
}

// link renders a link to e's section and records its anchor for the
// self-check.
func (g *generator) link(e *typeEntry) string {
	g.links = append(g.links, e.anchor)
	return "[" + e.displayMD + "](#" + e.anchor + ")"
}

// resolveSuper finds the closure entry for a declared extends reference by
// resolving it in the schema that declares t, which is the scope the
// reference was written in. Returns false when the reference names no type
// in this document's type map.
func (g *generator) resolveSuper(t schema.Type, ref schema.TypeRef) (*typeEntry, bool) {
	sch, ok := g.declaredIn[t.ID()]
	if !ok {
		return nil, false
	}
	super, ok := sch.ResolveType(ref)
	if !ok {
		return nil, false
	}
	e, ok := g.types[super.ID()]
	return e, ok
}
