package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
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

// checkKnownBroken reports a row's outcome against its knownBroken entry: a
// listed row that passes fails, so the repair must remove its entry.
func checkKnownBroken(t *testing.T, knownBroken map[string]string, name string, ok bool, detail string) {
	t.Helper()
	reason, broken := knownBroken[name]
	switch {
	case broken && ok:
		t.Errorf("listed as broken (%s) and now passes: remove its knownBroken entry", reason)
	case broken:
		t.Logf("known broken: %s\n%s", reason, detail)
	case !ok:
		t.Error(detail)
	}
}

func checkEveryEntryNamesARow(t *testing.T, knownBroken map[string]string, names map[string]bool) {
	t.Helper()
	for name := range knownBroken {
		if !names[name] {
			t.Errorf("knownBroken names no row: %q", name)
		}
	}
}

// TestRenderedLocations_RelativeToTheRoot holds a failed load's text location
// to the host's own path from the root the load used to the schema file, as
// [hostRelative] computes it.
func TestRenderedLocations_RelativeToTheRoot(t *testing.T) {
	t.Parallel()
	yammmtest.RequireNoModuleRoot(t, schema.FindModuleRoot)

	knownBroken := map[string]string{
		"a schema reached through a symlinked file":                  "the root is the link's directory, which does not hold the resolved source (B4)",
		"a directory with a decomposed name":                         "the root's host bytes are not the source's NFC identity (PB1)",
		"a directory with a decomposed name, given as --module-root": "the root's host bytes are not the source's NFC identity (PB1)",
	}
	if runtime.GOOS == "windows" {
		knownBroken["a plain directory"] = "a backslash-separated root never prefixes a /-separated identity (B3)"
	}

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

	names := make(map[string]bool, len(rows))
	for _, row := range rows {
		names[row.name] = true
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			args, root, file := row.setup(t, t.TempDir())
			code, stderr := executeCmdStderr(t, args...)
			if code != cli.ExitValidation {
				t.Fatalf("exit %d, want %d; stderr:\n%s", code, cli.ExitValidation, stderr)
			}
			want := hostRelative(t, root, file) + ":5:2: "
			checkKnownBroken(t, knownBroken, row.name, strings.HasPrefix(stderr, want),
				"stderr:\n"+stderr+"\nwant the prefix "+want)
		})
	}
	checkEveryEntryNamesARow(t, knownBroken, names)
}

// TestRenderedLocations_FmtUnderASymlinkedDirectory runs fmt from a working
// directory reached through a symlink. It changes the process's working
// directory, so it cannot run in parallel.
func TestRenderedLocations_FmtUnderASymlinkedDirectory(t *testing.T) {
	const name = "fmt run from a symlinked working directory"
	knownBroken := map[string]string{
		name: "fmt's root is its unresolved working directory, which does not hold the resolved source (B6)",
	}

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
	want := hostRelative(t, real, filepath.Join(real, "fmtbad.yammm")) + ":"
	checkKnownBroken(t, knownBroken, name, strings.HasPrefix(stderr, want),
		"stderr:\n"+stderr+"\nwant the prefix "+want)
	checkEveryEntryNamesARow(t, knownBroken, map[string]bool{name: true})
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
// loads a schema over one that fails to load, through a terminal sink. The
// failure's excerpt must render, as a load that only warns renders one.
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

	knownBroken := make(map[string]string, len(rows))
	names := make(map[string]bool, len(rows))
	for _, row := range rows {
		knownBroken[row.name] = "a failed load returns no schema, so the sink holds no source to excerpt (B2)"
		names[row.name] = true
	}

	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			schemaPath := writeRenderedFile(t, filepath.Join(dir, "bad.yammm"), renderedBadSchema)
			dataPath := writeRenderedFile(t, filepath.Join(dir, "data.json"), "{}")
			out := runWithTerminalSink(t, row.args(schemaPath, dataPath, filepath.Join(dir, "out.ys")), row.run)
			if !strings.Contains(out, "E_UNKNOWN_TYPE") {
				t.Fatalf("the command did not report the failed load:\n%s", out)
			}
			checkKnownBroken(t, knownBroken, row.name, strings.Contains(out, "\n5 | \tname Strin\n"),
				"rendered:\n"+out)
		})
	}
	checkEveryEntryNamesARow(t, knownBroken, names)
}
