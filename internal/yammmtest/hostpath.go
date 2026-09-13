package yammmtest

import (
	"path/filepath"
	"runtime"
)

// windowsVolume is the drive HostAbs and FileURI put a fixture on under
// Windows. Pure path and URI functions never touch the disk, so it need not
// exist.
const windowsVolume = "C:"

// HostAbs returns a slash-separated fixture path in this host's absolute form:
// unchanged on Unix, and on Windows on the windowsVolume drive with backslash
// separators, since a Windows path with no volume is not absolute.
func HostAbs(slashPath string) string {
	if runtime.GOOS == "windows" {
		return windowsVolume + filepath.FromSlash(slashPath)
	}
	return slashPath
}

// FileURI returns the file URI naming HostAbs(uriPath). uriPath is
// slash-separated and already escaped for a URI, such as "/a%20b.yammm".
func FileURI(uriPath string) string {
	if runtime.GOOS == "windows" {
		return "file:///" + windowsVolume + uriPath
	}
	return "file://" + uriPath
}
