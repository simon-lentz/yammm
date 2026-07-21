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

// TestMarshal_NonSourceBacked pins that Marshal accepts a Builder-built
// schema with no source content: invariants degrade to message-only and an
// unresolved relation target renders unlinked, with no error.
func TestMarshal_NonSourceBacked(t *testing.T) {
	t.Parallel()

	s, res := schema.NewBuilder().WithName("built").
		AddType("Thing").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithRelation("POINTS_AT", schema.NewTypeRef("ext", "Other", location.Span{}), true, false).
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
	if !strings.Contains(doc, "- `--> POINTS_AT (_)` ext.Other") {
		t.Errorf("unresolved relation target not rendered unlinked:\n%s", doc)
	}
}

// TestSelfCheck exercises the structural output guard directly on
// hand-authored documents.
func TestSelfCheck(t *testing.T) {
	t.Parallel()

	anchors := map[string]bool{"person": true}

	t.Run("valid document passes", func(t *testing.T) {
		t.Parallel()
		doc := "# T\n\n```mermaid\nclassDiagram\n```\n\n### Person\n\n" +
			"| A | B |\n| --- | --- |\n| 1 | 2 |\n\n[Person](#person)\n"
		if err := selfCheck([]byte(doc), anchors); err != nil {
			t.Errorf("selfCheck = %v, want nil", err)
		}
	})

	t.Run("unclosed fence fails", func(t *testing.T) {
		t.Parallel()
		doc := "```mermaid\nclassDiagram\n"
		if err := selfCheck([]byte(doc), anchors); err == nil {
			t.Error("selfCheck = nil, want unclosed-fence error")
		}
	})

	t.Run("heading inside open fence fails", func(t *testing.T) {
		t.Parallel()
		doc := "```mermaid\n## Types\n```\n"
		if err := selfCheck([]byte(doc), anchors); err == nil {
			t.Error("selfCheck = nil, want heading-inside-fence error")
		}
	})

	t.Run("link to unknown anchor fails", func(t *testing.T) {
		t.Parallel()
		doc := "[Ghost](#ghost)\n"
		if err := selfCheck([]byte(doc), anchors); err == nil {
			t.Error("selfCheck = nil, want unresolved-link error")
		}
	})

	t.Run("separator column mismatch fails", func(t *testing.T) {
		t.Parallel()
		doc := "| A | B | C |\n| --- | --- |\n"
		if err := selfCheck([]byte(doc), anchors); err == nil {
			t.Error("selfCheck = nil, want column-mismatch error")
		}
	})

	t.Run("escaped pipes do not affect column count", func(t *testing.T) {
		t.Parallel()
		doc := "| A | B |\n| --- | --- |\n| a\\|b | c |\n"
		if err := selfCheck([]byte(doc), anchors); err != nil {
			t.Errorf("selfCheck = %v, want nil", err)
		}
	})
}
