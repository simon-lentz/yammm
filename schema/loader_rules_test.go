package schema_test

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/simon-lentz/yammm/internal/yammmtest"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

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
	var first string
	for i := range 20 {
		s, res := schema.LoadSourcesWithEntry(t.Context(), sources, "", "", schema.WithSourcesOnly(true))
		got := "schema loaded"
		if res.Err() != nil {
			got = "error: " + res.Err().Error()
		} else if s != nil {
			got = "schema " + s.Name()
		}
		if i == 0 {
			first = got
			continue
		}
		if got != first {
			t.Fatalf("run %d chose %q where run 0 chose %q", i, got, first)
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
	if second.ModuleRoot() != first.ModuleRoot() {
		t.Errorf("the registered object's root moved: %q then %q", first.ModuleRoot(), second.ModuleRoot())
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
	if res.Err() == nil || !strings.Contains(res.Err().Error(), "bytes changed") {
		t.Fatalf("an edited source must not re-register; got %v", res.Err())
	}
	if second != nil {
		t.Error("a failed load returns no schema")
	}
	if kept, _ := reg.LookupBySourceID(first.SourceID()); kept != first {
		t.Error("the registry must keep the first object")
	}
}
