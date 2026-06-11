package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
)

// TestDiagnosticsRoot_Relativizes pins where rendered diagnostic locations
// relativize: against the loaded schema's module root (or its canonicalized
// fallback), the same for every subcommand and independent of symlinks in
// the paths the user typed. Locations must render root-relative
// ("bad.yammm:3:1"), never as "." or as absolute resolved paths.
func TestDiagnosticsRoot_Relativizes(t *testing.T) {
	t.Parallel()

	// The only error is a missing primary key, anchored at the type
	// declaration on line 3.
	const badSchema = "schema \"bad\"\n\ntype Thing {\n\tname String\n}\n"

	t.Run("neo4j diff renders schema-load locations relative", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		path := filepath.Join(dir, "bad.yammm")
		if err := os.WriteFile(path, []byte(badSchema), 0o600); err != nil {
			t.Fatal(err)
		}

		code, stderr := executeCmdStderr(t, "neo4j", "diff", "--uri", "bolt://localhost:7687", path)
		if code != cli.ExitValidation {
			t.Errorf("exit code = %d, want %d", code, cli.ExitValidation)
		}
		if !strings.Contains(stderr, "bad.yammm:3") {
			t.Errorf("location should be root-relative, stderr:\n%s", stderr)
		}
		if resolved, err := filepath.EvalSymlinks(dir); err == nil && strings.Contains(stderr, resolved) {
			t.Errorf("location must not render absolute, stderr:\n%s", stderr)
		}
	})

	t.Run("gen with symlinked module root renders relative", func(t *testing.T) {
		t.Parallel()
		base := t.TempDir()
		real := filepath.Join(base, "real")
		if err := os.MkdirAll(real, 0o750); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(base, "ln")
		if err := os.Symlink("real", link); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		if err := os.WriteFile(filepath.Join(real, "bad.yammm"), []byte(badSchema), 0o600); err != nil {
			t.Fatal(err)
		}

		code, stderr := executeCmdStderr(t, "gen", "--to", "go", "--module-root", link, filepath.Join(link, "bad.yammm"))
		if code != cli.ExitValidation {
			t.Errorf("exit code = %d, want %d", code, cli.ExitValidation)
		}
		if !strings.Contains(stderr, "bad.yammm:3") {
			t.Errorf("location should be root-relative, stderr:\n%s", stderr)
		}
		if resolved, err := filepath.EvalSymlinks(real); err == nil && strings.Contains(stderr, resolved) {
			t.Errorf("location must not render absolute, stderr:\n%s", stderr)
		}
	})
}
