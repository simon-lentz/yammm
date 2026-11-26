//go:build unix

package cli

import (
	"io/fs"
	"os"
	"syscall"
)

// descriptorInfo returns the info of the file descriptor n holds, through a
// duplicate, so closing it leaves n open.
func descriptorInfo(n int) (fs.FileInfo, bool) {
	d, err := syscall.Dup(n)
	if err != nil {
		return nil, false
	}
	f := os.NewFile(uintptr(d), "") //nolint:gosec // Dup returns a non-negative descriptor or an error
	defer f.Close()
	info, err := f.Stat()
	return info, err == nil
}
