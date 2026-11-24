package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
)

// TestCommands_ATrailingSeparatorNamesADirectory holds every command to the
// kernel's reading of a path that ends in a separator or a "." element: it
// names a directory, so a regular file spelled that way is refused, never
// read or written under its bare name.
func TestCommands_ATrailingSeparatorNamesADirectory(t *testing.T) {
	t.Parallel()

	for _, suffix := range []string{"/", string(filepath.Separator) + "."} {
		t.Run("check, the data file", func(t *testing.T) {
			t.Parallel()
			code, _, stderr := executeCmdOutput(t, "check", "testdata/valid.yammm", "testdata/data.json"+suffix)
			if code == cli.ExitOK {
				t.Errorf("check read data.json%s as data.json; stderr:\n%s", suffix, stderr)
			}
		})

		t.Run("gen, the schema", func(t *testing.T) {
			t.Parallel()
			code, _, stderr := executeCmdOutput(t, "gen", "--to", "md", "testdata/valid.yammm"+suffix)
			if code == cli.ExitOK {
				t.Errorf("gen loaded valid.yammm%s as valid.yammm; stderr:\n%s", suffix, stderr)
			}
		})

		t.Run("snapshot save, the seed it merges into", func(t *testing.T) {
			t.Parallel()
			seed := filepath.Join(t.TempDir(), "seed.ys")
			copyFile(t, "testdata/valid_snapshot.ys", seed)
			before, err := os.ReadFile(seed)
			if err != nil {
				t.Fatal(err)
			}
			code, _, stderr := executeCmdOutput(t, "snapshot", "save", "--into", seed+suffix,
				"testdata/valid.yammm", "testdata/data_carol.json")
			if code == cli.ExitOK {
				t.Errorf("snapshot save merged into seed.ys%s as seed.ys; stderr:\n%s", suffix, stderr)
			}
			after, err := os.ReadFile(seed)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Errorf("snapshot save rewrote seed.ys through seed.ys%s", suffix)
			}
		})
	}
}

// TestCommands_ADirectoryFormIsRefusedAsNotADirectory holds the refusal the
// tool states for a path in directory form whose last element is a file, or,
// for a write, nothing yet: exit 3, the words "not a directory" on every host,
// and no file read or written under the bare name.
func TestCommands_ADirectoryFormIsRefusedAsNotADirectory(t *testing.T) {
	t.Parallel()
	sep := string(filepath.Separator)

	t.Run("check, the data file", func(t *testing.T) {
		t.Parallel()
		code, msg := refusal(t, "check", "testdata/valid.yammm", "testdata/data.json"+sep)
		if code != cli.ExitRuntime || !strings.Contains(msg, "not a directory") {
			t.Errorf("check data.json%s: exit %d, %q; want exit %d naming not a directory", sep, code, msg, cli.ExitRuntime)
		}
	})

	t.Run("check, an absent data file", func(t *testing.T) {
		t.Parallel()
		code, msg := refusal(t, "check", "testdata/valid.yammm", "testdata/absent.json"+sep)
		if code != cli.ExitRuntime || strings.Contains(msg, "not a directory") {
			t.Errorf("check absent.json%s: exit %d, %q; want exit %d naming the missing file", sep, code, msg, cli.ExitRuntime)
		}
	})

	t.Run("snapshot save -o, a link whose target is in directory form", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		link := filepath.Join(dir, "to-new")
		if err := os.Symlink("new.ys"+sep, link); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		code, msg := refusal(t, "snapshot", "save", "-o", link, "testdata/valid.yammm", "testdata/data_carol.json")
		if code != cli.ExitRuntime || !strings.Contains(msg, "not a directory") {
			t.Errorf("snapshot save -o to-new: exit %d, %q; want exit %d naming not a directory", code, msg, cli.ExitRuntime)
		}
		if entries, _ := os.ReadDir(dir); len(entries) != 1 {
			t.Errorf("snapshot save -o to-new left %d entries, want the link alone", len(entries))
		}
	})

	t.Run("snapshot info --header-only, the snapshot file", func(t *testing.T) {
		t.Parallel()
		code, msg := refusal(t, "snapshot", "info", "--header-only", "testdata/valid_snapshot.ys"+sep)
		if code != cli.ExitRuntime || !strings.Contains(msg, "not a directory") {
			t.Errorf("snapshot info --header-only valid_snapshot.ys%s: exit %d, %q; want exit %d naming not a directory", sep, code, msg, cli.ExitRuntime)
		}
	})

	t.Run("snapshot save -o, under a missing directory", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		out := filepath.Join(dir, "nodir", "out.ys") + sep
		code, msg := refusal(t, "snapshot", "save", "-o", out, "testdata/valid.yammm", "testdata/data_carol.json")
		if code != cli.ExitRuntime || strings.Contains(msg, "not a directory") {
			t.Errorf("snapshot save -o %s: exit %d, %q; want exit %d naming the missing path", out, code, msg, cli.ExitRuntime)
		}
		if entries, _ := os.ReadDir(dir); len(entries) != 0 {
			t.Errorf("snapshot save -o %s left %d entries", out, len(entries))
		}
	})

	for _, existing := range []bool{true, false} {
		t.Run(fmt.Sprintf("snapshot save -o, an existing file %t", existing), func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			out := filepath.Join(dir, "out.ys")
			if existing {
				if err := os.WriteFile(out, []byte("old"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			code, msg := refusal(t, "snapshot", "save", "-o", out+sep,
				"testdata/valid.yammm", "testdata/data_carol.json")
			if code != cli.ExitRuntime || !strings.Contains(msg, "not a directory") {
				t.Errorf("snapshot save -o out.ys%s: exit %d, %q; want exit %d naming not a directory", sep, code, msg, cli.ExitRuntime)
			}
			got, err := os.ReadFile(out)
			switch {
			case existing && string(got) != "old":
				t.Errorf("snapshot save -o out.ys%s changed out.ys to %q (%v)", sep, got, err)
			case !existing && err == nil:
				t.Errorf("snapshot save -o out.ys%s wrote out.ys", sep)
			}
			if entries, _ := os.ReadDir(dir); len(entries) != map[bool]int{true: 1, false: 0}[existing] {
				t.Errorf("snapshot save -o out.ys%s left %d entries", sep, len(entries))
			}
		})
	}
}

// refusal runs the command and returns its exit code and the text of the error
// run() prints for it.
func refusal(t *testing.T, args ...string) (int, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	cmd := newRootCmd("test")
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs(args)
	err := execute(cmd)
	if err == nil {
		return cli.ExitOK, ""
	}
	return cli.ExitForError(err), err.Error()
}
