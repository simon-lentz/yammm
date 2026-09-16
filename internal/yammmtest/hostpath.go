package yammmtest

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
)

// windowsVolume is the drive HostAbs and FileURI put a fixture on under
// Windows. Pure path and URI functions never touch the disk, so it need not
// exist.
const windowsVolume = "C:"

// HostAbs returns a slash-separated fixture path in this host's absolute form:
// unchanged on Unix, and on Windows on the windowsVolume drive with backslash
// separators, since a Windows path with no volume is not absolute. It panics
// when slashPath does not start with '/', which no host reads as absolute.
func HostAbs(slashPath string) string {
	mustBeRooted("HostAbs", slashPath)
	if runtime.GOOS == "windows" {
		return windowsVolume + filepath.FromSlash(slashPath)
	}
	return slashPath
}

// FileURI returns the file URI naming HostAbs of uriPath unescaped. uriPath is
// slash-separated and already escaped for a URI: "/a%20b.yammm" names
// HostAbs("/a b.yammm"). It panics when uriPath does not start with '/', which
// would name a host.
func FileURI(uriPath string) string {
	mustBeRooted("FileURI", uriPath)
	if runtime.GOOS == "windows" {
		return "file:///" + windowsVolume + uriPath
	}
	return "file://" + uriPath
}

// mustBeRooted panics rather than hand a fixture a path that is relative on
// Unix and drive-relative on Windows.
func mustBeRooted(helper, slashPath string) {
	if !strings.HasPrefix(slashPath, "/") {
		panic(fmt.Sprintf("yammmtest.%s(%q): the fixture path must start with /", helper, slashPath))
	}
}
