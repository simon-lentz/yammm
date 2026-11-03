package cli

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

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
	staged, err := os.CreateTemp(dir, stagingPattern)
	if err != nil {
		t.Fatalf("stage: %v", err)
	}
	staged.Close() //nolint:gosec // nothing is written to it

	if !strings.HasSuffix(staged.Name(), snapshot.TmpSuffix) {
		t.Errorf("staging name %q does not end in %q", filepath.Base(staged.Name()), snapshot.TmpSuffix)
	}

	entries, result := snapshot.ScanDirSlice(context.Background(), dir)
	if err := result.Err(); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("a directory scan reported the staging file: %v", entries)
	}

	// A staging name that ends in .ys is reported, so the suffix is what hides it.
	old := filepath.Join(dir, ".yammm-save-999999.ys")
	if err := os.WriteFile(old, []byte("{}"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	entries, _ = snapshot.ScanDirSlice(context.Background(), dir)
	if len(entries) != 1 {
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
// check before anything is staged. The cleanup that runs when a write fails
// AFTER staging needs an I/O failure this platform cannot be made to produce,
// and is defence rather than a path a test reaches.
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
