package gogen

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/schema"
)

// loadFixture loads a testdata schema from disk, optionally under an explicit
// module root, the way Marshal's callers do.
func loadFixture(t *testing.T, name, moduleRoot string) *schema.Schema {
	t.Helper()
	path, err := filepath.Abs(filepath.Join("testdata", name+".yammm"))
	if err != nil {
		t.Fatalf("abs path: %v", err)
	}
	var opts []schema.LoadOption
	if moduleRoot != "" {
		root, err := filepath.Abs(filepath.Join("testdata", moduleRoot))
		if err != nil {
			t.Fatalf("abs module root: %v", err)
		}
		opts = append(opts, schema.WithModuleRoot(root))
	}
	s, res := schema.Load(t.Context(), path, opts...)
	if res.HasErrors() {
		t.Fatalf("load %s: %v", name, res.Err())
	}
	return s
}

// TestSerializedEntry_SingleSourceMatchesSourceKey pins the half of the re-load
// pair that is computed rather than mechanical. The map conversion cannot be
// wrong, so this key is the only place the derivation can be — and a wrong entry
// key is a hermetic-load miss at the consumer, not a generation failure.
func TestSerializedEntry_SingleSourceMatchesSourceKey(t *testing.T) {
	t.Parallel()

	s := loadFixture(t, "scalars", "")
	ids := s.Sources().SourceIDs()
	if len(ids) != 1 {
		t.Fatalf("expected a single-source fixture, got %d sources", len(ids))
	}
	want := sourceKey(s.ModuleRoot(), s.SourceID(), ids[0])

	got, err := Marshal(s)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	decl := fmt.Sprintf("const SerializedEntry = %q\n", want)
	if !strings.Contains(string(got), decl) {
		t.Errorf("emitted entry key does not match sourceKey; want the line %q", strings.TrimSuffix(decl, "\n"))
	}
}

// TestUniformSources_ReloadUnderSyntheticRoot is the coverage the round-trip
// self-check deliberately does not carry: the emitted uniform shape re-loaded
// under the synthetic root its own recipe recommends. Running it over the corpus
// exercises realistic closures — a diamond, a nested module root, a
// cross-schema name collision — while excluding the relative-import fixture by
// construction rather than by duplicating the loader's prefix rule here.
func TestUniformSources_ReloadUnderSyntheticRoot(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ name, moduleRoot string }{
		{name: "scalars"},
		{name: "full"},
		{name: "imports/main"},
		{name: "imports/inherit_main"},
		{name: "imports/collision_main"},
		{name: "imports/diamond_main"},
		{name: "modroot/a/b/entry", moduleRoot: "modroot"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := loadFixture(t, tc.name, tc.moduleRoot)
			if _, err := Marshal(s); err != nil {
				t.Fatalf("Marshal: %v", err)
			}

			// Rebuild exactly what SerializedSources()/SerializedEntry emit.
			srcs := s.Sources()
			entry := s.SourceID()
			root := s.ModuleRoot()
			sources := map[string][]byte{}
			for _, id := range srcs.SourceIDs() {
				content, ok := srcs.ContentBySource(id)
				if !ok {
					t.Fatalf("source %s content unavailable", id)
				}
				sources[sourceKey(root, entry, id)] = content
			}

			got, res := schema.LoadSourcesWithEntry(context.Background(), sources,
				sourceKey(root, entry, entry), "",
				schema.WithSourcesOnly(true), schema.WithSyntheticRoot("embedded://fixture"))
			if res.HasErrors() {
				t.Fatalf("synthetic-root re-load: %v", res.Err())
			}
			if h, want := schema.StructuralHash(got), schema.StructuralHash(s); h != want {
				t.Errorf("synthetic-root re-load hash = %s, want %s", h, want)
			}
			for _, cs := range got.Closure() {
				if !strings.HasPrefix(cs.SourceID().String(), "embedded://fixture/") {
					t.Errorf("closure member %s is not under the synthetic root", cs.SourceID())
				}
			}
		})
	}
}
