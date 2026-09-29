package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
)

// TestRun_SnapshotThatOpensAndCannotBeReadExitsRuntime pins one exit code for
// a snapshot path that opens and fails to read: a directory, which os.Open
// accepts on unix and whose first read fails. The body read reports it as an
// error return and the header-only read as a diagnostic, and both exit 3.
func TestRun_SnapshotThatOpensAndCannotBeReadExitsRuntime(t *testing.T) {
	dir := t.TempDir()
	for _, args := range [][]string{
		{"snapshot", "info", dir},
		{"snapshot", "info", "--header-only", dir},
	} {
		if code, _, stderr := runCLI(t, args...); code != 3 {
			t.Errorf("%v: exit %d, want 3 (stderr %q)", args, code, stderr)
		}
	}
}

// A command whose snapshot read is cancelled exits 3: the cancellation is a
// failure outside the input, whichever read site reports it.
func TestRun_ACancelledSnapshotReadExitsRuntime(t *testing.T) {
	t.Parallel()
	snap := filepath.Join(t.TempDir(), "s.ys")
	copyFile(t, "testdata/valid_snapshot.ys", snap)
	for _, args := range [][]string{
		{"snapshot", "info", snap},
		{"snapshot", "update-metadata", "-s", "k=v", snap},
	} {
		t.Run(strings.Join(args[:2], " "), func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			cmd := newRootCmd("test")
			cmd.SetContext(ctx)
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			cmd.SetArgs(args)
			if code := cli.ExitForError(execute(cmd)); code != cli.ExitRuntime {
				t.Errorf("%v under a cancelled context: exit %d, want %d", args, code, cli.ExitRuntime)
			}
		})
	}
}

// snapshot info --dir takes the worst of its entries whatever order they
// list in: a malformed entry listed first still fails a directory whose other
// entry is clean, and an entry that cannot be read outranks a malformed one.
func TestRun_SnapshotDirTakesTheWorstEntry(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name    string
		entries map[string]string
		want    int
	}{
		{"a malformed entry first", map[string]string{"a_corrupt.ys": "{", "b_clean.ys": ""}, cli.ExitValidation},
		{"an unreadable entry beside a malformed one", map[string]string{"a_dangling.ys": "->", "b_corrupt.ys": "{"}, cli.ExitRuntime},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			for name, content := range c.entries {
				p := filepath.Join(dir, name)
				switch content {
				case "":
					copyFile(t, "testdata/valid_snapshot.ys", p)
				case "->":
					if err := os.Symlink("missing.ys", p); err != nil {
						t.Skipf("symlinks unavailable: %v", err)
					}
				default:
					writeFixture(t, p, content)
				}
			}
			if code, _, stderr := executeCmdOutput(t, "snapshot", "info", "--dir", dir); code != c.want {
				t.Errorf("exit %d, want %d: %s", code, c.want, stderr)
			}
		})
	}
}
