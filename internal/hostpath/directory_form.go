package hostpath

import (
	"errors"
	"io/fs"
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

// RefusedWrite returns [ErrNotDirectory] when p, a path about to be written,
// names a directory ([NamesDirectory]) and the element it names, or the first
// element above it that exists, is not a directory, and when that element does
// not exist in an existing directory, where the write would create a file. It
// refuses with the host's own error an element under a directory that does not
// exist, or one its stat fails for, and returns nil for any other p. A dangling
// link is judged at the path the kernel reaches through it. Windows renames
// onto "f\" as onto "f", so a writer cannot leave this to the rename.
func RefusedWrite(p string) error {
	return refusedWriteIn(hostLookups{}, p)
}

// refusedWriteIn is [RefusedWrite] over l. It follows maxLinkHops dangling
// links and refuses the next, which a static tree never reaches, since the
// kernel answers ELOOP to a stat of a longer chain.
func refusedWriteIn(l Lookups, p string) error {
	if !NamesDirectory(p) {
		return nil
	}
	element := directoryElement(p)
	for hops := 0; ; hops++ {
		info, err := l.Stat(element)
		switch {
		case err == nil && !info.IsDir(), unixNotDirectory(err):
			return ErrNotDirectory
		case err == nil:
			return nil
		case !errors.Is(err, fs.ErrNotExist):
			return bareErr(err)
		}
		next, link := danglingHop(l, element)
		if !link {
			return absentElement(l, element, err)
		}
		if hops == maxLinkHops {
			return errTooManyLinks
		}
		element = directoryElement(next)
	}
}

// absentElement judges an element that does not exist, and is no link, by the
// first element above it that exists: a non-directory there is refused, as is
// the element in an existing directory, and above a missing one the write is
// refused with err, the element's own stat failure. A stat that answered
// missing for the element reached every element above it that exists, so the
// climb meets no other stat failure.
func absentElement(l Lookups, element string, err error) error {
	parent := Parent(element)
	for dir := parent; ; {
		info, serr := l.Stat(dir)
		switch {
		case serr == nil && !info.IsDir(), serr == nil && dir == parent:
			return ErrNotDirectory
		case serr == nil:
			return bareErr(err)
		}
		up := Parent(dir)
		if up == dir {
			return bareErr(err)
		}
		dir = up
	}
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
