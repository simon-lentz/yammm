package yammm_test

import (
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
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
