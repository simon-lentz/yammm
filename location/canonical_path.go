package location

import (
	"path/filepath"

	"golang.org/x/text/unicode/norm"
)

// canonicalPath is a file-backed source identity: an absolute path in NFC,
// written with forward slashes. It is an identity, not a path to open; the
// package documentation states the rules it follows. The zero value is no
// identity.
type canonicalPath struct {
	path string
}

// String returns the identity's text.
func (c canonicalPath) String() string {
	return c.path
}

// IsZero reports whether c is the zero value.
func (c canonicalPath) IsZero() bool {
	return c.path == ""
}

// identityOf writes a resolved host path as an identity: NFC, with forward
// slashes, and a network share's root with its trailing separator as a drive's
// root has one, because filepath.Clean keeps `\\server\share\` and
// `\\server\share` as two spellings of one directory.
func identityOf(host string) canonicalPath {
	if vol := filepath.VolumeName(host); len(vol) > 2 && host == vol {
		host += string(filepath.Separator)
	}
	return canonicalPath{path: filepath.ToSlash(norm.NFC.String(host))}
}

// looksLikeAbsolute reports whether s starts as an absolute path does on some
// host: a '/', two backslashes, or an ASCII letter, a colon and a separator.
// [NewSourceID] refuses it on every host, so a synthetic identifier cannot
// collide with a file-backed one or read as a host path.
func looksLikeAbsolute(s string) bool {
	if len(s) == 0 {
		return false
	}
	// Unix absolute or UNC with forward slashes
	if s[0] == '/' {
		return true
	}
	// UNC with backslashes
	if len(s) >= 2 && s[0] == '\\' && s[1] == '\\' {
		return true
	}
	// Windows volume: C:/ or C:\
	if len(s) >= 3 && isLetter(s[0]) && s[1] == ':' && (s[2] == '/' || s[2] == '\\') {
		return true
	}
	return false
}

// isLetter reports whether c is an ASCII letter.
func isLetter(c byte) bool {
	return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
}
