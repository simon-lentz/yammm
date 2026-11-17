package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
)

// TestCommands_ATrailingSeparatorNamesADirectory holds every command to the
// kernel's reading of a path that ends in a separator or a "." element: it
// names a directory, so a regular file spelled that way is refused, never
// read or written under its bare name.
func TestCommands_ATrailingSeparatorNamesADirectory(t *testing.T) {
	t.Parallel()

	for _, suffix := range []string{"/", string(filepath.Separator) + "."} {
		t.Run("check, the data file", func(t *testing.T) {
			t.Parallel()
			code, _, stderr := executeCmdOutput(t, "check", "testdata/valid.yammm", "testdata/data.json"+suffix)
			if code == cli.ExitOK {
				t.Errorf("check read data.json%s as data.json; stderr:\n%s", suffix, stderr)
			}
		})

		t.Run("gen, the schema", func(t *testing.T) {
			t.Parallel()
			code, _, stderr := executeCmdOutput(t, "gen", "--to", "md", "testdata/valid.yammm"+suffix)
			if code == cli.ExitOK {
				t.Errorf("gen loaded valid.yammm%s as valid.yammm; stderr:\n%s", suffix, stderr)
			}
		})

		t.Run("snapshot save, the seed it merges into", func(t *testing.T) {
			t.Parallel()
			seed := filepath.Join(t.TempDir(), "seed.ys")
			copyFile(t, "testdata/valid_snapshot.ys", seed)
			before, err := os.ReadFile(seed)
			if err != nil {
				t.Fatal(err)
			}
			code, _, stderr := executeCmdOutput(t, "snapshot", "save", "--into", seed+suffix,
				"testdata/valid.yammm", "testdata/data_carol.json")
			if code == cli.ExitOK {
				t.Errorf("snapshot save merged into seed.ys%s as seed.ys; stderr:\n%s", suffix, stderr)
			}
			after, err := os.ReadFile(seed)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Errorf("snapshot save rewrote seed.ys through seed.ys%s", suffix)
			}
		})
	}
}
