//go:build windows

package hostpath

import (
	"errors"
	"io/fs"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

// The dwFlags of GetFinalPathNameByHandleW, from fileapi.h.
const (
	fileNameNormalized = 0x0
	fileNameOpened     = 0x8
	volumeNameDOS      = 0x0
	volumeNameGUID     = 0x1
)

// spellOnDisk returns the existing path p as the filesystem spells it: the
// final path of a handle opened with no access, which the kernel resolved
// through every link and mount point. The package documentation states why
// filepath.EvalSymlinks is not the answer here. A file p stops naming while the
// handle is open answers as missing, as Unix answers it.
func spellOnDisk(p string) (string, error) {
	name, err := syscall.UTF16PtrFromString(lookupName(p))
	if err != nil {
		return "", &fs.PathError{Op: "final path", Path: p, Err: err}
	}
	for range maxSpellings {
		final, id, err := spellHandle(name, p)
		if err != nil {
			return "", err
		}
		// A handle outlives its file's name: NTFS gives a file removed after
		// the open a final path under \$Extend\$Deleted, and a replaced one
		// keeps the old file's name. So the answer stands only while p names
		// the file the handle holds; p is opened as the handle was, so any
		// failure now means the file left between the two opens.
		now, err := openID(name, p)
		switch {
		case err != nil:
			return "", &fs.PathError{Op: "final path", Path: p, Err: windows.ERROR_FILE_NOT_FOUND}
		case now == id:
			return final, nil
		}
	}
	return "", &fs.PathError{Op: "final path", Path: p, Err: errReplacedEachSpelling}
}

// maxSpellings bounds the spellings of a path whose file a writer replaces
// between each open and the check after it.
const maxSpellings = 16

var errReplacedEachSpelling = errors.New("the file was replaced during each spelling")

// fileID is a file's identity on its volume, as os.SameFile compares it.
type fileID struct {
	volume, high, low uint32
}

// spellHandle opens name, the lookup form of p, and returns the final path of
// the handle and the identity of the file it holds.
func spellHandle(name *uint16, p string) (string, fileID, error) {
	h, err := openNoAccess(name, p)
	if err != nil {
		return "", fileID{}, err
	}
	defer windows.CloseHandle(h) //nolint:errcheck // a handle opened with no access holds nothing to flush

	final, err := finalPath(h, p, fileNameNormalized|volumeNameDOS)
	switch {
	case errors.Is(err, windows.ERROR_PATH_NOT_FOUND):
		// The volume has no drive letter, so its GUID path is its name.
		final, err = finalPath(h, p, fileNameNormalized|volumeNameGUID)
	case errors.Is(err, windows.ERROR_ACCESS_DENIED):
		// SMB cannot normalize a component the user may not query.
		final, err = finalPath(h, p, fileNameOpened|volumeNameDOS)
	}
	if err != nil {
		return "", fileID{}, err
	}
	id, err := handleID(h, p)
	if err != nil {
		return "", fileID{}, err
	}
	return dropExtendedPrefix(final), id, nil
}

// openID returns the identity of the file name, the lookup form of p, opens to.
func openID(name *uint16, p string) (fileID, error) {
	h, err := openNoAccess(name, p)
	if err != nil {
		return fileID{}, err
	}
	defer windows.CloseHandle(h) //nolint:errcheck // a handle opened with no access holds nothing to flush
	return handleID(h, p)
}

// openNoAccess opens name, the lookup form of p, with no access and every
// share mode, following links, as a directory or a file.
func openNoAccess(name *uint16, p string) (windows.Handle, error) {
	const share = windows.FILE_SHARE_READ | windows.FILE_SHARE_WRITE | windows.FILE_SHARE_DELETE
	h, err := windows.CreateFile(name, 0, share, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return 0, &fs.PathError{Op: "open", Path: p, Err: err}
	}
	return h, nil
}

func handleID(h windows.Handle, p string) (fileID, error) {
	var d windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(h, &d); err != nil {
		return fileID{}, &fs.PathError{Op: "final path", Path: p, Err: err}
	}
	return fileID{volume: d.VolumeSerialNumber, high: d.FileIndexHigh, low: d.FileIndexLow}, nil
}

// finalPath is GetFinalPathNameByHandleW for the handle h opened on p, with
// its buffer grown to the size it asks for.
func finalPath(h windows.Handle, p string, flags uint32) (string, error) {
	var size uint32 = 260
	for {
		buf := make([]uint16, size)
		n, err := windows.GetFinalPathNameByHandle(h, &buf[0], size, flags)
		if err != nil {
			return "", &fs.PathError{Op: "final path", Path: p, Err: err}
		}
		if n < size {
			return syscall.UTF16ToString(buf[:n]), nil
		}
		size = n
	}
}

// dropExtendedPrefix writes a final path in the form every Windows API reads:
// \\?\C:\x as C:\x and \\?\UNC\host\share as \\host\share. A volume GUID path
// keeps its prefix, as os.Readlink keeps it.
func dropExtendedPrefix(p string) string {
	const prefix = `\\?\`
	rest, ok := strings.CutPrefix(p, prefix)
	switch {
	case !ok:
		return p
	case len(rest) >= 2 && rest[1] == ':':
		return rest
	case strings.HasPrefix(rest, `UNC\`):
		return `\\` + rest[len(`UNC\`):]
	default:
		return p
	}
}

// lookupName returns the extended form of an absolute path, \\?\ and the path
// cleaned, or \\?\UNC\ and a share's path, which Win32 hands to the
// filesystem as it is. The walk's path was normalized once, by filepath.Abs or
// by lexicalTarget, so no later lookup trims an element's trailing dots or
// spaces. go1.26's os adds the prefix only past 248 bytes, and not at all where
// the process may use long paths, so whether its lookup trims them depends on
// the path's length and the host's setting. A relative path, and one in a
// device or extended form already, is returned as it is.
func lookupName(p string) string {
	if !filepath.IsAbs(p) || prefixed(p) {
		return p
	}
	c := filepath.Clean(p)
	if rest, ok := strings.CutPrefix(c, `\\`); ok {
		return `\\?\UNC\` + rest
	}
	return `\\?\` + c
}

// prefixed reports whether p is in a device or extended form already: \??\, or
// \\?\ or \\.\ written with either separator, as os reads an extended prefix.
func prefixed(p string) bool {
	if len(p) < 4 {
		return false
	}
	sep := func(b byte) bool { return b == '\\' || b == '/' }
	return p[:4] == `\??\` || sep(p[0]) && sep(p[1]) && (p[2] == '?' || p[2] == '.') && sep(p[3])
}

// linkTarget is lexicalTarget: Windows evaluates a target's ".." on the text.
func linkTarget(dir, target string) (string, error) {
	return lexicalTarget(dir, target), nil
}
