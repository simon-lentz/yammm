//go:build unix && !aix && !solaris

package schema_test

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// TestLoad_RefusesAModuleRootItCannotOpenAsADirectory pins that Load refuses,
// whatever the schema imports, an explicit root that is a FIFO, a device, a
// directory it cannot open, or a directory named in bytes that are not valid
// UTF-8. A FIFO root is refused without blocking.
func TestLoad_RefusesAModuleRootItCannotOpenAsADirectory(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for name, content := range map[string]string{
		"solo.yammm":    soloSchema,
		"main.yammm":    importingSchema,
		"lib/dep.yammm": depSchema,
	} {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	fifo := filepath.Join(dir, "fifo")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	notDir := syscall.ENOTDIR.Error()
	type rootCase struct{ root, cause string }
	roots := map[string]rootCase{"a FIFO": {fifo, notDir}, "a device": {os.DevNull, notDir}}
	if os.Geteuid() != 0 {
		locked := filepath.Join(dir, "locked")
		if err := os.Mkdir(locked, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(locked, 0); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := os.Chmod(locked, 0o700); err != nil { //nolint:gosec // restoring the test's own directory so TempDir can remove it
				t.Error(err)
			}
		})
		roots["a directory it cannot open"] = rootCase{locked, syscall.EACCES.Error()}
	}
	if notUTF8 := filepath.Join(dir, "d\xff"); os.Mkdir(notUTF8, 0o750) == nil {
		if entries, err := os.ReadDir(dir); err == nil && holdsEntry(entries, "d\xff") {
			roots["a directory named in bytes that are not UTF-8"] = rootCase{notUTF8, location.ErrInvalidUTF8Path.Error()}
		}
	}
	for _, entry := range []string{"solo.yammm", "main.yammm"} {
		for name, c := range roots {
			t.Run(entry+", "+name, func(t *testing.T) {
				t.Parallel()
				done := make(chan struct{})
				go func() {
					defer close(done)
					checkRefusedRoot(t, filepath.Join(dir, entry), c.root, c.cause)
				}()
				select {
				case <-done:
				case <-time.After(30 * time.Second):
					t.Fatalf("Load under module root %s did not return", c.root)
				}
			})
		}
	}
}

// holdsEntry reports whether entries holds one named name, byte for byte.
func holdsEntry(entries []os.DirEntry, name string) bool {
	for _, e := range entries {
		if e.Name() == name {
			return true
		}
	}
	return false
}

// TestLoadSourcesWithEntry_AFIFORootNeverBlocks pins that an in-memory load
// whose missing import falls back to a root that is a FIFO fails at that
// import, where opening the root waited for a writer forever.
func TestLoadSourcesWithEntry_AFIFORootNeverBlocks(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fifo := filepath.Join(dir, "fifo")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	entry := filepath.Join(dir, "main.yammm")
	sources := map[string][]byte{entry: []byte(importingSchema)}
	done := make(chan diag.Result, 1)
	go func() {
		_, res := schema.LoadSourcesWithEntry(context.Background(), sources, entry, fifo)
		done <- res
	}()
	select {
	case res := <-done:
		if !res.HasCode(diag.E_IMPORT_RESOLVE) {
			t.Errorf("got %s, want E_IMPORT_RESOLVE at the import", res)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("LoadSourcesWithEntry under a FIFO root did not return")
	}
}
