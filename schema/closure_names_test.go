package schema_test

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/location"
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

// TestBuilder_RefusesAClosureThatDeclaresOneSchemaNameTwice pins that a built
// schema whose import closure holds two schemas of one name is refused, since a
// $defs key, a generated Go name and the structural hash each name a closure
// member by its schema name: the entry against an import, the entry against a
// transitive import, and two imports; a closure of distinct names builds.
func TestBuilder_RefusesAClosureThatDeclaresOneSchemaNameTwice(t *testing.T) {
	t.Parallel()

	x, res := schema.LoadSourcesWithEntry(context.Background(), sourcesOf(map[string]string{"x.yammm": xSrc, "geo_a.yammm": geoA}), "x.yammm", ".", schema.WithSourcesOnly(true))
	if res.HasErrors() {
		t.Fatalf("load x: %v", res.Err())
	}
	geo, res := schema.LoadString(context.Background(), geoB, "geo_b.yammm")
	if res.HasErrors() {
		t.Fatalf("load geo: %v", res.Err())
	}
	reg := schema.NewRegistry()
	for _, s := range []*schema.Schema{x, geo} {
		if err := reg.Register(s); err != nil {
			t.Fatal(err)
		}
	}
	build := func(name string, imports ...string) (*schema.Schema, diag.Result) {
		b := schema.NewBuilder().WithName(name).WithSourceID(location.MustNewSourceID("test://built.yammm")).WithRegistry(reg)
		for _, imp := range imports {
			b.AddImport(imp, imp)
		}
		b.AddType("Region").WithPrimaryKey("id", schema.NewStringConstraint())
		return b.Build()
	}

	for label, tc := range map[string]struct {
		name    string
		imports []string
	}{
		"the entry and an import":           {"geo", []string{"geo"}},
		"the entry and a transitive import": {"geo", []string{"x"}},
		"two imports":                       {"main", []string{"x", "geo"}},
	} {
		t.Run(label, func(t *testing.T) {
			t.Parallel()
			s, res := build(tc.name, tc.imports...)
			if s != nil {
				t.Fatal("a closure declaring the schema name geo twice built")
			}
			assertOneSchemaNameClash(t, res, "geo")
		})
	}

	if s, res := build("main", "geo"); s == nil || res.HasErrors() {
		t.Errorf("a closure of distinct names is refused: %v", res.Err())
	}
}

// TestLoad_RefusesAClosureThatDeclaresOneSchemaNameTwice pins the same refusal
// on a load: a schema the shared registry holds brings its closure's members,
// which the registry never registered, beside a source this load reads under
// one of their names.
func TestLoad_RefusesAClosureThatDeclaresOneSchemaNameTwice(t *testing.T) {
	t.Parallel()

	x, res := schema.LoadSourcesWithEntry(context.Background(), sourcesOf(map[string]string{"x.yammm": xSrc, "geo_a.yammm": geoA}), "x.yammm", ".", schema.WithSourcesOnly(true))
	if res.HasErrors() {
		t.Fatalf("load x: %v", res.Err())
	}
	reg := schema.NewRegistry()
	if err := reg.Register(x); err != nil {
		t.Fatal(err)
	}
	load := func(geoImport string) (*schema.Schema, diag.Result) {
		main := "schema \"main\"\n\nimport \"./x.yammm\" as x\nimport \"./" + geoImport + "\" as geo\n\ntype M {\n\tm_id String primary\n\t--> TO (one) geo.Region\n}\n"
		return schema.LoadSourcesWithEntry(context.Background(), sourcesOf(map[string]string{
			"main.yammm": main, "x.yammm": xSrc, "geo_a.yammm": geoA, "geo_b.yammm": geoB,
		}), "main.yammm", ".", schema.WithSourcesOnly(true), schema.WithRegistry(reg))
	}

	s, res := load("geo_b.yammm")
	if s != nil {
		t.Fatalf("a closure holding geo_a.yammm and geo_b.yammm, both schema geo, loaded: %v", s.Closure())
	}
	assertOneSchemaNameClash(t, res, "geo")
	for issue := range res.Issues() {
		if span := issue.Span(); !strings.HasSuffix(span.Source.String(), "main.yammm") {
			t.Errorf("the refusal is placed at %v, want the entry's declaration in main.yammm", span)
		}
	}

	if s, res := load("geo_a.yammm"); s == nil || res.HasErrors() {
		t.Errorf("a closure reaching one geo by two paths is refused: %v", res.Err())
	}
}
