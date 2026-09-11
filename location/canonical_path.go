package location

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"

	"golang.org/x/text/unicode/norm"
)

// CanonicalPath is a file-backed source identity: an absolute, clean,
// NFC-normalized path written with forward slashes, whose symlinks
// NewCanonicalPath resolves when the path exists. It is an identity, not a path
// to open; the package documentation states the rules it follows. The zero
// value is invalid; use IsZero to check.
type CanonicalPath struct {
	path string
}

// NewCanonicalPath canonicalizes p by the host's path rules: it makes p
// absolute and clean, resolves its symlinks when the path exists, applies NFC,
// and writes its separators as forward slashes. A path that does not exist is
// kept unresolved; any other symlink-resolution failure is returned.
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
// element that looks absolute returns [ErrAbsoluteJoinElement]. On Unix a
// backslash in an element is part of a file name.
func (c CanonicalPath) Join(elem ...string) (CanonicalPath, error) {
	if c.IsZero() {
		return CanonicalPath{}, nil
	}
	parts := make([]string, 0, len(elem)+1)
	parts = append(parts, filepath.FromSlash(c.path))
	for _, e := range elem {
		if looksLikeAbsolute(e) {
			return CanonicalPath{}, fmt.Errorf("%w: %s; use relative path or NewCanonicalPath for absolute paths", ErrAbsoluteJoinElement, e)
		}
		parts = append(parts, filepath.FromSlash(e))
	}
	return CanonicalPath{path: filepath.ToSlash(norm.NFC.String(filepath.Join(parts...)))}, nil
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

// symlinkMode selects how canonicalize treats symbolic links.
type symlinkMode int

const (
	// symlinksNone leaves links as written and touches no filesystem.
	symlinksNone symlinkMode = iota
	// symlinksBestEffort resolves links when the path exists.
	symlinksBestEffort
	// symlinksStrict resolves links and fails when resolution fails.
	symlinksStrict
)

// canonicalize is the one rule behind every file-backed identity: the host's
// own path semantics, then NFC and forward slashes. On Unix a backslash is a
// file-name character; only Windows reads it as a separator.
func canonicalize(p string, requireAbs bool, links symlinkMode) (string, error) {
	var abs string
	if requireAbs {
		if !filepath.IsAbs(p) {
			return "", fmt.Errorf("%w: %q", ErrNotAbsolute, p)
		}
		abs = filepath.Clean(p)
	} else {
		a, err := filepath.Abs(p)
		if err != nil {
			return "", fmt.Errorf("absolute path: %w", err)
		}
		abs = a
	}
	switch links {
	case symlinksNone:
	case symlinksBestEffort:
		resolved, err := filepath.EvalSymlinks(abs)
		switch {
		case err == nil:
			abs = resolved
		case !errors.Is(err, fs.ErrNotExist):
			return "", fmt.Errorf("resolve symlinks: %w", err)
		}
	case symlinksStrict:
		resolved, err := filepath.EvalSymlinks(abs)
		if err != nil {
			return "", fmt.Errorf("resolve symlinks: %w", err)
		}
		abs = resolved
	}
	return filepath.ToSlash(norm.NFC.String(abs)), nil
}

// isLetter reports whether c is an ASCII letter.
func isLetter(c byte) bool {
	return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
}
