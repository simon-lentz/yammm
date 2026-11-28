package gogen_test

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/schema/gogen"
)

// TestMarshal_ANoRootEntryBehindADescriptorLinkKeysItsSiblings holds the keys of
// a load with no root to the entry's directory as its siblings are spelled:
// an entry that is a link to a descriptor path, in a directory typed through a
// link, keys itself and the file beside it by their base names, and the store
// re-loads.
func TestMarshal_ANoRootEntryBehindADescriptorLinkKeysItsSiblings(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no descriptor paths")
	}
	top := t.TempDir()
	if err := os.Mkdir(filepath.Join(top, "real"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real", filepath.Join(top, "ldir")); err != nil {
		t.Fatal(err)
	}
	held, err := os.CreateTemp(top, "held")
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	entry := filepath.Join(top, "ldir", "main.yammm")
	if err := os.Symlink(fmt.Sprintf("/dev/fd/%d", held.Fd()), entry); err != nil {
		t.Fatal(err)
	}
	main := []byte("schema \"main\"\n\nimport \"./dep\" as dep\n\ntype M {\n\tid String primary\n\t--> TO_D (one) dep.D\n}\n")
	dep := []byte("schema \"dep\"\n\ntype D {\n\tid String primary\n}\n")
	s, err := schema.LoadSourcesWithEntry(t.Context(), map[string][]byte{
		entry:                                   main,
		filepath.Join(top, "ldir", "dep.yammm"): dep,
	}, entry, schema.WithSourcesOnly(true))
	if err != nil {
		t.Fatal(err)
	}
	got, err := gogen.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	store, key := emittedStore(t, got)
	keys := keysOf(store)
	slices.Sort(keys)
	if key != "main.yammm" || !slices.Equal(keys, []string{"dep.yammm", "main.yammm"}) {
		t.Errorf("emitted store %v with entry %q; want dep.yammm and main.yammm, entry main.yammm", keys, key)
	}
	if _, err := schema.LoadSourcesWithEntry(t.Context(), store, key, schema.WithSourcesOnly(true), schema.WithSyntheticRoot("consumer://keys")); err != nil {
		t.Errorf("the emitted store does not re-load: %v", err)
	}
}
