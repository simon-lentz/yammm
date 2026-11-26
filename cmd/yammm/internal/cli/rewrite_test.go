package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// A command that writes back a file it read refuses a write that would go
// through a stream: the write appends after the bytes already there, so the
// file would hold two documents. A write that replaces a file, or that goes
// through a stream to a file the command did not read, is no rewrite of what it
// read and stays.
func TestCheckRewrite_RefusesAStreamWriteToTheFileRead(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no descriptor path")
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "x.ys")
	other := filepath.Join(dir, "y.ys")
	for _, p := range []string{file, other} {
		if err := os.WriteFile(p, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	f, err := os.OpenFile(file, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	fd := fmt.Sprintf("/dev/fd/%d", f.Fd())
	// A shell's ">>" opens a descriptor for writing alone, which an open of
	// its descriptor path for reading is refused on darwin.
	w, err := os.OpenFile(file, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	writeOnly := fmt.Sprintf("/dev/fd/%d", w.Fd())
	linkToWriteOnly := filepath.Join(dir, "fd_link")
	if err := os.Symlink(writeOnly, linkToWriteOnly); err != nil {
		t.Fatal(err)
	}

	for _, c := range []struct {
		name, write, read string
		refused           bool
	}{
		{"a descriptor path written back", fd, fd, true},
		{"a descriptor path to the file read", fd, file, true},
		{"a write-only descriptor path to the file read", writeOnly, file, true},
		{"a link to a write-only descriptor path to the file read", linkToWriteOnly, file, true},
		{"a descriptor path to another file", fd, other, false},
		{"a write-only descriptor path to another file", writeOnly, other, false},
		// No process holds so high a descriptor, so the path names no file
		// and the write reports it.
		{"a descriptor path no descriptor holds", "/dev/fd/1048576", file, false},
		{"a file read through a descriptor path and replaced", file, fd, false},
		{"a file replaced", file, file, false},
		{"a file that does not exist yet", filepath.Join(dir, "new.ys"), file, false},
	} {
		err := CheckRewrite(c.write, c.read)
		if (err != nil) != c.refused {
			t.Errorf("%s: CheckRewrite(%s, %s) = %v, want refused %v", c.name, c.write, c.read, err, c.refused)
		}
		if got, _ := os.ReadFile(file); string(got) != "{}" {
			t.Fatalf("%s: the check changed the file: %q", c.name, got)
		}
	}
	for _, c := range platformTargetCases() {
		target, err := c.setup(dir)
		if err != nil {
			t.Fatal(err)
		}
		if err := CheckRewrite(target, target); err == nil {
			t.Errorf("%s: a FIFO written back is not refused", c.name)
		}
	}
}
