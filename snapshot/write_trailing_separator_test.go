package snapshot_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/simon-lentz/yammm/snapshot"
)

// TestWriteFile_ATrailingSeparatorNamesADirectory holds WriteFile to the
// kernel's reading of a path that ends in a separator or a "." element: it
// names a directory, so no regular file is written under the bare name, and
// the file such a path spells over is left as it was.
func TestWriteFile_ATrailingSeparatorNamesADirectory(t *testing.T) {
	t.Parallel()

	for _, suffix := range []string{string(filepath.Separator), string(filepath.Separator) + "."} {
		dir := t.TempDir()
		existing := filepath.Join(dir, "old.ys")
		if err := os.WriteFile(existing, []byte("old"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := snapshot.WriteFile(existing+suffix, []byte("new")); err == nil {
			t.Errorf("WriteFile(%q) wrote over a regular file", existing+suffix)
		}
		if got, _ := os.ReadFile(existing); string(got) != "old" {
			t.Errorf("WriteFile(%q) changed old.ys to %q", existing+suffix, got)
		}

		absent := filepath.Join(dir, "new.ys")
		if err := snapshot.WriteFile(absent+suffix, []byte("new")); err == nil {
			t.Errorf("WriteFile(%q) wrote a regular file", absent+suffix)
		}
		if _, err := os.Lstat(absent); err == nil {
			t.Errorf("WriteFile(%q) left a file at new.ys", absent+suffix)
		}
		entries, _ := os.ReadDir(dir)
		if len(entries) != 1 {
			t.Errorf("WriteFile left %d entries in the directory, want old.ys alone", len(entries))
		}
	}
}

// TestWriteFile_ADirectoryFormIsRefusedBeforeStaging holds WriteFile's own
// refusal of a directory-form path on every host: an [fs.PathError] that names
// the path as given, matches syscall.ENOTDIR and never fs.ErrNotExist, tagged
// with the resolve step, so no staging file is made. An existing directory is
// refused and left a directory.
func TestWriteFile_ADirectoryFormIsRefusedBeforeStaging(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	existing := filepath.Join(dir, "old.ys")
	if err := os.WriteFile(existing, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	sep := string(filepath.Separator)

	for _, typed := range []string{
		existing + sep,
		existing + sep + ".",
		filepath.Join(dir, "new.ys") + sep,
		filepath.Join(existing, "x.ys") + sep,
		filepath.Join(dir, "x") + sep + ".." + sep + "old.ys" + sep,
	} {
		err := snapshot.WriteFile(typed, []byte("new"))
		pe, ok := errors.AsType[*fs.PathError](err)
		if !ok || pe.Path != typed || pe.Op != "stat" {
			t.Errorf("WriteFile(%q) = %v; want a stat fs.PathError naming the path", typed, err)
		}
		if !errors.Is(err, syscall.ENOTDIR) || errors.Is(err, fs.ErrNotExist) {
			t.Errorf("WriteFile(%q) = %v; want ENOTDIR and no fs.ErrNotExist", typed, err)
		}
		if err != nil && !strings.HasPrefix(err.Error(), "resolve target: ") {
			t.Errorf("WriteFile(%q) = %v; want the refusal before staging", typed, err)
		}
	}

	underMissing := filepath.Join(dir, "nodir", "new.ys") + sep
	err := snapshot.WriteFile(underMissing, []byte("new"))
	pe, ok := errors.AsType[*fs.PathError](err)
	if !ok || pe.Path != underMissing || !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("WriteFile(%q) = %v; want the missing directory refused as missing, naming the path", underMissing, err)
	}
	if ok {
		if _, nested := errors.AsType[*fs.PathError](pe.Err); nested {
			t.Errorf("WriteFile(%q) = %v; want one path error, naming the path once", underMissing, err)
		}
	}
	if err != nil && !strings.HasPrefix(err.Error(), "resolve target: ") {
		t.Errorf("WriteFile(%q) = %v; want the refusal before staging", underMissing, err)
	}

	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, typed := range []string{sub, sub + sep} {
		if err := snapshot.WriteFile(typed, []byte("new")); err == nil {
			t.Errorf("WriteFile(%q) wrote over a directory", typed)
		}
		if info, err := os.Stat(sub); err != nil || !info.IsDir() {
			t.Errorf("WriteFile(%q) replaced the directory: %v", typed, err)
		}
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 2 {
		t.Errorf("WriteFile left %d entries in the directory, want old.ys and sub alone", len(entries))
	}
}

// TestWriteFile_ALinkTargetInDirectoryFormIsRefused holds WriteFile to the
// directory-form rule at every link it follows: a link whose target text ends in
// a separator is refused before staging, as that text typed would be, so the
// file it names is neither replaced nor created. A link typed in directory form
// is judged at the typed path before any link is followed: Win32 drops a trailing
// "\." and reads the link itself. A loop of links in directory form is refused
// before staging too.
func TestWriteFile_ALinkTargetInDirectoryFormIsRefused(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	existing := filepath.Join(dir, "old.ys")
	if err := os.WriteFile(existing, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	sep := string(filepath.Separator)
	for link, target := range map[string]string{"to-old": "old.ys" + sep, "to-new": "new.ys" + sep, "plain": "old.ys"} {
		if err := os.Symlink(target, filepath.Join(dir, link)); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
	}
	for _, link := range []string{"to-old", "to-new"} {
		err := snapshot.WriteFile(filepath.Join(dir, link), []byte("new"))
		if !errors.Is(err, syscall.ENOTDIR) || errors.Is(err, fs.ErrNotExist) {
			t.Errorf("WriteFile(%s) = %v; want ENOTDIR and no fs.ErrNotExist", link, err)
		}
		if pe, ok := errors.AsType[*fs.PathError](err); !ok || pe.Op != "stat" || !strings.HasSuffix(pe.Path, sep) {
			t.Errorf("WriteFile(%s) = %v; want a stat fs.PathError naming the target in directory form", link, err)
		}
		if err != nil && !strings.HasPrefix(err.Error(), "resolve target: ") {
			t.Errorf("WriteFile(%s) = %v; want the refusal before staging", link, err)
		}
	}
	typed := filepath.Join(dir, "plain") + sep + "."
	err := snapshot.WriteFile(typed, []byte("new"))
	if pe, ok := errors.AsType[*fs.PathError](err); !ok || pe.Op != "stat" || pe.Path != typed || !errors.Is(err, syscall.ENOTDIR) {
		t.Errorf("WriteFile(%q) = %v; want a stat fs.PathError naming the typed path, ENOTDIR", typed, err)
	}
	if got, err := os.ReadFile(existing); err != nil || string(got) != "old" {
		t.Errorf("old.ys = %q, %v; want it unchanged", got, err)
	}

	loop := filepath.Join(dir, "loop")
	if err := os.Symlink("loop", loop); err != nil {
		t.Fatal(err)
	}
	err = snapshot.WriteFile(loop+sep, []byte("new"))
	if err == nil || !strings.HasPrefix(err.Error(), "resolve target: ") {
		t.Errorf("WriteFile(loop%s) = %v; want the loop refused before staging", sep, err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 5 {
		t.Errorf("WriteFile left %d entries, want old.ys and the four links", len(entries))
	}
}
