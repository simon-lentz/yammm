package snapshot

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCreateStaging_NameKeepsTheExtensionBeforeTmpSuffix pins the staging
// name's shape: beside path, the stem, a token, then the extension and
// TmpSuffix. A sweep keyed on ".ys"+TmpSuffix still recognizes a crashed
// write's residue, and no two writers share a name.
func TestCreateStaging_NameKeepsTheExtensionBeforeTmpSuffix(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, base := range []string{"snap.ys", "a.b.ys", "noext"} {
		path := filepath.Join(dir, base)
		ext := filepath.Ext(base)
		stem := strings.TrimSuffix(base, ext)

		first, err := createStaging(path)
		if err != nil {
			t.Fatalf("%s: %v", base, err)
		}
		second, err := createStaging(path)
		if err != nil {
			t.Fatalf("%s: %v", base, err)
		}
		for _, f := range []*os.File{first, second} {
			name := f.Name()
			if err := f.Close(); err != nil {
				t.Fatal(err)
			}
			got := filepath.Base(name)
			if filepath.Dir(name) != dir {
				t.Errorf("%s: staging file %s is not beside the target", base, name)
			}
			if !strings.HasPrefix(got, stem+".") || !strings.HasSuffix(got, ext+TmpSuffix) {
				t.Errorf("%s: staging name %q is not stem, token, extension and %q", base, got, TmpSuffix)
			}
			if got == base+TmpSuffix {
				t.Errorf("%s: staging name %q is the one every writer shares", base, got)
			}
		}
		if first.Name() == second.Name() {
			t.Errorf("%s: two writers got one staging file %s", base, first.Name())
		}
	}
}

// TestCreateStaging_ModeMatchesOSCreate pins that the staging file, and so the
// committed file, gets the mode os.Create gives under the process umask.
func TestCreateStaging_ModeMatchesOSCreate(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	f, err := createStaging(filepath.Join(dir, "snap.ys"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	ref, err := os.Create(filepath.Join(dir, "reference"))
	if err != nil {
		t.Fatal(err)
	}
	defer ref.Close()

	got, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	want, err := ref.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if got.Mode().Perm() != want.Mode().Perm() {
		t.Errorf("staging mode %v, os.Create gives %v", got.Mode().Perm(), want.Mode().Perm())
	}
}
