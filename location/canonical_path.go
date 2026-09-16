package location

import (
	"fmt"
	"path/filepath"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// CanonicalPath is a file-backed source identity: an absolute, clean,
// NFC-normalized path written with forward slashes, spelled as the filesystem
// spells it. It is an identity, not a path to open; the package documentation
// states the rules it follows. The zero value is invalid; use IsZero to check.
type CanonicalPath struct {
	path string
}

// NewCanonicalPath is the identity of the host path [ResolveHostPath] returns
// for p: absolute, clean, symlink-resolved and spelled on disk, then NFC with
// forward slashes. It fails where ResolveHostPath does.
func NewCanonicalPath(p string) (CanonicalPath, error) {
	host, err := resolveHostPath(p, false)
	if err != nil {
		return CanonicalPath{}, fmt.Errorf("canonicalize path %q: %w", p, err)
	}
	return identityOf(host), nil
}

// MustCanonicalPath is like NewCanonicalPath but panics on error.
// Use only in initialization code where the path is known to be valid.
func MustCanonicalPath(p string) CanonicalPath {
	cp, err := NewCanonicalPath(p)
	if err != nil {
		panic("location.MustCanonicalPath: " + err.Error())
	}
	return cp
}

// String returns the canonical path string.
// This is the only way to extract the path value.
func (c CanonicalPath) String() string {
	return c.path
}

// IsZero reports whether this is a zero-value CanonicalPath (empty path).
// The zero value is invalid and should not be used.
func (c CanonicalPath) IsZero() bool {
	return c.path == ""
}

// Dir returns the directory of c as a CanonicalPath, cleaning c first. At a
// volume root it returns the root, and for the zero value the zero value.
func (c CanonicalPath) Dir() CanonicalPath {
	if c.IsZero() {
		return CanonicalPath{}
	}
	return identityOf(filepath.Dir(filepath.Clean(filepath.FromSlash(c.path))))
}

// Join appends path elements to c and returns the identity of the result,
// resolved as [NewCanonicalPath] resolves a path. An element that is not
// relative on the host returns [ErrAbsoluteJoinElement]; on Windows that
// includes a rooted `\x` or `/x` and a drive-relative `C:x`, and on Unix a
// backslash is part of a file name. An element that is not valid UTF-8 returns
// [ErrInvalidUTF8Path]. For the zero value it returns the zero value.
func (c CanonicalPath) Join(elem ...string) (CanonicalPath, error) {
	if c.IsZero() {
		return CanonicalPath{}, nil
	}
	parts := make([]string, 0, len(elem)+1)
	parts = append(parts, filepath.FromSlash(c.path))
	for _, e := range elem {
		if !utf8.ValidString(e) {
			return CanonicalPath{}, fmt.Errorf("%w: %q", ErrInvalidUTF8Path, e)
		}
		if filepath.IsAbs(e) || hostRootsElement(e) {
			return CanonicalPath{}, fmt.Errorf("%w: %s; use relative path or NewCanonicalPath for absolute paths", ErrAbsoluteJoinElement, e)
		}
		parts = append(parts, filepath.FromSlash(e))
	}
	return NewCanonicalPath(filepath.Join(parts...))
}

// hostRootsElement reports whether the host resolves e against something other
// than the path it is joined to: on Windows a leading separator roots e at the
// current drive, and a volume name selects a drive. Unix has no such element.
func hostRootsElement(e string) bool {
	if filepath.Separator != '\\' || e == "" {
		return false
	}
	return e[0] == '\\' || e[0] == '/' || filepath.VolumeName(e) != ""
}

// identityOf writes a resolved host path as an identity: NFC, with forward
// slashes, and a network share's root with its trailing separator as a drive's
// root has one, because filepath.Clean keeps `\\server\share\` and
// `\\server\share` as two spellings of one directory.
func identityOf(host string) CanonicalPath {
	if vol := filepath.VolumeName(host); len(vol) > 2 && host == vol {
		host += string(filepath.Separator)
	}
	return CanonicalPath{path: filepath.ToSlash(norm.NFC.String(host))}
}

// looksLikeAbsolute reports whether s looks like an absolute path on any host:
// a leading '/', a backslash UNC prefix, or a Windows volume root. It is the
// shape [ValidateSyntheticSourceID] refuses everywhere, so a synthetic
// identifier cannot collide with a file-backed one minted on another host.
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
