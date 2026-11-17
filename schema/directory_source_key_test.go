package schema_test

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema"
)

// TestLoadSources_AKeyThatNamesADirectoryRefusesAFile holds an in-memory source
// key that ends in a separator or a "." element to the resolver's rule, relative
// or absolute: a key whose last element is a regular file on disk is refused,
// where the same key without the separator loads.
func TestLoadSources_AKeyThatNamesADirectoryRefusesAFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	source := []byte("schema \"s\"\n\ntype A {\n\tid String primary\n}\n")
	if err := os.WriteFile(filepath.Join(root, "x.yammm"), source, 0o600); err != nil {
		t.Fatal(err)
	}
	sep := string(filepath.Separator)
	load := func(key string) diag.Result {
		_, res := schema.LoadSourcesWithEntry(t.Context(), map[string][]byte{key: source}, key, root,
			schema.WithSourcesOnly(true))
		return res
	}
	if res := load("x.yammm"); res.HasErrors() {
		t.Fatalf("the plain key was refused: %s", firstError(res))
	}
	for _, key := range []string{
		"x.yammm" + sep,
		"x.yammm" + sep + ".",
		filepath.Join(root, "x.yammm") + sep,
	} {
		res := load(key)
		if !res.HasFatal() {
			t.Errorf("key %q loaded", key)
			continue
		}
		// A load reports the refusal as text, so its message names the host's
		// ENOTDIR, which on Windows is ERROR_PATH_NOT_FOUND's text.
		if msg := firstError(res); !strings.Contains(msg, syscall.ENOTDIR.Error()) {
			t.Errorf("key %q: %s, want the not-a-directory refusal", key, msg)
		}
	}
}
