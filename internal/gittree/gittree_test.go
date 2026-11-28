package gittree

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestWithoutRepositoryVars_RemovesTheVariablesGitNames(t *testing.T) {
	t.Parallel()
	env := []string{"GIT_DIR=/other/.git", "PATH=/bin", "GIT_INDEX_FILE=/other/.git/index", "GIT_AUTHOR_NAME=a", "GIT_WORK_TREE=/other"}
	if got, want := WithoutRepositoryVars(t, env), []string{"PATH=/bin", "GIT_AUTHOR_NAME=a"}; !slices.Equal(got, want) {
		t.Errorf("WithoutRepositoryVars = %q, want %q", got, want)
	}
}

func TestWriteFile_CreatesTheDirectoriesAboveTheFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	WriteFile(t, dir, "a/b/run.sh", []byte("#!/bin/sh\n"), 0o700)
	path := filepath.Join(dir, "a", "b", "run.sh")
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "#!/bin/sh\n" {
		t.Fatalf("read %q, %v", got, err)
	}
	// Windows reports no execute bit.
	if info, err := os.Stat(path); err != nil || runtime.GOOS != "windows" && info.Mode().Perm() != 0o700 {
		t.Errorf("mode %v, %v, want 0700", info.Mode().Perm(), err)
	}
}

// lsFiles lists dir's index with the repository variables removed.
func lsFiles(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", "ls-files")
	cmd.Dir = dir
	cmd.Env = WithoutRepositoryVars(t, os.Environ())
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git ls-files: %v", err)
	}
	return string(out)
}

func TestIndex_AddsTheNamedPathsOrEveryFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	WriteFile(t, dir, "a.txt", []byte("a\n"), 0o600)
	WriteFile(t, dir, "sub/b.txt", []byte("b\n"), 0o600)

	Index(t, dir, "a.txt")
	if got := lsFiles(t, dir); got != "a.txt\n" {
		t.Errorf("after Index(a.txt) the index holds %q, want a.txt alone", got)
	}
	Index(t, dir)
	if got := lsFiles(t, dir); got != "a.txt\nsub/b.txt\n" {
		t.Errorf("after Index() the index holds %q, want both files", got)
	}
	head := exec.CommandContext(t.Context(), "git", "-C", dir, "rev-parse", "--verify", "-q", "HEAD")
	head.Env = WithoutRepositoryVars(t, os.Environ())
	if out, err := head.Output(); err == nil {
		t.Errorf("Index committed: HEAD is %s", strings.TrimSpace(string(out)))
	}
}

// Not parallel: it sets GIT_DIR for the whole process, as a linked worktree's
// hook does.
func TestEnv_KeepsTheRepositoryVariablesAtTheEnclosingTopAlone(t *testing.T) {
	top, err := enclosingTop()
	if err != nil {
		t.Fatal(err)
	}
	if top == "" {
		t.Skip("no enclosing repository: the tree is not a git work tree")
	}
	const gitDir = "GIT_DIR=/elsewhere/.git"
	t.Setenv("GIT_DIR", "/elsewhere/.git")
	for _, tt := range []struct {
		root string
		keep bool
	}{
		{root: top, keep: true},
		{root: filepath.Join(top, "internal"), keep: false},
		{root: t.TempDir(), keep: false},
	} {
		env, err := Env(tt.root)
		if err != nil {
			t.Fatal(err)
		}
		if got := slices.Contains(env, gitDir); got != tt.keep {
			t.Errorf("Env(%s) holds %s: %v, want %v", tt.root, gitDir, got, tt.keep)
		}
	}
}

// Not parallel: it points PATH at an empty directory for the whole process, so
// no git is found.
func TestLocalEnvVars_NamesTheCommandThatFailed(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if _, err := localEnvVars(); err == nil || !strings.Contains(err.Error(), "git rev-parse --local-env-vars: ") {
		t.Errorf("localEnvVars with no git on PATH = %v, want an error naming the command", err)
	}
}

// runGit runs git in dir with the repository variables removed, as a fixture's
// own repository needs.
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	cmd.Env = WithoutRepositoryVars(t, os.Environ())
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// Each kind of root reads one tree: git's listing where the root belongs to a
// work tree, the files on disk where it is a copy. Either reading is sorted by
// path, so a-b comes before a/x, and names each file from the root.
func TestFiles_ReadsTheTreeOfEachRoot(t *testing.T) {
	t.Parallel()
	fill := func(dir string) {
		WriteFile(t, dir, "a/x", []byte("x\n"), 0o600)
		WriteFile(t, dir, "a-b", []byte("b\n"), 0o600)
		WriteFile(t, dir, ".github/w.yml", []byte("w\n"), 0o600)
	}
	for _, tt := range []struct {
		name string
		root func(t *testing.T) string
		want []string
	}{
		{"no .git at or above, as the module cache holds a release", func(t *testing.T) string {
			t.Helper()
			dir := t.TempDir()
			fill(dir)
			return dir
		}, []string{".github/w.yml", "a-b", "a/x"}},
		{"a copy inside a repository that tracks none of it", func(t *testing.T) string {
			t.Helper()
			dir := t.TempDir()
			WriteFile(t, dir, "top.txt", []byte("top\n"), 0o600)
			Index(t, dir, "top.txt")
			fill(filepath.Join(dir, "copy"))
			return filepath.Join(dir, "copy")
		}, []string{".github/w.yml", "a-b", "a/x"}},
		{"a root below the top that the repository tracks", func(t *testing.T) string {
			t.Helper()
			dir := t.TempDir()
			fill(filepath.Join(dir, "mod"))
			WriteFile(t, dir, "mod/untracked", []byte("u\n"), 0o600)
			Index(t, dir, "mod/a", "mod/a-b")
			return filepath.Join(dir, "mod")
		}, []string{"a-b", "a/x"}},
		{"the top of a repository with an empty index", func(t *testing.T) string {
			t.Helper()
			dir := t.TempDir()
			fill(dir)
			runGit(t, dir, "init", "-q")
			return dir
		}, nil},
		{"the top of a repository", func(t *testing.T) string {
			t.Helper()
			dir := t.TempDir()
			fill(dir)
			Index(t, dir)
			WriteFile(t, dir, "untracked", []byte("u\n"), 0o600)
			return dir
		}, []string{".github/w.yml", "a-b", "a/x"}},
		{"a linked worktree, whose .git is a file", func(t *testing.T) string {
			t.Helper()
			dir := t.TempDir()
			fill(dir)
			Index(t, dir)
			runGit(t, dir, "-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false", "commit", "-q", "--no-verify", "-m", "m")
			linked := filepath.Join(t.TempDir(), "linked")
			runGit(t, dir, "worktree", "add", "-q", linked)
			WriteFile(t, linked, "untracked", []byte("u\n"), 0o600)
			return linked
		}, []string{".github/w.yml", "a-b", "a/x"}},
		{"a root that is a link", func(t *testing.T) string {
			t.Helper()
			target := t.TempDir()
			fill(target)
			link := filepath.Join(t.TempDir(), "link")
			if err := os.Symlink(target, link); err != nil {
				t.Skipf("this host makes no symbolic link: %v", err)
			}
			return link
		}, []string{".github/w.yml", "a-b", "a/x"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := Files(tt.root(t))
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("Files = %q, want %q", got, tt.want)
			}
		})
	}
}

// A root git refuses, or one where git names another work tree than the .git
// found, is an error, never a copy: reading it as one would judge a tree
// nobody tracks.
func TestFiles_RefusesARootGitCannotRead(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		root func(t *testing.T) string
		want string
	}{
		{"a .git that names no repository", func(t *testing.T) string {
			t.Helper()
			dir := t.TempDir()
			WriteFile(t, dir, ".git", []byte("gitdir: "+filepath.Join(dir, "missing")+"\n"), 0o600)
			return dir
		}, "git rev-parse --show-toplevel: "},
		{"a root inside .git", func(t *testing.T) string {
			t.Helper()
			dir := t.TempDir()
			runGit(t, dir, "init", "-q")
			return filepath.Join(dir, ".git")
		}, "git rev-parse --show-toplevel: "},
		{"an empty .git inside another repository", func(t *testing.T) string {
			t.Helper()
			dir := t.TempDir()
			Index(t, dir)
			if err := os.MkdirAll(filepath.Join(dir, "sub", ".git"), 0o750); err != nil {
				t.Fatal(err)
			}
			return filepath.Join(dir, "sub")
		}, "and the nearest .git is in "},
		{"a root that does not exist", func(t *testing.T) string {
			t.Helper()
			return filepath.Join(t.TempDir(), "missing")
		}, "reading the tree at "},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := tt.root(t)
			got, err := Files(root)
			if err == nil || !strings.Contains(err.Error(), tt.want) || !strings.Contains(err.Error(), root) {
				t.Errorf("Files(%s) = %q, %v; want an error naming the root and holding %q", root, got, err, tt.want)
			}
		})
	}
}

// Not parallel: it points PATH at an empty directory for the whole process.
// A .git found with no git to read it is an error; a copy needs no git.
func TestFiles_NeedsGitOnlyWhereItFindsAGitEntry(t *testing.T) {
	if _, err := repositoryVars(); err != nil {
		t.Fatal(err)
	}
	if _, err := enclosingTop(); err != nil {
		t.Fatal(err)
	}
	repo := t.TempDir()
	Index(t, repo)
	plain := t.TempDir()
	WriteFile(t, plain, "a", []byte("a\n"), 0o600)
	t.Setenv("PATH", t.TempDir())
	if _, err := Files(repo); err == nil {
		t.Error("Files read a repository with no git on PATH")
	}
	if got, err := Files(plain); err != nil || !slices.Equal(got, []string{"a"}) {
		t.Errorf("Files of a copy with no git on PATH = %q, %v; want [a]", got, err)
	}
}

// A commit with -a runs the pre-commit hook with GIT_INDEX_FILE naming the
// commit's temporary index, and a hook can inherit GIT_DIR. The enclosing work
// tree's top is read through the index the variable names, which under a hook
// is the tree being committed; any other root is read through its own index.
//
// Not parallel: it sets each variable for the whole process, as the hook does.
func TestFiles_FollowTheRepositoryVariablesAtTheTopAlone(t *testing.T) {
	top, err := enclosingTop()
	if err != nil {
		t.Fatal(err)
	}
	if top == "" {
		t.Skip("no enclosing repository: the tree is not a git work tree")
	}
	for _, name := range []string{"GIT_INDEX_FILE", "GIT_DIR"} {
		t.Run(name, func(t *testing.T) {
			// The enclosing run's repository variables are cleared first, so name is the one set.
			kept := WithoutRepositoryVars(t, os.Environ())
			for _, kv := range os.Environ() {
				if enclosing, _, _ := strings.Cut(kv, "="); !slices.Contains(kept, kv) {
					t.Setenv(enclosing, "")
					if err := os.Unsetenv(enclosing); err != nil {
						t.Fatal(err)
					}
				}
			}
			other := t.TempDir()
			WriteFile(t, other, "held.go", []byte("package held\n"), 0o600)
			Index(t, other)
			index := filepath.Join(other, ".git", "index")
			before, err := os.ReadFile(index)
			if err != nil {
				t.Fatal(err)
			}
			if name == "GIT_INDEX_FILE" {
				t.Setenv(name, index)
			} else {
				t.Setenv(name, filepath.Join(other, ".git"))
			}

			if got, err := Files(top); err != nil || !slices.Equal(got, []string{"held.go"}) {
				t.Errorf("Files read %d files at the top under %s, the first %q, %v; want the named index's [held.go]", len(got), name, got[:min(len(got), 3)], err)
			}
			listed := t.TempDir()
			WriteFile(t, listed, "held.txt", []byte("held\n"), 0o600)
			Index(t, listed)
			if got, err := Files(listed); err != nil || !slices.Equal(got, []string{"held.txt"}) {
				t.Errorf("Files read %q, %v under %s; want the tree's own [held.txt]", got, err, name)
			}
			after, err := os.ReadFile(index)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Errorf("a read wrote the index of the repository %s names", name)
			}
		})
	}
}
