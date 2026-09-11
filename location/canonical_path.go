package location

import (
	"fmt"
	"path/filepath"
	"strings"
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
// forward slashes. A path that does not exist yet keeps its missing tail as
// typed; a resolution that fails for any other reason is returned.
func NewCanonicalPath(p string) (CanonicalPath, error) {
	canonical, err := canonicalize(p, false, symlinksBestEffort)
	if err != nil {
		return CanonicalPath{}, fmt.Errorf("canonicalize path %q: %w", p, err)
	}
	return CanonicalPath{path: canonical}, nil
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
	dir := filepath.Dir(filepath.Clean(filepath.FromSlash(c.path)))
	return CanonicalPath{path: filepath.ToSlash(norm.NFC.String(dir))}
}

// Join appends path elements to c by the host's path rules and returns the
// cleaned result. It is lexical: symlinks in the result are not resolved. An
// element that is not relative on the host returns [ErrAbsoluteJoinElement];
// on Windows that includes a rooted `\x` and a drive-relative `C:x`. On Unix a
// backslash in an element is part of a file name. For the zero value it returns
// the zero value, as [CanonicalPath.Dir] does.
func (c CanonicalPath) Join(elem ...string) (CanonicalPath, error) {
	if c.IsZero() {
		return CanonicalPath{}, nil
	}
	parts := make([]string, 0, len(elem)+1)
	parts = append(parts, filepath.FromSlash(c.path))
	for _, e := range elem {
		if looksLikeAbsolute(e) || hostRootsElement(e) {
			return CanonicalPath{}, fmt.Errorf("%w: %s; use relative path or NewCanonicalPath for absolute paths", ErrAbsoluteJoinElement, e)
		}
		parts = append(parts, filepath.FromSlash(e))
	}
	return CanonicalPath{path: filepath.ToSlash(norm.NFC.String(filepath.Join(parts...)))}, nil
}

// hostRootsElement reports whether the host resolves e against something other
// than the path it is joined to: on Windows a leading backslash roots e at the
// current drive, and a volume name selects a drive. Unix has no such element.
func hostRootsElement(e string) bool {
	if filepath.Separator != '\\' {
		return false
	}
	return strings.HasPrefix(e, `\`) || filepath.VolumeName(e) != ""
}

// looksLikeAbsolute reports whether s looks like an absolute filesystem
// path: Unix absolute (a leading '/', which also covers forward-slash UNC),
// UNC with backslashes, or a Windows volume root (C:/ or C:\).
//
// Two guards share this predicate: [CanonicalPath.Join] rejects absolute
// elements (joining one would produce a nonsensical path — the caller
// should use NewCanonicalPath), and [ValidateSyntheticSourceID] rejects
// synthetic identifiers that could collide with file-backed SourceIDs.
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

// symlinkMode selects what canonicalize asks the filesystem.
type symlinkMode int

const (
	// symlinksNone touches no filesystem, and pairs with requireAbs.
	symlinksNone symlinkMode = iota
	// symlinksBestEffort spells the path on disk as far as it exists.
	symlinksBestEffort
	// symlinksStrict fails for a path whose leaf does not exist.
	symlinksStrict
)

// canonicalize is the one rule behind every file-backed identity: the host path
// the filesystem answers with, then NFC and forward slashes. On Unix a
// backslash is a file-name character; only Windows reads it as a separator.
func canonicalize(p string, requireAbs bool, links symlinkMode) (string, error) {
	if !utf8.ValidString(p) {
		return "", fmt.Errorf("%w: %q", ErrInvalidUTF8Path, p)
	}
	var abs string
	if requireAbs {
		if !filepath.IsAbs(p) {
			return "", fmt.Errorf("%w: %q", ErrNotAbsolute, p)
		}
		abs = filepath.Clean(p)
	} else {
		resolved, err := resolveHostPath(p, links == symlinksStrict)
		if err != nil {
			return "", err
		}
		abs = resolved
	}
	return filepath.ToSlash(norm.NFC.String(abs)), nil
}

// isLetter reports whether c is an ASCII letter.
func isLetter(c byte) bool {
	return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
}
