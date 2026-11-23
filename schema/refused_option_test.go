package schema_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema"
)

// TestLoad_RefusesAnOptionItDoesNotTake holds each load to the options it
// takes: any other is refused before anything is read, whatever its value, by
// an error that names the option and the load and carries no diagnostic code.
func TestLoad_RefusesAnOptionItDoesNotTake(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "main.yammm")
	const src = "schema \"s\"\n\ntype T {\n\tid String primary\n}\n"
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	absent := filepath.Join(dir, "absent.yammm")
	sources := map[string][]byte{"main.yammm": []byte(src)}
	var captured schema.Sources
	takes := func(diag.Issue) {}

	type load func(opts ...schema.Option) (schema.Schema, error)
	loadFile := func(p string) load {
		return func(opts ...schema.Option) (schema.Schema, error) { return schema.Load(t.Context(), p, opts...) }
	}
	loadString := func(opts ...schema.Option) (schema.Schema, error) {
		return schema.LoadString(t.Context(), src, "s.yammm", opts...)
	}
	loadSources := func(opts ...schema.Option) (schema.Schema, error) {
		return schema.LoadSourcesWithEntry(t.Context(), sources, "main.yammm", opts...)
	}

	refused := []struct {
		name string
		load load
		opts []schema.Option
		want string
	}{
		{
			"Load WithSourcesOnly(true)", loadFile(absent),
			[]schema.Option{schema.WithSourcesOnly(true)},
			"schema.Load does not take the WithSourcesOnly option",
		},
		{
			"Load WithSourcesOnly(false)", loadFile(path),
			[]schema.Option{schema.WithSourcesOnly(false)},
			"schema.Load does not take the WithSourcesOnly option",
		},
		{
			"Load WithSyntheticRoot", loadFile(absent),
			[]schema.Option{schema.WithSyntheticRoot("embedded://app")},
			"schema.Load does not take the WithSyntheticRoot option",
		},
		{
			"LoadString WithModuleRoot", loadString,
			[]schema.Option{schema.WithModuleRoot(dir)},
			"schema.LoadString does not take the WithModuleRoot option",
		},
		{
			"LoadString WithModuleRoot empty", loadString,
			[]schema.Option{schema.WithModuleRoot("")},
			"schema.LoadString does not take the WithModuleRoot option",
		},
		{
			"LoadString WithImportsAllowed(true)", loadString,
			[]schema.Option{schema.WithImportsAllowed(true)},
			"schema.LoadString does not take the WithImportsAllowed option",
		},
		{
			"LoadString WithImportsAllowed(false)", loadString,
			[]schema.Option{schema.WithImportsAllowed(false)},
			"schema.LoadString does not take the WithImportsAllowed option",
		},
		{
			"LoadString WithSourcesOnly", loadString,
			[]schema.Option{schema.WithSourcesOnly(false)},
			"schema.LoadString does not take the WithSourcesOnly option",
		},
		{
			"LoadString WithSyntheticRoot", loadString,
			[]schema.Option{schema.WithSyntheticRoot("embedded://app")},
			"schema.LoadString does not take the WithSyntheticRoot option",
		},
		{
			"the first refused option is named", loadString,
			[]schema.Option{schema.WithWarnings(takes), schema.WithImportsAllowed(false), schema.WithModuleRoot(dir)},
			"schema.LoadString does not take the WithImportsAllowed option",
		},
		{
			"LoadSourcesWithEntry WithSyntheticRoot with WithModuleRoot", loadSources,
			[]schema.Option{schema.WithSyntheticRoot("embedded://app"), schema.WithModuleRoot("/project")},
			"schema.LoadSourcesWithEntry does not take the WithSyntheticRoot option with WithModuleRoot: the synthetic root stands in for the module root",
		},
		{
			"LoadSourcesWithEntry WithModuleRoot empty with WithSyntheticRoot", loadSources,
			[]schema.Option{schema.WithModuleRoot(""), schema.WithSyntheticRoot("embedded://app")},
			"schema.LoadSourcesWithEntry does not take the WithSyntheticRoot option with WithModuleRoot: the synthetic root stands in for the module root",
		},
	}
	for _, row := range refused {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			s, err := row.load(row.opts...)
			if err == nil || err.Error() != row.want {
				t.Fatalf("err = %v; want %q", err, row.want)
			}
			if !s.IsZero() {
				t.Error("a refused load returned a schema")
			}
			if _, ok := errors.AsType[*diag.ResultError](err); ok {
				t.Errorf("err = %#v; want an error with no diagnostics", err)
			}
			if errors.Is(err, diag.E_LOAD_IO_FAILURE) {
				t.Error("the refusal carries E_LOAD_IO_FAILURE")
			}
		})
	}

	taken := []struct {
		name string
		load load
		opts []schema.Option
	}{
		{"Load", loadFile(path), []schema.Option{
			schema.WithModuleRoot(dir), schema.WithImportsAllowed(false), schema.CaptureSources(&captured), schema.WithWarnings(takes),
		}},
		{"LoadString", loadString, []schema.Option{schema.CaptureSources(&captured), schema.WithWarnings(takes)}},
		{"LoadSourcesWithEntry with a module root", loadSources, []schema.Option{
			schema.WithModuleRoot(dir), schema.WithSourcesOnly(true), schema.WithImportsAllowed(false),
			schema.CaptureSources(&captured), schema.WithWarnings(takes),
		}},
		{"LoadSourcesWithEntry with a synthetic root", loadSources, []schema.Option{
			schema.WithSyntheticRoot("embedded://app"), schema.WithSourcesOnly(false), schema.WithImportsAllowed(true),
			schema.CaptureSources(&captured), schema.WithWarnings(takes),
		}},
	}
	for _, row := range taken {
		t.Run("takes/"+row.name, func(t *testing.T) {
			s, err := row.load(row.opts...)
			if err != nil || s.IsZero() {
				t.Fatalf("load = (zero %t, %v); want a schema", s.IsZero(), err)
			}
		})
	}
}
