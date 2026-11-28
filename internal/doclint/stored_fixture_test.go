package doclint_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// storedModuleFile is the name a fixture under testdata gives each of its
// go.mod files. A module zip leaves out every directory below the module root
// that holds a go.mod, so a fixture stored with its own would be missing from
// the module cache, and every test reading it would fail there or pass with
// nothing to read.
const storedModuleFile = "go.mod.fixture"

// materialize copies the fixture module testdata/name into the test's temporary
// directory, restores each stored module file as go.mod, and returns the copy's
// root. The copy holds no repository, so the gate reads its files on disk.
func materialize(t *testing.T, name string) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), name)
	if err := os.CopyFS(root, os.DirFS(filepath.Join("testdata", name))); err != nil {
		t.Fatalf("copying fixture %s: %v", name, err)
	}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Name() != storedModuleFile {
			return err
		}
		return os.Rename(p, filepath.Join(filepath.Dir(p), "go.mod"))
	})
	if err != nil {
		t.Fatalf("restoring fixture %s's module files: %v", name, err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("fixture %s stores no %s at its root, so the gate has no module to read: %v", name, storedModuleFile, err)
	}
	return root
}
