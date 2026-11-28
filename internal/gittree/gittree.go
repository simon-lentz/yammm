// Package gittree reads the files of a tree as git tracks them, gives a test a
// throwaway directory with a git index of its own, and makes the list of
// tracked files an input of go test's result cache.
//
// # The tree at a root
//
// [Files] decides on the filesystem whether a root has a tracked set. It
// resolves the root's links, then finds the nearest .git at or above it: a
// directory, or the file a linked worktree or a submodule holds. The root
// belongs to that work tree when it is the work tree's top, or when the
// repository tracks a file under it, and then its files are git's listing.
// Any other root is a copy of a tree, as the module cache holds a release, or
// a test's directory inside a repository that tracks none of it, and its files
// on disk are the tree.
//
// The reader holds git to the .git it found and refuses the rest: git must
// name the same work tree from the root (git rev-parse --show-toplevel), and
// any git failure there is an error that carries git's message. git skips an
// invalid .git, such as an empty directory, and would read the next repository
// up. An exit status alone does not tell a refusing repository from no
// repository, and git's messages are localized, so the reader reads neither to
// choose the branch. What git computes from its configuration beyond those two
// commands, such as a sparse checkout, is git's and is not modelled.
//
// # The result cache
//
// go test replays a package's last pass when its test binary, and the files
// and environment variables its tests read, are unchanged. A test that asks git
// for the tracked files reads the index through a child process, which that
// cache does not see: a pass would be replayed after a file was added to the
// index. [Files] calls [MarkTrackedFiles], and so does any test that runs git
// over an index of its own.
package gittree

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
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

// Files returns the files of the tree at root, slash-separated, relative to
// root and sorted, as [os.ReadDir] returns a directory's entries. They are
// git's tracked files when root belongs to a work tree, and the files on disk
// otherwise; the package documentation states the rule.
func Files(root string) ([]string, error) {
	MarkTrackedFiles()
	dir, err := filepath.Abs(root)
	if err == nil {
		dir, err = filepath.EvalSymlinks(dir)
	}
	if err != nil {
		return nil, fmt.Errorf("reading the tree at %s: %w", root, err)
	}
	top, err := nearestWorkTree(dir)
	if err != nil {
		return nil, fmt.Errorf("reading the tree at %s: %w", root, err)
	}
	if top != "" {
		files, tracked, err := trackedFiles(dir, top)
		if err != nil {
			return nil, fmt.Errorf("reading the tree at %s: %w", root, err)
		}
		if tracked {
			return files, nil
		}
	}
	files, err := walk(dir)
	if err != nil {
		return nil, fmt.Errorf("reading the tree at %s: %w", root, err)
	}
	return files, nil
}

// nearestWorkTree returns the nearest directory at or above dir that holds a
// .git entry, or "" when none does.
func nearestWorkTree(dir string) (string, error) {
	for {
		_, err := os.Lstat(filepath.Join(dir, ".git"))
		if err == nil {
			return dir, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("looking for .git: %w", err)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", nil
		}
		dir = parent
	}
}

// trackedFiles lists the files git tracks under dir, whose nearest .git is in
// top. The second result is false when dir is below top and the repository
// tracks nothing under it, so dir is a copy.
func trackedFiles(dir, top string) (files []string, tracked bool, err error) {
	env, err := Env(dir)
	if err != nil {
		return nil, false, err
	}
	out, err := git(dir, env, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, false, err
	}
	if named := strings.TrimSpace(string(out)); !sameDir(named, top) {
		return nil, false, fmt.Errorf("git reads the work tree at %s, and the nearest .git is in %s", named, top)
	}
	out, err = git(dir, env, "ls-files", "-z")
	if err != nil {
		return nil, false, err
	}
	for name := range strings.SplitSeq(string(out), "\x00") {
		if name != "" {
			files = append(files, name)
		}
	}
	if dir != top && len(files) == 0 {
		return nil, false, nil
	}
	slices.Sort(files)
	return files, true, nil
}

// git runs git with args in dir and returns its output. A failure carries
// git's message.
func git(dir string, env []string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(context.Background(), "git", args...)
	cmd.Dir = dir
	cmd.Env = env
	out, err := cmd.Output()
	if exit := (*exec.ExitError)(nil); errors.As(err, &exit) {
		return nil, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, bytes.TrimSpace(exit.Stderr))
	} else if err != nil {
		return nil, fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return out, nil
}

// walk lists every entry under dir that is not a directory. A copy holds the
// tracked tree alone, so no directory is skipped: git's listing holds tracked
// dot-directories such as .github too.
func walk(dir string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return fmt.Errorf("relativizing %s: %w", p, err)
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking %s: %w", dir, err)
	}
	slices.Sort(files)
	return files, nil
}
