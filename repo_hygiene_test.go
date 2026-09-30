package yammm_test

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/mod/zip"
)

// TestRepository_TracksNoModuleRootMarker keeps this repository free of a
// committed module-root marker outside a fixture directory.
//
// A marker at the repository root would widen every fixture load's import
// sandbox to the whole checkout and move the root that dozens of tests assert
// — silently, because a wider sandbox fails nothing. None of the fixtures
// needs one: none uses a repository-relative import.
//
// The tracked tree is the subject, never a filesystem walk: a walk sees
// ignored build output and vendored directories that are not part of the
// repository.
func TestRepository_TracksNoModuleRootMarker(t *testing.T) {
	t.Parallel()

	out, err := exec.CommandContext(t.Context(), "git", "ls-files", "-z").Output()
	if err != nil {
		t.Skipf("git ls-files unavailable: %v", err)
	}

	for path := range strings.SplitSeq(string(out), "\x00") {
		if path == "" || filepath.Base(path) != "yammm.mod" {
			continue
		}
		if slices.Contains(strings.Split(filepath.ToSlash(path), "/"), "testdata") {
			continue
		}
		t.Errorf("%s is tracked: a module-root marker outside testdata widens every fixture load's sandbox", path)
	}
}

// TestRepository_ModuleZipHoldsEveryTrackedFile keeps the module cache's copy
// of a release whole. The zip leaves out a directory holding another go.mod, a
// vendored package and any file that is not regular, and a test reading such a
// file then fails from the module cache, or passes with nothing to read. The
// proxy's own implementation, golang.org/x/mod/zip, judges the tracked tree.
func TestRepository_ModuleZipHoldsEveryTrackedFile(t *testing.T) {
	t.Parallel()

	out, err := exec.CommandContext(t.Context(), "git", "ls-files", "-z").Output()
	if err != nil {
		t.Skipf("git ls-files unavailable: %v", err)
	}

	var files []zip.File
	for path := range strings.SplitSeq(string(out), "\x00") {
		if path == "" {
			continue
		}
		if _, err := os.Lstat(filepath.FromSlash(path)); errors.Is(err, fs.ErrNotExist) {
			// The index still lists a file the working tree has deleted.
			continue
		}
		files = append(files, trackedFile(path))
	}
	checked, err := zip.CheckFiles(files)
	for _, f := range checked.Omitted {
		t.Errorf("%s is tracked, and the module zip leaves it out: %v", f.Path, f.Err)
	}
	for _, f := range checked.Invalid {
		t.Errorf("%s is tracked, and the module zip refuses it: %v", f.Path, f.Err)
	}
	if err != nil && len(checked.Omitted) == 0 && len(checked.Invalid) == 0 {
		t.Errorf("the tracked tree is not a valid module zip: %v", err)
	}
	if len(checked.Valid) == 0 {
		t.Error("the module zip would hold no tracked file")
	}
}

// trackedFile is a tracked path, relative to the repository root, as a
// [zip.File].
type trackedFile string

func (f trackedFile) Path() string { return string(f) }

func (f trackedFile) Lstat() (os.FileInfo, error) { return os.Lstat(filepath.FromSlash(string(f))) }

func (f trackedFile) Open() (io.ReadCloser, error) { return os.Open(filepath.FromSlash(string(f))) }
