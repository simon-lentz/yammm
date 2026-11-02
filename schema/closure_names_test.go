package schema_test

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/internal/yammmtest"
	"github.com/simon-lentz/yammm/schema"
)

// sourcesOf turns a map of source texts into LoadSourcesWithEntry's input.
func sourcesOf(texts map[string]string) map[string][]byte {
	out := make(map[string][]byte, len(texts))
	for k, v := range texts {
		out[k] = []byte(v)
	}
	return out
}

// assertOneSchemaNameClash fails unless res holds exactly one issue, an Error
// E_DUPLICATE_SCHEMA naming name in its detail.
func assertOneSchemaNameClash(t *testing.T, res diag.Result, name string) {
	t.Helper()
	issues := slices.Collect(res.Issues())
	if !res.HasErrors() || len(issues) != 1 || issues[0].Code() != diag.E_DUPLICATE_SCHEMA || issues[0].Severity() != diag.Error {
		t.Fatalf("result = %v, want one Error E_DUPLICATE_SCHEMA", res)
	}
	if want := (diag.Detail{Key: diag.DetailKeySchemaName, Value: name}); !slices.Contains(issues[0].Details(), want) {
		t.Errorf("details = %v, want %v", issues[0].Details(), want)
	}
}

const (
	geoA = "schema \"geo\"\n\ntype Region {\n\ta_id String primary\n}\n"
	geoB = "schema \"geo\"\n\ntype Region {\n\tb_id String primary\n}\n"
	xSrc = "schema \"x\"\n\nimport \"./geo_a.yammm\" as g\n\ntype Xt {\n\tx_id String primary\n\t--> TO (one) g.Region\n}\n"
)

// TestLoad_RefusesAClosureThatDeclaresOneSchemaNameTwice pins that a load whose
// import closure holds two schemas of one name is refused, since a $defs key, a
// generated Go name and the structural hash each name a closure member by its
// schema name: the entry against an import, the entry against a transitive
// import, and two imports. A closure reaching one geo by two paths loads.
func TestLoad_RefusesAClosureThatDeclaresOneSchemaNameTwice(t *testing.T) {
	t.Parallel()

	load := func(mainName, geoImport string) (schema.Schema, diag.Result) {
		main := "schema \"" + mainName + "\"\n\nimport \"./x.yammm\" as x\nimport \"./" + geoImport + "\" as geo\n\ntype M {\n\tm_id String primary\n\t--> TO (one) geo.Region\n}\n"
		return schema.LoadSourcesWithEntry(context.Background(), sourcesOf(map[string]string{
			"main.yammm": main, "x.yammm": xSrc, "geo_a.yammm": geoA, "geo_b.yammm": geoB,
		}), "main.yammm", ".", schema.WithSourcesOnly(true))
	}

	t.Run("two imports", func(t *testing.T) {
		t.Parallel()
		s, res := load("main", "geo_b.yammm")
		if !s.IsZero() {
			t.Fatalf("a closure holding geo_a.yammm and geo_b.yammm, both schema geo, loaded: %v", slices.Collect(s.Closure()))
		}
		// The second declaration is the refused one, and its importer reports the
		// failed import as it does for any other.
		var got []string
		for issue := range res.Issues() {
			got = append(got, fmt.Sprintf("%s %s:%d", issue.Code(), filepath.Base(issue.Span().Source.String()), issue.Span().Start.Line))
		}
		yammmtest.Diff(t, []string{"E_DUPLICATE_SCHEMA geo_b.yammm:1", "E_UPSTREAM_FAIL main.yammm:4"}, got)
	})
	t.Run("the entry and an import", func(t *testing.T) {
		t.Parallel()
		s, res := schema.LoadSourcesWithEntry(context.Background(), sourcesOf(map[string]string{
			"main.yammm":  "schema \"geo\"\n\nimport \"./geo_a.yammm\" as a\n\ntype M {\n\tm_id String primary\n}\n",
			"geo_a.yammm": geoA,
		}), "main.yammm", ".", schema.WithSourcesOnly(true))
		if !s.IsZero() {
			t.Fatal("a closure declaring the schema name geo twice loaded")
		}
		assertOneSchemaNameClash(t, res, "geo")
	})
	t.Run("the entry and a transitive import", func(t *testing.T) {
		t.Parallel()
		s, res := schema.LoadSourcesWithEntry(context.Background(), sourcesOf(map[string]string{
			"main.yammm":  "schema \"geo\"\n\nimport \"./x.yammm\" as x\n\ntype M {\n\tm_id String primary\n}\n",
			"x.yammm":     xSrc,
			"geo_a.yammm": geoA,
		}), "main.yammm", ".", schema.WithSourcesOnly(true))
		if !s.IsZero() {
			t.Fatal("a closure declaring the schema name geo twice loaded")
		}
		assertOneSchemaNameClash(t, res, "geo")
	})

	if s, res := load("main", "geo_a.yammm"); s.IsZero() || res.HasErrors() {
		t.Errorf("a closure reaching one geo by two paths is refused: %v", res.Err())
	}
}

// TestLoad_ASchemaAlreadyFailedDrawsNoDuplicateNameReport holds that only a schema
// that would otherwise load reports the name it shares with a sibling: one
// failed by a syntax error or a failed import reports that failure alone.
func TestLoad_ASchemaAlreadyFailedDrawsNoDuplicateNameReport(t *testing.T) {
	t.Parallel()
	rows := []struct {
		name, second string
		own          diag.Code
	}{
		{"a syntax error", "schema \"X\"\ntype B { id String primary\n $$$ }\n", diag.E_SYNTAX},
		{"a failed import", "schema \"X\"\nimport \"./missing\"\ntype B { id String primary }\n", diag.E_IMPORT_RESOLVE},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			s, res := schema.LoadSourcesWithEntry(t.Context(), sourcesOf(map[string]string{
				"main.yammm": "schema \"m\"\nimport \"./a\"\nimport \"./b\"\ntype T { id String primary }\n",
				"a.yammm":    "schema \"X\"\ntype A { id String primary }\n",
				"b.yammm":    row.second,
			}), "main.yammm", t.TempDir())
			if !s.IsZero() {
				t.Fatal("the load succeeded")
			}
			counts := codeCounts(res)
			if counts[diag.E_DUPLICATE_SCHEMA] != 0 || counts[row.own] == 0 || counts[diag.E_UPSTREAM_FAIL] != 1 {
				t.Errorf("result = %v, want %s and E_UPSTREAM_FAIL and no E_DUPLICATE_SCHEMA", res, row.own)
			}
		})
	}
}
