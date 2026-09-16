package schema_test

import (
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
			if s != nil {
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
	if s != nil {
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

// TestBuilderImportPath_BackslashIsRefusedOnThePathRoute holds the Builder's
// half of the rule: a relative path holding a backslash resolves to nothing and
// draws the same sentence, even when a schema is registered under the SourceID
// the path would name.
func TestBuilderImportPath_BackslashIsRefusedOnThePathRoute(t *testing.T) {
	t.Parallel()

	depID, err := location.SourceIDFromPath(yammmtest.HostAbs(`/project/a\b.yammm`))
	if err != nil {
		t.Fatal(err)
	}
	dep, depRes := schema.NewBuilder().WithName("dep").WithSourceID(depID).
		AddType("Dep").WithPrimaryKey("id", schema.NewUUIDConstraint()).Done().Build()
	if dep == nil {
		t.Fatalf("the dependency did not build: %v", depRes.Err())
	}
	registry := schema.NewRegistry()
	if err := registry.Register(dep); err != nil {
		t.Fatal(err)
	}
	mainID, err := location.SourceIDFromPath(yammmtest.HostAbs("/project/main.yammm"))
	if err != nil {
		t.Fatal(err)
	}

	s, res := schema.NewBuilder().WithName("main").WithSourceID(mainID).
		WithRegistry(registry).AddImport(`./a\b`, "d").
		AddType("T").WithPrimaryKey("id", schema.NewUUIDConstraint()).Done().Build()
	if s != nil {
		t.Fatalf("the Builder resolved a backslash path; res %v", res.Err())
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
}

// TestBuilderImportPath_ASchemaNameMayHoldABackslash holds the separator rule
// off case 3, the schema-name lookup: a name is not a path, and a schema
// registered under a name holding a backslash still imports by that name.
func TestBuilderImportPath_ASchemaNameMayHoldABackslash(t *testing.T) {
	t.Parallel()

	const name = `a\b`
	dep, depRes := schema.NewBuilder().WithName(name).
		WithSourceID(location.MustNewSourceID("builder://dep")).
		AddType("Dep").WithPrimaryKey("id", schema.NewUUIDConstraint()).Done().Build()
	if dep == nil {
		t.Fatalf("a schema named %q did not build: %v", name, depRes.Err())
	}
	registry := schema.NewRegistry()
	if err := registry.Register(dep); err != nil {
		t.Fatal(err)
	}

	s, res := schema.NewBuilder().WithName("main").
		WithSourceID(location.MustNewSourceID("builder://main")).
		WithRegistry(registry).AddImport(name, "d").
		AddType("T").WithPrimaryKey("id", schema.NewUUIDConstraint()).Done().Build()
	if s == nil {
		var got []string
		for issue := range res.Issues() {
			got = append(got, issue.Code().String()+": "+issue.Message())
		}
		t.Fatalf("a schema named %q did not import by name; got %q", name, got)
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
	if s != nil {
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
