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

func TestWriteFileSet_EveryFileArrivesOrNoneDoes(t *testing.T) {
	t.Parallel()
	ab := []NamedFile{{Name: "a.csv", Data: []byte("a.csv")}, {Name: "b.csv", Data: []byte("b.csv")}}

	t.Run("every file is put in place", func(t *testing.T) {
		t.Parallel()
		dir := filepath.Join(t.TempDir(), "out")
		if err := WriteFileSet(dir, ab); err != nil {
			t.Fatalf("write set: %v", err)
		}
		for _, nf := range ab {
			path := filepath.Join(dir, nf.Name)
			if got, err := os.ReadFile(path); err != nil || string(got) != string(nf.Data) {
				t.Errorf("%s holds %q (%v), want %q", nf.Name, got, err, nf.Data)
			}
			if runtime.GOOS != "windows" {
				assertPerm(t, path, NewFileMode)
			}
		}
		assertNoDebris(t, dir, filepath.Join(dir, "a.csv"), filepath.Join(dir, "b.csv"))
	})

	t.Run("one blocked target leaves the directory as it was", func(t *testing.T) {
		t.Parallel()
		dir := filepath.Join(t.TempDir(), "out")
		blocker := filepath.Join(dir, "b.csv")
		if err := os.MkdirAll(blocker, 0o750); err != nil {
			t.Fatalf("mkdir blocker: %v", err)
		}
		if err := WriteFileSet(dir, ab); err == nil {
			t.Fatal("a blocked target reported success")
		}
		// a.csv must NOT have been renamed into place: a partial set reads as
		// a complete export with one type quietly missing.
		if _, err := os.Stat(filepath.Join(dir, "a.csv")); err == nil {
			t.Error("a.csv was left behind; the set is partial and nothing says so")
		}
		assertNoDebris(t, dir, blocker)
	})

	t.Run("a directory that appears after staging is refused before any rename", func(t *testing.T) {
		t.Parallel()
		dir := filepath.Join(t.TempDir(), "out")
		set, err := stageFileSet(dir, ab)
		if err != nil {
			t.Fatalf("stage: %v", err)
		}
		blocker := filepath.Join(dir, "b.csv")
		if err := os.Mkdir(blocker, 0o750); err != nil {
			t.Fatalf("mkdir blocker: %v", err)
		}
		err = set.commit()
		if err == nil {
			t.Fatal("a directory standing where b.csv belongs reported success")
		}
		if !strings.Contains(err.Error(), "b.csv") || strings.Contains(err.Error(), snapshot.TmpSuffix) {
			t.Errorf("error %q must name b.csv and no staging file", err)
		}
		if _, err := os.Stat(filepath.Join(dir, "a.csv")); err == nil {
			t.Error("a.csv was renamed into place; the set is partial and nothing says so")
		}
		assertNoDebris(t, dir, blocker)
	})

	t.Run("a refused rename names the file, not its staging name", func(t *testing.T) {
		t.Parallel()
		if runtime.GOOS == "windows" {
			t.Skip(noDirPermissionsOnWindows)
		}
		if os.Geteuid() == 0 {
			t.Skip("root ignores the write bit")
		}
		base := unsealedTempDir(t)
		dir := filepath.Join(base, "out")
		set, err := stageFileSet(dir, ab[:1])
		if err != nil {
			t.Fatalf("stage: %v", err)
		}
		// Sealed after staging, so the rename itself is what fails.
		if err := sealDir(dir); err != nil {
			t.Fatalf("seal: %v", err)
		}
		err = set.commit()
		if err == nil {
			t.Fatal("a rename into a sealed directory reported success")
		}
		if !strings.Contains(err.Error(), "a.csv") || strings.Contains(err.Error(), snapshot.TmpSuffix) {
			t.Errorf("error %q must name a.csv and no staging file", err)
		}
		if _, err := os.Stat(filepath.Join(dir, "a.csv")); err == nil {
			t.Error("a.csv was renamed into a directory the commit reported failing")
		}
	})

	t.Run("a refused rename removes every staging file not yet renamed", func(t *testing.T) {
		t.Parallel()
		if runtime.GOOS == "windows" {
			t.Skip(noDirPermissionsOnWindows)
		}
		if os.Geteuid() == 0 {
			t.Skip("root ignores the write bit")
		}
		base := unsealedTempDir(t)
		dir := filepath.Join(base, "out")
		other := filepath.Join(base, "other")
		for _, d := range []string{dir, other} {
			if err := os.Mkdir(d, 0o750); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.Symlink(filepath.Join(other, "a.csv"), filepath.Join(dir, "a.csv")); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		set, err := stageFileSet(dir, ab)
		if err != nil {
			t.Fatalf("stage: %v", err)
		}
		// Sealed after staging, so a.csv's rename fails and b.csv's never runs.
		if err := sealDir(other); err != nil {
			t.Fatal(err)
		}
		if err := set.commit(); err == nil {
			t.Fatal("a rename into a sealed directory reported success")
		}
		assertNoDebris(t, dir, "a.csv")
	})

	t.Run("a link to a directory that appears after staging is refused before any rename", func(t *testing.T) {
		t.Parallel()
		dir := filepath.Join(t.TempDir(), "out")
		set, err := stageFileSet(dir, ab)
		if err != nil {
			t.Fatalf("stage: %v", err)
		}
		if err := os.Mkdir(filepath.Join(dir, "elsewhere"), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("elsewhere", filepath.Join(dir, "b.csv")); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		if err := set.commit(); err == nil || !strings.Contains(err.Error(), "b.csv") {
			t.Fatalf("error %v, want a refusal naming b.csv", err)
		}
		if info, err := os.Lstat(filepath.Join(dir, "b.csv")); err != nil || info.Mode()&fs.ModeSymlink == 0 {
			t.Errorf("the link at b.csv was replaced (%v)", err)
		}
		if _, err := os.Stat(filepath.Join(dir, "a.csv")); err == nil {
			t.Error("a.csv was renamed into place; the set is partial and nothing says so")
		}
		assertNoDebris(t, dir, "b.csv", "elsewhere")
	})

	t.Run("a discarded set leaves no staging file", func(t *testing.T) {
		t.Parallel()
		dir := filepath.Join(t.TempDir(), "out")
		set, err := stageFileSet(dir, ab)
		if err != nil {
			t.Fatalf("stage: %v", err)
		}
		set.discard()
		set.discard()
		assertNoDebris(t, dir)
	})
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
