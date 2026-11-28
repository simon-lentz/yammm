// Package gittree gives a test a throwaway directory with a git index of its
// own, and makes the list of tracked files an input of go test's result cache.
//
// go test replays a package's last pass when its test binary, and the files
// and environment variables its tests read, are unchanged. A test that asks git
// for the tracked files reads the index through a child process, which that
// cache does not see: a pass would be replayed after a file was added to the
// index. Such a test calls [MarkTrackedFiles].
package gittree

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
)

// repositoryVars returns the variables `git rev-parse --local-env-vars` names,
// those local to one repository, GIT_DIR and GIT_INDEX_FILE among them. A
// commit with -a gives its pre-commit hook GIT_INDEX_FILE, and a fixture's git
// that inherits it locks, and can write, the real repository's index.
var repositoryVars = sync.OnceValues(localEnvVars)

func localEnvVars() ([]string, error) {
	out, err := exec.CommandContext(context.Background(), "git", "rev-parse", "--local-env-vars").Output()
	if err != nil {
		return nil, fmt.Errorf("git rev-parse --local-env-vars: %w", err)
	}
	return strings.Fields(string(out)), nil
}

// WithoutRepositoryVars returns env without the variables repositoryVars names.
// It filters env in place.
func WithoutRepositoryVars(t *testing.T, env []string) []string {
	t.Helper()
	vars, err := repositoryVars()
	if err != nil {
		t.Fatal(err)
	}
	return without(env, vars)
}

func without(env, vars []string) []string {
	return slices.DeleteFunc(env, func(kv string) bool {
		name, _, _ := strings.Cut(kv, "=")
		return slices.Contains(vars, name)
	})
}

// Env returns the environment for a git command that reads root. Root keeps
// the repository variables only when it is the top of the work tree this
// process runs in, where a hook's variables name the tree being committed. Any
// other root has a repository of its own or none, and an absolute GIT_DIR, as
// a linked worktree's hook exports, points git at the enclosing repository
// wherever it runs.
func Env(root string) ([]string, error) {
	vars, err := repositoryVars()
	if err != nil {
		return nil, err
	}
	env := os.Environ()
	top, err := enclosingTop()
	if err != nil {
		return nil, err
	}
	if top != "" && sameDir(root, top) {
		return env, nil
	}
	return without(env, vars), nil
}

// enclosingTop is the top of the work tree git finds from this process's
// working directory with the repository variables removed, or "" outside one.
var enclosingTop = sync.OnceValues(func() (string, error) {
	vars, err := repositoryVars()
	if err != nil {
		return "", err
	}
	cmd := exec.CommandContext(context.Background(), "git", "rev-parse", "--show-toplevel")
	cmd.Env = without(os.Environ(), vars)
	out, err := cmd.Output()
	if exit := (*exec.ExitError)(nil); errors.As(err, &exit) {
		return "", nil // outside a work tree there is no enclosing repository
	} else if err != nil {
		return "", fmt.Errorf("git rev-parse --show-toplevel: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
})

func sameDir(a, b string) bool {
	ai, err := os.Stat(a)
	if err != nil {
		return false
	}
	bi, err := os.Stat(b)
	return err == nil && os.SameFile(ai, bi)
}

// WriteFile writes content with mode to rel, a slash-separated path under dir,
// and creates the directories above it.
func WriteFile(t *testing.T, dir, rel string, content []byte, mode os.FileMode) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, mode); err != nil {
		t.Fatal(err)
	}
}

// Index creates dir's repository and adds paths to its index, or every file
// when none are named. Nothing is committed: git ls-files reads the index.
func Index(t *testing.T, dir string, paths ...string) {
	t.Helper()
	if len(paths) == 0 {
		paths = []string{"-A"}
	}
	for _, args := range [][]string{{"init", "-q"}, append([]string{"add"}, paths...)} {
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = dir
		cmd.Env = WithoutRepositoryVars(t, os.Environ())
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
}

// TrackedFilesVar names the variable scripts/committest.sh sets to a digest of
// the tracked file names.
const TrackedFilesVar = "YAMMM_TRACKED_FILES"

// MarkTrackedFiles makes the tracked file list an input of go test's result
// cache, by reading [TrackedFilesVar]. In a run that sets no such variable, as
// a plain go test, that input never changes, and go test may then replay a pass
// after a file is added to the index.
func MarkTrackedFiles() {
	_ = os.Getenv(TrackedFilesVar)
}
