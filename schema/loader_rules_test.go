package schema_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/internal/yammmtest"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// An empty schema name is an authoring error, refused where it was written.
func TestLoad_EmptySchemaNameIsRefused(t *testing.T) {
	t.Parallel()

	s, res := schema.LoadString(t.Context(), "schema \"\"\n\ntype T {\n    id String primary\n}\n", "s.yammm")
	if s != nil {
		t.Error("a nameless schema must not load")
	}
	if !res.HasCode(diag.E_INVALID_NAME) {
		t.Errorf("want E_INVALID_NAME; got %v", res.Err())
	}
	if res.HasCode(diag.E_INTERNAL) || res.HasCode(diag.E_DUPLICATE_TYPE) {
		t.Errorf("an authoring error must not be reported as the loader's; got %v", res.Err())
	}
}

// Every Registry.Register refusal renders under the code its cause deserves:
// a name clash and a changed source are the caller's, the two the parser and
// the Builder make unreachable are internal.
func TestRegisterFailureIssue_RendersByCause(t *testing.T) {
	t.Parallel()

	reg := schema.NewRegistry()
	s := schema.TestNewSchema("s", location.SourceID{}, location.Span{}, "")
	for _, tc := range []struct {
		name string
		err  error
		want diag.Code
	}{
		{"duplicate name", &schema.RegistryError{Kind: schema.DuplicateName, Message: "name"}, diag.E_DUPLICATE_SCHEMA},
		{"changed source", &schema.RegistryError{Kind: schema.DuplicateSourceID, Message: "bytes changed"}, diag.E_LOAD_SOURCE_CHANGED},
		{"empty name", &schema.RegistryError{Kind: schema.InvalidName, Message: "empty"}, diag.E_INTERNAL},
		{"zero source id", &schema.RegistryError{Kind: schema.InvalidSourceID, Message: "zero"}, diag.E_INTERNAL},
		{"not a registry error", errors.New("boom"), diag.E_INTERNAL},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := schema.TestRegisterFailureIssue(reg, s, tc.err); got.Code() != tc.want {
				t.Errorf("got %s (%s), want %s", got.Code(), got.Message(), tc.want)
			}
		})
	}
}

func writeFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, src := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// Two loads sharing a registry can both complete one import in the window
// between the cache miss and registration. Whichever registered first is the
// one object every load then holds: the import wired into each entry schema
// is the registry's pointer, never a discarded duplicate.
func TestSharedRegistry_ConcurrentLoadsAdoptOneObjectPerSource(t *testing.T) {
	t.Parallel()

	dir := writeFiles(t, map[string]string{
		"base.yammm": `schema "base"

type Customer {
    id String primary
}
`,
		"first.yammm": `schema "first"

import "./base.yammm" as b

type A {
    id String primary
    --> C (one) b.Customer
}
`,
		"second.yammm": `schema "second"

import "./base.yammm" as b

type B {
    id String primary
    --> C (one) b.Customer
}
`,
	})

	for range 8 {
		reg := schema.NewRegistry()
		var (
			wg      sync.WaitGroup
			mu      sync.Mutex
			entries []*schema.Schema
		)
		for _, entry := range []string{"first.yammm", "second.yammm"} {
			wg.Go(func() {
				s, res := schema.Load(t.Context(), filepath.Join(dir, entry), schema.WithModuleRoot(dir), schema.WithRegistry(reg))
				if res.Err() != nil {
					t.Errorf("load %s: %v", entry, res.Err())
					return
				}
				mu.Lock()
				entries = append(entries, s)
				mu.Unlock()
			})
		}
		wg.Wait()

		for _, s := range entries {
			imp := s.ImportsSlice()[0]
			canonical, ok := reg.LookupBySourceID(imp.ResolvedSourceID())
			if !ok {
				t.Fatal("the import's schema is not in the registry")
			}
			if imp.Schema() != canonical {
				t.Fatalf("%s wires a duplicate of %s rather than the registered object", s.Name(), canonical.Name())
			}
		}
	}
}

// Every sealed object is sealed once, by its creator; a second call is a bug
// and panics as the mutators it protects do.
func TestSeal_SecondCallPanics(t *testing.T) {
	t.Parallel()

	rel := schema.TestNewRelation(schema.RelationAssociation, "R", "r", schema.TypeRef{}, schema.TypeID{},
		location.Span{}, "", false, false, "Owner", nil)
	schema.TestSealRelation(rel)
	yammmtest.AssertPanics(t, func() { schema.TestSealRelation(rel) })

	imp := schema.TestNewImport("./x.yammm", "x", location.SourceID{}, location.Span{})
	schema.TestSealImport(imp)
	yammmtest.AssertPanics(t, func() { schema.TestSealImport(imp) })

	s := schema.TestNewSchema("s", location.SourceID{}, location.Span{}, "")
	schema.TestSealSchema(s)
	yammmtest.AssertPanics(t, func() { schema.TestSealSchema(s) })

	typ := schema.TestNewType("T", location.SourceID{}, location.Span{}, "", false, false)
	schema.TestSealType(typ)
	yammmtest.AssertPanics(t, func() { schema.TestSealType(typ) })

	dt := schema.TestNewDataType("D", schema.NewStringConstraint(), location.Span{}, "")
	schema.TestSealDataType(dt)
	yammmtest.AssertPanics(t, func() { schema.TestSealDataType(dt) })
}

// A schema source is read up to a fixed bound; a larger file is refused before
// it is parsed, whether it is the entry or an import.
func TestLoad_RefusesAnOversizedSource(t *testing.T) {
	t.Parallel()

	big := make([]byte, 16<<20+1)
	copy(big, "schema \"big\"\n")
	for i := len("schema \"big\"\n"); i < len(big); i++ {
		big[i] = ' '
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "big.yammm"), big, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.yammm"), []byte("schema \"main\"\n\nimport \"./big.yammm\" as b\n\ntype T {\n    id String primary\n}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, res := schema.Load(t.Context(), filepath.Join(dir, "big.yammm"), schema.WithModuleRoot(dir))
	if res.Err() == nil || !strings.Contains(res.Err().Error(), "larger than") {
		t.Errorf("an oversized entry must be refused by size; got %v", res.Err())
	}
	_, res = schema.Load(t.Context(), filepath.Join(dir, "main.yammm"), schema.WithModuleRoot(dir))
	if res.Err() == nil || !strings.Contains(res.Err().Error(), "larger than") {
		t.Errorf("an oversized import must be refused by size; got %v", res.Err())
	}
}

// Entry selection over the sources map is deterministic even when a key is
// the empty string, which is a legal key and not a "nothing chosen" sentinel.
func TestLoadSourcesWithEntry_EmptyKeyIsAKey(t *testing.T) {
	t.Parallel()

	sources := map[string][]byte{
		"":        []byte("schema \"alpha\"\n\ntype A {\n    id String primary\n}\n"),
		"b.yammm": []byte("schema \"bravo\"\n\ntype B {\n    id String primary\n}\n"),
	}
	for i := range 20 {
		s, res := schema.LoadSourcesWithEntry(t.Context(), sources, "", "", schema.WithSourcesOnly(true))
		if res.Err() != nil {
			t.Fatalf("run %d: %v", i, res.Err())
		}
		if s == nil || s.Name() != "alpha" {
			t.Fatalf("run %d chose %v; want the schema under the empty key", i, s)
		}
	}
}

// A root re-loaded against a registry that already holds it is the registered
// object, and so carries the module root of the load that first compiled it,
// as a cache-reused import does.
func TestSharedRegistry_ReloadedRootKeepsTheFirstLoadsRoot(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "nested"), 0o750); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "nested", "a.yammm")
	if err := os.WriteFile(path, []byte("schema \"a\"\n\ntype A {\n    id String primary\n}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	reg := schema.NewRegistry()
	first, res := schema.Load(t.Context(), path, schema.WithModuleRoot(filepath.Dir(path)), schema.WithRegistry(reg))
	if res.Err() != nil {
		t.Fatal(res.Err())
	}
	second, res := schema.Load(t.Context(), path, schema.WithModuleRoot(dir), schema.WithRegistry(reg))
	if res.Err() != nil {
		t.Fatal(res.Err())
	}
	if first != second {
		t.Fatal("the second load must return the registered object")
	}
	if got, want := second.ModuleRoot(), first.ModuleRoot(); got != want || got == dir {
		t.Errorf("the second load's root is %q; want the first load's %q, not its own %q", got, want, dir)
	}
}

// The structural hash excludes annotations and documentation by design, so a
// registry keyed on it alone would serve a stale object after either changed.
// Bytes decide: the edited file fails to re-register, loudly.
func TestSharedRegistry_EditedSourceIsNotTheSameSource(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "a.yammm")
	write := func(annotation string) {
		t.Helper()
		src := "schema \"a\"\n\ntype A {\n    id String primary\n    name String " + annotation + "\n}\n"
		if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	reg := schema.NewRegistry()
	write("")
	first, res := schema.Load(t.Context(), path, schema.WithModuleRoot(dir), schema.WithRegistry(reg))
	if res.Err() != nil {
		t.Fatal(res.Err())
	}
	write("@index")
	second, res := schema.Load(t.Context(), path, schema.WithModuleRoot(dir), schema.WithRegistry(reg))
	if res.Err() == nil || !strings.Contains(res.Err().Error(), "changed since it was registered") {
		t.Fatalf("an edited source must not re-register; got %v", res.Err())
	}
	if !res.HasCode(diag.E_LOAD_SOURCE_CHANGED) {
		t.Errorf("a changed source is the caller's condition, not the loader's; got %v", res.Err())
	}
	if second != nil {
		t.Error("a failed load returns no schema")
	}
	if kept, _ := reg.LookupBySourceID(first.SourceID()); kept != first {
		t.Error("the registry must keep the first object")
	}
}

// A schema with sources beside one without is not the same source: a
// Builder-built schema registered under a file's SourceID does not stand in
// for a load of that file, whichever arrives first.
func TestSharedRegistry_AsymmetricProvenanceIsNotTheSameSource(t *testing.T) {
	t.Parallel()

	dir := writeFiles(t, map[string]string{
		"a.yammm": "schema \"a\"\n\ntype A {\n    id String primary\n}\n",
	})
	path := filepath.Join(dir, "a.yammm")
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	id, err := location.SourceIDFromAbsolutePath(canonical)
	if err != nil {
		t.Fatal(err)
	}
	built, bres := schema.NewBuilder().WithName("a").WithSourceID(id).
		AddType("A").WithPrimaryKey("id", schema.NewStringConstraint()).Done().Build()
	if bres.Err() != nil {
		t.Fatal(bres.Err())
	}

	t.Run("built first", func(t *testing.T) {
		t.Parallel()
		reg := schema.NewRegistry()
		if err := reg.Register(built); err != nil {
			t.Fatal(err)
		}
		loaded, res := schema.Load(t.Context(), path, schema.WithModuleRoot(dir), schema.WithRegistry(reg))
		if !res.HasCode(diag.E_LOAD_SOURCE_CHANGED) {
			t.Fatalf("a load must not adopt a source-less registered schema; got %v", res.Err())
		}
		if loaded != nil {
			t.Error("a refused load returns no schema")
		}
	})

	t.Run("loaded first", func(t *testing.T) {
		t.Parallel()
		reg := schema.NewRegistry()
		if _, res := schema.Load(t.Context(), path, schema.WithModuleRoot(dir), schema.WithRegistry(reg)); res.Err() != nil {
			t.Fatal(res.Err())
		}
		err := reg.Register(built)
		regErr, ok := errors.AsType[*schema.RegistryError](err)
		if !ok || regErr.Kind != schema.DuplicateSourceID {
			t.Fatalf("a source-less schema must not replace a loaded one; got %v", err)
		}
	})
}

// The byte rule reads every source both schemas carry, so an import edited
// beneath an unchanged entry is a changed source too when the load supplies
// the import's bytes — as an in-memory load does.
func TestSharedRegistry_EditedImportIsNotTheSameSource(t *testing.T) {
	t.Parallel()

	entry := []byte("schema \"app\"\n\nimport \"./base.yammm\" as b\n\ntype T {\n    id String primary\n    --> C (one) b.Customer\n}\n")
	base := func(annotation string) []byte {
		return []byte("schema \"base\"\n\ntype Customer {\n    id String primary\n    name String " + annotation + "\n}\n")
	}
	reg := schema.NewRegistry()
	sources := map[string][]byte{"main.yammm": entry, "base.yammm": base("")}
	if _, res := schema.LoadSourcesWithEntry(t.Context(), sources, "main.yammm", "", schema.WithSourcesOnly(true), schema.WithRegistry(reg)); res.Err() != nil {
		t.Fatal(res.Err())
	}
	sources["base.yammm"] = base("@index")
	_, res := schema.LoadSourcesWithEntry(t.Context(), sources, "main.yammm", "", schema.WithSourcesOnly(true), schema.WithRegistry(reg))
	if !res.HasCode(diag.E_LOAD_SOURCE_CHANGED) {
		t.Errorf("an edited import beneath an unchanged entry must not re-register; got %v", res.Err())
	}
}

// A source of exactly the bound is legal; the first byte beyond it is refused.
func TestLoad_SourceBoundIsInclusive(t *testing.T) {
	t.Parallel()

	head := "schema \"edge\"\n\ntype T {\n    id String primary\n}\n"
	exact := make([]byte, 16<<20)
	copy(exact, head)
	for i := len(head); i < len(exact); i++ {
		exact[i] = ' '
	}
	dir := writeFiles(t, map[string]string{"edge.yammm": string(exact)})
	if _, res := schema.Load(t.Context(), filepath.Join(dir, "edge.yammm"), schema.WithModuleRoot(dir)); res.Err() != nil {
		t.Errorf("a source of exactly the bound must load; got %v", res.Err())
	}
}
