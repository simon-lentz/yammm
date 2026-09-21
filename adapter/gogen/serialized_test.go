package gogen

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/internal/yammmtest"
	"github.com/simon-lentz/yammm/schema"
)

// loadFixture loads a testdata schema from disk, optionally under an explicit
// module root, the way Marshal's callers do.
func loadFixture(t *testing.T, name, moduleRoot string) *schema.Schema {
	t.Helper()
	yammmtest.RequireNoModuleRoot(t, schema.FindModuleRoot)
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
	keys, err := resolveKeyRoot(s)
	if err != nil {
		t.Fatal(err)
	}
	want, err := keys.key(ids[0])
	if err != nil {
		t.Fatal(err)
	}

	got, err := Marshal(s)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	decl := fmt.Sprintf("const SerializedEntry = %q\n", want)
	if !strings.Contains(string(got), decl) {
		t.Errorf("emitted entry key does not match keyRoot.key; want the line %q", strings.TrimSuffix(decl, "\n"))
	}
}
