package markdown

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// Option configures Marshal.
type Option func(*config)

type config struct {
	classDiagram bool
}

// WithClassDiagram toggles the Mermaid class-diagram section (default true).
func WithClassDiagram(include bool) Option {
	return func(c *config) {
		c.classDiagram = include
	}
}

// Marshal renders a schema and its transitive import closure as one
// self-contained Markdown document: a Mermaid class diagram followed by
// per-type reference sections and data-type tables, with one section per
// imported schema. Output is deterministic — byte-identical across runs
// for the same schema — and structurally verified before return; an error
// reports a generator bug or unusable input, never broken Markdown. The
// schema does not need source backing: on a Builder-built schema,
// invariant sections degrade to their message line.
func Marshal(s *schema.Schema, opts ...Option) ([]byte, error) {
	cfg := config{classDiagram: true}
	for _, o := range opts {
		o(&cfg)
	}
	g, err := newGenerator(s)
	if err != nil {
		return nil, err
	}
	g.emitDocument(cfg)
	out := g.buf.Bytes()
	if err := selfCheck(out, g.anchors); err != nil {
		return nil, err
	}
	return out, nil
}

// emitDocument writes the full document skeleton: title, schema
// documentation, class diagram, the entry schema's type sections and
// data-type table, then one section per imported schema in closure order.
// Sections with nothing to say are omitted entirely.
func (g *generator) emitDocument(cfg config) {
	title := "Schema " + g.entry.Name()
	g.anchors[slug(title)] = true
	g.buf.WriteString("# " + title + "\n")

	if doc := g.entry.Documentation(); doc != "" {
		g.buf.WriteString("\n" + doc + "\n")
	}
	if cfg.classDiagram {
		g.buf.WriteString("\n")
		g.emitClassDiagram()
	}
	if types := g.entry.TypesSlice(); len(types) > 0 {
		g.anchors["types"] = true
		g.buf.WriteString("\n## Types\n")
		for _, t := range types {
			g.buf.WriteString("\n")
			g.emitTypeSection(t)
		}
	}
	if dts := g.entry.DataTypesSlice(); len(dts) > 0 {
		g.anchors["data-types"] = true
		g.buf.WriteString("\n## Data Types\n\n")
		g.buf.WriteString(dataTypeTable(dts))
	}

	for _, sch := range g.closure[1:] {
		heading := "Schema " + sch.Name()
		if alias := g.entry.FindImportAlias(sch.SourceID()); alias != "" {
			heading += " (imported as " + alias + ")"
		}
		g.anchors[slug(heading)] = true
		g.buf.WriteString("\n## " + heading + "\n")
		if doc := sch.Documentation(); doc != "" {
			g.buf.WriteString("\n" + doc + "\n")
		}
		for _, t := range sch.TypesSlice() {
			g.buf.WriteString("\n")
			g.emitTypeSection(t)
		}
		if dts := sch.DataTypesSlice(); len(dts) > 0 {
			g.anchors["data-types"] = true
			g.buf.WriteString("\n### Data Types\n\n")
			g.buf.WriteString(dataTypeTable(dts))
		}
	}
}

// dataTypeTable renders the Name | Definition | Description table over a
// schema's named DataTypes.
func dataTypeTable(dts []*schema.DataType) string {
	var b bytes.Buffer
	writeTableHeader(&b, "Name", "Definition", "Description")
	for _, dt := range dts {
		def := ""
		if c := dt.Constraint(); c != nil {
			def = c.String()
		}
		writeTableRow(&b, codeCell(dt.Name()), codeCell(def), escapeCell(dt.Documentation()))
	}
	return b.String()
}

// selfCheck structurally verifies emitted output before Marshal returns:
// code fences balance and no heading opens inside one, every internal link
// resolves to an emitted heading's anchor, and every table separator row
// matches its header's column count. A failure is a generator bug
// surfaced as an error, never emitted output.
func selfCheck(out []byte, anchors map[string]bool) error {
	if err := checkFences(out); err != nil {
		return err
	}
	if err := checkLinks(out, anchors); err != nil {
		return err
	}
	return checkTables(out)
}

// checkFences verifies that every opened code fence closes (a closing
// fence needs at least the opening run's backtick count) and that no
// column-zero heading line appears while a fence is open — the symptom of
// a fence swallowing the rest of the document.
func checkFences(out []byte) error {
	inFence := false
	openRun := 0
	for line := range strings.SplitSeq(string(out), "\n") {
		trimmed := strings.TrimLeft(line, " ")
		run := 0
		for run < len(trimmed) && trimmed[run] == '`' {
			run++
		}
		if !inFence {
			if run >= 3 {
				inFence = true
				openRun = run
			}
			continue
		}
		if run >= openRun && strings.TrimRight(trimmed, "`") == "" {
			inFence = false
			continue
		}
		if strings.HasPrefix(line, "#") {
			return fmt.Errorf("markdown: self-check: heading %q inside an open code fence", line)
		}
	}
	if inFence {
		return errors.New("markdown: self-check: unclosed code fence at end of document")
	}
	return nil
}

// checkLinks verifies every emitted internal link targets an anchor
// registered when its heading was written — the analog of jschema's $ref
// audit, catching slug-rule drift between link and heading emission.
func checkLinks(out []byte, anchors map[string]bool) error {
	doc := string(out)
	for i := 0; ; {
		start := strings.Index(doc[i:], "](#")
		if start < 0 {
			return nil
		}
		start += i + len("](#")
		end := strings.IndexByte(doc[start:], ')')
		if end < 0 {
			return fmt.Errorf("markdown: self-check: unterminated internal link at byte %d", start)
		}
		anchor := doc[start : start+end]
		if !anchors[anchor] {
			return fmt.Errorf("markdown: self-check: internal link #%s resolves to no emitted heading", anchor)
		}
		i = start + end
	}
}

// checkTables verifies each table separator row declares the same column
// count as the header row above it.
func checkTables(out []byte) error {
	lines := strings.Split(string(out), "\n")
	for i := 1; i < len(lines); i++ {
		if !isSeparatorRow(lines[i]) {
			continue
		}
		header := strings.TrimLeft(lines[i-1], " ")
		if !strings.HasPrefix(header, "|") {
			return fmt.Errorf("markdown: self-check: table separator %q has no header row", lines[i])
		}
		if h, s := countCells(header), countCells(strings.TrimLeft(lines[i], " ")); h != s {
			return fmt.Errorf("markdown: self-check: table separator declares %d columns, header %q has %d", s, header, h)
		}
	}
	return nil
}

// isSeparatorRow reports whether the line is a table separator: pipes
// delimiting cells that contain only hyphens.
func isSeparatorRow(line string) bool {
	trimmed := strings.TrimLeft(line, " ")
	if !strings.HasPrefix(trimmed, "|") {
		return false
	}
	cells := strings.SplitSeq(strings.Trim(trimmed, "|"), "|")
	for cell := range cells {
		c := strings.TrimSpace(cell)
		if c == "" || strings.Trim(c, "-") != "" {
			return false
		}
	}
	return true
}

// countCells counts a table row's cells: unescaped pipe delimiters minus
// the trailing edge.
func countCells(row string) int {
	pipes := 0
	for i := range len(row) {
		if row[i] == '|' && (i == 0 || row[i-1] != '\\') {
			pipes++
		}
	}
	return pipes - 1
}

// typeEntry records how one type in the closure is addressed in the emitted
// document: the display name used for its heading and in links to it (bare
// for entry-schema types, schema-qualified for imported ones), and the
// anchor that heading produces.
type typeEntry struct {
	typ     *schema.Type
	display string
	anchor  string
}

// generator accumulates the emitted document and the cross-referencing
// state the emitters and the self-check share: the closure-wide type index,
// the set of anchors emitted so far, and the source registry used to
// extract invariant declaration text.
type generator struct {
	buf     bytes.Buffer
	entry   *schema.Schema
	closure []*schema.Schema
	types   map[schema.TypeID]*typeEntry
	anchors map[string]bool
	sources *schema.Sources
}

// newGenerator builds the generator for a schema and its import closure.
// Entry-schema types display under their bare name; every imported type
// displays schema-qualified. Display names are unique across the closure, but
// slug normalization strips the dots that qualify a name, so two distinct
// displays can still collapse to one heading anchor. Every internal link
// targets a type anchor, so such a collision would silently resolve a link to
// the wrong type's section; it is rejected here instead, mirroring
// adapter/jschema's $defs key-collision guard. (Section anchors like the
// duplicated per-schema "data-types" are intentionally excluded — nothing
// links to them.)
func newGenerator(s *schema.Schema) (*generator, error) {
	g := &generator{
		entry:   s,
		closure: closureSchemas(s),
		types:   make(map[schema.TypeID]*typeEntry),
		anchors: make(map[string]bool),
		sources: s.Sources(),
	}
	anchorOwner := make(map[string]string) // anchor -> first claiming display name
	for i, sch := range g.closure {
		for _, t := range sch.TypesSlice() {
			display := t.Name()
			if i > 0 {
				display = sch.Name() + "." + t.Name()
			}
			anchor := slug(display)
			if first, taken := anchorOwner[anchor]; taken {
				return nil, fmt.Errorf(
					"markdown: type headings %q and %q both anchor to #%s; rename one type",
					first, display, anchor,
				)
			}
			anchorOwner[anchor] = display
			g.types[t.ID()] = &typeEntry{typ: t, display: display, anchor: anchor}
		}
	}
	return g, nil
}

// resolveSuper finds the closure entry for a declared extends reference by
// matching it against the type's resolved supertype linearization. Returns
// false when the reference never resolved (a deferred import).
func (g *generator) resolveSuper(t *schema.Type, ref schema.TypeRef) (*typeEntry, bool) {
	for _, super := range t.SuperTypesSlice() {
		if super.Ref().String() != ref.String() {
			continue
		}
		if e, ok := g.types[super.ID()]; ok {
			return e, true
		}
	}
	return nil, false
}

// closureSchemas returns the schema plus its transitive import closure in
// deterministic breadth-first order, entry first, deduplicated by source.
func closureSchemas(s *schema.Schema) []*schema.Schema {
	var out []*schema.Schema
	seen := map[location.SourceID]bool{}
	queue := []*schema.Schema{s}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if cur == nil || seen[cur.SourceID()] {
			continue
		}
		seen[cur.SourceID()] = true
		out = append(out, cur)
		for _, imp := range cur.ImportsSlice() {
			if dep := imp.Schema(); dep != nil {
				queue = append(queue, dep)
			}
		}
	}
	return out
}
