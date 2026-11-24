package hostpath

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// ErrNotDirectory refuses a path whose last element must be a directory and is
// not. errors.Is matches it to syscall.ENOTDIR, and it reads "not a directory"
// on every host. syscall.ENOTDIR itself is ERROR_PATH_NOT_FOUND on Windows,
// which reads as a missing path and matches fs.ErrNotExist for a file that
// exists.
var ErrNotDirectory error = notDirectory{}

type notDirectory struct{}

func (notDirectory) Error() string { return "not a directory" }

func (notDirectory) Is(target error) bool { return target == syscall.ENOTDIR }

// RefusedRead returns [ErrNotDirectory] when p's spelling names a directory
// ([NamesDirectory]) and the element it names, or the first element above it
// that exists, is not a directory, and nil otherwise. A dangling link is judged
// at the path the kernel reaches through it. A caller checks it before it opens
// p, so the refusal reads the same on every host.
func RefusedRead(p string) error {
	return refused(p, false)
}

// RefusedWrite is [RefusedRead] for a path about to be written. It also
// refuses an element that does not exist in an existing directory, where the
// write would create a file, and refuses with the host's own error an element
// under a directory that does not exist, or one its stat fails for. Windows
// renames onto "f\" as onto "f", so a writer cannot leave this to the rename.
func RefusedWrite(p string) error {
	return refused(p, true)
}

// refused judges the element p names with p's directory-form ending removed
// and nothing else cleaned, so a ".." in a link target is taken as the kernel
// takes it. A Unix stat answers ENOTDIR for an element under a non-directory.
// Windows answers ERROR_PATH_NOT_FOUND, its ENOTDIR, which matches
// fs.ErrNotExist, so there an absent element is judged by the first element
// above it that exists.
func refused(p string, write bool) error {
	if !NamesDirectory(p) {
		return nil
	}
	element := directoryElement(p)
	for hops := 0; ; hops++ {
		info, err := os.Stat(element)
		switch {
		case err == nil && !info.IsDir(), unixNotDirectory(err):
			return ErrNotDirectory
		case err == nil:
			return nil
		case !errors.Is(err, fs.ErrNotExist):
			return writeRefusal(write, err)
		}
		next, link := danglingLink(element)
		if !link {
			return absentElement(element, err, write)
		}
		if hops == maxLinkHops {
			return writeRefusal(write, errTooManyLinks)
		}
		element = directoryElement(next)
	}
}

// absentElement judges an element that does not exist, and is no link, by the
// first element above it that exists: a non-directory there is refused, and a
// write is refused in an existing directory and, above a missing one, with
// err, the element's own stat failure. A stat that answered missing for the
// element reached every element above it that exists, so the climb meets no
// other stat failure.
func absentElement(element string, err error, write bool) error {
	parent := Parent(element)
	for dir := parent; ; {
		info, serr := os.Stat(dir)
		switch {
		case serr == nil && !info.IsDir():
			return ErrNotDirectory
		case serr == nil && dir == parent && write:
			return ErrNotDirectory
		case serr == nil:
			return writeRefusal(write, err)
		}
		up := Parent(dir)
		if up == dir {
			return writeRefusal(write, err)
		}
		dir = up
	}
}

// danglingLink returns the path the kernel reaches through the link at
// element, when element is one. An empty target reaches nothing, as
// [Resolve] reads it.
func danglingLink(element string) (string, bool) {
	info, err := os.Lstat(element)
	if err != nil || info.Mode()&fs.ModeSymlink == 0 {
		return "", false
	}
	dest, err := os.Readlink(element)
	if err != nil || dest == "" {
		return "", false
	}
	return FollowLink(element, dest), true
}

// writeRefusal is err, unwrapped, for a write, and nil for a read, which
// leaves the failure to the caller's own open.
func writeRefusal(write bool, err error) error {
	if !write {
		return nil
	}
	return bareErr(err)
}

// directoryElement returns p without the trailing separators and "." elements
// that put it in directory form. A root is kept whole.
func directoryElement(p string) string {
	vol := filepath.VolumeName(p)
	rest := p[len(vol):]
	for {
		trimmed := strings.TrimRightFunc(rest, isSeparator)
		switch trimmed {
		case "":
			return vol + rest[:min(len(rest), 1)]
		case ".":
			return vol + trimmed
		}
		i := strings.LastIndexFunc(trimmed, isSeparator)
		if trimmed[i+1:] != "." {
			return vol + trimmed
		}
		rest = trimmed[:i+1]
	}
}

func isSeparator(r rune) bool { return r == '/' || r == filepath.Separator }

// unixNotDirectory reports whether a stat failed with Unix's ENOTDIR: an element
// of the path exists and is not a directory. Windows' ENOTDIR also matches
// fs.ErrNotExist, so it says only that the path is missing.
func unixNotDirectory(err error) bool {
	return errors.Is(err, syscall.ENOTDIR) && !errors.Is(err, fs.ErrNotExist)
}

// bareErr returns the error a path error wraps, so a caller names the path as
// it was given.
func bareErr(err error) error {
	if pe, ok := errors.AsType[*fs.PathError](err); ok {
		return pe.Err
	}
	return err
}
