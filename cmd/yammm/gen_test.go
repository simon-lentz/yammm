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
// becomes a usage error: a schema gogen refuses for its names is still a
// generation failure.
func TestGen_GeneratorFailureExitsRuntime(t *testing.T) {
	path := filepath.Join(t.TempDir(), "clash.yammm")
	if err := os.WriteFile(path, []byte("schema \"geo\"\n\ntype Region = String\n\ntype Region {\n\tid String primary\n}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, _, errOut := runCLI(t, "gen", "--to", "go", "--package", "geo", path)
	if code != cli.ExitRuntime {
		t.Errorf("exit code = %d, want %d", code, cli.ExitRuntime)
	}
	if !strings.Contains(errOut, "generate go:") {
		t.Errorf("stderr does not report a generation failure:\n%s", errOut)
	}
}
