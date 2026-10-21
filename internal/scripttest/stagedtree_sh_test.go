package scripttest

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// The script passes a working tree that is the index, ignored files beside it,
// and refuses each change the index does not hold, naming the path.
func TestStagedTreeScript_RefusesAWorkingTreeThatIsNotTheIndex(t *testing.T) {
	t.Parallel()
	rows := []struct {
		name   string
		change func(t *testing.T, f *fixture)
		stderr string // "" when the tree passes
	}{
		{"the index", func(*testing.T, *fixture) {}, ""},
		{"an ignored file", func(_ *testing.T, f *fixture) { f.write("bin/yammm", "built\n") }, ""},
		{
			"an unstaged edit", func(_ *testing.T, f *fixture) { f.write("ok/ok.go", "package ok\n\nconst X = 1\n") },
			"stagedtree: the working tree differs from the index in 1 tracked file(s):\n  ok/ok.go\n",
		},
		{"a deleted file", func(t *testing.T, f *fixture) {
			t.Helper()
			if err := os.Remove(filepath.Join(f.dir, "ok", "ok_test.go")); err != nil {
				t.Fatal(err)
			}
		}, "stagedtree: the working tree differs from the index in 1 tracked file(s):\n  ok/ok_test.go\n"},
		{
			"an untracked Go file", func(_ *testing.T, f *fixture) { f.write("ok/helper.go", "package ok\n") },
			"stagedtree: 1 untracked file(s) git does not ignore:\n  ok/helper.go\n",
		},
		{
			"an untracked note", func(_ *testing.T, f *fixture) { f.write("notes.txt", "todo\n") },
			"stagedtree: 1 untracked file(s) git does not ignore:\n  notes.txt\n",
		},
	}
	if runtime.GOOS != "windows" {
		rows = append(rows, struct {
			name   string
			change func(t *testing.T, f *fixture)
			stderr string
		}{"a mode change", func(t *testing.T, f *fixture) {
			t.Helper()
			if err := os.Chmod(filepath.Join(f.dir, "ok", "ok.go"), 0o700); err != nil { //nolint:gosec // an executable bit git records
				t.Fatal(err)
			}
		}, "stagedtree: the working tree differs from the index in 1 tracked file(s):\n  ok/ok.go\n"})
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t)
			f.copyScript("stagedtree.sh")
			f.write(".gitignore", "bin/\n")
			f.index()
			row.change(t, f)

			r := f.run("stagedtree.sh")
			if row.stderr == "" {
				r.wantCode(t, 0)
				if r.stdout+r.stderr != "" {
					t.Errorf("a passing tree printed\nstdout:\n%s\nstderr:\n%s", r.stdout, r.stderr)
				}
				return
			}
			r.wantCode(t, 1)
			r.wantStderr(t, row.stderr)
			r.wantStderr(t, "stagedtree: the full gate judges the index; stage each file, or stash it with git stash --include-untracked\n")
		})
	}
}

// The script counts every file it names, and judges the whole repository from
// any directory in it.
func TestStagedTreeScript_CountsEveryFileFromAnyDirectory(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.copyScript("stagedtree.sh")
	f.index()
	f.write("ok/ok.go", "package ok\n\nconst X = 1\n")
	f.write("ok/ok_test.go", "package ok\n")
	f.write("notes.txt", "todo\n")

	r := f.runFrom(filepath.Join(f.dir, "ok"), "stagedtree.sh")
	r.wantCode(t, 1)
	r.wantStderr(t, "stagedtree: the working tree differs from the index in 2 tracked file(s):\n  ok/ok.go\n  ok/ok_test.go\n")
	r.wantStderr(t, "stagedtree: 1 untracked file(s) git does not ignore:\n  notes.txt\n")
}
