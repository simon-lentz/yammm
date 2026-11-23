package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
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

// An --initialisms acronym no word of a name can equal is a usage error naming
// the flag, judged before the schema is read, as a bad --package is.
func TestGen_InvalidInitialismExitsUsage(t *testing.T) {
	for _, tc := range []struct {
		flags          []string
		refused, input string
	}{
		{[]string{"S3"}, "S3", "testdata/valid.yammm"},
		{[]string{"JWT,"}, "", "testdata/valid.yammm"},
		{[]string{""}, "", "testdata/valid.yammm"},
		{[]string{"JWT", ""}, "", "testdata/valid.yammm"},
		{[]string{"", "JWT"}, "", "testdata/valid.yammm"},
		{[]string{"JWT,o-idc"}, "o-idc", "testdata/valid.yammm"},
		{[]string{"S3,JWT"}, "S3", "testdata/valid.yammm"},
		{[]string{"UTF8"}, "UTF8", "testdata/no_such_schema.yammm"},
	} {
		t.Run(strings.Join(tc.flags, " "), func(t *testing.T) {
			args := []string{"gen", "--to", "go"}
			for _, f := range tc.flags {
				args = append(args, "--initialisms", f)
			}
			code, stdout, errOut := runCLI(t, append(args, tc.input)...)
			if code != cli.ExitUsage {
				t.Errorf("exit code = %d, want %d:\n%s", code, cli.ExitUsage, errOut)
			}
			if stdout != "" {
				t.Errorf("stdout is not empty: %q", stdout)
			}
			if !strings.Contains(errOut, `--initialisms "`+tc.refused+`"`) {
				t.Errorf("stderr does not name --initialisms %q:\n%s", tc.refused, errOut)
			}
			if strings.Contains(errOut, "WithInitialisms") {
				t.Errorf("stderr names the library option, not the flag:\n%s", errOut)
			}
		})
	}
	code, stdout, errOut := runCLI(t, "gen", "--to", "go", "--initialisms", "JWT", "--initialisms", "url,OIDC", "testdata/valid.yammm")
	if code != 0 || stdout == "" {
		t.Errorf("valid acronyms over two occurrences exit %d:\n%s", code, errOut)
	}
}

// Shell completion offers --initialisms again once it is given, as it offers
// every flag that may repeat.
func TestGen_CompletionOffersInitialismsAgain(t *testing.T) {
	code, stdout, errOut := runCLI(t, "__complete", "gen", "testdata/valid.yammm", "--to", "go", "--initialisms", "JWT", "--i")
	if code != 0 {
		t.Fatalf("completion exit %d:\n%s", code, errOut)
	}
	if !slices.ContainsFunc(strings.Split(stdout, "\n"), func(l string) bool {
		return strings.HasPrefix(l, "--initialisms")
	}) {
		t.Errorf("completion after one --initialisms does not offer it again:\n%s", stdout)
	}
}

// gen's help prints --initialisms with no default, as it prints every flag
// whose default is empty.
func TestGen_InitialismsHelpPrintsNoDefault(t *testing.T) {
	code, stdout, errOut := runCLI(t, "gen", "--help")
	if code != 0 {
		t.Fatalf("gen --help exit %d:\n%s", code, errOut)
	}
	var line string
	for l := range strings.Lines(stdout) {
		if strings.Contains(l, "--initialisms strings") {
			line = l
		}
	}
	if line == "" {
		t.Fatalf("help has no --initialisms line:\n%s", stdout)
	}
	if strings.Contains(line, "default") {
		t.Errorf("help prints a default for --initialisms: %q", line)
	}
}

// TestGen_GeneratorFailureExitsRuntime pins that only the flag refusals become
// usage errors: a schema gogen refuses is still a generation failure.
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

// TestGen_AnEmptyOutputIsRefused pins that -o "" names no file: it is a usage
// error, where an omitted -o writes to stdout.
func TestGen_AnEmptyOutputIsRefused(t *testing.T) {
	// The schema does not exist, so a refusal judged after the read would exit 3.
	missing := filepath.Join(t.TempDir(), "missing.yammm")
	for _, args := range [][]string{
		{"gen", "--to", "md", "-o", "", missing},
		{"gen", "--to", "md", "--output=", missing},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			code, stdout, stderr := runCLI(t, args...)
			if code != cli.ExitUsage || stdout != "" || !strings.Contains(stderr, "output file") {
				t.Errorf("exit code = %d with stdout %q, want %d, no stdout and a refusal naming the output file; stderr:\n%s",
					code, stdout, cli.ExitUsage, stderr)
			}
		})
	}
}

// TestGen_CypherReportsEveryInvalidIdentifier pins that gen --to cypher keeps
// no issue limit: 150 types that each declare a property named by a Cypher
// keyword draw 150 errors and no dropped-issue note.
func TestGen_CypherReportsEveryInvalidIdentifier(t *testing.T) {
	t.Parallel()

	var src strings.Builder
	src.WriteString("schema \"res\"\n")
	for i := range 150 {
		fmt.Fprintf(&src, "\ntype T%03d {\n    id String primary\n    where String\n    extra String\n\n    @@index(where, extra)\n}\n", i)
	}
	schemaPath := filepath.Join(t.TempDir(), "res150.yammm")
	if err := os.WriteFile(schemaPath, []byte(src.String()), 0o600); err != nil {
		t.Fatal(err)
	}

	code, stdout, stderr := executeCmdOutput(t, "gen", "--to", "cypher", schemaPath)
	if code != cli.ExitValidation {
		t.Errorf("exit code = %d, want %d", code, cli.ExitValidation)
	}
	if stdout != "" {
		t.Errorf("a failed generation printed a script:\n%s", stdout)
	}
	if got := strings.Count(stderr, "error[E_NEO4J_INVALID_IDENTIFIER]"); got != 150 {
		t.Errorf("gen printed %d E_NEO4J_INVALID_IDENTIFIER errors, want 150", got)
	}
	for _, typeName := range []string{"T000", "T149"} {
		if !strings.Contains(stderr, fmt.Sprintf(`invalid property "where" declared in %s: the name is a Cypher reserved keyword`, typeName)) {
			t.Errorf("no error names %s's property", typeName)
		}
	}
	if strings.Contains(stderr, "dropped at an issue limit") {
		t.Error("stderr carries a dropped-issue note")
	}
}
