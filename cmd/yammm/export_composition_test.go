package main

import (
	"os"
	"path/filepath"
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
