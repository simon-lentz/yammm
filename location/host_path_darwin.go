package location

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"syscall"
	"unsafe"
)

// darwinPathMax is PATH_MAX, the size F_GETPATH writes into.
const darwinPathMax = 1024

// spellOnDisk returns the existing path p as the filesystem spells it. It
// opens only a directory or a regular file, because a FIFO's open blocks and a
// socket's fails, and falls back to EvalSymlinks — which opens nothing —
// for every other kind and for a path it may not read. See the package doc.
func spellOnDisk(p string, mode fs.FileMode) (string, error) {
	if !mode.IsRegular() && !mode.IsDir() {
		return evalSymlinks(p)
	}
	f, err := os.OpenFile(p, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return evalSymlinks(p)
	}
	defer f.Close()
	spelled, err := fcntlGetPath(f)
	if err != nil {
		return evalSymlinks(p)
	}
	return spelled, nil
}

// fcntlGetPath asks the kernel for the path of f's vnode. The call goes
// through syscall because golang.org/x/sys/unix exposes no pointer-taking
// fcntl.
func fcntlGetPath(f *os.File) (string, error) {
	conn, err := f.SyscallConn()
	if err != nil {
		return "", fmt.Errorf("syscall conn for %q: %w", f.Name(), err)
	}
	buf := make([]byte, darwinPathMax)
	var errno syscall.Errno
	if err := conn.Control(func(fd uintptr) {
		_, _, errno = syscall.Syscall(syscall.SYS_FCNTL, fd, syscall.F_GETPATH, uintptr(unsafe.Pointer(&buf[0])))
	}); err != nil {
		return "", fmt.Errorf("fcntl F_GETPATH on %q: %w", f.Name(), err)
	}
	if errno != 0 {
		return "", errno
	}
	end := bytes.IndexByte(buf, 0)
	if end < 0 {
		end = len(buf)
	}
	return string(buf[:end]), nil
}
