package hostpath

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Descriptor reports whether path names /dev/stdin, /dev/stdout, /dev/stderr or
// /dev/fd/N, judged by the directory holding it as the kernel reaches it, each
// link and ".." in it taken on disk: a link to /dev is seen, and on Linux every
// descriptor table procfs holds counts as /dev/fd.
func Descriptor(path string) bool {
	// Windows reads "/dev" as a directory of the current drive.
	if runtime.GOOS == "windows" {
		return false
	}
	dir := Parent(path)
	switch base := filepath.Base(path); {
	case base == "stdin" || base == "stdout" || base == "stderr":
		return sameFile(dir, "/dev")
	case strings.Trim(base, "0123456789") == "":
		return sameFile(dir, "/dev/fd") || procDescriptorTable(dir)
	}
	return false
}

// sameFile reports whether a and b both stat and name one file.
func sameFile(a, b string) bool {
	ai, err := os.Stat(a)
	if err != nil {
		return false
	}
	bi, err := os.Stat(b)
	return err == nil && os.SameFile(ai, bi)
}
