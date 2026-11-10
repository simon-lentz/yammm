package markdown

import (
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/schema"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/text"
)

// readCounts counts a generator's Markdown parses and the trees it has an
// HTML5 parser build: each is one render of a whole text and one parse of the
// HTML.
type readCounts struct{ parses, trees int }

type countingMarkdown struct {
	goldmark.Markdown
	counts *readCounts
}

func (m countingMarkdown) Parser() parser.Parser {
	return countingParser{m.Markdown.Parser(), m.counts}
}

type countingParser struct {
	parser.Parser
	counts *readCounts
}

func (p countingParser) Parse(r text.Reader, opts ...parser.ParseOption) ast.Node {
	p.counts.parses++
	return p.Parser.Parse(r, opts...)
}

type countingRenderer struct {
	renderer.Renderer
	counts *readCounts
}

func (r countingRenderer) Render(w io.Writer, src []byte, n ast.Node) error {
	r.counts.trees++
	return r.Renderer.Render(w, src, n)
}

// marshalCounting runs Marshal's steps over s and returns what it read.
func marshalCounting(t *testing.T, s schema.Schema) readCounts {
	t.Helper()
	var counts readCounts
	g := newTestGenerator(t, s)
	g.md = countingMarkdown{g.md, &counts}
	g.headings.renderer = countingRenderer{g.headings.renderer, &counts}
	if _, err := g.marshal(); err != nil {
		t.Fatalf("marshal = %v, want a document", err)
	}
	return counts
}

// typesSource writes n types of ten properties and one relation, each
// property under the doc comment doc and each type under typeDoc. An empty doc
// writes no comment.
func typesSource(n int, typeDoc, doc string) string {
	comment := func(indent, doc string) string {
		if doc == "" {
			return ""
		}
		return indent + "/* " + doc + " */\n"
	}
	var b strings.Builder
	b.WriteString("schema \"bench\"\n\n")
	for i := range n {
		fmt.Fprintf(&b, "%stype T%d {\n%s\tid String primary\n", comment("", typeDoc), i, comment("\t", doc))
		for p := 1; p < 10; p++ {
			fmt.Fprintf(&b, "%s\tp%d String\n", comment("\t", doc), p)
		}
		fmt.Fprintf(&b, "\t--> NEXT (one) T%d\n}\n\n", (i+1)%n)
	}
	return b.String()
}

// TestMarshal_ReadsTheDocumentOnce pins Marshal's cost by what it reads. The
// document is parsed once, for the self-check, whatever its size and whatever
// its description cells hold: no cell is parsed, and no HTML5 tree is built.
// A doc comment written as a block is read alone only when it holds a "<" or
// a run of three backticks or tildes, once per distinct text: one parse to
// find the block it leaves open, one more for each closer tried, and for a "<"
// one parse and one tree for each HTML closer tried, the first try being none.
// The document is parsed a second time, and its tree built, only when a doc
// comment may hold a heading (a "#", an underline, or an h1 to h6 tag) or
// leaves open HTML that no closer closes.
func TestMarshal_ReadsTheDocumentOnce(t *testing.T) {
	t.Parallel()

	const cell = "First line with `code\nacross` a break and a <a\nhref=\"#x\">tag</a>.\n\nA | pipe, a ``` run and a trailing backslash\\\nend."
	for _, tt := range []struct {
		name string
		src  string
		want readCounts
	}{
		{"no doc comment", typesSource(40, "", ""), readCounts{1, 0}},
		{"400 cells of five lines", typesSource(40, "", cell), readCounts{1, 0}},
		{"1,600 cells", typesSource(160, "", cell), readCounts{1, 0}},
		{"a plain block on every type", typesSource(40, "Two lines\nof prose.", ""), readCounts{1, 0}},
		{"a block of two paragraphs and a list on every type", typesSource(40, "One.\n\nTwo, a - b = c:\n\n- x\n- y", ""), readCounts{1, 0}},
		{"one block holding a tag, on every type", typesSource(40, "A <b>bold</b> word.", ""), readCounts{3, 1}},
		{"one block holding a closed fence, on every type", typesSource(40, "```\ncode\n```", ""), readCounts{2, 0}},
		{"one block that leaves a fence open, on every type", typesSource(40, "```\ncode", ""), readCounts{3, 0}},
		{"one block that leaves a comment open, on every type", typesSource(40, "<!-- todo", ""), readCounts{4, 1}},
		{"one block that leaves an attribute open, on every type", typesSource(40, "<div title=\"x", ""), readCounts{6, 4}},
		{"a block holding a heading", typesSource(40, "# Usage", ""), readCounts{2, 1}},
		{"a block holding an underline", typesSource(40, "Usage\n-----", ""), readCounts{2, 1}},
		{"a block holding a number sign", typesSource(40, "See issue #5.", ""), readCounts{2, 1}},
		{"a block holding a heading tag", typesSource(40, "<h2>Usage</h2>", ""), readCounts{4, 2}},
		{"a cell holding a heading tag", typesSource(40, "", "<h2>Usage</h2>"), readCounts{2, 1}},
		{"a cell holding a number sign and an underline", typesSource(40, "", "issue #5\n---"), readCounts{1, 0}},
		{"a block that leaves HTML open that no closer closes", typesSource(40, "<div>\n<select><option x=\"", ""), readCounts{8, 6}},
		{"a heading that moves a type's anchor", "schema \"s\"\n/* # B */\ntype A {\n\tid String primary\n\t--> TO (one) B\n}\ntype B {\n\tid String primary\n}\n", readCounts{2, 1}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := marshalCounting(t, loadSchema(t, tt.src)); got != tt.want {
				t.Errorf("Marshal parsed Markdown %d times and built %d trees, want %d and %d", got.parses, got.trees, tt.want.parses, tt.want.trees)
			}
		})
	}
}

// closureSources writes a closure of five schemas of 150 types: inheritance
// chains ten deep, eight or nine properties, two associations and one
// invariant a type. The entry imports the other four.
func closureSources() map[string][]byte {
	names := []string{"entry", "s1", "s2", "s3", "s4"}
	sources := make(map[string][]byte, len(names))
	for k, name := range names {
		var b strings.Builder
		fmt.Fprintf(&b, "schema \"%s\"\n\n", name)
		if k == 0 {
			for _, other := range names[1:] {
				fmt.Fprintf(&b, "import \"%s.yammm\" as %s\n", other, other)
			}
			b.WriteString("\n")
		}
		for i := range 150 {
			if i%10 == 0 {
				fmt.Fprintf(&b, "type K%dT%d {\n\tid String primary\n", k, i)
			} else {
				fmt.Fprintf(&b, "type K%dT%d extends K%dT%d {\n", k, i, k, i-1)
			}
			for p := range 8 + i%2 {
				fmt.Fprintf(&b, "\tp%d_%d Integer\n", i, p)
			}
			fmt.Fprintf(&b, "\t--> A%d (many) K%dT%d\n", i, k, (i+1)%150)
			fmt.Fprintf(&b, "\t--> B%d (one) K%dT%d\n", i, k, (i+7)%150)
			fmt.Fprintf(&b, "\t! \"p%d_0 is not negative\" p%d_0 == nil || p%d_0 >= 0\n}\n\n", i, i, i)
		}
		sources[name+".yammm"] = []byte(b.String())
	}
	return sources
}

// BenchmarkMarshal measures one Marshal call over schemas of a stated size:
// what it allocates does not depend on the host.
func BenchmarkMarshal(b *testing.B) {
	closure := func(b *testing.B) schema.Schema {
		b.Helper()
		s, res := schema.LoadSourcesWithEntry(b.Context(), closureSources(), "entry.yammm", ".", schema.WithSourcesOnly(true))
		if err := res.Err(); err != nil {
			b.Fatalf("LoadSourcesWithEntry: %v", err)
		}
		return s
	}
	source := func(src string) func(b *testing.B) schema.Schema {
		return func(b *testing.B) schema.Schema {
			b.Helper()
			s, res := schema.LoadString(b.Context(), src, "bench.yammm")
			if err := res.Err(); err != nil {
				b.Fatalf("LoadString: %v", err)
			}
			return s
		}
	}
	cell := func(lines int) string {
		return "schema \"bench\"\n\ntype T {\n\tid String primary\n\t/* " + strings.Repeat("a line of `the` text\n", lines) + "end */\n\tp String\n}\n"
	}
	for _, bm := range []struct {
		name string
		load func(b *testing.B) schema.Schema
		opts []Option
	}{
		{"a closure of 750 types", closure, nil},
		{"100 types of ten properties", source(typesSource(100, "", "")), nil},
		{"800 types of ten properties", source(typesSource(800, "", "")), nil},
		{"800 types of ten properties without class members", source(typesSource(800, "", "")), []Option{WithClassMembers(false)}},
		{"800 types of ten documented properties", source(typesSource(800, "What the type holds.", "What the property holds, in one line.")), nil},
		{"one cell of 1000 lines", source(cell(1000)), nil},
		{"one cell of 16000 lines", source(cell(16000)), nil},
	} {
		b.Run(bm.name, func(b *testing.B) {
			s := bm.load(b)
			b.ReportAllocs()
			for b.Loop() {
				if _, err := Marshal(s, bm.opts...); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
