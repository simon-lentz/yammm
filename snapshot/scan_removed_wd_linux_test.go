package snapshot_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// On Linux a removed working directory with PWD unset has no name, so a dir
// that climbs out of it cannot be made absolute: the scan fails with the
// cause, under the dir as passed, and lists nothing.
func TestScanDir_AClimbFromARemovedWorkingDirectoryFailsWithItsCause(t *testing.T) {
	wd := filepath.Join(t.TempDir(), "gone")
	if err := os.Mkdir(wd, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(wd)
	t.Setenv("PWD", "")
	if err := os.Remove(wd); err != nil {
		t.Fatal(err)
	}
	entries, err := collectScanDir(t.Context(), "../x")
	if len(entries) != 0 {
		t.Errorf("listed %d entries, want none", len(entries))
	}
	pathErr, ok := errors.AsType[*fs.PathError](err)
	if !ok {
		t.Fatalf("err = %v, want an *fs.PathError", err)
	}
	if pathErr.Op != "open" || pathErr.Path != "../x" {
		t.Errorf("err names op %q and path %q, want open and ../x", pathErr.Op, pathErr.Path)
	}
	if !errors.Is(err, syscall.ENOENT) {
		t.Errorf("err = %v, want ENOENT as its cause", err)
	}
	if _, inner := errors.AsType[*fs.PathError](pathErr.Err); inner {
		t.Errorf("the cause wraps a second path: %v", err)
	}
}
