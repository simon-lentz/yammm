package analysis

import (
	"path/filepath"
	"testing"

	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/lsp/internal/lsputil"
	"github.com/simon-lentz/yammm/schema"
)

// assertImportsResolved walks s's import closure and fails on an import with no schema.
func assertImportsResolved(t *testing.T, s schema.Schema, seen map[location.SourceID]bool) {
	t.Helper()
	if seen[s.SourceID()] {
		return
	}
	seen[s.SourceID()] = true
	for imp := range s.Imports() {
		resolved := imp.Schema()
		if resolved.IsZero() {
			t.Fatalf("import %q of %s holds no schema", imp.Path(), s.SourceID())
		}
		assertImportsResolved(t, resolved, seen)
	}
}

// The language server's readers of an import rely on this: a snapshot's schema
// holds no unresolved import, whatever the document says.
func TestAnalyze_ASnapshotSchemaHoldsOnlyResolvedImports(t *testing.T) {
	const dep = "schema \"dep\"\ntype Wheel {\n  id String primary\n}\n"
	cases := []struct {
		name    string
		sources map[string]string
		opts    []schema.LoadOption
		loads   bool
	}{
		{
			name: "a resolved import and its own import",
			sources: map[string]string{
				"main.yammm": "schema \"main\"\nimport \"./mid\" as mid\n",
				"mid.yammm":  "schema \"mid\"\nimport \"./dep\" as dep\n",
				"dep.yammm":  dep,
			},
			loads: true,
		},
		{
			name:    "a missing file",
			sources: map[string]string{"main.yammm": "schema \"main\"\nimport \"./gone\" as gone\n"},
		},
		{
			name: "an import cycle",
			sources: map[string]string{
				"main.yammm": "schema \"main\"\nimport \"./back\" as back\n",
				"back.yammm": "schema \"back\"\nimport \"./main\" as main\n",
			},
		},
		{
			name:    "a schema importing itself",
			sources: map[string]string{"main.yammm": "schema \"main\"\nimport \"./main\" as self\n"},
		},
		{
			name: "an import that fails to compile",
			sources: map[string]string{
				"main.yammm": "schema \"main\"\nimport \"./bad\" as bad\n",
				"bad.yammm":  "schema \"bad\"\ntype Wheel {\n  id Nope primary\n}\n",
			},
		},
		{
			name: "an import of an import that fails",
			sources: map[string]string{
				"main.yammm": "schema \"main\"\nimport \"./mid\" as mid\n",
				"mid.yammm":  "schema \"mid\"\nimport \"./gone\" as gone\n",
			},
		},
		{
			name: "an alias colliding with a local type",
			sources: map[string]string{
				"main.yammm": "schema \"main\"\nimport \"./dep\" as Wheel\ntype Wheel {\n  id String primary\n}\n",
				"dep.yammm":  dep,
			},
		},
		{
			name: "one alias declared twice",
			sources: map[string]string{
				"main.yammm": "schema \"main\"\nimport \"./dep\" as d\nimport \"./dep2\" as d\n",
				"dep.yammm":  dep,
				"dep2.yammm": "schema \"dep2\"\n",
			},
		},
		{
			name: "one file imported under two aliases",
			sources: map[string]string{
				"main.yammm": "schema \"main\"\nimport \"./dep\" as a\nimport \"./dep\" as b\n",
				"dep.yammm":  dep,
			},
		},
		{
			name:    "a path that leaves the module root",
			sources: map[string]string{"main.yammm": "schema \"main\"\nimport \"../../outside\" as out\n"},
		},
		{
			name:    "a path literal with no value",
			sources: map[string]string{"main.yammm": "schema \"main\"\nimport \"./\\q\" as q\n"},
		},
		{
			name: "imports the load does not allow",
			sources: map[string]string{
				"main.yammm": "schema \"main\"\nimport \"./dep\" as dep\n",
				"dep.yammm":  dep,
			},
			opts: []schema.LoadOption{schema.WithImportsAllowed(false)},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			overlays := make(map[string][]byte, len(tc.sources))
			for name, text := range tc.sources {
				overlays[filepath.Join(root, name)] = []byte(text)
			}
			snapshot, _ := NewAnalyzer(nil).Analyze(t.Context(), filepath.Join(root, "main.yammm"), overlays, root, lsputil.PositionEncodingUTF16, tc.opts...)
			if got := !snapshot.Schema.IsZero(); got != tc.loads {
				t.Fatalf("snapshot holds a schema: %v, want %v; diagnostics: %s", got, tc.loads, snapshot.Result)
			}
			if !tc.loads {
				if len(snapshot.SymbolsBySource) != 0 {
					t.Fatalf("a failed load indexed symbols for %d sources", len(snapshot.SymbolsBySource))
				}
				return
			}
			assertImportsResolved(t, snapshot.Schema, map[location.SourceID]bool{})
		})
	}
}

// A schema a registry holds reaches the analyzer without the loader's wiring.
// The Builder wires its imports, so the closure still holds no unresolved import.
func TestAnalyze_ABuilderSchemaFromARegistryHoldsOnlyResolvedImports(t *testing.T) {
	root := t.TempDir()
	registry := schema.NewRegistry()
	leaf, result := schema.NewBuilder().
		WithName("leaf").
		WithSourceID(location.MustNewSourceID("test://leaf.yammm")).
		AddType("Wheel").WithPrimaryKey("id", schema.NewStringConstraint()).Done().
		Build()
	if result.HasErrors() {
		t.Fatal(result)
	}
	if err := registry.Register(leaf); err != nil {
		t.Fatal(err)
	}
	depPath := filepath.Join(root, "dep.yammm")
	depID, err := location.SourceIDFromPath(depPath)
	if err != nil {
		t.Fatal(err)
	}
	built, result := schema.NewBuilder().
		WithName("dep").
		WithSourceID(depID).
		WithRegistry(registry).
		AddImport("leaf", "leaf").
		Build()
	if result.HasErrors() {
		t.Fatal(result)
	}
	if err := registry.Register(built); err != nil {
		t.Fatal(err)
	}

	main := filepath.Join(root, "main.yammm")
	overlays := map[string][]byte{main: []byte("schema \"main\"\nimport \"./dep\" as dep\n")}
	snapshot, err := NewAnalyzer(nil).Analyze(t.Context(), main, overlays, root, lsputil.PositionEncodingUTF16, schema.WithRegistry(registry))
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Schema.IsZero() {
		t.Fatalf("the load refused the registered import: %s", snapshot.Result)
	}
	assertImportsResolved(t, snapshot.Schema, map[location.SourceID]bool{})
}
