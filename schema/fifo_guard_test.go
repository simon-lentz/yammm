//go:build unix

package schema_test

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/simon-lentz/yammm/schema"
)

// A schema source must be a regular file. Opening a FIFO blocks until a writer
// appears and neither the open nor the read takes a context, so a named pipe in
// place of a schema would hang the loader with no way to cancel it.
//
// The test runs the load in a goroutine and fails on timeout rather than
// blocking the suite, so a regression is reported instead of hanging CI.
func TestLoad_RefusesANonRegularFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	fifo := filepath.Join(dir, "entry.yammm")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Skipf("mkfifo unavailable: %v", err)
	}

	type outcome struct{ err error }
	done := make(chan outcome, 1)
	go func() {
		_, res := schema.Load(t.Context(), fifo, schema.WithModuleRoot(dir))
		done <- outcome{res.Err()}
	}()

	select {
	case got := <-done:
		if got.err == nil {
			t.Fatal("a FIFO in place of a schema must not load")
		}
		if !strings.Contains(got.err.Error(), "not a regular file") {
			t.Errorf("the refusal should name the file kind; got %v", got.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Load blocked on a FIFO: the file-kind guard is gone")
	}
	_ = os.Remove(fifo)
}
