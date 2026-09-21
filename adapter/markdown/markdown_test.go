package markdown

import (
	"bytes"
	"context"
	"path/filepath"
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
		if _, err := g.finish(); err == nil {
			t.Error("finish = nil, want line-break error")
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
		if _, err := g.finish(); err == nil || !strings.Contains(err.Error(), "inside an open code fence") {
			t.Errorf("finish = %v, want a heading-inside-fence error", err)
		}
	})

	t.Run("table row of the wrong width fails", func(t *testing.T) {
		t.Parallel()
		g := fresh()
		var b bytes.Buffer
		g.newTable(&b, "A", "B", "C").row("1", "2")
		if _, err := g.finish(); err == nil {
			t.Error("finish = nil, want column-mismatch error")
		}
	})
}

// TestSealFences pins the seal against CommonMark's fence rules: a doc
// comment's fence is closed only when a Markdown parser reads it as open, and
// the closing line repeats the opener's indent and run.
func TestSealFences(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, in, want string
	}{
		{"balanced", "```\nx\n```", "```\nx\n```"},
		{"open backticks", "```go\nx", "```go\nx\n```"},
		{"open tildes", "~~~~\nx", "~~~~\nx\n~~~~"},
		{"a tilde fence's info string may hold a backtick", "~~~a`b\nx", "~~~a`b\nx\n~~~"},
		{"a backtick in a backtick info string opens nothing", "```a`b\nx", "```a`b\nx"},
		{"four spaces is indented code, not a fence", "    ```\nx", "    ```\nx"},
		{"an opener indented three spaces", "   ```\nx", "   ```\nx\n   ```"},
		{"a closer indented four spaces is content", "```\nx\n    ```", "```\nx\n    ```\n```"},
		{"a closer indented three spaces closes", "```\nx\n   ```", "```\nx\n   ```"},
		{"a shorter run does not close", "````\nx\n```", "````\nx\n```\n````"},
		{"text after a closing run does not close", "```\nx\n``` y", "```\nx\n``` y\n```"},
		{"a tab after a closing run closes", "```\nx\n```\t", "```\nx\n```\t"},
		{"inline code at a line's start opens nothing", "```x``` is code", "```x``` is code"},
		{"two backticks open nothing", "``\nx", "``\nx"},
		{"a closer of the other character does not close", "```\n~~~\nx", "```\n~~~\nx\n```"},
		{"a tab-indented opener is indented code", "\t```\nx", "\t```\nx"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := sealFences(tt.in); got != tt.want {
				t.Errorf("sealFences(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
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
