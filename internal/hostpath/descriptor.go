package hostpath

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
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

// DescriptorNumber returns the number of the process's own descriptor that path
// names, as [Descriptor] judges the path: 0, 1 and 2 for /dev/stdin,
// /dev/stdout and /dev/stderr, and N for /dev/fd/N. A descriptor table of
// another process names no descriptor of this one.
func DescriptorNumber(path string) (int, bool) {
	if runtime.GOOS == "windows" {
		return 0, false
	}
	dir := Parent(path)
	switch base := filepath.Base(path); base {
	case "stdin", "stdout", "stderr":
		return slices.Index([]string{"stdin", "stdout", "stderr"}, base), sameFile(dir, "/dev")
	default:
		if strings.Trim(base, "0123456789") != "" {
			return 0, false
		}
		n, err := strconv.Atoi(base)
		return n, err == nil && sameFile(dir, "/dev/fd")
	}
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
