package gogen_test

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/adapter/gogen"
	"github.com/simon-lentz/yammm/schema"
)

// consumerRoot is a synthetic root no generator code names, so a re-load under
// it depends on nothing but the emitted keys.
const consumerRoot = "embedded://consumer"

// writeTree writes files, keyed by slash-separated path, under root.
func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// symlink makes link point at target, skipping the test where the host cannot.
func symlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
}

// loadEntry loads root/entry with root as the module root.
func loadEntry(t *testing.T, root, entry string) *schema.Schema {
	t.Helper()
	s, res := schema.Load(context.Background(), filepath.Join(root, filepath.FromSlash(entry)), schema.WithModuleRoot(root))
	if res.HasErrors() {
		t.Fatalf("load %s: %v", entry, res.Err())
	}
	return s
}

// emittedStore reads the embedded store and SerializedEntry out of generated
// source, the artefact a consumer compiles, rather than out of the generator.
func emittedStore(t *testing.T, src []byte) (map[string][]byte, string) {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), "gen.go", src, 0)
	if err != nil {
		t.Fatalf("parse generated source: %v", err)
	}
	unquote := func(e ast.Expr) string {
		t.Helper()
		lit, ok := e.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			t.Fatalf("expected a string literal, got %T", e)
		}
		s, err := strconv.Unquote(lit.Value)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	store := map[string][]byte{}
	entry, sawStore := "", false
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok || len(vs.Names) != 1 || len(vs.Values) != 1 {
				continue
			}
			switch vs.Names[0].Name {
			case "serializedSources":
				sawStore = true
				for _, elt := range vs.Values[0].(*ast.CompositeLit).Elts {
					kv := elt.(*ast.KeyValueExpr)
					key := unquote(kv.Key)
					if _, dup := store[key]; dup {
						t.Fatalf("the store holds the key %q twice", key)
					}
					store[key] = []byte(unquote(kv.Value))
				}
			case "SerializedEntry":
				entry = unquote(vs.Values[0])
			}
		}
	}
	if !sawStore || entry == "" {
		t.Fatalf("generated source holds no store or no SerializedEntry (store %v, entry %q)", sawStore, entry)
	}
	return store, entry
}

// reloadEmitted re-loads generated source's embedded store through the recipe
// the file prints, under consumerRoot, and requires the input's hash.
func reloadEmitted(t *testing.T, src []byte, want *schema.Schema) (map[string][]byte, string) {
	t.Helper()
	store, entry := emittedStore(t, src)
	got, res := schema.LoadSourcesWithEntry(context.Background(), store, entry, "",
		schema.WithSourcesOnly(true), schema.WithSyntheticRoot(consumerRoot))
	if res.HasErrors() {
		t.Fatalf("the emitted store does not re-load through its recipe: %v", res.Err())
	}
	if g, w := schema.StructuralHash(got), schema.StructuralHash(want); g != w {
		t.Errorf("re-loaded hash %s, want %s", g, w)
	}
	for _, cs := range got.Closure() {
		if !strings.HasPrefix(cs.SourceID().String(), consumerRoot+"/") {
			t.Errorf("closure member %s is not under the synthetic root", cs.SourceID())
		}
	}
	return store, entry
}

// TestMarshal_SymlinkedImportDirKeysByImportText pins that an import through a
// symlinked directory (lib -> real) keys by the text that imports it. Its
// identity is real/dep.yammm, which the re-load never asks for, so from any
// working directory the store holds lib/dep.yammm and re-loads.
func TestMarshal_SymlinkedImportDirKeysByImportText(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"a/b/entry.yammm": "schema \"registry\"\n\nimport \"lib/dep\" as dep\n\ntype Contract {\n\tcontract_id String primary\n\t--> SUPPLIED_BY (one) dep.Vendor\n}\n",
		"real/dep.yammm":  "schema \"deplib\"\n\ntype Vendor {\n\tvendor_id String primary\n}\n",
	})
	symlink(t, "real", filepath.Join(root, "lib"))
	s := loadEntry(t, root, "a/b/entry.yammm")

	for _, cwd := range []string{root, t.TempDir()} {
		t.Chdir(cwd)
		got, err := gogen.Marshal(s)
		if err != nil {
			t.Fatalf("Marshal from %s: %v", cwd, err)
		}
		store, _ := reloadEmitted(t, got, s)
		if _, ok := store["lib/dep.yammm"]; !ok || len(store) != 2 {
			t.Errorf("from %s the store holds %v, want a/b/entry.yammm and lib/dep.yammm", cwd, keysOf(store))
		}
	}
}

// TestMarshal_SourceImportedByTwoPathsIsRefused pins the refusal of one source
// two import paths reach under two keys: a store holding both keys fails to
// re-load, since the loader refuses one schema name registered twice.
func TestMarshal_SourceImportedByTwoPathsIsRefused(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"main.yammm":     "schema \"main\"\n\nimport \"a\" as a\nimport \"b\" as b\n\ntype M {\n\tid String primary\n\t--> TO_A (one) a.A\n\t--> TO_B (one) b.B\n}\n",
		"a.yammm":        "schema \"a\"\n\nimport \"lib/dep\" as dep\n\ntype A {\n\tid String primary\n\t--> TO_D (one) dep.D\n}\n",
		"b.yammm":        "schema \"b\"\n\nimport \"real/dep\" as dep\n\ntype B {\n\tid String primary\n\t--> TO_D (one) dep.D\n}\n",
		"real/dep.yammm": "schema \"dep\"\n\ntype D {\n\tid String primary\n}\n",
	})
	symlink(t, "real", filepath.Join(root, "lib"))
	s := loadEntry(t, root, "main.yammm")

	for _, cwd := range []string{root, t.TempDir()} {
		t.Chdir(cwd)
		_, err := gogen.Marshal(s)
		if err == nil {
			t.Fatalf("Marshal from %s generated a source imported by two paths", cwd)
		}
		for _, want := range []string{`"lib/dep"`, `"real/dep"`, `"lib/dep.yammm"`, `"real/dep.yammm"`} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error does not name %s: %v", want, err)
			}
		}
	}
}

// TestMarshal_OneSourceUnderOneKeyByTwoSpellings pins that the refusal above
// compares keys, not import texts: "lib/dep" from the entry and "./dep" from
// lib/x.yammm name one key, so the source embeds once.
func TestMarshal_OneSourceUnderOneKeyByTwoSpellings(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"main.yammm":    "schema \"main\"\n\nimport \"lib/dep\" as dep\nimport \"lib/x\" as x\n\ntype M {\n\tid String primary\n\t--> TO_D (one) dep.D\n\t--> TO_X (one) x.X\n}\n",
		"lib/x.yammm":   "schema \"x\"\n\nimport \"./dep\" as dep\n\ntype X {\n\tid String primary\n\t--> TO_D (one) dep.D\n}\n",
		"lib/dep.yammm": "schema \"dep\"\n\ntype D {\n\tid String primary\n}\n",
	})
	s := loadEntry(t, root, "main.yammm")
	got, err := gogen.Marshal(s)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	store, _ := reloadEmitted(t, got, s)
	if len(store) != 3 {
		t.Errorf("the store holds %v, want main.yammm, lib/x.yammm and lib/dep.yammm", keysOf(store))
	}
}

// TestMarshal_TwoSourcesTakingOneKeyAreRefused pins the other half of the key
// invariant. A relative import resolves through the importing file's real
// directory on load, and by its key's text on re-load. Through a symlinked
// directory (lib -> deep/real), lib/x.yammm's "../dep" reads deep/dep.yammm and
// keys as dep.yammm, which the root's own dep.yammm also takes.
func TestMarshal_TwoSourcesTakingOneKeyAreRefused(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"main.yammm":        "schema \"main\"\n\nimport \"dep\" as dep\nimport \"lib/x\" as x\n\ntype M {\n\tid String primary\n\t--> TO_D (one) dep.D\n\t--> TO_X (one) x.X\n}\n",
		"dep.yammm":         "schema \"dep\"\n\ntype D {\n\tid String primary\n}\n",
		"deep/real/x.yammm": "schema \"x\"\n\nimport \"../dep\" as other\n\ntype X {\n\tid String primary\n\t--> TO_O (one) other.O\n}\n",
		"deep/dep.yammm":    "schema \"other\"\n\ntype O {\n\tid String primary\n}\n",
	})
	symlink(t, filepath.Join("deep", "real"), filepath.Join(root, "lib"))
	s := loadEntry(t, root, "main.yammm")
	_, err := gogen.Marshal(s)
	if err == nil {
		t.Fatal("Marshal embedded two sources under one key")
	}
	for _, want := range []string{`"dep.yammm"`, "deep/dep.yammm", `"../dep"`, `imported as "dep" by`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not name %s: %v", want, err)
		}
	}
}

// TestMarshal_ImportTakingTheEntrysKeyIsRefused pins the key invariant
// against the entry: through sub -> deep/other, sub/x.yammm's "../main" reads
// deep/main.yammm and keys as main.yammm, the entry's own key.
func TestMarshal_ImportTakingTheEntrysKeyIsRefused(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"main.yammm":         "schema \"main\"\n\nimport \"sub/x\" as x\n\ntype M {\n\tid String primary\n\t--> TO_X (one) x.X\n}\n",
		"deep/other/x.yammm": "schema \"x\"\n\nimport \"../main\" as m\n\ntype X {\n\tid String primary\n\t--> TO_N (one) m.N\n}\n",
		"deep/main.yammm":    "schema \"m2\"\n\ntype N {\n\tid String primary\n}\n",
	})
	symlink(t, filepath.Join("deep", "other"), filepath.Join(root, "sub"))
	s := loadEntry(t, root, "main.yammm")
	_, err := gogen.Marshal(s)
	if err == nil {
		t.Fatal("Marshal embedded an import under the entry's key")
	}
	for _, want := range []string{`"main.yammm"`, "the entry", `"../main"`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not name %s: %v", want, err)
		}
	}
}

// TestMarshal_KeysForSourcesNothingImports pins the keys of a source the
// closure holds but no import names: it keys by its place under the root, as
// the entry does, under a file root and under a synthetic root.
func TestMarshal_KeysForSourcesNothingImports(t *testing.T) {
	t.Parallel()
	sources := map[string][]byte{
		"main.yammm":    []byte("schema \"main\"\n\nimport \"lib/dep\" as dep\n\ntype M {\n\tid String primary\n\t--> TO_D (one) dep.D\n}\n"),
		"lib/dep.yammm": []byte("schema \"dep\"\n\ntype D {\n\tid String primary\n}\n"),
		"extra.yammm":   []byte("schema \"extra\"\n\ntype E {\n\tid String primary\n}\n"),
	}
	for name, opts := range map[string][]schema.LoadOption{
		"file root":      {schema.WithSourcesOnly(true)},
		"synthetic root": {schema.WithSourcesOnly(true), schema.WithSyntheticRoot("embedded://gen")},
	} {
		root := ""
		if name == "file root" {
			root = t.TempDir()
		}
		s, res := schema.LoadSourcesWithEntry(context.Background(), sources, "main.yammm", root, opts...)
		if res.HasErrors() {
			t.Fatalf("%s: load: %v", name, res.Err())
		}
		got, err := gogen.Marshal(s)
		if err != nil {
			t.Fatalf("%s: Marshal: %v", name, err)
		}
		store, _ := emittedStore(t, got)
		for _, key := range []string{"main.yammm", "lib/dep.yammm", "extra.yammm"} {
			if store[key] == nil {
				t.Errorf("%s: the store holds %v, want %s", name, keysOf(store), key)
			}
		}
	}
}

// TestMarshal_NoRootFileEntryKeysUnderItsDirectory pins a file entry loaded
// with no module root: its keys are relative to its own directory, with ".."
// segments for a relative import that climbs out of it.
func TestMarshal_NoRootFileEntryKeysUnderItsDirectory(t *testing.T) {
	t.Parallel()
	top := t.TempDir()
	dep := []byte("schema \"dep\"\n\ntype D {\n\tid String primary\n}\n")
	up := []byte("schema \"up\"\n\ntype U {\n\tid String primary\n}\n")
	main := []byte("schema \"main\"\n\nimport \"./dep\" as dep\nimport \"../up/dep\" as up\n\ntype M {\n\tid String primary\n\t--> TO_D (one) dep.D\n\t--> TO_U (one) up.U\n}\n")
	s, res := schema.LoadSourcesWithEntry(context.Background(), map[string][]byte{
		filepath.Join(top, "app", "main.yammm"): main,
		filepath.Join(top, "app", "dep.yammm"):  dep,
		filepath.Join(top, "up", "dep.yammm"):   up,
	}, filepath.Join(top, "app", "main.yammm"), "", schema.WithSourcesOnly(true))
	if res.HasErrors() {
		t.Fatalf("load: %v", res.Err())
	}
	got, err := gogen.Marshal(s)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	store, entry := reloadEmitted(t, got, s)
	if entry != "main.yammm" || store["dep.yammm"] == nil || store["../up/dep.yammm"] == nil {
		t.Errorf("entry %q, store %v; want main.yammm, dep.yammm and ../up/dep.yammm", entry, keysOf(store))
	}
}

// TestMarshal_SourceAtTheRootItselfIsRefused pins the refusal of a source
// whose identity is the root directory, which has no relative path: as the
// entry and as a source nothing imports.
func TestMarshal_SourceAtTheRootItselfIsRefused(t *testing.T) {
	t.Parallel()
	src := []byte("schema \"p\"\n\ntype P {\n\tid String primary\n}\n")
	for name, tc := range map[string]struct {
		sources map[string][]byte
		entry   string
	}{
		"the entry":             {map[string][]byte{".": src}, "."},
		"a source not imported": {map[string][]byte{"main.yammm": src, ".": src}, "main.yammm"},
	} {
		s, res := schema.LoadSourcesWithEntry(context.Background(), tc.sources, tc.entry, t.TempDir(), schema.WithSourcesOnly(true))
		if res.HasErrors() {
			t.Fatalf("%s: load: %v", name, res.Err())
		}
		if _, err := gogen.Marshal(s); err == nil || !strings.Contains(err.Error(), "has no key relative to the schema's root") {
			t.Errorf("%s: Marshal = %v, want the no-relative-key refusal", name, err)
		}
	}
}

// TestMarshal_KeysUnderADecomposedModuleRoot pins that the root is compared as
// an identity: a module root spelled with a decomposed character is NFC in
// every source identity, so its host bytes must not reach the keys.
func TestMarshal_KeysUnderADecomposedModuleRoot(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), "café")
	writeTree(t, root, map[string]string{
		"main.yammm":    "schema \"main\"\n\nimport \"lib/dep\" as dep\n\ntype M {\n\tid String primary\n\t--> TO_D (one) dep.D\n}\n",
		"lib/dep.yammm": "schema \"dep\"\n\ntype D {\n\tid String primary\n}\n",
	})
	s := loadEntry(t, root, "main.yammm")
	got, err := gogen.Marshal(s)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	store, entry := reloadEmitted(t, got, s)
	if entry != "main.yammm" || len(store) != 2 || store["lib/dep.yammm"] == nil {
		t.Errorf("entry %q, store %v; want main.yammm and lib/dep.yammm", entry, keysOf(store))
	}
}

// TestMarshal_EntryOutsideTheModuleRoot pins the ".." key for an entry the
// module root does not hold, a legal layout: imports are sandboxed, the entry
// is not.
func TestMarshal_EntryOutsideTheModuleRoot(t *testing.T) {
	t.Parallel()
	top := t.TempDir()
	writeTree(t, top, map[string]string{
		"app/main.yammm":    "schema \"main\"\n\nimport \"lib/dep\" as dep\n\ntype M {\n\tid String primary\n\t--> TO_D (one) dep.D\n}\n",
		"mod/lib/dep.yammm": "schema \"dep\"\n\ntype D {\n\tid String primary\n}\n",
	})
	s, res := schema.Load(context.Background(), filepath.Join(top, "app", "main.yammm"), schema.WithModuleRoot(filepath.Join(top, "mod")))
	if res.HasErrors() {
		t.Fatalf("load: %v", res.Err())
	}
	got, err := gogen.Marshal(s)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	store, entry := reloadEmitted(t, got, s)
	if entry != "../app/main.yammm" || store["lib/dep.yammm"] == nil {
		t.Errorf("entry %q, store %v; want ../app/main.yammm and lib/dep.yammm", entry, keysOf(store))
	}
}

// TestMarshal_LoadStringKeysByBaseName pins the key of a source LoadString
// minted: it has no root and imports nothing, so it keys by its base name
// under either separator, or by its schema's name where the name has no base.
func TestMarshal_LoadStringKeysByBaseName(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"person.yammm", "dir/person.yammm", `C:\schemas\person.yammm`, `\\srv\share\person.yammm`} {
		s, res := schema.LoadString(context.Background(), "schema \"p\"\n\ntype Person {\n\tid String primary\n}\n", name)
		if res.HasErrors() {
			t.Fatalf("load: %v", res.Err())
		}
		got, err := gogen.Marshal(s)
		if err != nil {
			t.Fatalf("Marshal %s: %v", name, err)
		}
		if _, entry := reloadEmitted(t, got, s); entry != "person.yammm" {
			t.Errorf("LoadString(%q) keys as %q, want person.yammm", name, entry)
		}
	}
	// A name with no usable base keys by the schema's name.
	for _, name := range []string{".", "x/.", "..", "/"} {
		s, res := schema.LoadString(context.Background(), "schema \"p\"\n\ntype Person {\n\tid String primary\n}\n", name)
		if res.HasErrors() {
			t.Fatalf("load %q: %v", name, res.Err())
		}
		got, err := gogen.Marshal(s)
		if err != nil {
			t.Fatalf("Marshal %q: %v", name, err)
		}
		if _, entry := reloadEmitted(t, got, s); entry != "p.yammm" {
			t.Errorf("LoadString(%q) keys as %q, want p.yammm", name, entry)
		}
	}
}

// TestMarshal_SyntheticRootLoadWithARelativeImport pins a schema loaded under
// a synthetic root, whose relative import resolves by its key.
func TestMarshal_SyntheticRootLoadWithARelativeImport(t *testing.T) {
	t.Parallel()
	s, res := schema.LoadSourcesWithEntry(context.Background(), map[string][]byte{
		"app/main.yammm": []byte("schema \"main\"\n\nimport \"../lib/dep\" as dep\n\ntype M {\n\tid String primary\n\t--> TO_D (one) dep.D\n}\n"),
		"lib/dep.yammm":  []byte("schema \"dep\"\n\ntype D {\n\tid String primary\n}\n"),
	}, "app/main.yammm", "", schema.WithSourcesOnly(true), schema.WithSyntheticRoot("embedded://gen"))
	if res.HasErrors() {
		t.Fatalf("load: %v", res.Err())
	}
	got, err := gogen.Marshal(s)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	store, entry := reloadEmitted(t, got, s)
	if entry != "app/main.yammm" || store["lib/dep.yammm"] == nil {
		t.Errorf("entry %q, store %v; want app/main.yammm and lib/dep.yammm", entry, keysOf(store))
	}
}

// TestMarshal_EmbeddedStoreReloadsThroughItsRecipe is the second
// implementation of the embedded-key contract. The generator derives keys; the
// loader looks them up. It reads the store out of the generated file, never
// out of the generator, and re-loads it through the printed recipe under a
// root no generator code names, from a directory holding no schema.
func TestMarshal_EmbeddedStoreReloadsThroughItsRecipe(t *testing.T) {
	type input struct {
		name string
		s    *schema.Schema
	}
	var inputs []input
	for _, name := range []string{"scalars", "full", "temporal", "imports/main", "imports/inherit_main", "imports/collision_main", "imports/diamond_main", "imports/rel_main", "imports/tagform_collision_main", "imports/qualified_name_main", "imports/temporal_main", "imports/entry_shadow_main"} {
		inputs = append(inputs, input{name, loadSchema(t, name)})
	}
	inputs = append(inputs, input{"modroot", loadModrootSchema(t)})

	t.Chdir(t.TempDir())
	for _, in := range inputs {
		got, err := gogen.Marshal(in.s)
		if err != nil {
			t.Fatalf("%s: Marshal: %v", in.name, err)
		}
		store, _ := reloadEmitted(t, got, in.s)
		if len(store) != len(in.s.Sources().SourceIDs()) {
			t.Errorf("%s: the store holds %d sources, the closure %d", in.name, len(store), len(in.s.Sources().SourceIDs()))
		}
		for key := range store {
			if filepath.IsAbs(key) || strings.HasPrefix(key, "/") || strings.Contains(key, ":") {
				t.Errorf("%s: key %q is a generation-machine path", in.name, key)
			}
		}
	}
}

func keysOf(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
