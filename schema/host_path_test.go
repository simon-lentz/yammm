package schema_test

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/internal/yammmtest"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

const (
	hostPathDep   = "schema \"reldep\"\n\ntype Zone {\n\tcode String primary\n}\n"
	hostPathEntry = "schema \"relmain\"\n\nimport \"./rel_dep\" as dep\n\ntype Site {\n\tid String primary\n\t--> IN_ZONE (one) dep.Zone\n}\n"
	// hostPathDepImporting replaces hostPathDep where the import has a
	// relative import of its own.
	hostPathDepImporting = "schema \"reldep\"\n\nimport \"./rel_leaf\" as leaf\n\ntype Zone {\n\tcode String primary\n\t--> IN_REGION (one) leaf.Region\n}\n"
	hostPathLeaf         = "schema \"relleaf\"\n\ntype Region {\n\tcode String primary\n}\n"
)

// TestHostPath_Loads holds the loader to the host's path rules: a schema is
// read, and its imports resolved, by the name the host gives each file. The
// decomposed-name rows hold every import to the host path its importer was
// read from, since the identity spells that name in NFC and may name no file.
// The backslash rows hold that a backslash is part of a file name on Unix.
func TestHostPath_Loads(t *testing.T) {
	t.Parallel()

	rows := []struct {
		name     string
		unixOnly bool
		run      func(t *testing.T, tmp string) (ok bool, detail string)
	}{
		{
			name: "Load, plain directory, explicit root",
			run: func(t *testing.T, tmp string) (bool, string) {
				t.Helper()
				dir := filepath.Join(tmp, "plain")
				return loadOutcome(schema.Load(t.Context(), writeHostPathModule(t, dir, "rel_main.yammm"), schema.WithModuleRoot(dir)))
			},
		},
		{
			name: "Load, composed directory, explicit root",
			run: func(t *testing.T, tmp string) (bool, string) {
				t.Helper()
				dir := filepath.Join(tmp, "caf\u00e9")
				return loadOutcome(schema.Load(t.Context(), writeHostPathModule(t, dir, "rel_main.yammm"), schema.WithModuleRoot(dir)))
			},
		},
		{
			name: "Load, decomposed directory, explicit root",
			run: func(t *testing.T, tmp string) (bool, string) {
				t.Helper()
				dir := filepath.Join(tmp, "cafe\u0301")
				return loadOutcome(schema.Load(t.Context(), writeHostPathModule(t, dir, "rel_main.yammm"), schema.WithModuleRoot(dir)))
			},
		},
		{
			name: "Load, decomposed directory, implicit root",
			run: func(t *testing.T, tmp string) (bool, string) {
				t.Helper()
				dir := filepath.Join(tmp, "cafe\u0301")
				return loadOutcome(schema.Load(t.Context(), writeHostPathModule(t, dir, "rel_main.yammm")))
			},
		},
		{
			name: "Load, decomposed directory, an import with a relative import of its own",
			run: func(t *testing.T, tmp string) (bool, string) {
				t.Helper()
				dir := filepath.Join(tmp, "cafe\u0301")
				entry := writeHostPathModule(t, dir, "rel_main.yammm")
				writeHostPathFile(t, filepath.Join(dir, "rel_dep.yammm"), hostPathDepImporting)
				writeHostPathFile(t, filepath.Join(dir, "rel_leaf.yammm"), hostPathLeaf)
				return loadOutcome(schema.Load(t.Context(), entry, schema.WithModuleRoot(dir)))
			},
		},
		{
			name: "LoadSourcesWithEntry, composed directory",
			run: func(t *testing.T, tmp string) (bool, string) {
				t.Helper()
				return loadSourcesOutcome(t, filepath.Join(tmp, "caf\u00e9"))
			},
		},
		{
			name: "LoadSourcesWithEntry, decomposed directory",
			run: func(t *testing.T, tmp string) (bool, string) {
				t.Helper()
				return loadSourcesOutcome(t, filepath.Join(tmp, "cafe\u0301"))
			},
		},
		{
			name:     "Load, import under a directory named x\\y",
			unixOnly: true,
			run: func(t *testing.T, tmp string) (bool, string) {
				t.Helper()
				dir := filepath.Join(tmp, `x\y`)
				return loadOutcome(schema.Load(t.Context(), writeHostPathModule(t, dir, "rel_main.yammm"), schema.WithModuleRoot(dir)))
			},
		},
		{
			name:     "Load, entry named m\\main.yammm",
			unixOnly: true,
			run: func(t *testing.T, tmp string) (bool, string) {
				t.Helper()
				dir := filepath.Join(tmp, "e")
				return loadOutcome(schema.Load(t.Context(), writeHostPathModule(t, dir, `m\main.yammm`), schema.WithModuleRoot(dir)))
			},
		},
		{
			name:     "a\\b.yammm and a/b.yammm are two sources",
			unixOnly: true,
			run: func(t *testing.T, tmp string) (bool, string) {
				t.Helper()
				dir := filepath.Join(tmp, "two")
				writeHostPathFile(t, filepath.Join(dir, `a\b.yammm`), "schema \"one\"\n\ntype One {\n\tid String primary\n}\n")
				writeHostPathFile(t, filepath.Join(dir, "a", "b.yammm"), "schema \"two\"\n\ntype Two {\n\tid String primary\n}\n")
				one, resOne := schema.Load(t.Context(), filepath.Join(dir, `a\b.yammm`), schema.WithModuleRoot(dir))
				two, resTwo := schema.Load(t.Context(), filepath.Join(dir, "a", "b.yammm"), schema.WithModuleRoot(dir))
				if one == nil || two == nil {
					return false, fmt.Sprintf("a load failed: %v / %v", issueCodes(resOne), issueCodes(resTwo))
				}
				return one.SourceID() != two.SourceID(), fmt.Sprintf("SourceIDs %q and %q", one.SourceID(), two.SourceID())
			},
		},
	}

	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			if row.unixOnly && runtime.GOOS == "windows" {
				t.Skip("a backslash is a separator on Windows")
			}
			if ok, detail := row.run(t, t.TempDir()); !ok {
				t.Errorf("the row fails: %s", detail)
			}
		})
	}
}

// TestHostPath_LoadsWhenTheRootAndTheEntryAreSpelledDifferently holds the
// loader to the FILE a path names rather than to the bytes it was typed with.
// Where the filesystem finds one directory by two spellings, an import
// resolves whichever spelling the root and the entry were given in, from disk
// and from an in-memory source under an absolute key. The schema records the
// root as its directory lists it.
func TestHostPath_LoadsWhenTheRootAndTheEntryAreSpelledDifferently(t *testing.T) {
	t.Parallel()

	rows := []struct {
		name string
		// spell derives the pair (root, entry) handed to the loader from the
		// created directory and the created entry path.
		spell func(dir, entry, lowerDir, lowerEntry string) (root, path string)
	}{
		{
			name:  "the root is typed in another case",
			spell: func(_, entry, lowerDir, _ string) (string, string) { return lowerDir, entry },
		},
		{
			name:  "the entry is typed in another case",
			spell: func(dir, _, _, lowerEntry string) (string, string) { return dir, lowerEntry },
		},
	}

	doors := []struct {
		name string
		load func(t *testing.T, root, path string) (*schema.Schema, diag.Result)
	}{
		{
			name: "Load",
			load: func(t *testing.T, root, path string) (*schema.Schema, diag.Result) {
				t.Helper()
				return schema.Load(t.Context(), path, schema.WithModuleRoot(root))
			},
		},
		{
			name: "LoadSourcesWithEntry",
			load: func(t *testing.T, root, path string) (*schema.Schema, diag.Result) {
				t.Helper()
				return schema.LoadSourcesWithEntry(t.Context(), map[string][]byte{path: []byte(hostPathEntry)}, path, root)
			},
		},
	}

	for _, door := range doors {
		for _, row := range rows {
			t.Run(door.name+", "+row.name, func(t *testing.T) {
				t.Parallel()
				base := t.TempDir()
				if !yammmtest.CaseFoldingFilesystem(t, base) {
					t.Skip("the filesystem is case-sensitive, so two spellings name two directories")
				}
				dir := filepath.Join(base, "Proj")
				entry := writeHostPathModule(t, dir, "rel_main.yammm")
				lowerDir := filepath.Join(base, "proj")
				lowerEntry := filepath.Join(lowerDir, "rel_main.yammm")

				root, path := row.spell(dir, entry, lowerDir, lowerEntry)
				s, res := door.load(t, root, path)
				if ok, detail := loadOutcome(s, res); !ok {
					t.Fatalf("loading %q under root %q fails: %s", path, root, detail)
				}
				if want := canonicalPath(t, root); s.ModuleRoot() != want {
					t.Errorf("ModuleRoot() = %q; want the root as its directory lists it, %q", s.ModuleRoot(), want)
				}
			})
		}
	}
}

// TestHostPath_ImportThroughALinkedDirectoryResolvesFromItsTarget holds an
// import's own relative imports to the directory its file was read from. The
// import is reached through a link inside the module root, so its "../" climbs
// out of the link's target, not out of the directory holding the link.
func TestHostPath_ImportThroughALinkedDirectoryResolvesFromItsTarget(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeHostPathFile(t, filepath.Join(root, "a", "b", "rel_dep.yammm"),
		"schema \"reldep\"\n\nimport \"../rel_leaf\" as leaf\n\ntype Zone {\n\tcode String primary\n\t--> IN_REGION (one) leaf.Region\n}\n")
	writeHostPathFile(t, filepath.Join(root, "a", "rel_leaf.yammm"), hostPathLeaf)
	if err := os.Symlink(filepath.Join("a", "b"), filepath.Join(root, "link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	entry := filepath.Join(root, "rel_main.yammm")
	writeHostPathFile(t, entry,
		"schema \"relmain\"\n\nimport \"./link/rel_dep\" as dep\n\ntype Site {\n\tid String primary\n\t--> IN_ZONE (one) dep.Zone\n}\n")

	s, res := schema.Load(t.Context(), entry, schema.WithModuleRoot(root))
	if ok, detail := loadOutcome(s, res); !ok {
		t.Fatalf("the load fails: %s", detail)
	}
	dep, ok := s.ImportByAlias("dep")
	if !ok || dep.Schema() == nil {
		t.Fatal("the entry's import dep is not bound")
	}
	leaf, ok := dep.Schema().ImportByAlias("leaf")
	if !ok || leaf.Schema() == nil {
		t.Fatal("dep's import leaf is not bound")
	}
	want, err := location.SourceIDFromPath(canonicalPath(t, filepath.Join(root, "a", "rel_leaf.yammm")))
	if err != nil {
		t.Fatal(err)
	}
	if got := leaf.ResolvedSourceID(); got != want {
		t.Errorf("dep's import leaf resolves to %s, want %s", got, want)
	}
}

// writeHostPathModule writes a two-file module into dir — an entry named entry
// that imports "./rel_dep", and the imported file — and returns the entry path.
func writeHostPathModule(t *testing.T, dir, entry string) string {
	t.Helper()
	writeHostPathFile(t, filepath.Join(dir, "rel_dep.yammm"), hostPathDep)
	p := filepath.Join(dir, entry)
	writeHostPathFile(t, p, hostPathEntry)
	return p
}

func writeHostPathFile(t *testing.T, p, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// loadSourcesOutcome loads the entry from memory as the editor does: the
// overlay is keyed by the entry's resolved host path, the same path names the
// entry and its directory is the root, and the import is left on disk.
func loadSourcesOutcome(t *testing.T, dir string) (bool, string) {
	t.Helper()
	entry, err := location.ResolveHostPath(writeHostPathModule(t, dir, "rel_main.yammm"))
	if err != nil {
		t.Fatal(err)
	}
	return loadOutcome(schema.LoadSourcesWithEntry(t.Context(),
		map[string][]byte{entry: []byte(hostPathEntry)}, entry, filepath.Dir(entry)))
}

func loadOutcome(s *schema.Schema, res diag.Result) (bool, string) {
	return s != nil && !res.HasErrors(), fmt.Sprintf("schema loaded: %v, issues: %v", s != nil, issueCodes(res))
}

// TestHostPath_ModuleRootDetailIsTheRootsIdentity holds an import-resolution
// diagnostic's module root to the form every span source beside it takes: the
// root's identity, not the bytes the host gives its directory.
func TestHostPath_ModuleRootDetailIsTheRootsIdentity(t *testing.T) {
	t.Parallel()

	rows := []struct{ name, dir string }{
		{"a plain root directory", "plain"},
		{"a root directory with a decomposed name", "cafe\u0301"},
	}

	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			root := filepath.Join(t.TempDir(), row.dir)
			entry := filepath.Join(root, "main.yammm")
			writeHostPathFile(t, entry, "schema \"main\"\n\nimport \"./missing\" as missing\n\ntype T {\n\tid String primary\n}\n")

			_, res := schema.Load(t.Context(), entry, schema.WithModuleRoot(root))
			want := rootIdentity(t, root)

			var detail, message string
			found := false
			for issue := range res.Issues() {
				if issue.Code() != diag.E_IMPORT_RESOLVE {
					continue
				}
				found, message = true, issue.Message()
				for _, d := range issue.Details() {
					if d.Key == diag.DetailKeyModuleRoot {
						detail = d.Value
					}
				}
				break
			}
			if !found {
				t.Fatalf("no E_IMPORT_RESOLVE in %v", issueCodes(res))
			}

			if detail != want || !strings.Contains(message, want) {
				t.Errorf("detail %q, message %q: want both to carry %q", detail, message, want)
			}
		})
	}
}

// TestHostPath_MalformedMarkerDetailIsTheRootsIdentity holds
// E_LOAD_MODULE_ROOT_MALFORMED's module root to the same form: the marker
// directory's identity.
func TestHostPath_MalformedMarkerDetailIsTheRootsIdentity(t *testing.T) {
	t.Parallel()

	rows := []struct{ name, dir string }{
		{"a plain marker directory", "plain"},
		{"a marker directory with a decomposed name", "cafe\u0301"},
	}

	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			root := filepath.Join(t.TempDir(), row.dir)
			writeHostPathFile(t, filepath.Join(root, schema.ModuleRootMarker), "module example.com/thing\n")
			entry := filepath.Join(root, "main.yammm")
			writeHostPathFile(t, entry, "schema \"main\"\n\ntype T {\n\tid String primary\n}\n")

			_, res := schema.Load(t.Context(), entry)
			want := rootIdentity(t, root)

			found := false
			for issue := range res.Issues() {
				if issue.Code() != diag.E_LOAD_MODULE_ROOT_MALFORMED {
					continue
				}
				found = true
				for _, d := range issue.Details() {
					if d.Key == diag.DetailKeyModuleRoot && d.Value != want {
						t.Errorf("module_root detail %q, want the marker directory's identity %q", d.Value, want)
					}
				}
			}
			if !found {
				t.Fatalf("no E_LOAD_MODULE_ROOT_MALFORMED in %v", issueCodes(res))
			}
		})
	}
}

func issueCodes(res diag.Result) []string {
	var codes []string
	for issue := range res.Issues() {
		codes = append(codes, issue.Code().String())
	}
	return codes
}
