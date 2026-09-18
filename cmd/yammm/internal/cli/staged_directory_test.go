package cli

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// A refused set leaves the directory as it was found, which for a directory
// that did not exist means it still does not: every level NewStagedFiles
// created is removed, and the level that already existed is kept.
func TestStagedFiles_RollbackRemovesTheDirectoriesItCreated(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dir := filepath.Join(root, "new", "deeper")

	s, err := NewStagedFiles(dir)
	if err != nil {
		t.Fatalf("NewStagedFiles: %v", err)
	}
	w, err := s.Create("a.csv")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := w.Write([]byte("x\n")); err != nil {
		t.Fatal(err)
	}
	s.Rollback()

	if _, err := os.Stat(filepath.Join(root, "new")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("a rolled-back set left %s behind (stat: %v)", filepath.Join(root, "new"), err)
	}
	if _, err := os.Stat(root); err != nil {
		t.Errorf("the directory that already existed is gone: %v", err)
	}
}

// A directory that holds a file after the rollback is not removed, whoever
// put the file there.
func TestStagedFiles_RollbackKeepsACreatedDirectoryThatIsNotEmpty(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "new")
	s, err := NewStagedFiles(dir)
	if err != nil {
		t.Fatalf("NewStagedFiles: %v", err)
	}
	other := filepath.Join(dir, "operator.txt")
	if err := os.WriteFile(other, []byte("mine\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s.Rollback()
	if _, err := os.Stat(other); err != nil {
		t.Errorf("a file another writer put in the directory is gone: %v", err)
	}
}

// A committed set is in place, so its directory stays after the deferred
// Rollback every caller runs, even when the set holds no file.
func TestStagedFiles_CommitKeepsTheDirectoryItCreated(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "new")
	s, err := NewStagedFiles(dir)
	if err != nil {
		t.Fatalf("NewStagedFiles: %v", err)
	}
	if err := s.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	s.Rollback()
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Errorf("a committed set's directory is gone (stat: %v)", err)
	}
}
