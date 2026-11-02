package schema_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema"
)

func importedSchema(s schema.Schema, name string) schema.Schema {
	for imp := range s.Imports() {
		if sub := imp.Schema(); !sub.IsZero() && sub.Name() == name {
			return sub
		}
	}
	return schema.Schema{}
}

func issueLines(res diag.Result) string {
	var lines []string
	for issue := range res.Issues() {
		lines = append(lines, issue.Code().String()+": "+issue.Message())
	}
	return strings.Join(lines, "; ")
}

func writeSchema(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestLoad_InheritsThroughASchemaItNeverImports holds a load whose entry imports
// b, where b.B extends c.C and the entry never imports c, to X extends b.B
// carrying c.C's property.
func TestLoad_InheritsThroughASchemaItNeverImports(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeSchema(t, dir, "c.yammm", "schema \"c\"\n\ntype C {\n\tid String primary\n\textra String\n}\n")
	writeSchema(t, dir, "b.yammm", "schema \"b\"\n\nimport \"./c\" as c\n\ntype B extends c.C {\n\tname String\n}\n")
	writeSchema(t, dir, "x.yammm", "schema \"x\"\n\nimport \"./b\" as b\n\ntype X extends b.B {\n\tmine String\n}\n")
	x, res := schema.Load(t.Context(), filepath.Join(dir, "x.yammm"), schema.WithModuleRoot(dir))
	if x.IsZero() || res.HasErrors() {
		t.Fatalf("load x: %s", issueLines(res))
	}
	xt, ok := x.Type("X")
	if !ok {
		t.Fatal("x declares no type X")
	}
	got := map[string]bool{}
	for p := range xt.AllProperties() {
		got[p.Name()] = true
	}
	for _, want := range []string{"id", "extra", "name", "mine"} {
		if !got[want] {
			t.Errorf("X lacks the inherited property %q; it has %v", want, got)
		}
	}
}

// TestLoad_APartInheritsAnAssociationPastASchemaItNeverImports holds a part type
// that extends b.B, where b.B extends c.C and the entry never imports c, to one
// refusal of the association it inherits.
func TestLoad_APartInheritsAnAssociationPastASchemaItNeverImports(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeSchema(t, dir, "c.yammm", "schema \"c\"\n\ntype C {\n\tid String primary\n}\n")
	writeSchema(t, dir, "b.yammm", "schema \"b\"\n\nimport \"./c\" as c\n\ntype Other {\n\tid String primary\n}\n\ntype B extends c.C {\n\t--> TO_OTHER (one) Other\n}\n")
	writeSchema(t, dir, "x.yammm", "schema \"x\"\n\nimport \"./b\" as b\n\npart type X extends b.B {\n\tmine String\n}\n")
	x, res := schema.Load(t.Context(), filepath.Join(dir, "x.yammm"), schema.WithModuleRoot(dir))
	if !x.IsZero() {
		t.Fatal("a part type carrying an association loaded")
	}
	if n := countCode(res, diag.E_INVALID_ASSOCIATION_TARGET); n != 1 {
		t.Errorf("E_INVALID_ASSOCIATION_TARGET drawn %d times, want 1: %s", n, issueLines(res))
	}
}

// TestLoad_OriginOfACopyOfACopyIsTheDeclaredProperty holds a merge whose survivor
// is itself a merged copy: the declarations are in c, which the entry never
// imports, so the walk reaches b.M's copy first and Y's annotation copies it
// again. Origin still names the declaration in c.
func TestLoad_OriginOfACopyOfACopyIsTheDeclaredProperty(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeSchema(t, dir, "c.yammm", "schema \"c\"\n\nabstract type N1 {\n\tid String primary\n\tx Timestamp\n}\n\nabstract type N2 {\n\tx Timestamp @writeOnce\n}\n")
	writeSchema(t, dir, "b.yammm", "schema \"b\"\n\nimport \"./c\" as c\n\nabstract type M extends c.N1, c.N2 {\n\ty String\n}\n")
	writeSchema(t, dir, "x.yammm", "schema \"x\"\n\nimport \"./b\" as b\n\nabstract type Y {\n\tx Timestamp @index\n}\n\ntype T extends b.M, Y {\n\tz String\n}\n")
	x, res := schema.Load(t.Context(), filepath.Join(dir, "x.yammm"), schema.WithModuleRoot(dir))
	if x.IsZero() || res.HasErrors() {
		t.Fatalf("load x: %s", issueLines(res))
	}

	property := func(s schema.Schema, typeName string) schema.Property {
		t.Helper()
		typ, ok := s.Type(typeName)
		if !ok {
			t.Fatalf("%s declares no type %s", s.Name(), typeName)
		}
		p, ok := typ.Property("x")
		if !ok {
			t.Fatalf("%s holds no property x", typeName)
		}
		return p
	}
	b := importedSchema(x, "b")
	declared := property(importedSchema(b, "c"), "N1")
	copied := property(b, "M")
	merged := property(x, "T")
	if copied == declared || merged == copied {
		t.Fatal("each merge should carry its own copy, so this test no longer holds a copy of a copy")
	}
	if merged.Origin() != declared {
		t.Error("Origin() on a copy of a copy should return the declared property, not the copy it was merged from")
	}
}
