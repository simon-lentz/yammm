package markdown

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"

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
// self-contained Markdown document: a Mermaid class diagram followed by
// per-type reference sections and data-type tables, with one section per
// imported schema. Output is deterministic — byte-identical across runs
// for the same schema — and structurally verified before return; an error
// reports a generator bug, never broken Markdown. The schema does not need
// source backing: on a Builder-built schema, invariant sections degrade to
// their message line.
func Marshal(s *schema.Schema, opts ...Option) ([]byte, error) {
	cfg := config{classDiagram: true, classMembers: true}
	for _, o := range opts {
		o(&cfg)
	}
	g := newGenerator(s, cfg)
	g.emitDocument()
	moved, err := g.reanchor()
	if err != nil {
		return nil, err
	}
	if moved {
		g.reset()
		g.emitDocument()
	}
	return g.finish()
}

// reanchor allocates every anchor over the headings the parser reads in the
// emitted document, doc-comment headings included, as GitHub allocates them,
// and reports whether an outline anchor moved. A heading in a doc comment is
// invisible to the outline, so it can take an anchor the outline gave a later
// heading; the links written to that heading are then wrong and the document
// is emitted again. Heading text does not depend on anchors, so a second pass
// allocates the same anchors.
func (g *generator) reanchor() (bool, error) {
	read := g.read()
	parsed, err := headings(g.md, read.root, read.src)
	if err != nil {
		return false, fmt.Errorf("markdown: reading the emitted document: %w", err)
	}
	at := make(map[int]int, len(g.outline))
	for i, h := range g.outline {
		at[h.offset] = i
	}
	var alloc anchorAllocator
	moved := false
	for _, p := range parsed {
		anchor := alloc.allocate(p.text)
		if i, ok := at[p.offset]; ok && p.offset >= 0 && g.outline[i].anchor != anchor {
			g.outline[i].anchor = anchor
			moved = true
		}
	}
	for _, h := range g.outline {
		if h.kind == kindTypeSection {
			g.types[h.typ.ID()].anchor = h.anchor
		}
	}
	return moved, nil
}

// read returns the emitted document as the parser reads it, parsed once per
// emission and shared by reanchor and the self-check.
func (g *generator) read() probe {
	if g.parsed == nil {
		p := probeText(g.md, g.buf.Bytes())
		g.parsed = &p
	}
	return *g.parsed
}

// reset discards the emitted document and the state emitting it recorded.
func (g *generator) reset() {
	g.parsed = nil
	g.authored = nil
	g.buf.Reset()
	g.links = nil
	g.tables = nil
	g.labelled = false
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
	text   string         // the heading's text as rendered, which the anchor is derived from
	md     string         // the heading's Markdown source, with inline syntax escaped
	anchor string         // allocated by anchorAllocator in document order
	offset int            // where emitDocument wrote the heading line, for the self-check
	schema *schema.Schema // kindDataTypes, kindImportedSchema
	typ    *schema.Type   // kindTypeSection
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
				g.writeAuthorText(doc, 0)
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
			g.writeTable(g.dataTypeTable(h.schema.DataTypesSlice()), 0)
		case kindImportedSchema:
			g.buf.WriteString("## " + h.md + "\n")
			if doc := h.schema.Documentation(); doc != "" {
				g.buf.WriteString("\n")
				g.writeAuthorText(doc, 0)
				g.buf.WriteString("\n")
			}
		}
	}
}

// dataTypeTable renders the Name | Definition | Description table over a
// schema's named DataTypes.
func (g *generator) dataTypeTable(dts []*schema.DataType) string {
	var b bytes.Buffer
	tw := g.newTable(&b, "Name", "Definition", "Description")
	for _, dt := range dts {
		def := ""
		if c := dt.Constraint(); c != nil {
			def = c.String()
		}
		tw.row(codeCell(dt.Name()), codeCell(def), escapeCell(dt.Documentation()))
	}
	return b.String()
}

// selfCheck reads the emitted document with a CommonMark parser and verifies
// the structure the generator wrote: nothing is left open at the end; every
// outline heading is a top-level heading of its level whose text is the text
// the generator meant, holding the anchor GitHub allocates it; every internal
// link the generator wrote is read as a link to an outline heading; and every
// table it wrote is read as a table of its columns and rows. Doc-comment text
// is the author's Markdown and is not a subject. A failure is a generator bug.
func (g *generator) selfCheck() error {
	doc := g.read()
	if _, open := doc.open(); open {
		return errors.New("markdown: self-check: the document ends inside an open block")
	}
	root, out := doc.root, doc.src
	type allocated struct {
		heading parsedHeading
		anchor  string
	}
	at := map[int]allocated{}
	var alloc anchorAllocator
	parsed, err := headings(g.md, root, out)
	if err != nil {
		return fmt.Errorf("markdown: self-check: %w", err)
	}
	for _, p := range parsed {
		anchor := alloc.allocate(p.text)
		if p.offset >= 0 {
			at[p.offset] = allocated{p, anchor}
		}
	}
	anchors := make(map[string]bool, len(g.outline))
	for _, h := range g.outline {
		got, ok := at[h.offset]
		switch {
		case !ok || !got.heading.topLevel:
			return fmt.Errorf("markdown: self-check: heading %q is not read as a top-level heading", h.text)
		case got.heading.level != h.level || got.heading.text != h.text:
			return fmt.Errorf("markdown: self-check: heading %q is read as level %d %q", h.text, got.heading.level, got.heading.text)
		case got.anchor != h.anchor:
			return fmt.Errorf("markdown: self-check: heading %q takes anchor #%s where the generator gave it #%s", h.text, got.anchor, h.anchor)
		}
		anchors[h.anchor] = true
	}
	written := map[string]int{}
	for _, a := range g.links {
		written[a]++
	}
	read, err := linkTargets(root, g.inAuthorText)
	if err != nil {
		return fmt.Errorf("markdown: self-check: %w", err)
	}
	for a, n := range read {
		if written[a] == 0 {
			return fmt.Errorf("markdown: self-check: internal link #%s is read %d times outside doc comments and written by no emitter", a, n)
		}
	}
	for a, n := range written {
		if !anchors[a] {
			return fmt.Errorf("markdown: self-check: internal link #%s resolves to no emitted heading", a)
		}
		if read[a] != n {
			return fmt.Errorf("markdown: self-check: internal link #%s is written %d times and read %d", a, n, read[a])
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
		got, ok := shapes[tw.offset]
		if !ok {
			return errors.New("markdown: self-check: a table the generator wrote is not read as a table")
		}
		if got.cols != tw.cols || got.rows != tw.rows {
			return fmt.Errorf("markdown: self-check: a table written with %d columns and %d rows is read with %d and %d", tw.cols, tw.rows, got.cols, got.rows)
		}
	}
	return nil
}

// writeAuthorText writes a doc comment, indented by indent, with the block it
// leaves open closed.
func (g *generator) writeAuthorText(doc string, indent int) {
	text := closeAuthorText(g.md, doc)
	if indent > 0 {
		text = indentUnderBullet(text)
	}
	start := g.buf.Len()
	g.buf.WriteString(text)
	g.authored = append(g.authored, span{start: start, end: g.buf.Len()})
}

// span is a byte range of the emitted document.
type span struct{ start, end int }

// inAuthorText reports whether pos falls inside a doc comment the generator
// wrote.
func (g *generator) inAuthorText(pos int) bool {
	for _, s := range g.authored {
		if pos >= s.start && pos < s.end {
			return true
		}
	}
	return false
}

// writeTable writes text, which holds the one table last built by newTable,
// indented by indent, and records where the table starts.
func (g *generator) writeTable(text string, indent int) {
	start := g.buf.Len()
	if indent > 0 {
		text = indentUnderBullet(text)
	}
	g.buf.WriteString(text)
	for _, tw := range g.tables {
		if tw.offset < 0 {
			tw.offset = start
		}
	}
}

// tableWriter writes one table and records its shape: its column and row
// counts, the offset writeTable placed it at, and a row whose cell count
// differs from its header's, which the self-check reports. The parser cannot
// see such a row, because GitHub's tables drop a surplus cell and fill a
// missing one.
type tableWriter struct {
	b      *bytes.Buffer
	cols   int
	rows   int
	offset int
	bad    string
}

// newTable writes a table's header and separator rows and returns the writer
// for its body rows.
func (g *generator) newTable(b *bytes.Buffer, cols ...string) *tableWriter {
	tw := &tableWriter{b: b, cols: len(cols), offset: -1}
	g.tables = append(g.tables, tw)
	writeTableRow(b, cols...)
	sep := make([]string, len(cols))
	for i := range sep {
		sep[i] = "---"
	}
	writeTableRow(b, sep...)
	return tw
}

func (tw *tableWriter) row(cells ...string) {
	if len(cells) != tw.cols && tw.bad == "" {
		tw.bad = fmt.Sprintf("table row has %d cells, its header %d", len(cells), tw.cols)
	}
	tw.rows++
	writeTableRow(tw.b, cells...)
}

// typeEntry records how one type in the closure is addressed in the emitted
// document: its display text, the anchor its heading takes, and its Mermaid
// class id.
type typeEntry struct {
	typ       *schema.Type
	display   string // what a reader sees: see displayName
	displayMD string // display with inline Markdown syntax escaped
	anchor    string
	mermaidID string
}

// generator accumulates the emitted document and the cross-referencing
// state the emitters and the self-check share.
type generator struct {
	buf        bytes.Buffer
	entry      *schema.Schema
	closure    []*schema.Schema
	declaredIn map[schema.TypeID]*schema.Schema // each type's declaring schema
	types      map[schema.TypeID]*typeEntry
	outline    []outlineEntry
	sources    *schema.Sources
	cfg        config

	md       goldmark.Markdown // the parser the self-check and the closing of doc comments read with
	parsed   *probe            // the emitted document as md reads it; nil until read
	links    []string          // the anchor of every internal link emitted
	authored []span            // every doc comment written, in document order
	tables   []*tableWriter    // every table emitted

	// labelled records that writeClass emitted a labelled class, which is
	// what the floor sentence is about.
	labelled bool
}

// newGenerator builds the generator for a schema and its import closure: each
// type's display name and Mermaid id, and the outline with every heading's
// anchor. An anchor is allocated as GitHub allocates it — the heading's slug,
// suffixed -1, -2, … when an earlier heading took it — so two headings that
// slug alike each keep a working link and no schema is refused for its names.
func newGenerator(s *schema.Schema, cfg config) *generator {
	g := &generator{
		entry:      s,
		closure:    s.Closure(),
		declaredIn: map[schema.TypeID]*schema.Schema{},
		types:      make(map[schema.TypeID]*typeEntry),
		sources:    s.Sources(),
		cfg:        cfg,
		md:         newParser(),
	}
	mermaidIDs := map[string]bool{}
	for _, sch := range g.closure {
		for _, t := range sch.TypesSlice() {
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
func (g *generator) displayName(sch *schema.Schema, t *schema.Type) (string, string) {
	if tag, ok := schema.AddressableTag(g.entry, t.ID()); ok {
		return tag, tag
	}
	name := printableName(sch.Name())
	return t.Name() + " (" + name + ")", t.Name() + " (" + escapeInline(name) + ")"
}

// buildOutline lists the document's headings in the order emitDocument writes
// them and allocates each anchor. A section with nothing to say has no entry.
func (g *generator) buildOutline() {
	var alloc anchorAllocator
	add := func(e outlineEntry) {
		e.md = keepTrailingSpaces(e.md)
		e.anchor = alloc.allocate(e.text)
		g.outline = append(g.outline, e)
	}
	entryName := printableName(g.entry.Name())
	add(outlineEntry{kind: kindTitle, level: 1, text: "Schema " + entryName, md: "Schema " + escapeInline(entryName)})
	if g.cfg.classDiagram {
		add(outlineEntry{kind: kindClassDiagram, level: 2, text: "Class Diagram", md: "Class Diagram"})
	}
	g.addSchemaTypes(add, g.entry, true)
	for _, sch := range g.closure[1:] {
		name := printableName(sch.Name())
		text, md := "Schema "+name, "Schema "+escapeInline(name)
		if alias := g.entry.FindImportAlias(sch.SourceID()); alias != "" {
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
func (g *generator) addSchemaTypes(add func(outlineEntry), sch *schema.Schema, entry bool) {
	types := sch.TypesSlice()
	if entry && len(types) > 0 {
		add(outlineEntry{kind: kindTypes, level: 2, text: "Types", md: "Types"})
	}
	for _, t := range types {
		e := g.types[t.ID()]
		add(outlineEntry{kind: kindTypeSection, level: 3, text: e.display, md: e.displayMD, typ: t})
	}
	if len(sch.DataTypesSlice()) > 0 {
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
func (g *generator) resolveSuper(t *schema.Type, ref schema.TypeRef) (*typeEntry, bool) {
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
