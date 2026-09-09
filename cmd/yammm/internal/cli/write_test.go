package cli

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
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

// B9: the staging file an interrupted write leaves behind must be invisible to
// a directory scan. The convention is the shared suffix, and the contrast is
// what the CLI used to stage on.
func TestStagingName_IsInvisibleToADirectoryScan(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	target := filepath.Join(dir, "snap.ys")

	staged, err := os.CreateTemp(dir, stagingPattern(target))
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

	// The name the CLI staged on before is reported, which is the defect.
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

func TestStagedFiles_EveryFileArrivesOrNoneDoes(t *testing.T) {
	t.Parallel()

	t.Run("commit puts them all in place", func(t *testing.T) {
		t.Parallel()
		dir := filepath.Join(t.TempDir(), "out")
		staged, err := NewStagedFiles(dir)
		if err != nil {
			t.Fatalf("stage: %v", err)
		}
		for _, name := range []string{"a.csv", "b.csv"} {
			w, err := staged.Create(name)
			if err != nil {
				t.Fatalf("create %s: %v", name, err)
			}
			if _, err := io.WriteString(w, name); err != nil {
				t.Fatalf("write %s: %v", name, err)
			}
		}
		if err := staged.Commit(); err != nil {
			t.Fatalf("commit: %v", err)
		}
		for _, name := range []string{"a.csv", "b.csv"} {
			assertPerm(t, filepath.Join(dir, name), NewFileMode)
		}
		assertNoDebris(t, dir, filepath.Join(dir, "a.csv"), filepath.Join(dir, "b.csv"))
	})

	t.Run("one blocked target leaves the directory as it was", func(t *testing.T) {
		t.Parallel()
		dir := filepath.Join(t.TempDir(), "out")
		staged, err := NewStagedFiles(dir)
		if err != nil {
			t.Fatalf("stage: %v", err)
		}
		blocker := filepath.Join(dir, "b.csv")
		if err := os.Mkdir(blocker, 0o750); err != nil {
			t.Fatalf("mkdir blocker: %v", err)
		}
		for _, name := range []string{"a.csv", "b.csv"} {
			w, err := staged.Create(name)
			if err != nil {
				t.Fatalf("create %s: %v", name, err)
			}
			if _, err := io.WriteString(w, name); err != nil {
				t.Fatalf("write %s: %v", name, err)
			}
		}
		if err := staged.Commit(); err == nil {
			t.Fatal("a blocked target reported success")
		}
		// a.csv must NOT have been renamed into place: a partial set reads as
		// a complete export with one type quietly missing.
		if _, err := os.Stat(filepath.Join(dir, "a.csv")); err == nil {
			t.Error("a.csv was left behind; the set is partial and nothing says so")
		}
		assertNoDebris(t, dir, blocker)
	})

	t.Run("rollback without commit leaves nothing", func(t *testing.T) {
		t.Parallel()
		dir := filepath.Join(t.TempDir(), "out")
		staged, err := NewStagedFiles(dir)
		if err != nil {
			t.Fatalf("stage: %v", err)
		}
		for _, name := range []string{"a.csv", "b.csv"} {
			w, err := staged.Create(name)
			if err != nil {
				t.Fatalf("create %s: %v", name, err)
			}
			if _, err := io.WriteString(w, name); err != nil {
				t.Fatalf("write %s: %v", name, err)
			}
		}
		// The caller abandoned the set — what a failure between Create and
		// Commit does. Nothing staged may outlive it.
		staged.Rollback()
		assertNoDebris(t, dir)
	})

	t.Run("rollback is safe after commit", func(t *testing.T) {
		t.Parallel()
		dir := filepath.Join(t.TempDir(), "out")
		staged, err := NewStagedFiles(dir)
		if err != nil {
			t.Fatalf("stage: %v", err)
		}
		w, err := staged.Create("a.csv")
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		if _, err := io.WriteString(w, "a"); err != nil {
			t.Fatalf("write: %v", err)
		}
		if err := staged.Commit(); err != nil {
			t.Fatalf("commit: %v", err)
		}
		staged.Rollback()
		staged.Rollback()
		if _, err := os.Stat(filepath.Join(dir, "a.csv")); err != nil {
			t.Errorf("rollback after commit removed the committed file: %v", err)
		}
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
