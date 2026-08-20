package snapshot

import (
	"os"
	"path/filepath"
	"testing"
)

// writeMinimalYS writes an openable, stat-able .ys. These tests observe opens
// and stats, never header content, so the bytes need not parse.
func writeMinimalYS(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// The filter must decide BEFORE the open, and no black-box test can prove it:
// a filter applied to the yield instead of the open produces an identical entry
// set. Observing what was opened is the only probe that kills that mutation,
// which is why withScanOpen exists unexported rather than not at all.
func TestScanDirWith_FilterRunsBeforeTheOpen(t *testing.T) {
	dir := t.TempDir()
	writeMinimalYS(t, filepath.Join(dir, "keep.ys"))
	writeMinimalYS(t, filepath.Join(dir, "drop.ys"))

	var opened []string
	spy := func(path string) (*os.File, error) {
		opened = append(opened, filepath.Base(path))
		return os.Open(path)
	}

	for range ScanDirWith(t.Context(), dir,
		withScanOpen(spy),
		WithScanFilter(func(c ScanCandidate) bool { return c.Name != "drop.ys" })) {
	}

	if len(opened) != 1 || opened[0] != "keep.ys" {
		t.Errorf("opened %v, want only [keep.ys]", opened)
	}

	opened = nil
	for range ScanDirWith(t.Context(), dir, withScanOpen(spy)) {
	}
	if len(opened) != 2 {
		t.Errorf("unfiltered control opened %v, want both files", opened)
	}
}

// One stat per candidate however often the answer is asked for.
func TestStatOnce_ResolvesOnce(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.ys")
	writeMinimalYS(t, path)

	st := &statOnce{path: path}
	first, err := st.get()
	if err != nil {
		t.Fatalf("first get: %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove: %v", err)
	}
	second, err := st.get()
	if err != nil {
		t.Errorf("second get returned an error after the file was removed: %v", err)
	}
	if second != first {
		t.Error("second get re-stat'd rather than reusing the first answer")
	}
}

// isRegular decides a regular dirent without a syscall and pays for a symlink's
// stat, which is what makes a filter's Info free on links and absent elsewhere.
func TestIsRegular_ResolvesOnlyForASymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.dat")
	writeMinimalYS(t, target)
	if err := os.Symlink(target, filepath.Join(dir, "link.ys")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	dirents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}

	var checked int
	for _, d := range dirents {
		st := &statOnce{path: filepath.Join(dir, d.Name())}
		isRegular(d, st)
		switch d.Name() {
		case "target.dat":
			checked++
			if st.done {
				t.Error("a regular dirent cost a stat")
			}
		case "link.ys":
			checked++
			if !st.done {
				t.Error("a symlink dirent did not resolve its stat")
			}
		}
	}
	if checked != 2 {
		t.Fatalf("checked %d dirents, want 2", checked)
	}
}
