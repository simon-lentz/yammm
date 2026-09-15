//go:build windows

package location

import (
	"errors"
	"io/fs"
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
// filepath.EvalSymlinks is not the answer here.
func spellOnDisk(p string) (string, error) {
	name, err := syscall.UTF16PtrFromString(p)
	if err != nil {
		return "", &fs.PathError{Op: "final path", Path: p, Err: err}
	}
	const share = windows.FILE_SHARE_READ | windows.FILE_SHARE_WRITE | windows.FILE_SHARE_DELETE
	h, err := windows.CreateFile(name, 0, share, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return "", &fs.PathError{Op: "open", Path: p, Err: err}
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
		return "", err
	}
	return dropExtendedPrefix(final), nil
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

// linkTarget is lexicalTarget: Windows evaluates a target's ".." on the text.
func linkTarget(dir, target string) string {
	return lexicalTarget(dir, target)
}
