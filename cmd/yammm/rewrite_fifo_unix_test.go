//go:build unix && !aix && !solaris

package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
)

// update-metadata refuses a FIFO before it reads it, so the bytes a writer
// waits to send stay for the next reader.
func TestSnapshotUpdateMetadata_RefusesAFIFOBeforeReadingIt(t *testing.T) {
	t.Parallel()
	content, err := os.ReadFile(filepath.Join("testdata", "valid_snapshot.ys"))
	if err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(t.TempDir(), "pipe.ys")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	// opened closes once a reader attaches: a command that read the FIFO
	// attached one before its read reached the writer's end of file.
	opened := make(chan struct{})
	sent := make(chan error, 1)
	go func() {
		w, err := os.OpenFile(fifo, os.O_WRONLY, 0)
		close(opened)
		if err != nil {
			sent <- err
			return
		}
		_, err = w.Write(content)
		sent <- errors.Join(err, w.Close())
	}()
	code, _, stderr := executeCmdOutput(t, "snapshot", "update-metadata", "--set", "k=v", fifo)
	if code != cli.ExitRuntime {
		t.Errorf("exit %d, want %d: %s", code, cli.ExitRuntime, stderr)
	}
	select {
	case <-opened:
		t.Fatal("the command opened the FIFO it refuses")
	default:
	}
	r, err := os.Open(fifo)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if err := <-sent; err != nil {
		t.Fatal(err)
	}
	if len(got) != len(content) {
		t.Errorf("the next reader got %d bytes, want the %d the writer sent", len(got), len(content))
	}
}

// A FIFO named for a scan is still no entry a scan admits, so a snapshot
// written through it warns.
func TestSnapshotSave_WarnsOnAFIFOWhoseNameEndsInTheExtension(t *testing.T) {
	t.Parallel()
	fifo := filepath.Join(t.TempDir(), "pipe.ys")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	drained := make(chan error, 1)
	go func() {
		r, err := os.Open(fifo)
		if err != nil {
			drained <- err
			return
		}
		_, err = io.Copy(io.Discard, r)
		drained <- errors.Join(err, r.Close())
	}()
	code, _, stderr := executeCmdOutput(t, "snapshot", "save", filepath.Join("testdata", "valid.yammm"),
		filepath.Join("testdata", "data.json"), "-o", fifo)
	if code != cli.ExitOK {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if err := <-drained; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr, "W_SNAPSHOT_PATH_EXTENSION") {
		t.Errorf("no extension warning for a FIFO: %s", stderr)
	}
}
