package markdown

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/simon-lentz/yammm/schema"
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
	return g.finish()
}

// finish runs the self-check over the emitted document and returns it. Marshal
// returns only what finish returns, so the check cannot be bypassed without
// losing the output.
func (g *generator) finish() ([]byte, error) {
	out := g.buf.Bytes()
	if err := g.selfCheck(out); err != nil {
		return nil, err
	}
	return out, nil
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
			g.buf.WriteString(g.dataTypeTable(h.schema.DataTypesSlice()))
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

// selfCheck verifies the structure the generator itself wrote: every code
// fence closes and no outline heading sits inside one, every internal link the
// generator emitted targets an anchor of the outline, and every table row it
// wrote has its header's column count on one line. Author text is not a
// subject: its fences are sealed where it is written, and a link, a table or a
// "#" line inside it is the author's own Markdown. A failure is a generator
// bug.
func (g *generator) selfCheck(out []byte) error {
	anchors := make(map[string]bool, len(g.outline))
	headings := make(map[int]bool, len(g.outline))
	for _, h := range g.outline {
		anchors[h.anchor] = true
		headings[h.offset] = true
	}
	if err := checkFences(out, headings, g.authored); err != nil {
		return err
	}
	for _, a := range g.links {
		if !anchors[a] {
			return fmt.Errorf("markdown: self-check: internal link #%s resolves to no emitted heading", a)
		}
	}
	for _, tw := range g.tables {
		if tw.bad != "" {
			return fmt.Errorf("markdown: self-check: %s", tw.bad)
		}
	}
	return nil
}

// authorSpan is where a doc comment was written into the document, and the
// indent of the container that holds it: 0 at the top level, bulletIndent
// under a relation's or an invariant's bullet.
type authorSpan struct {
	start, end int
	indent     int
}

// writeAuthorText writes a doc comment, sealed and indented by indent, and
// records its span so the self-check reads its lines relative to their
// container, as a Markdown parser does.
func (g *generator) writeAuthorText(doc string, indent int) {
	text := sealFences(doc)
	if indent > 0 {
		text = indentUnderBullet(text)
	}
	start := g.buf.Len()
	g.buf.WriteString(text)
	g.authored = append(g.authored, authorSpan{start: start, end: g.buf.Len(), indent: indent})
}

// checkFences verifies that every opened code fence closes and that no
// outline heading, named by the offset its line starts at, sits inside an open
// fence — the symptom of a fence swallowing the rest of the document. A "#"
// line inside an author's own fence is that fence's content. A line inside an
// authored span is read relative to that span's container indent.
func checkFences(out []byte, headings map[int]bool, authored []authorSpan) error {
	var f fenceScanner
	offset, next := 0, 0
	for line := range strings.SplitSeq(string(out), "\n") {
		if f.open() && headings[offset] {
			return fmt.Errorf("markdown: self-check: heading %q inside an open code fence", line)
		}
		for next < len(authored) && authored[next].end <= offset {
			next++
		}
		indent := 0
		if next < len(authored) && authored[next].start <= offset {
			indent = authored[next].indent
		}
		f.scan(strings.TrimPrefix(line, strings.Repeat(" ", indent)))
		offset += len(line) + 1
	}
	if f.open() {
		return errors.New("markdown: self-check: unclosed code fence at end of document")
	}
	return nil
}

// fenceScanner tracks code-fence state line by line under CommonMark's fence
// rules. A fence opens on a run of three or more backticks or tildes indented
// at most three spaces; a backtick fence's info string holds no backtick. It
// closes on a run of the same character at least as long, indented at most
// three spaces, with nothing after it but spaces and tabs. checkFences and
// sealFences share it, so text sealFences has closed never trips checkFences.
// The scanner reads fences alone: a fence inside an HTML block or inside a list
// the author wrote is read as if it stood at the top level.
type fenceScanner struct {
	char   byte
	run    int
	indent int // the opener's indent, which the seal repeats
}

func (f *fenceScanner) open() bool { return f.run > 0 }

func (f *fenceScanner) scan(line string) {
	trimmed := strings.TrimLeft(line, " ")
	indent := len(line) - len(trimmed)
	if indent > 3 || trimmed == "" || (trimmed[0] != '`' && trimmed[0] != '~') {
		return
	}
	c := trimmed[0]
	run := 0
	for run < len(trimmed) && trimmed[run] == c {
		run++
	}
	rest := trimmed[run:]
	switch {
	case !f.open() && run >= 3 && (c == '~' || !strings.ContainsRune(rest, '`')):
		f.char, f.run, f.indent = c, run, indent
	case f.open() && c == f.char && run >= f.run && strings.Trim(rest, " \t") == "":
		f.run = 0
	}
}

// sealFences closes a code fence author text leaves open, so a doc comment
// cannot swallow the rest of the document. The closing line repeats the
// opener's indent and run, the shortest closer CommonMark accepts for it.
func sealFences(text string) string {
	var f fenceScanner
	for line := range strings.SplitSeq(text, "\n") {
		f.scan(line)
	}
	if !f.open() {
		return text
	}
	return text + "\n" + strings.Repeat(" ", f.indent) + strings.Repeat(string(f.char), f.run)
}

// tableWriter writes one table and records a row whose cell count differs
// from its header's, or whose cell holds a line break and so splits the row,
// which the self-check reports.
type tableWriter struct {
	b    *bytes.Buffer
	cols int
	bad  string
}

// newTable writes a table's header and separator rows and returns the writer
// for its body rows.
func (g *generator) newTable(b *bytes.Buffer, cols ...string) *tableWriter {
	tw := &tableWriter{b: b, cols: len(cols)}
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
	if tw.bad == "" {
		if len(cells) != tw.cols {
			tw.bad = fmt.Sprintf("table row has %d cells, its header %d", len(cells), tw.cols)
		} else if i := slices.IndexFunc(cells, func(c string) bool { return strings.ContainsAny(c, "\r\n") }); i >= 0 {
			tw.bad = fmt.Sprintf("table cell %q holds a line break", cells[i])
		}
	}
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

	links    []string       // the anchor of every internal link emitted
	tables   []*tableWriter // every table emitted
	authored []authorSpan   // every doc comment written, in document order

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
