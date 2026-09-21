package markdown

import (
	"bytes"
	"context"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/internal/yammmtest"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/schema/expr"
)

// loadTestdata loads a corpus schema from testdata, failing on any
// diagnostic error.
func loadTestdata(t *testing.T, name string) *schema.Schema {
	t.Helper()
	yammmtest.RequireNoModuleRoot(t, schema.FindModuleRoot)
	path, err := filepath.Abs(filepath.Join("testdata", name+".yammm"))
	if err != nil {
		t.Fatalf("abs path: %v", err)
	}
	s, res := schema.Load(context.Background(), path)
	if res.HasErrors() {
		t.Fatalf("load %s: %v", name, res.Err())
	}
	return s
}

// TestMarshal_Golden renders every corpus schema and compares byte-exact
// against its .md.golden. The corpus collectively covers the document
// skeleton, the class diagram, badges, flattened tables with inherited-row
// markers, relation lists with edge-property sub-tables, invariant fences,
// data-type tables, import sections, escaping, and the diagram option.
func TestMarshal_Golden(t *testing.T) {
	cases := []struct {
		name string
		opts []Option
	}{
		{name: "scalars"},
		{name: "named"},
		{name: "relations"},
		{name: "edge_props"},
		{name: "inheritance"},
		{name: "abstract"},
		{name: "imports/main"},
		{name: "invariants"},
		{name: "docs"},
		{name: "escaping"},
		{name: "no_diagram", opts: []Option{WithClassDiagram(false)}},
		{name: "no_members", opts: []Option{WithClassMembers(false)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := loadTestdata(t, tc.name)
			got, err := Marshal(s, tc.opts...)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			yammmtest.Golden(t, tc.name+".md", got)
		})
	}
}

// TestMarshal_Deterministic pins byte-identical output across repeated
// renders of the same loaded schema — the property the golden corpus and
// consumer-side regenerate-and-diff flows rely on.
func TestMarshal_Deterministic(t *testing.T) {
	t.Parallel()

	s := loadTestdata(t, "imports/main")
	first, err := Marshal(s)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	for range 3 {
		next, err := Marshal(s)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		if !bytes.Equal(first, next) {
			t.Fatal("repeated Marshal produced different bytes")
		}
	}
}

// TestMarshal_ClassDiagramOption pins that WithClassDiagram(false) omits
// the section and its fence entirely, and that the default includes both.
func TestMarshal_ClassDiagramOption(t *testing.T) {
	t.Parallel()

	s := loadTestdata(t, "relations")
	withDiagram, err := Marshal(s)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !strings.Contains(string(withDiagram), "## Class Diagram") ||
		!strings.Contains(string(withDiagram), "```mermaid") {
		t.Errorf("default output lacks the class-diagram section:\n%s", withDiagram)
	}

	without, err := Marshal(s, WithClassDiagram(false))
	if err != nil {
		t.Fatalf("Marshal(WithClassDiagram(false)): %v", err)
	}
	if strings.Contains(string(without), "## Class Diagram") ||
		strings.Contains(string(without), "```mermaid") {
		t.Errorf("WithClassDiagram(false) output still contains the diagram section:\n%s", without)
	}
}

// TestMarshal_ClassMembersOption pins that WithClassMembers(false) keeps the
// diagram, its classes, stereotypes and edges, and drops only the member
// lines — and that the default keeps them.
func TestMarshal_ClassMembersOption(t *testing.T) {
	t.Parallel()

	s := loadTestdata(t, "no_members")
	withMembers, err := Marshal(s)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !strings.Contains(string(withMembers), "        code String\n") {
		t.Errorf("default output lacks the member line:\n%s", withMembers)
	}

	without, err := Marshal(s, WithClassMembers(false))
	if err != nil {
		t.Fatalf("Marshal(WithClassMembers(false)): %v", err)
	}
	got := string(without)
	for _, want := range []string{
		"## Class Diagram", "```mermaid",
		"    class Entity {\n        <<Abstract>>\n    }\n",
		"    class Part {\n        <<Part>>\n    }\n",
		"    class Landmark\n",
		"    Entity <|-- Landmark\n",
		"    Landmark --> Landmark : NEAR (many)\n",
		"    Landmark *-- Part : PARTS (many)\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("WithClassMembers(false) output lacks %q:\n%s", want, got)
		}
	}
	for _, absent := range []string{"        code String\n", "        id UUID\n", "        label String\n"} {
		if strings.Contains(got, absent) {
			t.Errorf("WithClassMembers(false) output still carries the member line %q:\n%s", absent, got)
		}
	}
}

// TestMarshal_NonSourceBacked pins that Marshal accepts a Builder-built
// schema with no source content: invariants degrade to message-only and no
// source fence is emitted.
func TestMarshal_NonSourceBacked(t *testing.T) {
	t.Parallel()

	s, res := schema.NewBuilder().WithName("built").
		AddType("Other").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		Done().
		AddType("Thing").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithRelation("POINTS_AT", schema.NewTypeRef("", "Other", location.Span{}), true, false).
		WithInvariant("always true", expr.NewLiteral(true), "").
		Done().
		Build()
	if err := res.Err(); err != nil {
		t.Fatalf("Build: %v", err)
	}

	out, err := Marshal(s)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	doc := string(out)
	if !strings.Contains(doc, "# Schema built") || !strings.Contains(doc, "### Thing") {
		t.Errorf("document lacks expected skeleton:\n%s", doc)
	}
	if !strings.Contains(doc, "- \"always true\"") {
		t.Errorf("invariant message missing:\n%s", doc)
	}
	if strings.Contains(doc, "```yammm") {
		t.Errorf("source fence emitted for a non-source-backed schema:\n%s", doc)
	}
	if !strings.Contains(doc, "- `--> POINTS_AT (_)` [Other](#") {
		t.Errorf("relation target not rendered as a link:\n%s", doc)
	}
}

// TestMarshal_SlugCollisionTakesGitHubSuffix pins that two type headings
// whose display names slug alike each keep a working link: GitHub gives the
// second heading the suffix -1, and the link to it says so. Here entry
// "County" and imported "co.Unty" both slug to "county".
func TestMarshal_SlugCollisionTakesGitHubSuffix(t *testing.T) {
	t.Parallel()

	s := loadSources(t, map[string][]byte{
		"entry.yammm": []byte(`schema "geo"

import "co.yammm" as co

type County {
	id String primary
	--> PAIRED (one) co.Unty
}
`),
		"co.yammm": []byte(`schema "co"

type Unty {
	id String primary
}
`),
	})

	out, err := Marshal(s)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	doc := string(out)
	if !strings.Contains(doc, "[co.Unty](#county-1)") {
		t.Errorf("link to co.Unty does not target #county-1:\n%s", doc)
	}
}

// TestSelfCheck drives the structural guard over the state the generator
// records, through finish, which is the one path to Marshal's output.
func TestSelfCheck(t *testing.T) {
	t.Parallel()

	s := loadSchema(t, `schema "people"

type Person {
	id UUID primary
}
`)
	fresh := func() *generator {
		g := newTestGenerator(t, s)
		g.emitDocument()
		return g
	}

	t.Run("emitted document passes", func(t *testing.T) {
		t.Parallel()
		if _, err := fresh().finish(); err != nil {
			t.Errorf("finish = %v, want nil", err)
		}
	})

	t.Run("link to an anchor no heading holds fails", func(t *testing.T) {
		t.Parallel()
		g := fresh()
		g.links = append(g.links, "ghost")
		if _, err := g.finish(); err == nil {
			t.Error("finish = nil, want unresolved-link error")
		}
	})

	t.Run("unclosed generator fence fails", func(t *testing.T) {
		t.Parallel()
		g := fresh()
		g.buf.WriteString("```mermaid\nclassDiagram\n")
		if _, err := g.finish(); err == nil {
			t.Error("finish = nil, want unclosed-fence error")
		}
	})

	t.Run("heading inside an open fence fails", func(t *testing.T) {
		t.Parallel()
		g := fresh()
		g.buf.WriteString("```mermaid\n")
		g.outline = append(g.outline, outlineEntry{anchor: "swallowed", offset: g.buf.Len()})
		g.buf.WriteString("## Swallowed\n```\n")
		if _, err := g.finish(); err == nil {
			t.Error("finish = nil, want heading-inside-fence error")
		}
	})

	t.Run("table cell holding a line break fails", func(t *testing.T) {
		t.Parallel()
		g := fresh()
		var b bytes.Buffer
		g.newTable(&b, "A", "B").row("1", "2\n3")
		g.buf.WriteString("\n")
		g.writeTable(b.String(), 0)
		if _, err := g.finish(); err == nil || !strings.Contains(err.Error(), "is read with") {
			t.Errorf("finish = %v, want the split row refused", err)
		}
	})

	t.Run("a doc comment's link does not stand in for a generator link", func(t *testing.T) {
		t.Parallel()
		g := newTestGenerator(t, loadSchema(t, "schema \"s\"\ntype A {\n  id String primary\n  --> TO (one) B\n}\n/* see [b](#b) */\ntype B {\n  id String primary\n}\n"))
		g.emitDocument()
		before, after, ok := strings.Cut(g.buf.String(), "[B](#b)")
		if !ok {
			t.Fatalf("no generator link to B:\n%s", g.buf.String())
		}
		g.buf.Reset()
		g.buf.WriteString(before + "[B]#b()" + after)
		if _, err := g.finish(); err == nil || !strings.Contains(err.Error(), "read 0") {
			t.Errorf("finish = %v, want the lost link refused", err)
		}
	})

	t.Run("a table the generator never wrote fails", func(t *testing.T) {
		t.Parallel()
		g := fresh()
		var b bytes.Buffer
		g.newTable(&b, "A", "B").row("1", "2")
		if _, err := g.finish(); err == nil || !strings.Contains(err.Error(), "not read as a table") {
			t.Errorf("finish = %v, want the unwritten table refused", err)
		}
	})

	t.Run("a heading whose text does not render as written fails", func(t *testing.T) {
		t.Parallel()
		g := newTestGenerator(t, s)
		g.outline[0].md = "Schema *people"
		g.emitDocument()
		if _, err := g.finish(); err == nil || !strings.Contains(err.Error(), "is read as") {
			t.Errorf("finish = %v, want the heading's text refused", err)
		}
	})

	t.Run("a link the generator wrote to a missing anchor fails", func(t *testing.T) {
		t.Parallel()
		g := newTestGenerator(t, loadSchema(t, "schema \"s\"\ntype A {\n  id String primary\n  --> TO (one) B\n}\ntype B {\n  id String primary\n}\n"))
		g.types[findType(t, g, "B").ID()].anchor = "ghost"
		g.emitDocument()
		if _, err := g.finish(); err == nil || !strings.Contains(err.Error(), "#ghost") {
			t.Errorf("finish = %v, want the link to #ghost refused", err)
		}
	})

	t.Run("a heading the generator wrote inside its own fence fails", func(t *testing.T) {
		t.Parallel()
		g := newTestGenerator(t, s)
		g.outline[0].md += "\n```"
		g.emitDocument()
		if _, err := g.finish(); err == nil || !strings.Contains(err.Error(), "not read as a top-level heading") {
			t.Errorf("finish = %v, want the swallowed heading refused", err)
		}
	})

	t.Run("table row of the wrong width fails", func(t *testing.T) {
		t.Parallel()
		g := fresh()
		var b bytes.Buffer
		g.newTable(&b, "A", "B", "C").row("1", "2")
		g.buf.WriteString("\n")
		g.writeTable(b.String(), 0)
		if _, err := g.finish(); err == nil || !strings.Contains(err.Error(), "has 2 cells") {
			t.Errorf("finish = %v, want the short row refused although the parser fills it", err)
		}
	})

	t.Run("an outline heading inside a container fails", func(t *testing.T) {
		t.Parallel()
		g := fresh()
		doc := g.buf.String()
		g.buf.Reset()
		g.buf.WriteString("> # Schema peop" + doc[len("# Schema people"):])
		g.outline[0].text, g.outline[0].anchor = "Schema peop", "schema-peop"
		if _, err := g.finish(); err == nil || !strings.Contains(err.Error(), "top-level") {
			t.Errorf("finish = %v, want the quoted heading refused", err)
		}
	})

	t.Run("an outline heading of another level fails", func(t *testing.T) {
		t.Parallel()
		g := fresh()
		doc := g.buf.String()
		g.buf.Reset()
		g.buf.WriteString("## Schema peopl" + doc[len("# Schema people"):])
		g.outline[0].text, g.outline[0].anchor = "Schema peopl", "schema-peopl"
		if _, err := g.finish(); err == nil || !strings.Contains(err.Error(), "level 2") {
			t.Errorf("finish = %v, want the level-2 heading refused", err)
		}
	})

	t.Run("an outline heading holding another anchor fails", func(t *testing.T) {
		t.Parallel()
		g := fresh()
		g.outline[0].anchor = "elsewhere"
		if _, err := g.finish(); err == nil || !strings.Contains(err.Error(), "takes anchor") {
			t.Errorf("finish = %v, want the anchor mismatch refused", err)
		}
	})

	t.Run("a link written more often than read fails", func(t *testing.T) {
		t.Parallel()
		g := newTestGenerator(t, loadSchema(t, "schema \"s\"\ntype A {\n  id String primary\n  --> TO (one) B\n}\ntype B {\n  id String primary\n}\n"))
		g.emitDocument()
		g.links = append(g.links, "b")
		if _, err := g.finish(); err == nil || !strings.Contains(err.Error(), "written 2 times and read 1") {
			t.Errorf("finish = %v, want the missing link refused", err)
		}
	})

	t.Run("a link read more often than written fails", func(t *testing.T) {
		t.Parallel()
		g := newTestGenerator(t, loadSchema(t, "schema \"s\"\ntype A {\n  id String primary\n  --> TO (one) B\n  --> ALSO (one) B\n}\ntype B {\n  id String primary\n}\n"))
		g.emitDocument()
		i := slices.Index(g.links, "b")
		g.links = slices.Delete(g.links, i, i+1)
		if _, err := g.finish(); err == nil || !strings.Contains(err.Error(), "written 1 times and read 2") {
			t.Errorf("finish = %v, want the unrecorded link refused", err)
		}
	})

	t.Run("a link no emitter wrote fails", func(t *testing.T) {
		t.Parallel()
		g := newTestGenerator(t, loadSchema(t, "schema \"s\"\ntype A {\n  id String primary\n  --> TO (one) B\n}\ntype B {\n  id String primary\n}\n"))
		g.emitDocument()
		g.links = nil
		if _, err := g.finish(); err == nil || !strings.Contains(err.Error(), "written by no emitter") {
			t.Errorf("finish = %v, want the unrecorded link refused", err)
		}
	})
}

// TestCloseAuthorText pins that a doc comment's open block is closed exactly
// when a CommonMark parser reads it as open: a fenced code block, and an HTML
// block of a type a blank line does not end. A block inside the author's own
// list or quote is closed by its container and needs nothing.
func TestCloseAuthorText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, in, want string
	}{
		{"balanced", "```\nx\n```", "```\nx\n```"},
		{"open backticks", "```go\nx", "```go\nx\n```"},
		{"open tildes", "~~~~\nx", "~~~~\nx\n~~~~"},
		{"a tilde fence's info string may hold a backtick", "~~~a`b\nx", "~~~a`b\nx\n~~~"},
		{"a backtick in a backtick info string opens nothing", "```a`b\nx", "```a`b\nx"},
		{"inline code at a line's start opens nothing", "```x``` is code", "```x``` is code"},
		{"two backticks open nothing", "``\nx", "``\nx"},
		{"four spaces is indented code, not a fence", "    ```\nx", "    ```\nx"},
		{"a tab-indented opener is indented code", "\t```\nx", "\t```\nx"},
		{"an opener indented three spaces", "   ```\nx", "   ```\nx\n```"},
		{"a closer indented four spaces is content", "```\nx\n    ```", "```\nx\n    ```\n```"},
		{"a closer indented three spaces closes", "```\nx\n   ```", "```\nx\n   ```"},
		{"a shorter run does not close", "````\nx\n```", "````\nx\n```\n````"},
		{"text after a closing run does not close", "```\nx\n``` y", "```\nx\n``` y\n```"},
		{"a closer of the other character does not close", "```\n~~~\nx", "```\n~~~\nx\n```"},
		{"a fence in the author's list closes with the item", "- step:\n  ```sh\n  run", "- step:\n  ```sh\n  run"},
		{"a fence in the author's quote closes with the quote", "> ```\n> x", "> ```\n> x"},
		{"an open HTML comment", "note <!-- no\n<!-- open", "note <!-- no\n<!-- open\n-->"},
		{"an open pre element", "<pre>\nx", "<pre>\nx\n</pre>"},
		{"an open script element takes its own end tag", "<script>\nx", "<script>\nx\n</script>"},
		{"an upper-case opening tag takes its own end tag", "<SCRIPT>\nx", "<SCRIPT>\nx\n</script>"},
		{"an open processing instruction", "<?php\nx", "<?php\nx\n?>"},
		{"an open declaration", "<!DOCTYPE html\nx", "<!DOCTYPE html\nx\n>"},
		{"an open CDATA section", "<![CDATA[\nx", "<![CDATA[\nx\n]]>"},
		{"a div ends at the blank line", "<div>\nx", "<div>\nx"},
	}
	md := newParser()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := closeAuthorText(md, tt.in); got != tt.want {
				t.Errorf("closeAuthorText(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestMarshal_DocCommentHeadingTakesItsAnchor pins that a heading inside a doc
// comment takes its anchor in document order, as GitHub allocates it, so a
// link to a type whose heading slugs alike still lands on the type: here A's
// doc comment and a relation's doc comment each hold a heading "B", so type
// B's own heading takes #b-2. A "#" line inside a fence is no heading.
func TestMarshal_DocCommentHeadingTakesItsAnchor(t *testing.T) {
	t.Parallel()

	s := loadSchema(t, "schema \"s\"\n/* Intro\n\n### *B*\n\n```\n# B\n``` */\ntype A {\n  id String primary\n  /* Setext\n\nB\n--- */\n  --> TO (one) B\n}\ntype B {\n  id String primary\n}\n")
	out, err := Marshal(s)
	if err != nil {
		t.Fatalf("Marshal = %v, want nil", err)
	}
	doc := string(out)
	if !strings.Contains(doc, "- `--> TO (one)` [B](#b-2)\n") {
		t.Errorf("the link to B does not target #b-2:\n%s", doc)
	}
	if strings.Contains(doc, "](#b)") || strings.Contains(doc, "](#b-1)") {
		t.Errorf("a link lands on a doc comment's heading:\n%s", doc)
	}
}

// TestMarshal_ReemissionForgetsTheFirstEmissionsDocComments pins that the
// second emission reads its own doc comments alone: six links to B grow by
// "-1" each when a doc-comment heading moves B's anchor, which shifts the last
// relation's doc comment by twelve bytes, far enough that the first emission's
// span of it would cover the link to C on the bullet above it.
func TestMarshal_ReemissionForgetsTheFirstEmissionsDocComments(t *testing.T) {
	t.Parallel()

	var rels strings.Builder
	for i := range 6 {
		rels.WriteString("  --> R" + strconv.Itoa(i) + " (one) B\n")
	}
	s := loadSchema(t, "schema \"s\"\n/* ### B */\ntype A {\n  id String primary\n"+rels.String()+"  /* note */\n  --> TO (one) C\n}\ntype B {\n  id String primary\n}\ntype C {\n  id String primary\n}\n")
	out, err := Marshal(s)
	if err != nil {
		t.Fatalf("Marshal = %v, want nil", err)
	}
	if !strings.Contains(string(out), "- `--> TO (one)` [C](#c)\n\n  note\n") {
		t.Errorf("the link to C and its doc comment are not written:\n%s", out)
	}
}

// TestMarshal_OpenHTMLCommentIsClosed pins that a doc comment's unclosed HTML
// comment is closed at the end of the comment's block, so the type after it
// keeps its heading.
func TestMarshal_OpenHTMLCommentIsClosed(t *testing.T) {
	t.Parallel()

	s := loadSchema(t, "schema \"s\"\n/* Note:\n<!-- draft */\ntype A {\n  id String primary\n}\ntype B {\n  id String primary\n}\n")
	out, err := Marshal(s)
	if err != nil {
		t.Fatalf("Marshal = %v, want nil", err)
	}
	if !strings.Contains(string(out), "<!-- draft\n-->\n\n| Property") {
		t.Errorf("the HTML comment is not closed before the table:\n%s", out)
	}
}

// TestMarshal_AuthorFenceUnderABulletIsReadInItsItem pins that the self-check
// reads a relation's doc comment relative to the bullet that holds it: a
// closer indented three spaces inside the item closes the fence, as it does
// for a Markdown parser, although the line sits five spaces in.
func TestMarshal_AuthorFenceUnderABulletIsReadInItsItem(t *testing.T) {
	t.Parallel()

	s := loadSchema(t, "schema \"s\"\ntype T {\n  id String primary\n  /* Example:\n```\nx\n   ```\n*/\n  --> TO (one) U\n}\ntype U {\n  id String primary\n}\n")
	out, err := Marshal(s)
	if err != nil {
		t.Fatalf("Marshal = %v, want nil", err)
	}
	if !strings.Contains(string(out), "\n  ```\n  x\n     ```\n") {
		t.Errorf("the doc comment is not written unsealed under its bullet:\n%s", out)
	}
}

// TestMarshal_AuthorTextIsNotAGeneratorFault pins that a doc comment's own
// Markdown never fails generation: a link to an anchor the document lacks, a
// fence the comment leaves open, and a line shaped like a table separator.
// The open fence is closed at the end of the comment's block, right before
// the property table.
func TestMarshal_AuthorTextIsNotAGeneratorFault(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name, doc, want string
	}{
		{"link", "See [the glossary](#glossary) for terms.", "See [the glossary](#glossary) for terms.\n\n| Property"},
		{"fence", "Example:\n```\nunclosed", "unclosed\n```\n\n| Property"},
		{"tildes", "Example:\n~~~~\nunclosed", "unclosed\n~~~~\n\n| Property"},
		{"separator", "|---|---|", "|---|---|\n\n| Property"},
		{"comment line in a fence", "Example:\n```yaml\n# a comment\nx: 1\n```", "x: 1\n```\n\n| Property"},
		{"heading line in a fence", "Example:\n```\n### T\n```", "### T\n```\n\n| Property"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := loadSchema(t, "schema \"s\"\n/* "+tt.doc+" */\ntype T {\n  id String primary\n}\n")
			out, err := Marshal(s)
			if err != nil {
				t.Fatalf("Marshal = %v, want nil", err)
			}
			if !strings.Contains(string(out), tt.want) {
				t.Errorf("output does not hold %q:\n%s", tt.want, out)
			}
		})
	}
}
