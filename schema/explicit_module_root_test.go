package schema_test

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema"
)

// soloSchema imports nothing, so a load of it never needs its module root;
// importingSchema imports lib/dep, which a root holding lib/dep.yammm serves.
const (
	soloSchema      = "schema \"solo\"\n\ntype T {\n\tid String primary\n}\n"
	importingSchema = "schema \"main\"\n\nimport \"lib/dep\" as dep\n\ntype T {\n\tid String primary\n}\n"
	depSchema       = "schema \"dep\"\n\ntype D {\n\tid String primary\n}\n"
)

// TestLoad_RefusesAnExplicitModuleRootThatIsNotADirectory pins that Load
// refuses a WithModuleRoot value that is not an existing directory for a
// schema that imports nothing and for one that imports: one Fatal
// E_LOAD_IO_FAILURE naming the root, never the import's E_IMPORT_RESOLVE. An
// existing directory still loads both.
func TestLoad_RefusesAnExplicitModuleRootThatIsNotADirectory(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for name, content := range map[string]string{
		"solo.yammm":    soloSchema,
		"main.yammm":    importingSchema,
		"lib/dep.yammm": depSchema,
		"plain":         "",
	} {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	plain := filepath.Join(dir, "plain")
	type refusal struct {
		name  string
		root  string
		cause string // the cause the message names; a missing path's is the host's own text
	}
	refusals := []refusal{
		{"a missing path", filepath.Join(dir, "missing"), ""},
		{"a regular file", plain, syscall.ENOTDIR.Error()},
		{"a path under a regular file", filepath.Join(plain, "sub"), syscall.ENOTDIR.Error()},
	}
	// The message opens with the root as given; its cause names where the link
	// resolves it.
	if err := os.Symlink(dir, filepath.Join(dir, "via")); err == nil {
		refusals = append(refusals, refusal{"a regular file through a linked directory", filepath.Join(dir, "via", "plain"), syscall.ENOTDIR.Error()})
	}
	for _, entry := range []string{"solo.yammm", "main.yammm"} {
		for _, c := range refusals {
			t.Run(entry+", "+c.name, func(t *testing.T) {
				t.Parallel()
				checkRefusedRoot(t, filepath.Join(dir, entry), c.root, c.cause)
			})
		}
		// A ".." after a regular file takes its parent on the text, as every
		// path the loader resolves does.
		for name, root := range map[string]string{"an existing directory": dir, "a regular file's parent, spelled through it": plain + string(filepath.Separator) + ".."} {
			t.Run(entry+", "+name, func(t *testing.T) {
				t.Parallel()
				s, res := schema.Load(context.Background(), filepath.Join(dir, entry), schema.WithModuleRoot(root))
				if s == nil || res.HasErrors() {
					t.Errorf("Load under module root %s failed: %s", root, res)
				}
			})
		}
	}
}

// checkRefusedRoot loads entry under root and reports a schema, or anything but
// one Fatal E_LOAD_IO_FAILURE naming root and cause. It never stops the test, so
// a goroutine may call it.
func checkRefusedRoot(t *testing.T, entry, root, cause string) {
	t.Helper()
	s, res := schema.Load(context.Background(), entry, schema.WithModuleRoot(root))
	if s != nil {
		t.Errorf("Load returned a schema under module root %s", root)
	}
	var issues []diag.Issue
	for issue := range res.Issues() {
		issues = append(issues, issue)
	}
	if len(issues) != 1 {
		t.Errorf("got %d issues, want one: %s", len(issues), res)
		return
	}
	got := issues[0]
	if got.Severity() != diag.Fatal || got.Code() != diag.E_LOAD_IO_FAILURE {
		t.Errorf("got %s %s, want Fatal E_LOAD_IO_FAILURE", got.Severity(), got.Code())
	}
	for _, want := range []string{"invalid module root " + strconv.Quote(root), cause} {
		if !strings.Contains(got.Message(), want) {
			t.Errorf("message %q does not hold %q", got.Message(), want)
		}
	}
}

// TestLoadSourcesWithEntry_TakesARootNoDirectoryHolds pins the other side of
// the rule: an in-memory load's root names its sources' identities, and a load
// whose every import is in memory needs no directory there.
func TestLoadSourcesWithEntry_TakesARootNoDirectoryHolds(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), "missing")
	sources := map[string][]byte{
		"entry.yammm": []byte("schema \"entry\"\n\nimport \"./dep\" as dep\n\ntype T {\n\tid String primary\n}\n"),
		"dep.yammm":   []byte(depSchema),
	}
	s, res := schema.LoadSourcesWithEntry(context.Background(), sources, "entry.yammm", root)
	if s == nil || res.HasErrors() {
		t.Errorf("LoadSourcesWithEntry under root %s failed: %s", root, res)
	}
}
