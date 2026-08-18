package markdown

import (
	"testing"

	"github.com/simon-lentz/yammm/location"
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
	g, err := newGenerator(other)
	if err != nil {
		t.Fatalf("newGenerator: %v", err)
	}

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

	ref := schema.NewTypeRef("", "Root", location.Span{})
	if got, want := g.superLink(thing, ref), "Root"; got != want {
		t.Errorf("superLink = %q, want the reference spelling %q", got, want)
	}

	// The control: over its own document both resolve to links, so the
	// assertions above pin the fallback and not a renderer that never links.
	gOwn, err := newGenerator(owner)
	if err != nil {
		t.Fatalf("newGenerator(owner): %v", err)
	}
	if got := gOwn.relationTarget(rel); got == "Other" {
		t.Error("relationTarget did not link a target its own document contains")
	}
	if got := gOwn.superLink(thing, ref); got == "Root" {
		t.Error("superLink did not link a supertype its own document contains")
	}
}
