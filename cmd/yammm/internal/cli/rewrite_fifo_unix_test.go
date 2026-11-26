//go:build unix && !aix && !solaris

package cli

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// A FIFO the command would write back is judged by the file it is, through a
// link too, without blocking on it: no writer is attached, so a blocking open
// would wait for one. A FIFO no read permission opens is still the file the
// command names twice.
func TestCheckRewrite_JudgesAFIFOByTheFileItIs(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fifo := filepath.Join(dir, "pipe")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.ys")
	if err := os.Symlink(fifo, link); err != nil {
		t.Fatal(err)
	}
	if err := CheckRewrite(fifo, link); err == nil {
		t.Error("a FIFO written back through a link to it is not refused")
	}
	if err := CheckRewrite(link, fifo); err == nil {
		t.Error("a link to a FIFO written back to the FIFO is not refused")
	}
	writeOnly := filepath.Join(dir, "write_only")
	if err := syscall.Mkfifo(writeOnly, 0o200); err != nil {
		t.Fatal(err)
	}
	if err := CheckRewrite(writeOnly, writeOnly); err == nil {
		t.Error("a FIFO open only for writing, written back, is not refused")
	}
}
