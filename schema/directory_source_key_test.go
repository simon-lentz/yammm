package schema_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/schema"
)

// TestLoadSources_AKeyThatNamesADirectoryIsRefused holds an in-memory source
// key that ends in a separator or a "." or ".." element to one refusal, relative or
// absolute, under a module root on disk or a synthetic root: a key names a
// file, so the key is refused whatever the disk holds at its element, where
// the same key without the ending loads.
func TestLoadSources_AKeyThatNamesADirectoryIsRefused(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	source := []byte("schema \"s\"\n\ntype A {\n\tid String primary\n}\n")
	if err := os.WriteFile(filepath.Join(root, "x.yammm"), source, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "d.yammm"), 0o750); err != nil {
		t.Fatal(err)
	}
	sep := string(filepath.Separator)
	load := func(key string, opts ...schema.Option) error {
		_, err := schema.LoadSourcesWithEntry(t.Context(), map[string][]byte{key: source}, key,
			append(opts, schema.WithSourcesOnly(true))...)
		return err
	}
	onDisk := schema.WithModuleRoot(root)
	synthetic := schema.WithSyntheticRoot("embedded://app")
	if err := load("x.yammm", onDisk); err != nil {
		t.Fatalf("the plain key was refused under the module root: %v", err)
	}
	if err := load("x.yammm", synthetic); err != nil {
		t.Fatalf("the plain key was refused under the synthetic root: %v", err)
	}
	for _, row := range []struct {
		key  string
		opts []schema.Option
	}{
		{"x.yammm" + sep, []schema.Option{onDisk}},
		{"x.yammm" + sep + ".", []schema.Option{onDisk}},
		{"d.yammm" + sep, []schema.Option{onDisk}},
		{"absent.yammm" + sep, []schema.Option{onDisk}},
		{filepath.Join(root, "x.yammm") + sep, []schema.Option{onDisk}},
		{filepath.Join(root, "x.yammm") + sep, nil},
		{"x.yammm/", []schema.Option{synthetic}},
		{"x.yammm/.", []schema.Option{synthetic}},
		{".", []schema.Option{onDisk}},
		{"..", []schema.Option{onDisk}},
		{"sub" + sep + "..", []schema.Option{onDisk}},
		{"sub" + sep + ".." + sep + "..", []schema.Option{onDisk}},
		{"sub/x/..", []schema.Option{synthetic}},
	} {
		err := load(row.key, row.opts...)
		if err == nil {
			t.Errorf("key %q loaded", row.key)
			continue
		}
		if !strings.Contains(err.Error(), "names a directory, not a file") {
			t.Errorf("key %q: %v, want the directory-key refusal", row.key, err)
		}
	}
}
