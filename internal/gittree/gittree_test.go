package gittree

import (
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
	if out, err := exec.CommandContext(t.Context(), "git", "-C", dir, "rev-parse", "--verify", "-q", "HEAD").Output(); err == nil {
		t.Errorf("Index committed: HEAD is %s", strings.TrimSpace(string(out)))
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
