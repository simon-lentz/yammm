package snapshot_test

import (
	"os"
	"path/filepath"
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
