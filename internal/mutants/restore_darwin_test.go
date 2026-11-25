package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/simon-lentz/yammm/internal/gittree"
)

// A restore that cannot remove several paths, or give several files or
// directories back their mode, names the first in path order, run after run.
// The immutable flag makes each refusal here: darwin keeps it whatever the mode.
func TestTree_RestoreNamesTheFirstPathItCannotPutBack(t *testing.T) {
	t.Parallel()
	immutable := func(t *testing.T, path string) {
		t.Helper()
		if err := unix.Chflags(path, unix.UF_IMMUTABLE); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = unix.Chflags(path, 0) })
	}
	moved := func(mode os.FileMode, names ...string) func(t *testing.T, tree *tree) {
		return func(t *testing.T, tree *tree) {
			t.Helper()
			for _, name := range names {
				path := filepath.Join(tree.dir, filepath.FromSlash(name))
				if err := os.Chmod(path, mode); err != nil {
					t.Fatal(err)
				}
				immutable(t, path)
			}
		}
	}
	rows := []struct {
		name  string
		spoil func(t *testing.T, tree *tree)
		first string
		cause string // what the failure says before the first path
	}{
		{"paths the copy was not given", func(t *testing.T, tree *tree) {
			t.Helper()
			for _, name := range []string{"z.txt", "m/c.txt", "b.txt", "a.txt"} {
				gittree.WriteFile(t, tree.dir, name, []byte("left\n"), 0o600)
				immutable(t, filepath.Join(tree.dir, filepath.FromSlash(name)))
			}
		}, "a.txt", "remove "},
		{"held files with another mode", moved(0o400, "untracked.txt", "m/m.go", "go.mod"), "go.mod", "giving back its modes: chmod "},
		{"held directories with another mode", moved(0o700, "scripts", "m/sub", "m"), "m", "giving back its modes: chmod "},
	}
	for _, tt := range rows {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tree := copied(t)
			tt.spoil(t, tree)
			want := tree.dir + " cannot be restored: " + tt.cause + filepath.Join(tree.dir, filepath.FromSlash(tt.first)) + ": "
			for range 20 {
				_, err := tree.restore(t.Context())
				if err == nil || !strings.HasPrefix(err.Error(), want) {
					t.Fatalf("restore = %v, want a failure that starts %q", err, want)
				}
			}
		})
	}
}

// A restore that cannot list a held directory, as an access list can deny,
// says it cannot read the copy, and names the directory once, in the cause.
func TestTree_RestoreSaysItCannotReadTheCopy(t *testing.T) {
	t.Parallel()
	tree := copied(t)
	held := filepath.Join(tree.dir, "m")
	if out, err := exec.CommandContext(t.Context(), "chmod", "+a", "everyone deny list", held).CombinedOutput(); err != nil {
		t.Fatalf("chmod +a: %v: %s", err, out)
	}
	t.Cleanup(func() { _ = exec.Command("chmod", "-N", held).Run() }) //nolint:noctx // the test's context is done at cleanup
	_, err := tree.restore(t.Context())
	want := tree.dir + " cannot be restored: reading the copy: open " + held + ": "
	if err == nil || !strings.HasPrefix(err.Error(), want) || strings.Count(err.Error(), tree.dir) != 2 {
		t.Errorf("restore = %v, want a failure that starts %q and names the copy twice", err, want)
	}
}

// A repository restore cannot read, here a directory an access list denies a
// list of, is made again; when it cannot be removed either, restore fails.
func TestTree_RestoreFailsOnARepositoryItCannotReadOrRemove(t *testing.T) {
	t.Parallel()
	tree := copied(t)
	tags := filepath.Join(tree.dir, ".git", "refs", "tags")
	gittree.WriteFile(t, tree.dir, ".git/refs/tags/hidden", []byte("left\n"), 0o600)
	if out, err := exec.CommandContext(t.Context(), "chmod", "+a", "everyone deny list", tags).CombinedOutput(); err != nil {
		t.Fatalf("chmod +a: %v: %s", err, out)
	}
	t.Cleanup(func() { _ = exec.Command("chmod", "-N", tags).Run() }) //nolint:noctx // the test's context is done at cleanup
	_, err := tree.restore(t.Context())
	want := tree.dir + " cannot be restored: remove " + filepath.Join(tree.dir, ".git") + ": "
	if err == nil || !strings.HasPrefix(err.Error(), want) {
		t.Errorf("restore = %v, want a failure that starts %q", err, want)
	}
}
