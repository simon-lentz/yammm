package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
)

// A data path that opens and then fails on read is an I/O failure whichever
// command reads it, and exits as one. A directory is the portable way to get a
// path that opens and cannot be read.
//
// Driven through runCLI, the real entry point, because the exit code is what
// run() derives; it swaps process streams, so nothing here runs in parallel.
func TestDataCommands_AReadFailureExitsRuntime(t *testing.T) {
	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "schema.yammm")
	if err := os.WriteFile(schemaPath, []byte("schema \"s\"\n\ntype Entity {\n\tid String primary\n}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	data := filepath.Join(dir, "data.json")
	if err := os.Mkdir(data, 0o700); err != nil {
		t.Fatal(err)
	}

	for command, args := range map[string][]string{
		"check":         {"check", schemaPath, data},
		"snapshot save": {"snapshot", "save", schemaPath, data, "-o", filepath.Join(dir, "out.ys")},
	} {
		t.Run(command, func(t *testing.T) {
			code, _, stderr := runCLI(t, args...)
			if !strings.Contains(stderr, "is a directory") && !strings.Contains(stderr, "Incorrect function") {
				t.Fatalf("the fixture did not reach the read failure, so it asserts nothing; stderr:\n%s", stderr)
			}
			if code != cli.ExitRuntime {
				t.Errorf("exit code = %d, want %d; stderr:\n%s", code, cli.ExitRuntime, stderr)
			}
		})
	}
}
