package hostpath

import (
	"io/fs"
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
	_, ok := descriptorName(path)
	return ok
}

// descriptorName returns the name of the descriptor path a [Descriptor] path
// names, spelled from the directory that judged it: /dev/ or /dev/fd/ and its
// base name, so /DEV/stdin, a link to /dev and this process's procfs table,
// /proc/self/fd or /proc/<pid>/fd, give one name the kernel reads. An entry of
// another procfs descriptor table, as another process's or a thread's, keeps
// its spelling.
func descriptorName(path string) (string, bool) {
	// Windows reads "/dev" as a directory of the current drive.
	if runtime.GOOS == "windows" {
		return "", false
	}
	dir := Parent(path)
	base := filepath.Base(path)
	if base == "stdin" || base == "stdout" || base == "stderr" {
		return "/dev/" + base, sameFile(dir, "/dev")
	}
	_, n, ok := descriptorDigits(base)
	switch {
	case !ok:
		return "", false
	case sameFile(dir, "/dev/fd"):
		return "/dev/fd/" + n, true
	case procDescriptorTable(dir):
		return path, true
	}
	return "", false
}

// descriptorDigits returns the number a descriptor table's entry name states,
// and that number in its one decimal form. darwin's fdesc reads leading zeros,
// so "00" names descriptor 0 there; procfs names each entry in that one form
// alone, so a name in another form names no entry.
func descriptorDigits(base string) (int, string, bool) {
	if base == "" || strings.Trim(base, "0123456789") != "" {
		return 0, "", false
	}
	n, err := strconv.Atoi(base)
	if err != nil {
		return 0, "", false
	}
	canonical := strconv.Itoa(n)
	return n, canonical, canonical == base || !procfsEntries
}

// inDescriptorTable reports whether p, a speller's answer, is a descriptor path
// or lies under a descriptor table: darwin's realpath spells a file reached
// through a descriptor under /dev/fd by a name that may reach no file or
// another descriptor, so such an answer is no spelling.
func inDescriptorTable(p string) bool {
	if runtime.GOOS == "windows" || !strings.HasPrefix(p, "/dev/") && !strings.HasPrefix(p, "/proc/") {
		return false
	}
	for dir := p; ; {
		if Descriptor(dir) || sameFile(dir, "/dev/fd") || procDescriptorTable(dir) {
			return true
		}
		up := Parent(dir)
		if up == dir {
			return false
		}
		dir = up
	}
}

// reachesDescriptor reports whether the chain of symbolic links at p ends at, or
// passes through, a descriptor path. The host names such a descriptor's file by
// a path that may reach no file or another file, so only p is sure to reach it.
func reachesDescriptor(p string) bool {
	if runtime.GOOS == "windows" {
		return false
	}
	for hops := 0; hops <= maxLinkHops; hops++ {
		info, err := os.Lstat(p)
		if err != nil || info.Mode()&fs.ModeSymlink == 0 {
			return false
		}
		next, err := Hop(p)
		if err != nil {
			return false
		}
		if Descriptor(next) {
			return true
		}
		p = next
	}
	return false
}

// DescriptorNumber returns the number of the process's own descriptor that path
// names, as [Descriptor] judges the path: 0, 1 and 2 for /dev/stdin,
// /dev/stdout and /dev/stderr, and N for /dev/fd/N. A descriptor table of
// another process names no descriptor of this one; a table of one of its
// threads names the process's own ([ownDescriptorTable]).
func DescriptorNumber(path string) (int, bool) {
	if runtime.GOOS == "windows" {
		return 0, false
	}
	dir := Parent(path)
	switch base := filepath.Base(path); base {
	case "stdin", "stdout", "stderr":
		return slices.Index([]string{"stdin", "stdout", "stderr"}, base), sameFile(dir, "/dev")
	default:
		n, _, ok := descriptorDigits(base)
		return n, ok && ownDescriptorTable(dir)
	}
}

// tableName returns the name of the descriptor table dir: /dev/fd for the one
// /dev/fd reaches, and dir as typed for any other procfs table, as
// [descriptorName] names the tables' entries. So a missing entry is named as an
// existing one is, never by the process ID the host spells /proc/self with.
func tableName(dir string) (string, bool) {
	switch {
	case sameFile(dir, "/dev/fd"):
		return "/dev/fd", true
	case procDescriptorTable(dir):
		return dir, true
	}
	return "", false
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
