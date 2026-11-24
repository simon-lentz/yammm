package schema_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/internal/diagtest"
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
		_, res := diagtest.Capture(t, func(warn func(diag.Issue)) (schema.Schema, error) {
			return schema.LoadSourcesWithEntry(t.Context(), map[string][]byte{key: source}, key,
				schema.WithSourcesOnly(true), schema.WithModuleRoot(root), schema.WithWarnings(warn))
		})
		return res
	}
	if res := load("x.yammm"); diagtest.Err(res) != nil {
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
		// A load reports the refusal as text, which reads as Unix's ENOTDIR on
		// every host.
		if msg := firstError(res); !strings.Contains(msg, "not a directory") {
			t.Errorf("key %q: %s, want the not-a-directory refusal", key, msg)
		}
	}
}
