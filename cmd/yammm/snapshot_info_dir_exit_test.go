package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
	"github.com/simon-lentz/yammm/diag"
)

// commandFailures returns the E_COMMAND_FAILED issues of doc, which must be
// one diagnostics document.
func commandFailures(t *testing.T, doc string) []documentIssue {
	t.Helper()
	var failures []documentIssue
	for _, iss := range decodeDocument(t, doc).Issues {
		if iss.Code == diag.E_COMMAND_FAILED.String() {
			failures = append(failures, iss)
		}
	}
	return failures
}

// assertStatesTheExit checks that doc accounts for a failing exit with one
// E_COMMAND_FAILED Error whose message contains reason and whose exit_code is
// code.
func assertStatesTheExit(t *testing.T, doc string, code, reason string) {
	t.Helper()
	failures := commandFailures(t, doc)
	if len(failures) != 1 {
		t.Fatalf("document holds %d E_COMMAND_FAILED issues, want 1:\n%s", len(failures), doc)
	}
	got := failures[0]
	if got.Severity != "error" {
		t.Errorf("E_COMMAND_FAILED severity = %q, want error", got.Severity)
	}
	if !strings.Contains(got.Message, reason) {
		t.Errorf("E_COMMAND_FAILED message = %q, want it to contain %q", got.Message, reason)
	}
	if value, ok := got.detail(diag.DetailKeyExitCode); len(got.Details) != 1 || !ok || value != code {
		t.Errorf("E_COMMAND_FAILED details = %+v, want exit_code %s alone", got.Details, code)
	}
}

// TestSnapshotInfo_DirJSONDocumentStatesTheFailedEntries pins that a --dir scan
// whose entries failed accounts for its exit in the stderr document, counting
// the failed entries among all, though the entries' own issues are in the
// stdout report alone, and that text output prints nothing for it.
func TestSnapshotInfo_DirJSONDocumentStatesTheFailedEntries(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		clean, corrupt int
		want           string
	}{
		{1, 1, "1 of 2 snapshot files failed"},
		{1, 2, "2 of 3 snapshot files failed"},
		{2, 1, "1 of 3 snapshot files failed"},
	} {
		dir := t.TempDir()
		for i := range tt.clean {
			saveFixture(t, filepath.Join(dir, "clean"+strconv.Itoa(i)+".ys"))
		}
		for i := range tt.corrupt {
			if err := os.WriteFile(filepath.Join(dir, "corrupt"+strconv.Itoa(i)+".ys"), []byte("not json"), 0o600); err != nil {
				t.Fatalf("write corrupt fixture: %v", err)
			}
		}

		code, _, stderr := executeCmdOutput(t, "snapshot", "info", "--dir", dir, "--json")
		if code != cli.ExitValidation {
			t.Fatalf("%s: exit = %d, want %d; stderr:\n%s", tt.want, code, cli.ExitValidation, stderr)
		}
		assertStatesTheExit(t, stderr, "1", tt.want)

		code, _, stderr = executeCmdOutput(t, "snapshot", "info", "--dir", dir)
		if code != cli.ExitValidation || stderr != "" {
			t.Errorf("%s, text mode: exit = %d, stderr = %q; want %d and nothing", tt.want, code, stderr, cli.ExitValidation)
		}
	}
}

// TestSnapshotInfo_DirThatCannotBeReadIsItsOwnAccount pins that a Fatal
// E_SNAPSHOT_IO for the directory earns the exit itself, so the document holds
// no E_COMMAND_FAILED beside it.
func TestSnapshotInfo_DirThatCannotBeReadIsItsOwnAccount(t *testing.T) {
	t.Parallel()
	code, _, stderr := executeCmdOutput(t, "snapshot", "info", "--dir", filepath.Join(t.TempDir(), "missing"), "--json")
	if code != cli.ExitRuntime {
		t.Fatalf("exit = %d, want %d; stderr:\n%s", code, cli.ExitRuntime, stderr)
	}
	if failures := commandFailures(t, stderr); len(failures) != 0 {
		t.Errorf("document holds E_COMMAND_FAILED %+v beside the directory's own failure:\n%s", failures, stderr)
	}
}
