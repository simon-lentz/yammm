package cli

import (
	"errors"
	"io/fs"
	"os"
	"syscall"

	"github.com/simon-lentz/yammm/internal/hostpath"
)

// errStreamRewrite is the cause of a refused rewrite.
var errStreamRewrite = errors.New("names a stream, which a write appends to after the bytes the command read; name the file instead")

// CheckRewrite refuses a command that writes to write what it read from read,
// when [WriteFile] would write through write rather than replace a file (a
// descriptor path, a FIFO or a device) and write is read or reaches read's
// file. Such a write appends after the bytes already there, so the file would
// hold two documents. Call it before reading read: a stream that is refused
// then keeps its bytes. Any other failure is left to the read and the write.
func CheckRewrite(write, read string) error {
	stream, ok := writtenThrough(write)
	if !ok || (write != read && !sameFile(stream, read)) {
		return nil
	}
	return &fs.PathError{Op: "rewrite", Path: write, Err: errStreamRewrite}
}

// writtenThrough returns the descriptor path, FIFO or device that a write to
// path is written through, its links followed, and reports false for a path
// [WriteFile] replaces. A path that does not resolve answers false, and the
// write reports it.
func writtenThrough(path string) (string, bool) {
	host, err := HostPath(path)
	if err != nil {
		return "", false
	}
	t, err := resolveWriteTarget(host)
	return t.stream, err == nil && t.through
}

// sameFile reports whether stream, a host path [resolveWriteTarget] writes
// through, and read, read as [Open] reads it, are one file.
func sameFile(stream, read string) bool {
	wi, ok := streamInfo(stream)
	if !ok {
		return false
	}
	host, err := HostPath(read)
	if err != nil {
		return false
	}
	ri, ok := streamInfo(host)
	return ok && os.SameFile(wi, ri)
}

// streamInfo returns the info of the file host reaches. A descriptor of the
// process is judged by itself: on darwin a stat of /dev/fd/N answers for the
// descriptor device, and an open of it duplicates the descriptor, which a
// descriptor open only for writing refuses. Any other path is opened without
// blocking on a FIFO and judged by the descriptor the open returns.
func streamInfo(host string) (fs.FileInfo, bool) {
	if n, ok := hostpath.DescriptorNumber(host); ok {
		return descriptorInfo(n)
	}
	f, err := os.OpenFile(host, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	info, err := f.Stat()
	return info, err == nil
}
