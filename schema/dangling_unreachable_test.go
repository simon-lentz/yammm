package schema_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/simon-lentz/yammm/diag"
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
// The proof is by exhaustion over the loader's public entry points, crossed with
// every reference position a qualifier can occupy.
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

func assertRejected(t *testing.T, s schema.Schema, result diag.Result) {
	t.Helper()
	if !s.IsZero() {
		t.Error("a dangling qualified reference sealed into a schema")
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
