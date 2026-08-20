package snapshot_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/simon-lentz/yammm/snapshot"
)

// Every entry the scan yields carries the file's modification time, so a
// consumer filtering by age does not stat each path a second time.
func TestScanDir_PopulatesModTime(t *testing.T) {
	dir := t.TempDir()
	seedValidYS(t, filepath.Join(dir, "a.ys"))
	seedValidYS(t, filepath.Join(dir, "b.ys"))

	var seen int
	for entry, err := range snapshot.ScanDir(t.Context(), dir) {
		if err != nil {
			t.Fatalf("ScanDir: %v", err)
		}
		seen++
		info, statErr := os.Stat(entry.Path)
		if statErr != nil {
			t.Fatalf("stat %s: %v", entry.Path, statErr)
		}
		if info.ModTime().IsZero() {
			t.Fatalf("fixture %s has a zero mtime, so this asserts nothing", entry.Path)
		}
		if !entry.ModTime.Equal(info.ModTime()) {
			t.Errorf("%s: ScanEntry.ModTime = %v, want %v", entry.Name, entry.ModTime, info.ModTime())
		}
	}
	if seen != 2 {
		t.Errorf("scanned %d entries, want 2", seen)
	}
}

// The mtime is why the field sits on the entry rather than only on the header:
// a corrupt file has no header and still has a modification time.
func TestScanDir_PopulatesModTimeForACorruptEntry(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "corrupt.ys")
	seedCorruptYS(t, path)

	var seen int
	for entry, err := range snapshot.ScanDir(t.Context(), dir) {
		if err != nil {
			t.Fatalf("ScanDir: %v", err)
		}
		seen++
		if !entry.Result.HasErrors() {
			t.Fatal("the corrupt fixture parsed cleanly, so this asserts nothing")
		}
		if entry.Header != nil {
			t.Error("Header is non-nil on an error-severity entry")
		}
		info, statErr := os.Stat(path)
		if statErr != nil {
			t.Fatalf("stat: %v", statErr)
		}
		if !entry.ModTime.Equal(info.ModTime()) {
			t.Errorf("ScanEntry.ModTime = %v, want %v", entry.ModTime, info.ModTime())
		}
	}
	if seen != 1 {
		t.Errorf("scanned %d entries, want 1", seen)
	}
}

// f.Stat answers for the symlink's target, which is the file that was actually
// read. A link created now over a target backdated a year makes lstat and stat
// disagree, so an implementation reading the DirEntry fails here.
func TestScanDir_ModTimeFollowsASymlinkToItsTarget(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.dat")
	seedValidYS(t, target)

	backdated := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := os.Chtimes(target, backdated, backdated); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "link.ys")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	var seen int
	for entry, err := range snapshot.ScanDir(t.Context(), dir) {
		if err != nil {
			t.Fatalf("ScanDir: %v", err)
		}
		seen++
		if entry.Name != "link.ys" {
			t.Errorf("scanned %q, want link.ys", entry.Name)
		}
		if !entry.ModTime.Equal(backdated) {
			t.Errorf("ModTime = %v, want the target's %v", entry.ModTime, backdated)
		}
	}
	if seen != 1 {
		t.Errorf("scanned %d entries, want 1", seen)
	}
}

// An unopenable file never reaches the stat, and the zero Time is what says so.
func TestScanDir_BrokenSymlinkLeavesModTimeZero(t *testing.T) {
	dir := t.TempDir()
	if err := os.Symlink(filepath.Join(dir, "nothing-here"), filepath.Join(dir, "broken.ys")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	var seen int
	for entry, err := range snapshot.ScanDir(t.Context(), dir) {
		if err != nil {
			t.Fatalf("ScanDir: %v", err)
		}
		seen++
		if !entry.Result.HasErrors() {
			t.Error("broken.ys reported no error")
		}
		if !entry.ModTime.IsZero() {
			t.Errorf("ModTime = %v on an unopenable file, want the zero Time", entry.ModTime)
		}
	}
	if seen != 1 {
		t.Errorf("scanned %d entries, want 1", seen)
	}
}
