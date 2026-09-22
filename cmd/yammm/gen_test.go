package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
)

// TestGen_InvalidPackageExitsUsage pins that a --package value no package
// clause can hold is a usage error naming the flag, not a generator failure
// naming the library option the flag feeds.
func TestGen_InvalidPackageExitsUsage(t *testing.T) {
	for _, name := range []string{"my-pkg", "type", "_"} {
		t.Run(name, func(t *testing.T) {
			code, stdout, errOut := runCLI(t, "gen", "--to", "go", "--package", name, "testdata/valid.yammm")
			if code != cli.ExitUsage {
				t.Errorf("exit code = %d, want %d", code, cli.ExitUsage)
			}
			if stdout != "" {
				t.Errorf("stdout is not empty: %q", stdout)
			}
			if !strings.Contains(errOut, `--package "`+name+`"`) {
				t.Errorf("stderr does not name --package %q:\n%s", name, errOut)
			}
			if strings.Contains(errOut, "WithPackageName") {
				t.Errorf("stderr names the library option, not the flag:\n%s", errOut)
			}
		})
	}
}

// TestGen_GeneratorFailureExitsRuntime pins that only the package-name refusal
// becomes a usage error: a schema gogen refuses is still a generation failure.
// One source reached by two import paths, through a symlinked directory, has
// no embedded store the re-load can read.
func TestGen_GeneratorFailureExitsRuntime(t *testing.T) {
	root := t.TempDir()
	for name, body := range map[string]string{
		"main.yammm":     "schema \"main\"\n\nimport \"a\" as a\nimport \"b\" as b\n\ntype M {\n\tid String primary\n\t--> TO_A (one) a.A\n\t--> TO_B (one) b.B\n}\n",
		"a.yammm":        "schema \"a\"\n\nimport \"lib/dep\" as dep\n\ntype A {\n\tid String primary\n\t--> TO_D (one) dep.D\n}\n",
		"b.yammm":        "schema \"b\"\n\nimport \"real/dep\" as dep\n\ntype B {\n\tid String primary\n\t--> TO_D (one) dep.D\n}\n",
		"real/dep.yammm": "schema \"dep\"\n\ntype D {\n\tid String primary\n}\n",
	} {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink("real", filepath.Join(root, "lib")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	code, _, errOut := runCLI(t, "gen", "--to", "go", "--package", "geo", "--module-root", root, filepath.Join(root, "main.yammm"))
	if code != cli.ExitRuntime {
		t.Errorf("exit code = %d, want %d", code, cli.ExitRuntime)
	}
	if !strings.Contains(errOut, "generate go:") {
		t.Errorf("stderr does not report a generation failure:\n%s", errOut)
	}
}
