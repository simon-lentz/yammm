package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"golang.org/x/text/unicode/norm"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
	"github.com/simon-lentz/yammm/internal/yammmtest"
	"github.com/simon-lentz/yammm/schema"
)

// renderedBadSchema fails to load with one issue, spanned from line 5,
// column 2.
const renderedBadSchema = "schema \"bad\"\n\ntype T {\n\tid String primary\n\tname Strin\n}\n"

// hostRelative is the relativization the loader resolves imports by: the
// host's relative path from root to file, both symlink-resolved, written with
// "/" and in NFC, the form a SourceID takes.
func hostRelative(t *testing.T, root, file string) string {
	t.Helper()
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	resolvedFile, err := filepath.EvalSymlinks(file)
	if err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(resolvedRoot, resolvedFile)
	if err != nil {
		t.Fatal(err)
	}
	return norm.NFC.String(filepath.ToSlash(rel))
}

func writeRenderedFile(t *testing.T, p, content string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func symlinkOrSkip(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
}

// TestRenderedLocations_RelativeToTheRoot holds a failed load's text location
// to the host's own path from the root the load used to the schema file, as
// [hostRelative] computes it.
func TestRenderedLocations_RelativeToTheRoot(t *testing.T) {
	t.Parallel()
	yammmtest.RequireNoModuleRoot(t, schema.FindModuleRoot)

	rows := []struct {
		name  string
		setup func(t *testing.T, base string) (args []string, root, file string)
	}{
		{
			name: "a plain directory",
			setup: func(t *testing.T, base string) ([]string, string, string) {
				t.Helper()
				file := writeRenderedFile(t, filepath.Join(base, "plain", "bad.yammm"), renderedBadSchema)
				return []string{"validate", file}, filepath.Dir(file), file
			},
		},
		{
			name: "a schema reached through a symlinked file",
			setup: func(t *testing.T, base string) ([]string, string, string) {
				t.Helper()
				real := writeRenderedFile(t, filepath.Join(base, "real", "bad.yammm"), renderedBadSchema)
				link := filepath.Join(base, "link", "bad.yammm")
				if err := os.MkdirAll(filepath.Dir(link), 0o750); err != nil {
					t.Fatal(err)
				}
				symlinkOrSkip(t, filepath.Join("..", "real", "bad.yammm"), link)
				return []string{"validate", link}, filepath.Dir(real), real
			},
		},
		{
			name: "a directory with a decomposed name",
			setup: func(t *testing.T, base string) ([]string, string, string) {
				t.Helper()
				file := writeRenderedFile(t, filepath.Join(base, "cafe\u0301", "bad.yammm"), renderedBadSchema)
				return []string{"validate", file}, filepath.Dir(file), file
			},
		},
		{
			name: "a directory with a decomposed name, given as --module-root",
			setup: func(t *testing.T, base string) ([]string, string, string) {
				t.Helper()
				file := writeRenderedFile(t, filepath.Join(base, "cafe\u0301", "bad.yammm"), renderedBadSchema)
				return []string{"validate", "--module-root", filepath.Dir(file), file}, filepath.Dir(file), file
			},
		},
	}

	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			args, root, file := row.setup(t, t.TempDir())
			code, stderr := executeCmdStderr(t, args...)
			if code != cli.ExitValidation {
				t.Fatalf("exit %d, want %d; stderr:\n%s", code, cli.ExitValidation, stderr)
			}
			if want := hostRelative(t, root, file) + ":5:2: "; !strings.HasPrefix(stderr, want) {
				t.Errorf("stderr:\n%s\nwant the prefix %s", stderr, want)
			}
		})
	}
}

// TestRenderedLocations_FmtUnderASymlinkedDirectory runs fmt from a working
// directory reached through a symlink. It changes the process's working
// directory, so it cannot run in parallel.
func TestRenderedLocations_FmtUnderASymlinkedDirectory(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "real")
	writeRenderedFile(t, filepath.Join(real, "fmtbad.yammm"), "schema \"f\"\n\ntype T {\n\tid String primary\n\tname String {\n}\n")
	link := filepath.Join(base, "ln")
	symlinkOrSkip(t, "real", link)
	t.Chdir(link)

	code, stderr := executeCmdStderr(t, "fmt", "fmtbad.yammm")
	if code != cli.ExitValidation {
		t.Fatalf("exit %d, want %d; stderr:\n%s", code, cli.ExitValidation, stderr)
	}
	if want := hostRelative(t, real, filepath.Join(real, "fmtbad.yammm")) + ":"; !strings.HasPrefix(stderr, want) {
		t.Errorf("stderr:\n%s\nwant the prefix %s", stderr, want)
	}
}

// runWithTerminalSink runs one command's body with a sink that renders as it
// does on a terminal, without colour, and returns what the sink wrote. The
// shipped wrapper takes the terminal test from the process's stderr, which a
// test cannot make a terminal.
func runWithTerminalSink(t *testing.T, argv []string, run func(*cobra.Command, []string, *cli.DiagnosticSink) error) string {
	t.Helper()
	cmd, rest, err := newRootCmd("test").Find(argv)
	if err != nil {
		t.Fatalf("find %v: %v", argv, err)
	}
	if err := cmd.ParseFlags(rest); err != nil {
		t.Fatalf("parse flags %v: %v", rest, err)
	}
	cmd.SetContext(t.Context())
	cmd.SetOut(&bytes.Buffer{})
	var out bytes.Buffer
	sink := cli.NewDiagnosticSink(&out, cli.FormatText, true, true)
	_ = sink.Close(run(cmd, cmd.Flags().Args(), sink))
	return out.String()
}

// TestRenderedLocations_FailedLoadShowsItsExcerpt runs every command that
// loads a schema over one that fails to load, through a terminal sink, with and
// without --module-root. The failure's excerpt must render, as a load that only
// warns renders one.
func TestRenderedLocations_FailedLoadShowsItsExcerpt(t *testing.T) {
	t.Parallel()
	yammmtest.RequireNoModuleRoot(t, schema.FindModuleRoot)

	rows := []struct {
		name string
		args func(schemaPath, dataPath, snapshotPath string) []string
		run  func(*cobra.Command, []string, *cli.DiagnosticSink) error
	}{
		{"check", func(s, d, _ string) []string { return []string{"check", s, d} }, runCheck},
		{"export", func(s, d, _ string) []string { return []string{"export", "--to", "json", s, d} }, runExport},
		{"gen", func(s, _, _ string) []string { return []string{"gen", "--to", "go", s} }, runGen},
		{"load", func(s, d, _ string) []string { return []string{"load", s, d} }, runLoad},
		{"neo4j constraints", func(s, _, _ string) []string { return []string{"neo4j", "constraints", s} }, runNeo4jConstraints},
		{"neo4j diff", func(s, _, _ string) []string { return []string{"neo4j", "diff", "--uri", "bolt://localhost:7687", s} }, runNeo4jDiff},
		{"neo4j indexes", func(s, _, _ string) []string { return []string{"neo4j", "indexes", s} }, runNeo4jIndexes},
		{"snapshot save", func(s, d, o string) []string { return []string{"snapshot", "save", "--output", o, s, d} }, runSnapshotSave},
		{"snapshot verify", func(s, _, o string) []string { return []string{"snapshot", "verify", s, o} }, runSnapshotVerify},
		{"validate", func(s, _, _ string) []string { return []string{"validate", s} }, runValidate},
	}

	for _, row := range rows {
		for _, withRoot := range []bool{false, true} {
			name := row.name
			if withRoot {
				name += ", with --module-root"
			}
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				dir := t.TempDir()
				schemaPath := writeRenderedFile(t, filepath.Join(dir, "bad.yammm"), renderedBadSchema)
				dataPath := writeRenderedFile(t, filepath.Join(dir, "data.json"), "{}")
				args := row.args(schemaPath, dataPath, filepath.Join(dir, "out.ys"))
				if withRoot {
					args = append(args, "--module-root", dir)
				}
				out := runWithTerminalSink(t, args, row.run)
				if !strings.Contains(out, "E_UNKNOWN_TYPE") {
					t.Fatalf("the command did not report the failed load:\n%s", out)
				}
				if !strings.Contains(out, "\n5 | \tname Strin\n") {
					t.Errorf("the failed load's excerpt did not render:\n%s", out)
				}
			})
		}
	}
}
