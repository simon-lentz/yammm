package cli

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/internal/hostpath"
	"github.com/simon-lentz/yammm/snapshot"
)

// Staging and renaming needs write permission on the DIRECTORY and none on the
// file, so a read-only target would silently become writable the moment these
// paths stopped calling os.WriteFile. It does not.
func TestWriteFile_RefusesATargetItMayNotWrite(t *testing.T) {
	t.Parallel()

	if os.Geteuid() == 0 {
		t.Skip("root ignores the write bit")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "protected.txt")
	original := []byte("original\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	if err := os.Chmod(path, 0o400); err != nil {
		t.Fatalf("chmod fixture: %v", err)
	}

	err := WriteFile(path, []byte("replacement\n"))
	if err == nil {
		t.Fatal("a read-only target was overwritten")
	}
	if !errors.Is(err, fs.ErrPermission) {
		t.Errorf("error = %v, want a permission failure", err)
	}
	if got, _ := os.ReadFile(path); string(got) != string(original) {
		t.Errorf("the file changed: %q", got)
	}
	assertNoDebris(t, dir, path)
}

// TestStagingName_IsInvisibleToADirectoryScan pins that the staging file an
// interrupted write leaves behind is invisible to a directory scan. The shared
// .tmp suffix hides it, and a name that ends in .ys is the contrast.
func TestStagingName_IsInvisibleToADirectoryScan(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	stage, err := hostpath.Stage(filepath.Join(dir, "out.ys"), NewFileMode, stagingName)
	if err != nil {
		t.Fatalf("stage: %v", err)
	}
	defer stage.Discard()
	stage.File.Close() //nolint:gosec // nothing is written to it

	if !strings.HasSuffix(stage.File.Name(), snapshot.TmpSuffix) {
		t.Errorf("staging name %q does not end in %q", filepath.Base(stage.File.Name()), snapshot.TmpSuffix)
	}

	if entries := scanEntries(t, dir); len(entries) != 0 {
		t.Errorf("a directory scan reported the staging file: %v", entries)
	}

	// A staging name that ends in .ys is reported, so the suffix is what hides it.
	old := filepath.Join(dir, ".yammm-save-999999.ys")
	if err := os.WriteFile(old, []byte("{}"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	if entries := scanEntries(t, dir); len(entries) != 1 {
		t.Errorf("the old staging name should be reported by a scan; got %d entries", len(entries))
	}
}

func TestWriteFile_ModePolicy(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip(noModeBitsOnWindows)
	}

	t.Run("a new file is owner-only", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		path := filepath.Join(dir, "new.txt")
		if err := WriteFile(path, []byte("x")); err != nil {
			t.Fatalf("write: %v", err)
		}
		assertPerm(t, path, NewFileMode)
		assertNoDebris(t, dir, path)
	})

	t.Run("an existing file keeps its own mode", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		path := filepath.Join(dir, "existing.txt")
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
		// 0o640 on purpose: the property under test is that a mode WIDER
		// than the default survives the write untouched.
		if err := os.Chmod(path, 0o640); err != nil { //nolint:gosec // see above
			t.Fatalf("chmod fixture: %v", err)
		}
		if err := WriteFile(path, []byte("y")); err != nil {
			t.Fatalf("write: %v", err)
		}
		assertPerm(t, path, 0o640)
	})

	t.Run("both spellings of one path give one answer", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		direct := filepath.Join(dir, "same.txt")
		spelled := filepath.Join(dir, "sub", "..", "same.txt")
		if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o750); err != nil {
			t.Fatalf("mkdir: %v", err)
		}

		if err := WriteFile(direct, []byte("a")); err != nil {
			t.Fatalf("write: %v", err)
		}
		assertPerm(t, direct, NewFileMode)
		if err := WriteFile(spelled, []byte("b")); err != nil {
			t.Fatalf("write: %v", err)
		}
		assertPerm(t, direct, NewFileMode)
	})
}

// A write that cannot succeed puts nothing in the operator's directory — no
// output and no staging file.
//
// A directory standing where the file belongs is refused by the permission
// check before anything is staged. [TestWriteFile_AFailedWriteLeavesNoStagingFile]
// holds the cleanup after staging.
func TestWriteFile_ADirectoryTargetIsRefusedAndNothingIsWritten(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "blocked")
	if err := os.Mkdir(path, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	if err := WriteFile(path, []byte("x")); err == nil {
		t.Fatal("writing over a directory reported success")
	}
	assertNoDebris(t, dir, path)
}

// A refused rename names neither the staging file nor the target's resolved path,
// and removes the staging file. A directory at the target refuses the rename on
// every host and leaves the directory holding the staging file writable.
func TestReplace_ARefusedRenameLeavesNoStagingFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.Mkdir(target, 0o750); err != nil {
		t.Fatal(err)
	}
	err := replace(writeTarget{path: target, mode: NewFileMode}, []byte(targetPayload))
	if err == nil {
		t.Fatal("a rename over a directory reported success")
	}
	if text := err.Error(); !strings.HasPrefix(text, "rename into place: ") || strings.Contains(text, dir) {
		t.Errorf("error %q must state the rename and name no path", text)
	}
	assertNoDebris(t, dir, "target")
}

func assertPerm(t *testing.T, path string, want fs.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Errorf("%s has mode %v, want %v", filepath.Base(path), got, want)
	}
}

// assertNoDebris reports any entry in dir that is not one of the expected paths.
func assertNoDebris(t *testing.T, dir string, expected ...string) {
	t.Helper()
	keep := make(map[string]struct{}, len(expected))
	for _, p := range expected {
		keep[filepath.Base(p)] = struct{}{}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	for _, e := range entries {
		if _, ok := keep[e.Name()]; !ok {
			t.Errorf("left behind: %q", e.Name())
		}
	}
}

// scanEntries collects every entry snapshot.ScanDir yields for dir.
func scanEntries(t *testing.T, dir string) []snapshot.ScanEntry {
	t.Helper()
	var entries []snapshot.ScanEntry
	for entry, err := range snapshot.ScanDir(t.Context(), dir) {
		if err != nil {
			t.Fatalf("scan: %v", err)
		}
		entries = append(entries, entry)
	}
	return entries
}

// TestWriteFile_ReplacesAFileReachedThroughALongLink holds a replacement to
// the files the kernel writes for the path: darwin counts a link's body and the
// rest of the path against MAXPATHLEN, so through a link of 1,014 bytes
// "/x.dat" fits and a staging name spelled through the link does not.
func TestWriteFile_ReplacesAFileReachedThroughALongLink(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("the link needs a Unix kernel's MAXPATHLEN arithmetic")
	}
	dir := t.TempDir()
	link := filepath.Join(dir, "L")
	if err := os.Symlink(strings.Repeat("./", 507), link); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(link, "x.dat")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatalf("the target itself must be writable: %v", err)
	}
	for range 5 {
		if err := WriteFile(path, []byte("new")); err != nil {
			t.Fatalf("WriteFile through a 1,014-byte link = %v", err)
		}
	}
	if got, err := os.ReadFile(filepath.Join(dir, "x.dat")); err != nil || string(got) != "new" {
		t.Errorf("the file the link reaches holds %q, %v; want the written bytes", got, err)
	}
	assertNoDebris(t, dir, "L", "x.dat")
}

// TestReplace_StagesAtTheModeTheWriteEndsWith holds the staging file to the
// target's mode from its creation, before any byte is written: a new file's
// staging copy is owner-only, as the file it becomes.
func TestReplace_StagesAtTheModeTheWriteEndsWith(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("Windows keeps no Unix permission bits")
	}
	stage, err := writeTarget{path: filepath.Join(t.TempDir(), "out.ys"), mode: NewFileMode}.stage()
	if err != nil {
		t.Fatal(err)
	}
	info, err := stage.File.Stat()
	stage.Discard()
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got&^NewFileMode != 0 {
		t.Errorf("the staging file was created with mode %v; want no bit beyond %v", got, NewFileMode)
	}
}
