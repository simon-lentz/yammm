package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
)

// A data path that opens and then fails on read is an I/O failure whichever
// format names it and whichever command reads it. The JSON path reads the whole
// file and returns the error; the CSV path streams, so the same fault arrives as
// a diagnostic, and every command that turns that result into an exit code has
// to ask the exit rule rather than answer "validation" itself, or one fault
// earns two exit codes. A directory is the portable way to get a path that opens
// and cannot be read.
//
// Driven through runCLI, the real entry point, because the exit code is what
// run() derives; it swaps process streams, so nothing here runs in parallel.
func TestDataCommands_AReadFailureExitsRuntimeInEveryFormat(t *testing.T) {
	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "schema.yammm")
	if err := os.WriteFile(schemaPath, []byte("schema \"s\"\n\ntype Entity {\n\tid String primary\n}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"data.json", "data.csv", "data.tsv"} {
		if err := os.Mkdir(filepath.Join(dir, name), 0o700); err != nil {
			t.Fatal(err)
		}
	}

	for _, c := range []struct {
		command string
		args    func(data string) []string
	}{
		{"check", func(data string) []string { return []string{"check", schemaPath, data, "--type", "Entity"} }},
		{"export", func(data string) []string {
			return []string{"export", schemaPath, data, "--to", "json", "--type", "Entity"}
		}},
		{"snapshot save", func(data string) []string {
			return []string{"snapshot", "save", schemaPath, data, "--type", "Entity", "-o", filepath.Join(dir, "out.ys")}
		}},
	} {
		for _, name := range []string{"data.json", "data.csv", "data.tsv"} {
			t.Run(c.command+"/"+name, func(t *testing.T) {
				code, _, stderr := runCLI(t, c.args(filepath.Join(dir, name))...)
				if !strings.Contains(stderr, "is a directory") && !strings.Contains(stderr, "Incorrect function") {
					t.Fatalf("the fixture did not reach the read failure, so it asserts nothing; stderr:\n%s", stderr)
				}
				if code != cli.ExitRuntime {
					t.Errorf("exit code = %d, want %d; stderr:\n%s", code, cli.ExitRuntime, stderr)
				}
			})
		}
	}
}
