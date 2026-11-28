//go:build darwin || linux

package hostpath

import (
	"errors"
	"io/fs"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

// stageDir is a directory a staging file is created and renamed in, held by a
// descriptor opened for search alone, so a directory the process may write and
// enter but not list still takes a staging file. Each lookup through it names
// one element: darwin counts a link's body and the rest of the path against
// MAXPATHLEN, so a staging name longer than the target's own may not fit when
// spelled through the link.
type stageDir struct {
	fd   int
	name string
}

func openStageDir(dir string) (stageDir, error) {
	fd, err := retryEINTR(func() (int, error) { return unix.Open(dir, searchOnly|unix.O_DIRECTORY|unix.O_CLOEXEC, 0) })
	if err != nil {
		return stageDir{}, &fs.PathError{Op: "open", Path: dir, Err: err}
	}
	return stageDir{fd: fd, name: dir}, nil
}

func (d stageDir) create(name string, perm fs.FileMode) (*os.File, error) {
	fd, err := retryEINTR(func() (int, error) {
		return unix.Openat(d.fd, name, unix.O_RDWR|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC, uint32(perm.Perm()))
	})
	if err != nil {
		return nil, &fs.PathError{Op: "open", Path: d.path(name), Err: err}
	}
	return os.NewFile(uintptr(fd), d.path(name)), nil //nolint:gosec // a descriptor openat returned is not negative
}

func (d stageDir) rename(from, to string) error {
	if err := unix.Renameat(d.fd, from, d.fd, to); err != nil {
		return &os.LinkError{Op: "rename", Old: d.path(from), New: d.path(to), Err: err}
	}
	return nil
}

func (d stageDir) remove(name string) error {
	if err := unix.Unlinkat(d.fd, name, 0); err != nil {
		return &fs.PathError{Op: "remove", Path: d.path(name), Err: err}
	}
	return nil
}

// close releases the descriptor. It holds no data to flush, so a failed close
// reports nothing a caller acts on.
func (d stageDir) close() { unix.Close(d.fd) } //nolint:gosec // nothing to flush

// retryEINTR calls open again while it is interrupted by a signal, as os does.
func retryEINTR(open func() (int, error)) (int, error) {
	for {
		fd, err := open()
		if !errors.Is(err, syscall.EINTR) {
			return fd, err
		}
	}
}
