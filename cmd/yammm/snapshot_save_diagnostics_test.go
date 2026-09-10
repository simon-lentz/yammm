package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
)

// TestSnapshotSave_ExtensionWarningDescribesAWrittenFile: the warning's
// registered meaning is that a snapshot WAS written to a path an
// extension-discovering reader will not find, so it names that file and is
// raised only once the write has succeeded.
func TestSnapshotSave_ExtensionWarningDescribesAWrittenFile(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("root ignores the write bit")
	}

	t.Run("a failed write raises no extension warning", func(t *testing.T) {
		t.Parallel()
		sealed := filepath.Join(t.TempDir(), "sealed")
		if err := os.Mkdir(sealed, 0o550); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		t.Cleanup(func() { _ = os.Chmod(sealed, 0o750) }) //nolint:gosec // restoring the test's own directory
		code, _, stderr := executeCmdOutput(t, "snapshot", "save", "-o", filepath.Join(sealed, "o.dat"),
			"testdata/valid.yammm", "testdata/data.json")
		var failure string
		switch {
		case code != cli.ExitRuntime:
			failure = fmt.Sprintf("exit code = %d, want %d", code, cli.ExitRuntime)
		case strings.Contains(stderr, "W_SNAPSHOT_PATH_EXTENSION"):
			failure = "a snapshot that was not written is reported as written to a non-.ys path:\n" + stderr
		}
		checkRepairState(t, "snapshot save: a failed write raises no extension warning", failure)
	})

	t.Run("the warning names the output path", func(t *testing.T) {
		t.Parallel()
		out := filepath.Join(t.TempDir(), "made.dat")
		_, _, stderr := executeCmdOutput(t, "snapshot", "save", "-o", out,
			"testdata/valid.yammm", "testdata/data.json")
		var failure string
		if !strings.Contains(stderr, "made.dat: warning[W_SNAPSHOT_PATH_EXTENSION]") {
			failure = "the warning does not locate itself at the file it describes:\n" + stderr
		}
		checkRepairState(t, "snapshot save: the extension warning names the output path", failure)
	})

	t.Run("a .ys output raises no warning", func(t *testing.T) {
		t.Parallel()
		out := filepath.Join(t.TempDir(), "made.ys")
		_, _, stderr := executeCmdOutput(t, "snapshot", "save", "-o", out,
			"testdata/valid.yammm", "testdata/data.json")
		if strings.Contains(stderr, "W_SNAPSHOT_PATH_EXTENSION") {
			t.Errorf("a .ys output drew the extension warning:\n%s", stderr)
		}
	})
}

// TestSnapshotSave_SummaryCountsTheTypeTable: the summary describes the
// written document, whose type table is what `snapshot info` reports for the
// same file. A composed child has a type in that table and no root instance,
// so a count taken from the graph's root instances disagrees with the reader.
func TestSnapshotSave_SummaryCountsTheTypeTable(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "composed.yammm")
	books := filepath.Join(dir, "books.json")
	out := filepath.Join(dir, "out.ys")
	if err := os.WriteFile(schemaPath, []byte("schema \"composed\"\n\ntype Book {\n\tid String primary\n\t*-> CHAPTERS Chapter\n}\n\npart type Chapter {\n\ttitle String\n}\n"), 0o600); err != nil {
		t.Fatalf("write schema: %v", err)
	}
	if err := os.WriteFile(books, []byte(`{"Book": [{"id": "b1", "chapters": [{"title": "c1"}]}, {"id": "b2"}]}`), 0o600); err != nil {
		t.Fatalf("write data: %v", err)
	}

	code, _, stderr := executeCmdOutput(t, "snapshot", "save", schemaPath, books, "-o", out)
	if code != cli.ExitOK {
		t.Fatalf("exit code = %d, want %d\nstderr:\n%s", code, cli.ExitOK, stderr)
	}
	_, info, _ := executeCmdOutput(t, "snapshot", "info", out)
	summary := strings.Fields(lineContaining(t, stderr, "saved snapshot:"))
	reported := strings.Fields(lineContaining(t, info, "Types:"))
	var failure string
	if summary[5] != reported[1] {
		failure = fmt.Sprintf("save reported %s types and snapshot info reports %s — one file, two counts", summary[5], reported[1])
	}
	checkRepairState(t, "snapshot save: the summary's type count is the type table's", failure)
}

// TestSnapshotSaveHelp_StatesWhatIntoCarries: the Long text says when a
// created_at is written, and --into carrying the merged file's own is one of
// the two ways.
func TestSnapshotSaveHelp_StatesWhatIntoCarries(t *testing.T) {
	t.Parallel()

	long := newSnapshotSaveCmd().Long
	var failure string
	if !strings.Contains(long, "--into") || !strings.Contains(long, "created_at") {
		failure = "the Long text does not say that --into carries created_at forward:\n" + long
	}
	checkRepairState(t, "snapshot save: the Long text states what --into carries", failure)
}
