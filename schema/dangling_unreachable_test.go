package schema_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// danglingSources are the reference positions a qualified name can occupy.
// Each names alias "nope", which no import declares, so completion has nothing
// to resolve and no link step can ever supply it.
var danglingSources = map[string]string{
	"extends": `schema "d"

type Base extends nope.Missing {
	id String primary
}
`,
	"relation target": `schema "d"

type Base {
	id String primary
	--> LINKS (_) nope.Missing
}
`,
	"composition target": `schema "d"

type Base {
	id String primary
	*-> PARTS (many) nope.Missing
}
`,
	"qualified datatype": `schema "d"

type Base {
	id String primary
	weight nope.Grams
}
`,
}

// No public entry point seals a schema carrying a dangling qualified
// reference. The release enumeration states this as contract and the adapter
// and instance layers document their target re-checks as defense in depth on
// the strength of it, so it is proved here rather than assumed.
//
// The proof is by exhaustion over the two paths that can seal a schema —
// [schema.Builder.Build] and the loader — reached through every public
// entry point, crossed with every reference position a qualifier can occupy.
func TestDanglingQualifiedReference_UnreachableThroughEveryEntryPoint(t *testing.T) {
	t.Parallel()

	for position, src := range danglingSources {
		t.Run(position, func(t *testing.T) {
			t.Parallel()

			t.Run("LoadString", func(t *testing.T) {
				t.Parallel()
				s, result := schema.LoadString(t.Context(), src, "d.yammm")
				assertRejected(t, s, result)
			})

			t.Run("LoadSources", func(t *testing.T) {
				t.Parallel()
				s, result := schema.LoadSources(t.Context(),
					map[string][]byte{"d.yammm": []byte(src)}, ".")
				assertRejected(t, s, result)
			})

			t.Run("LoadSourcesWithEntry", func(t *testing.T) {
				t.Parallel()
				s, result := schema.LoadSourcesWithEntry(t.Context(),
					map[string][]byte{"d.yammm": []byte(src)}, "d.yammm", ".")
				assertRejected(t, s, result)
			})

			t.Run("Load", func(t *testing.T) {
				t.Parallel()
				dir := t.TempDir()
				path := filepath.Join(dir, "d.yammm")
				if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
					t.Fatal(err)
				}
				s, result := schema.Load(t.Context(), path)
				assertRejected(t, s, result)
			})
		})
	}
}

// Build is the second sealing path and takes no source text, so it is driven
// through its own API rather than the table above.
func TestDanglingQualifiedReference_UnreachableThroughBuilder(t *testing.T) {
	t.Parallel()

	t.Run("relation target", func(t *testing.T) {
		t.Parallel()
		s, result := schema.NewBuilder().
			WithName("d").
			WithSourceID(location.MustNewSourceID("test://d.yammm")).
			AddType("Base").
			WithPrimaryKey("id", schema.NewStringConstraint()).
			WithRelation("LINKS", schema.NewTypeRef("nope", "Missing", location.Span{}), false, false).
			Done().
			Build()
		assertRejected(t, s, result)
	})

	t.Run("extends", func(t *testing.T) {
		t.Parallel()
		s, result := schema.NewBuilder().
			WithName("d").
			WithSourceID(location.MustNewSourceID("test://d.yammm")).
			AddType("Base").
			WithPrimaryKey("id", schema.NewStringConstraint()).
			Extends(schema.NewTypeRef("nope", "Missing", location.Span{})).
			Done().
			Build()
		assertRejected(t, s, result)
	})

	t.Run("composition target", func(t *testing.T) {
		t.Parallel()
		s, result := schema.NewBuilder().
			WithName("d").
			WithSourceID(location.MustNewSourceID("test://d.yammm")).
			AddType("Base").
			WithPrimaryKey("id", schema.NewStringConstraint()).
			WithComposition("PARTS", schema.NewTypeRef("nope", "Missing", location.Span{}), false, true).
			Done().
			Build()
		assertRejected(t, s, result)
	})

	t.Run("qualified datatype", func(t *testing.T) {
		t.Parallel()
		s, result := schema.NewBuilder().
			WithName("d").
			WithSourceID(location.MustNewSourceID("test://d.yammm")).
			AddType("Base").
			WithPrimaryKey("id", schema.NewStringConstraint()).
			WithProperty("weight", schema.NewAliasConstraint("nope.Grams", nil)).
			Done().
			Build()
		assertRejected(t, s, result)
	})
}

// The two halves must cover the same positions, or the proof is by exhaustion
// over one path and by sampling over the other.
func TestDanglingQualifiedReference_BothHalvesCoverEveryPosition(t *testing.T) {
	t.Parallel()
	builderPositions := map[string]bool{
		"relation target":    true,
		"extends":            true,
		"composition target": true,
		"qualified datatype": true,
	}
	for position := range danglingSources {
		if !builderPositions[position] {
			t.Errorf("position %q is proved through the loader but not through Builder.Build", position)
		}
	}
	for position := range builderPositions {
		if _, ok := danglingSources[position]; !ok {
			t.Errorf("position %q is proved through Builder.Build but has no loader source", position)
		}
	}
}

// A schema that fails to seal is never registered, so a Registry cannot hand
// one out either — the third way a caller might obtain a sealed schema.
func TestDanglingQualifiedReference_NeverEntersTheRegistry(t *testing.T) {
	t.Parallel()

	reg := schema.NewRegistry()
	src := danglingSources["relation target"]
	if s, result := schema.LoadString(t.Context(), src, "d.yammm", schema.WithRegistry(reg)); s != nil {
		t.Fatalf("load returned a schema: %s", result)
	}
	if got := len(reg.All()); got != 0 {
		t.Errorf("registry holds %d schema(s) after a failed load, want 0", got)
	}
}

func assertRejected(t *testing.T, s *schema.Schema, result diag.Result) {
	t.Helper()
	if s != nil {
		t.Error("a dangling qualified reference sealed into a schema — the " +
			"defense-in-depth claim in docs/VERSIONING.md is false")
	}
	if !result.HasErrors() {
		t.Fatal("no diagnostic reported")
	}
	var codes []string
	for issue := range result.Issues() {
		codes = append(codes, issue.Code().String())
	}
	if !slices.Contains(codes, diag.E_UNKNOWN_TYPE.String()) {
		t.Errorf("codes = %v, want one to be %s", codes, diag.E_UNKNOWN_TYPE)
	}
}
