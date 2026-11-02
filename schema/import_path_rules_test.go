package schema_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/internal/yammmtest"
	"github.com/simon-lentz/yammm/schema"
)

// backslashRule is the sentence errImportBackslash states to a user. The tests
// below read it, so a reworded refusal that stops naming the rule fails here.
const backslashRule = "an import path separates its segments with / and holds no backslash"

// TestImportPath_BackslashIsRefused holds every import path holding a backslash
// to one E_IMPORT_RESOLVE naming the separator rule. A backslash reaches the
// path only through an escape, so every row writes one.
func TestImportPath_BackslashIsRefused(t *testing.T) {
	t.Parallel()
	yammmtest.RequireNoModuleRoot(t, schema.FindModuleRoot)

	rows := []struct {
		name       string
		importPath string
	}{
		{"a relative path", `./a\b`},
		{"a parent-relative path", `../a\b`},
		{"a module-style path", `a\b`},
		{"a leading backslash", `\a`},
		{"a trailing backslash", `./a\`},
	}

	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			entry := filepath.Join(root, "main.yammm")
			writeHostPathFile(t, entry,
				"schema \"main\"\n\nimport \""+strings.ReplaceAll(row.importPath, `\`, `\\`)+"\" as d\n\ntype T {\n\tid String primary\n}\n")

			s, res := schema.Load(t.Context(), entry, schema.WithModuleRoot(root))
			if !s.IsZero() {
				t.Fatalf("import %q loaded", row.importPath)
			}
			var got []string
			named := false
			for issue := range res.Issues() {
				got = append(got, issue.Code().String()+": "+issue.Message())
				if issue.Code() == diag.E_IMPORT_RESOLVE && strings.Contains(issue.Message(), backslashRule) {
					named = true
				}
			}
			if !named {
				t.Errorf("no E_IMPORT_RESOLVE naming the separator rule; got %q", got)
			}
		})
	}
}

// TestImportPath_BackslashRefusalIsLoadBearing holds the refusal to a file the
// import would otherwise reach: on Unix a backslash is an ordinary file-name
// character, so a\b.yammm is one file and the load would succeed without the
// rule. Windows reads the same import as the path a/b.
func TestImportPath_BackslashRefusalIsLoadBearing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a backslash is a path separator on Windows, so a\\b.yammm is not one file name")
	}
	t.Parallel()
	yammmtest.RequireNoModuleRoot(t, schema.FindModuleRoot)

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, `a\b.yammm`),
		[]byte("schema \"dep\"\n\ntype Dep {\n\tid String primary\n}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	entry := filepath.Join(root, "main.yammm")
	writeHostPathFile(t, entry,
		"schema \"main\"\n\nimport \"./a\\\\b\" as d\n\ntype T {\n\tid String primary\n\t--> USES (one) d.Dep\n}\n")

	s, res := schema.Load(t.Context(), entry, schema.WithModuleRoot(root))
	if !s.IsZero() {
		t.Fatal("the import reached the file a\\b.yammm names on this host")
	}
	named := false
	for issue := range res.Issues() {
		if issue.Code() == diag.E_IMPORT_RESOLVE && strings.Contains(issue.Message(), backslashRule) {
			named = true
		}
	}
	if !named {
		t.Errorf("the refusal did not name the separator rule: %v", res.Err())
	}
}

// TestImportPath_ModuleStyleRequiresAModuleRoot holds the sentence a
// module-style import draws when no module root is in play. The clause after it
// names the root's provenance; this row reads the refusal itself.
func TestImportPath_ModuleStyleRequiresAModuleRoot(t *testing.T) {
	t.Parallel()

	sources := map[string][]byte{
		"/main.yammm": []byte("schema \"main\"\n\nimport \"lib/common\" as c\n\ntype T {\n\tid String primary\n}\n"),
	}
	s, res := schema.LoadSourcesWithEntry(t.Context(), sources, "/main.yammm", "")
	if !s.IsZero() {
		t.Fatal("a module-style import resolved with no module root")
	}
	var got []string
	named := false
	for issue := range res.Issues() {
		got = append(got, issue.Code().String()+": "+issue.Message())
		if issue.Code() == diag.E_IMPORT_RESOLVE && strings.Contains(issue.Message(), "module-style imports require a module root") {
			named = true
		}
	}
	if !named {
		t.Errorf("no E_IMPORT_RESOLVE naming the module-root requirement; got %q", got)
	}
}

// writeImportRows writes each named file under root, parents included, with a
// schema body of its own.
func writeImportRows(t *testing.T, root string, names ...string) {
	t.Helper()
	for i, name := range names {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		body := "schema \"dep" + strconv.Itoa(i) + "\"\n\ntype Dep {\n\tid String primary\n}\n"
		writeHostPathFile(t, p, body)
	}
}

// TestImportPath_APathNamingNoFileIsRefused holds that a path with no file name
// draws E_IMPORT_RESOLVE though the dotfile or the sibling file the extension rule
// would reach is present.
func TestImportPath_APathNamingNoFileIsRefused(t *testing.T) {
	t.Parallel()
	yammmtest.RequireNoModuleRoot(t, schema.FindModuleRoot)

	rows := []struct{ name, entry, importPath string }{
		{"empty", "main.yammm", ""},
		{"the importer's directory", "main.yammm", "./"},
		{"the importer's directory under the root", "sub/main.yammm", "./"},
		{"a parent directory", "sub/deep/main.yammm", "../"},
		{"a bare dot", "main.yammm", "."},
		{"a bare dot dot", "main.yammm", ".."},
		{"a dot segment last", "sub/main.yammm", "./a/."},
		{"a dot dot segment last", "sub/main.yammm", "./a/.."},
		{"a module-style dot dot segment last", "main.yammm", "a/.."},
		{"a path of one separator", "main.yammm", "/"},
		{"a path of separators", "main.yammm", "//"},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			writeImportRows(t, root, ".yammm", "..yammm", "...yammm", "sub.yammm", "sub/a.yammm", "sub/deep.yammm", "a.yammm", "a/...yammm", "sub/a/...yammm", "sub/a/..yammm")
			entry := filepath.Join(root, filepath.FromSlash(row.entry))
			writeHostPathFile(t, entry, "schema \"main\"\n\nimport \""+row.importPath+"\" as d\n\ntype T {\n\tid String primary\n}\n")

			s, res := schema.Load(t.Context(), entry, schema.WithModuleRoot(root))
			if !s.IsZero() {
				t.Fatalf("import %q loaded", row.importPath)
			}
			wantCounts(t, res, map[diag.Code]int{diag.E_IMPORT_RESOLVE: 1})
			for issue := range res.Issues() {
				if !strings.Contains(issue.Message(), "ends in none") {
					t.Errorf("message = %q, want the file-name rule", issue.Message())
				}
			}
		})
	}
}

// TestImportPath_TrailingSeparatorsAreReadPast holds that a separator after a file
// name changes nothing: the import binds that file though the dotfile the extension
// rule would reach is present.
func TestImportPath_TrailingSeparatorsAreReadPast(t *testing.T) {
	t.Parallel()
	yammmtest.RequireNoModuleRoot(t, schema.FindModuleRoot)

	for _, importPath := range []string{"sub/b/", "sub/b//", "sub/b.yammm/", "./sub/b/", "./sub/b//", "./sub/b.yammm/"} {
		t.Run(importPath, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			writeHostPathFile(t, filepath.Join(root, "sub", "b.yammm"), "schema \"want\"\n")
			writeHostPathFile(t, filepath.Join(root, "sub", "b", ".yammm"), "schema \"dotfile\"\n")
			entry := filepath.Join(root, "main.yammm")
			writeHostPathFile(t, entry, "schema \"main\"\n\nimport \""+importPath+"\" as d\n\ntype T {\n\tid String primary\n}\n")

			s, res := schema.Load(t.Context(), entry, schema.WithModuleRoot(root))
			if s.IsZero() {
				t.Fatalf("import %q did not load: %v", importPath, res)
			}
			for imp := range s.Imports() {
				if got := imp.Schema().Name(); got != "want" {
					t.Errorf("import %q bound schema %q, want the file", importPath, got)
				}
			}
		})
	}
}

// TestImportPath_TwoSpellingsWithASeparatorAreADuplicateImport holds that the
// separator names no other file, so the second import is a duplicate.
func TestImportPath_TwoSpellingsWithASeparatorAreADuplicateImport(t *testing.T) {
	t.Parallel()
	yammmtest.RequireNoModuleRoot(t, schema.FindModuleRoot)

	root := t.TempDir()
	writeHostPathFile(t, filepath.Join(root, "b.yammm"), "schema \"dep\"\n")
	entry := filepath.Join(root, "main.yammm")
	writeHostPathFile(t, entry, "schema \"main\"\n\nimport \"b\" as x\nimport \"b/\" as y\n\ntype T {\n\tid String primary\n}\n")

	_, res := schema.Load(t.Context(), entry, schema.WithModuleRoot(root))
	wantCounts(t, res, map[diag.Code]int{diag.E_DUPLICATE_IMPORT: 1})
}

// TestLoadSources_APathOfSeparatorsBindsNoSourceKey holds that the in-memory arm
// answers as the disk does, though a source sits under the key the extension rule
// would reach.
func TestLoadSources_APathOfSeparatorsBindsNoSourceKey(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for _, importPath := range []string{"/", "//", "///"} {
		sources := map[string][]byte{
			"main.yammm": []byte("schema \"main\"\n\nimport \"" + importPath + "\" as d\n\ntype T {\n\tid String primary\n}\n"),
			"/.yammm":    []byte("schema \"absolute\"\n"),
		}
		s, res := schema.LoadSourcesWithEntry(t.Context(), sources, "main.yammm", root)
		if !s.IsZero() {
			t.Fatalf("import %q loaded", importPath)
		}
		wantCounts(t, res, map[diag.Code]int{diag.E_IMPORT_RESOLVE: 1})
	}
}

// TestSyntheticImportKey_TrailingSeparatorsAreReadPast holds the key rule to the
// file the import names.
func TestSyntheticImportKey_TrailingSeparatorsAreReadPast(t *testing.T) {
	t.Parallel()
	for _, importPath := range []string{"sub/b/", "sub/b//", "sub/b.yammm/", "./sub/b/"} {
		key, err := schema.SyntheticImportKey("m.yammm", importPath)
		if err != nil {
			t.Errorf("SyntheticImportKey(%q) = %v", importPath, err)
			continue
		}
		if key != "sub/b.yammm" {
			t.Errorf("SyntheticImportKey(%q) = %q, want sub/b.yammm", importPath, key)
		}
	}
}

// TestImportPath_SpellingTheRootEscapes holds that a relative path spelling the
// module root or an ancestor names the file beside that directory, above the root.
func TestImportPath_SpellingTheRootEscapes(t *testing.T) {
	t.Parallel()
	yammmtest.RequireNoModuleRoot(t, schema.FindModuleRoot)

	rows := []struct{ name, importPath string }{
		{"the root", "../z"},
		{"an ancestor of the root", "../../base"},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			base := filepath.Join(t.TempDir(), "base")
			root := filepath.Join(base, "z")
			writeImportRows(t, base, "z.yammm", "../base.yammm", "z/..yammm", "z/...yammm")
			entry := filepath.Join(root, "m.yammm")
			writeHostPathFile(t, entry, "schema \"main\"\n\nimport \""+row.importPath+"\" as d\n\ntype T {\n\tid String primary\n}\n")

			s, res := schema.Load(t.Context(), entry, schema.WithModuleRoot(root))
			if !s.IsZero() {
				t.Fatalf("import %q loaded", row.importPath)
			}
			wantCounts(t, res, map[diag.Code]int{diag.E_PATH_ESCAPE: 1})
		})
	}
}

// TestSyntheticImportPath_APathNamingNoFileIsRefused holds the same refusal where
// a load looks imports up by key, and in the function that states the key rule.
func TestSyntheticImportPath_APathNamingNoFileIsRefused(t *testing.T) {
	t.Parallel()
	for _, importPath := range []string{"", "./", "../", ".", "..", "./a/..", "/", "//"} {
		t.Run(importPath, func(t *testing.T) {
			t.Parallel()
			if key, err := schema.SyntheticImportKey("sub/m.yammm", importPath); err == nil {
				t.Errorf("SyntheticImportKey = %q, want a refusal", key)
			}
			sources := map[string][]byte{
				"sub/m.yammm": []byte("schema \"main\"\n\nimport \"" + importPath + "\" as d\n\ntype T {\n\tid String primary\n}\n"),
				"sub.yammm":   []byte("schema \"dep\"\n"),
				"..yammm":     []byte("schema \"dot\"\n"),
				".yammm":      []byte("schema \"empty\"\n"),
			}
			s, res := schema.LoadSourcesWithEntry(t.Context(), sources, "sub/m.yammm", "", schema.WithSyntheticRoot("test://r"))
			if !s.IsZero() {
				t.Fatalf("import %q loaded", importPath)
			}
			wantCounts(t, res, map[diag.Code]int{diag.E_IMPORT_RESOLVE: 1})
		})
	}
}
