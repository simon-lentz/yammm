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

	t.Run("a failed write raises no extension warning", func(t *testing.T) {
		t.Parallel()
		// A parent path that is a regular file fails the write on every host and for root.
		parent := filepath.Join(t.TempDir(), "file")
		if err := os.WriteFile(parent, nil, 0o600); err != nil {
			t.Fatalf("write the parent file: %v", err)
		}
		code, _, stderr := executeCmdOutput(t, "snapshot", "save", "-o", filepath.Join(parent, "o.dat"),
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
