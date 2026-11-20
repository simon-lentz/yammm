package markdown

import (
	"slices"
	"testing"

	"github.com/simon-lentz/yammm/schema"
)

// Both renderers fall back when the type they name is absent from the
// document's type map. Every public entry point now rejects the dangling
// references that once produced that state, and the generator emits the whole
// import closure — so a schema-level fixture cannot reach either branch, and
// they are driven directly instead. Without this, deleting either fallback
// leaves the suite green while a miss panics on a nil entry.
func TestRenderFallbacks_DegradeToTheReferenceSpelling(t *testing.T) {
	t.Parallel()

	// The model that owns the relation and the extends clause.
	owner, result := schema.LoadString(t.Context(), `schema "owner"

type Root {
	id String primary
}

type Thing extends Root {
	id String primary
	--> POINTS_AT (_) Other
}

type Other {
	id String primary
}
`, "owner.yammm")
	if result.HasErrors() {
		t.Fatalf("owner fixture must load: %v", result.Err())
	}

	// A document over an unrelated schema: its type map contains neither
	// Other nor Root, so both lookups miss.
	other, result := schema.LoadString(t.Context(), `schema "elsewhere"

type Unrelated {
	id String primary
}
`, "elsewhere.yammm")
	if result.HasErrors() {
		t.Fatalf("elsewhere fixture must load: %v", result.Err())
	}
	g := newTestGenerator(t, other)

	thing, ok := owner.Type("Thing")
	if !ok {
		t.Fatal("Thing missing from the owner fixture")
	}
	rel, ok := thing.Relation("POINTS_AT")
	if !ok {
		t.Fatal("POINTS_AT missing from Thing")
	}

	if got, want := g.relationTarget(rel), "Other"; got != want {
		t.Errorf("relationTarget = %q, want the reference spelling %q", got, want)
	}

	ref := onlyInherited(t, thing)
	if got, want := g.superLink(thing, ref), "Root"; got != want {
		t.Errorf("superLink = %q, want the reference spelling %q", got, want)
	}
	aliased := loadSources(t, map[string][]byte{
		"entry.yammm": []byte("schema \"e\"\nimport \"x.yammm\" as www\ntype T extends www.Root {\n  id String primary\n  --> AT (one) www.Other\n}\n"),
		"x.yammm":     []byte("schema \"x\"\nabstract type Root {\n  id String primary\n}\ntype Other {\n  id String primary\n}\n"),
	})
	tt, ok := aliased.Type("T")
	if !ok {
		t.Fatal("T missing from the aliased fixture")
	}
	at, ok := tt.Relation("AT")
	if !ok {
		t.Fatal("AT missing from T")
	}
	if got, want := g.relationTarget(at), "www<!---->.Other"; got != want {
		t.Errorf("relationTarget = %q, want the escaped reference spelling %q", got, want)
	}
	// The spelling is schema text: an alias www would otherwise autolink.
	if got, want := g.superLink(tt, onlyInherited(t, tt)), "www<!---->.Root"; got != want {
		t.Errorf("superLink = %q, want the escaped reference spelling %q", got, want)
	}

	// The control: over its own document both resolve to links, so the
	// assertions above pin the fallback and not a renderer that never links.
	gOwn := newTestGenerator(t, owner)
	if got := gOwn.relationTarget(rel); got == "Other" {
		t.Error("relationTarget did not link a target its own document contains")
	}
	if got := gOwn.superLink(thing, ref); got == "Root" {
		t.Error("superLink did not link a supertype its own document contains")
	}
}

// onlyInherited returns the one extends reference typ declares.
func onlyInherited(tb testing.TB, typ schema.Type) schema.TypeRef {
	tb.Helper()
	refs := slices.Collect(typ.Inherits())
	if len(refs) != 1 {
		tb.Fatalf("%s declares %d extends references, want 1", typ.Name(), len(refs))
	}
	return refs[0]
}
