package snapshot_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/simon-lentz/yammm/snapshot"
)

// Every entry the scan yields carries the file's size on disk, and a parsed
// header repeats it. Consumers accounting for disk space would otherwise stat
// each path a second time.
func TestScanDir_PopulatesFileSize(t *testing.T) {
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
		if info.Size() == 0 {
			t.Fatalf("fixture %s is empty, so this asserts nothing", entry.Path)
		}
		if entry.FileSize != info.Size() {
			t.Errorf("%s: ScanEntry.FileSize = %d, want %d", entry.Name, entry.FileSize, info.Size())
		}
		if entry.Header == nil {
			t.Fatalf("%s: header is nil on a valid fixture", entry.Name)
		}
		if entry.Header.FileSize != info.Size() {
			t.Errorf("%s: HeaderInfo.FileSize = %d, want %d", entry.Name, entry.Header.FileSize, info.Size())
		}
	}
	if seen != 2 {
		t.Errorf("scanned %d entries, want 2", seen)
	}
}

// The size is why the field sits on the entry rather than only on the header: a
// corrupt file has no header and still occupies disk.
func TestScanDir_PopulatesFileSizeForACorruptEntry(t *testing.T) {
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
		if entry.FileSize != info.Size() {
			t.Errorf("ScanEntry.FileSize = %d, want %d", entry.FileSize, info.Size())
		}
	}
	if seen != 1 {
		t.Errorf("scanned %d entries, want 1", seen)
	}
}

// A size is not knowable from an io.Reader, and the reader form still says so.
func TestHeaderOnlyRead_LeavesFileSizeZero(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.ys")
	seedValidYS(t, path)

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()

	header, result := snapshot.HeaderOnlyRead(t.Context(), f)
	if err := result.Err(); err != nil {
		t.Fatalf("HeaderOnlyRead: %v", err)
	}
	if header.FileSize != 0 {
		t.Errorf("HeaderOnlyRead FileSize = %d, want 0", header.FileSize)
	}
}

// The walk admits regular files rather than merely excluding directories. A
// symlink to a directory is the portable stand-in for the class: os.ReadDir
// reports it as a symlink, so the old IsDir-only filter let it through to
// os.Open, which succeeded on the directory and failed inside the header read.
func TestScanDir_SkipsASymlinkToADirectory(t *testing.T) {
	dir := t.TempDir()
	seedValidYS(t, filepath.Join(dir, "real.ys"))

	target := filepath.Join(dir, "subdir")
	if err := os.Mkdir(target, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "link.ys")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	var names []string
	for entry, err := range snapshot.ScanDir(t.Context(), dir) {
		if err != nil {
			t.Fatalf("ScanDir: %v", err)
		}
		names = append(names, entry.Name)
	}
	if len(names) != 1 || names[0] != "real.ys" {
		t.Errorf("scanned %v, want only [real.ys]", names)
	}
}

// The other half of the same rule: a symlink whose target is a regular file is
// followed and scanned, which is what the documented contract promises.
func TestScanDir_FollowsASymlinkToARegularFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.dat")
	seedValidYS(t, target)
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
		if entry.Result.HasErrors() {
			t.Errorf("link.ys reported errors: %v", entry.Result.Err())
		}
		// f.Stat answers for the symlink's target, which is the file that was
		// actually read.
		info, err := os.Stat(target)
		if err != nil {
			t.Fatalf("stat: %v", err)
		}
		if entry.FileSize != info.Size() {
			t.Errorf("FileSize = %d, want the target's %d", entry.FileSize, info.Size())
		}
	}
	if seen != 1 {
		t.Errorf("scanned %d entries, want 1", seen)
	}
}

// A broken symlink keeps its documented per-file E_SNAPSHOT_IO entry rather
// than vanishing from the walk.
func TestScanDir_BrokenSymlinkStillYieldsAnIOEntry(t *testing.T) {
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
			t.Errorf("broken.ys reported no error")
		}
		if entry.FileSize != 0 {
			t.Errorf("FileSize = %d on an unopenable file, want 0", entry.FileSize)
		}
	}
	if seen != 1 {
		t.Errorf("scanned %d entries, want 1", seen)
	}
}
