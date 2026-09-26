package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
)

// A CSV export of a graph holding composed children is refused, as a runtime
// failure naming the composition, and writes nothing to any destination. It
// reads run()'s own stderr through runCLI, so it does not run in parallel.
func TestExport_CSVRefusesComposedChildrenAndWritesNothing(t *testing.T) {
	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "orders.yammm")
	dataPath := filepath.Join(dir, "orders.json")
	for p, text := range map[string]string{
		schemaPath: `schema "orders"

type Order {
	order_id String primary
	*-> LINES (many) Line
}

part type Line {
	sku String required
}
`,
		dataPath: `{"Order": [{"order_id": "o1", "lines": [{"sku": "a"}]}]}`,
	} {
		if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	// out is a directory the command must not create: under --output-dir it
	// is two levels that do not exist yet, so a refusal that left the
	// directory behind would show.
	for _, c := range []struct {
		name string
		args func(out string) []string
	}{
		{"stdout", func(string) []string { return nil }},
		{"--output", func(out string) []string { return []string{"--output", filepath.Join(out, "orders.csv")} }},
		{"--output-dir", func(out string) []string { return []string{"--output-dir", filepath.Join(out, "new", "dir")} }},
	} {
		t.Run(c.name, func(t *testing.T) {
			out := t.TempDir()
			args := append([]string{"export", "--to", "csv"}, c.args(out)...)
			code, stdout, stderr := runCLI(t, append(args, schemaPath, dataPath)...)
			if code != cli.ExitRuntime {
				t.Errorf("exit %d, want %d", code, cli.ExitRuntime)
			}
			if !strings.Contains(stderr, `composition "LINES"`) {
				t.Errorf("stderr does not name the composition: %q", stderr)
			}
			if stdout != "" {
				t.Errorf("a refused export wrote to stdout: %q", stdout)
			}
			entries, err := os.ReadDir(out)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 0 {
				t.Errorf("a refused export left %d entries in %s, the first %s", len(entries), out, entries[0].Name())
			}
		})
	}
}

// A refused --output-dir export leaves the tree as it was found, however the
// operator spelled the directory: through a ".." past a new level, with a
// trailing slash, through a symlinked parent, and with a component no
// filesystem admits. The refusal comes before any level is made. Each spelling
// is concatenated, since filepath.Join would clean the ".." away.
func TestExport_CSVRefusalLeavesNoDirectoryHoweverTheOutputDirIsSpelled(t *testing.T) {
	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "orders.yammm")
	dataPath := filepath.Join(dir, "orders.json")
	if err := os.WriteFile(schemaPath, []byte("schema \"orders\"\n\ntype Order {\n\torder_id String primary\n\t*-> LINES (many) Line\n}\n\npart type Line {\n\tsku String required\n}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dataPath, []byte(`{"Order": [{"order_id": "o1", "lines": [{"sku": "a"}]}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	sep := string(filepath.Separator)
	long := strings.Repeat("x", 300)
	for _, c := range []struct {
		name, rel string
		symlink   bool
	}{
		{"dot-dot past a new level", "made" + sep + ".." + sep + "out", false},
		{"dot and dot-dot", "a" + sep + "." + sep + "b" + sep + ".." + sep + "c", false},
		{"trailing slash", "new" + sep + "out" + sep, false},
		{"symlinked parent", "link" + sep + ".." + sep + "out", true},
		{"a name too long", "new" + sep + long, false},
		{"a name too long, then more", "new" + sep + long + sep + "deeper", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			out := t.TempDir()
			if c.symlink {
				if err := os.MkdirAll(filepath.Join(out, "real", "deep", "x"), 0o750); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join("real", "deep", "x"), filepath.Join(out, "link")); err != nil {
					t.Skipf("symlink: %v", err)
				}
			}
			before := listTree(t, out)
			code, _, stderr := runCLI(t, "export", "--to", "csv", "--output-dir", out+sep+c.rel, schemaPath, dataPath)
			if code != cli.ExitRuntime {
				t.Errorf("exit %d, want %d: %s", code, cli.ExitRuntime, stderr)
			}
			if after := listTree(t, out); !slices.Equal(after, before) {
				t.Errorf("a refused export changed the tree:\nbefore %q\nafter  %q", before, after)
			}
		})
	}
}

// listTree lists every entry under root, relative to it, without following
// symlinks.
func listTree(t *testing.T, root string) []string {
	t.Helper()
	var entries []string
	err := filepath.WalkDir(root, func(p string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		entries = append(entries, rel)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return entries
}
