//go:build unix

package schema_test

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema"
)

// Every file the loader reads must be a regular file. Opening a FIFO blocks
// until a writer appears and neither the open nor the read takes a context, so
// a named pipe in place of a schema, an import or the module-root marker would
// hang the loader with no way to cancel it.
//
// Each test runs the load in a goroutine and fails on timeout rather than
// blocking the suite, so a regression is reported instead of hanging CI.

func loadWithWatchdog(t *testing.T, load func() diag.Result) diag.Result {
	t.Helper()
	done := make(chan diag.Result, 1)
	go func() { done <- load() }()
	select {
	case res := <-done:
		return res
	case <-time.After(5 * time.Second):
		t.Fatal("Load blocked on a FIFO: the file-kind guard is gone")
		return diag.Result{}
	}
}

func mkfifo(t *testing.T, path string) {
	t.Helper()
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Skipf("mkfifo unavailable: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(path) })
}

func TestLoad_RefusesANonRegularFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	fifo := filepath.Join(dir, "entry.yammm")
	mkfifo(t, fifo)

	res := loadWithWatchdog(t, func() diag.Result {
		_, res := schema.Load(t.Context(), fifo, schema.WithModuleRoot(dir))
		return res
	})
	if res.Err() == nil {
		t.Fatal("a FIFO in place of a schema must not load")
	}
	if !strings.Contains(res.Err().Error(), "not a regular file") {
		t.Errorf("the refusal should name the file kind; got %v", res.Err())
	}
}

func TestLoad_RefusesANonRegularImport(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	mkfifo(t, filepath.Join(dir, "imp.yammm"))
	entry := filepath.Join(dir, "main.yammm")
	if err := os.WriteFile(entry, []byte("schema \"main\"\n\nimport \"./imp.yammm\" as i\n\ntype T {\n    id String primary\n}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	res := loadWithWatchdog(t, func() diag.Result {
		_, res := schema.Load(t.Context(), entry, schema.WithModuleRoot(dir))
		return res
	})
	if res.Err() == nil {
		t.Fatal("a FIFO in place of an import must not load")
	}
	if !strings.Contains(res.Err().Error(), "not a regular file") {
		t.Errorf("the refusal should name the file kind; got %v", res.Err())
	}
}

// The module-root marker is read before either schema file, through the
// discovery walk a load with no explicit root performs.
func TestLoad_RefusesANonRegularModuleRootMarker(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	mkfifo(t, filepath.Join(dir, schema.ModuleRootMarker))
	entry := filepath.Join(dir, "main.yammm")
	if err := os.WriteFile(entry, []byte("schema \"main\"\n\ntype T {\n    id String primary\n}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	res := loadWithWatchdog(t, func() diag.Result {
		_, res := schema.Load(t.Context(), entry)
		return res
	})
	if !res.HasCode(diag.E_LOAD_MODULE_ROOT_MALFORMED) {
		t.Fatalf("a FIFO in place of the marker must be a malformed marker; got %v", res.Err())
	}
	if !strings.Contains(res.Err().Error(), "not a regular file") {
		t.Errorf("the refusal should name the file kind; got %v", res.Err())
	}
}
