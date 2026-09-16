package location

import (
	"bytes"
	"io/fs"
	"runtime"
	"syscall"
	"unsafe"
)

// darwinPathMax is PATH_MAX, the size realpath(3) writes into.
const darwinPathMax = 1024

// spellOnDisk returns the existing path p as the filesystem spells it, through
// realpath(3), which reads names from the directories a lookup passes through
// and opens nothing. The package documentation states why fcntl(F_GETPATH) is
// not used.
func spellOnDisk(p string) (string, error) {
	in, err := syscall.BytePtrFromString(p)
	if err != nil {
		return "", &fs.PathError{Op: "realpath", Path: p, Err: err}
	}
	buf := make([]byte, darwinPathMax)
	r1, _, errno := syscallPtr(libcRealpathTrampolineAddr,
		uintptr(unsafe.Pointer(in)), uintptr(unsafe.Pointer(&buf[0])), 0)
	runtime.KeepAlive(in)
	if r1 == 0 {
		return "", &fs.PathError{Op: "realpath", Path: p, Err: errno}
	}
	end := bytes.IndexByte(buf, 0)
	if end < 0 {
		end = len(buf)
	}
	return string(buf[:end]), nil
}

// syscallPtr is the runtime's libSystem call for a function that
// reports failure by returning NULL, reached as golang.org/x/sys/unix reaches
// it: a libc call keeps to the ABI Apple supports, where a raw trap does not.
//
//go:linkname syscallPtr syscall.syscallPtr
func syscallPtr(fn, a1, a2, a3 uintptr) (r1, r2 uintptr, err syscall.Errno)

// libcRealpathTrampolineAddr is the address of the assembly trampoline that
// jumps to libSystem's realpath.
var libcRealpathTrampolineAddr uintptr

//go:cgo_import_dynamic libc_realpath realpath "/usr/lib/libSystem.B.dylib"
