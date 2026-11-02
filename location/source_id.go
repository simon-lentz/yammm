package location

import (
	"errors"
	"fmt"
	"path"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// SourceID identifies a source within a build. File-backed identities compare
// up to NFC, and synthetic ones as written.
//
// A SourceID can represent:
//   - File-backed source: Created via SourceIDFromPath or ResolveSourcePath
//   - Synthetic source: Created via NewSourceID, such as
//     "<stdin>", "inline:test", or "test://unit/person.yammm"
//
// For file-backed sources, SourceID stores the CanonicalPath directly (not as
// a string). This ensures that CanonicalPath() returns the actual stored value
// without reconstruction.
//
// SourceID is a value type with unexported fields. Always pass by value.
// The zero value is invalid; use IsZero() to check.
//
// SourceID is comparable and safe for use as map keys. Equality is structural
// (field-wise comparison).
type SourceID struct {
	cp        CanonicalPath
	synthetic string
}

// NewSourceID creates a SourceID for synthetic (non-file) sources.
//
// It does not validate the identifier: an empty one gives a zero SourceID, and
// an absolute path collides with a file-backed SourceID. A caller that takes the
// identifier from outside judges it first with [ValidateSyntheticSourceID].
//
// Recommended synthetic identifier patterns:
//   - test://unit/person.yammm (unit tests)
//   - inline:fixture_schema (inline schemas)
//   - embedded://app/builtin.yammm (embedded content)
//   - <stdin> (standard input)
func NewSourceID(identifier string) SourceID {
	return SourceID{synthetic: identifier}
}

// ValidateSyntheticSourceID reports whether an identifier is safe as a
// synthetic SourceID: not empty ([ErrEmptySourceID]), valid UTF-8
// ([ErrInvalidUTF8Path]), holding only characters [strconv.IsPrint] reports
// printable ([ErrUnprintableSourceID]), and not shaped like an absolute file
// path ([ErrAbsolutePathSourceID]).
func ValidateSyntheticSourceID(identifier string) error {
	if identifier == "" {
		return ErrEmptySourceID
	}
	if !utf8.ValidString(identifier) {
		return fmt.Errorf("%w: %q", ErrInvalidUTF8Path, identifier)
	}
	for _, r := range identifier {
		if !strconv.IsPrint(r) {
			return fmt.Errorf("%w: %q holds %U", ErrUnprintableSourceID, identifier, r)
		}
	}
	if looksLikeAbsolute(identifier) {
		return fmt.Errorf("%w: %q; it starts as a file-backed source's path does: with a slash, two backslashes, or a drive letter, a colon and a separator", ErrAbsolutePathSourceID, identifier)
	}
	return nil
}

// NormalizeSyntheticKey returns key in the one form a synthetic root joins:
// slash-separated text, cleaned as [path.Clean] cleans it, in NFC, with a
// leading ".." kept for a source outside the root. It refuses a key holding a
// backslash, and a key whose raw or normalized form [ValidateSyntheticSourceID]
// refuses or that names the root or a directory above it rather than a file.
// A normalized key is a fixed point, and its form does not depend on the host.
func NormalizeSyntheticKey(key string) (string, error) {
	if key != "" {
		if err := ValidateSyntheticSourceID(key); errors.Is(err, ErrAbsolutePathSourceID) {
			return "", fmt.Errorf("source key %q must be relative to the synthetic root: %w", key, err)
		} else if err != nil {
			return "", fmt.Errorf("source key %q: %w", key, err)
		}
	}
	// One host reads a backslash as a separator and another as part of a file
	// name, so no single reading of such a key holds on every host.
	if strings.ContainsRune(key, '\\') {
		return "", fmt.Errorf("source key %q holds a backslash; a synthetic key separates its segments with / alone", key)
	}
	normalized := norm.NFC.String(path.Clean(key)) // "" cleans to "."
	switch {
	case normalized == ".":
		return "", fmt.Errorf("source key %q resolves to the synthetic root itself", key)
	case normalized == ".." || strings.HasSuffix(normalized, "/.."):
		return "", fmt.Errorf("source key %q names a directory above the synthetic root, not a file", key)
	}
	// Cleaning and NFC can each make a key look absolute: "./C:/x.yammm" cleans
	// to "C:/x.yammm", and NFC maps U+212A KELVIN SIGN to "K".
	if err := ValidateSyntheticSourceID(normalized); err != nil {
		return "", fmt.Errorf("source key %q must be relative to the synthetic root once normalized to %q: %w", key, normalized, err)
	}
	return normalized, nil
}

// SourceIDFromPath returns the file-backed identity of path, derived as
// [NewCanonicalPath] derives it. Two spellings that resolve to one host path give
// one identity. A path that does not exist yet is allowed.
func SourceIDFromPath(path string) (SourceID, error) {
	cp, err := NewCanonicalPath(path)
	if err != nil {
		return SourceID{}, fmt.Errorf("create source ID from path %q: %w", path, err)
	}
	return SourceID{cp: cp}, nil
}

// String returns the source identifier.
//
// For file-backed sources, returns the CanonicalPath string.
// For synthetic sources, returns the synthetic identifier.
func (s SourceID) String() string {
	if s.synthetic != "" {
		return s.synthetic
	}
	return s.cp.String()
}

// RelativeTo returns s written relative to root, and true, when s is a
// file-backed source under root. Both are identities, so they are compared
// segment by segment, and a root of "/" holds every absolute path. A synthetic
// source, a source outside root or equal to it, and a zero root return false.
func (s SourceID) RelativeTo(root CanonicalPath) (string, bool) {
	if s.cp.IsZero() || root.IsZero() {
		return "", false
	}
	base := root.path
	if !strings.HasSuffix(base, "/") {
		base += "/"
	}
	rel, ok := strings.CutPrefix(s.cp.path, base)
	if !ok || rel == "" {
		return "", false
	}
	return rel, true
}

// Rel returns s written relative to the directory dir, climbing with ".."
// segments where s is not under dir, and true. Both are identities, so they are
// compared segment by segment, never through the host. A synthetic or zero
// source, a zero dir, s equal to dir or to one of its ancestors, and a source
// on another drive or network share than dir return false. Under dir it
// returns what [SourceID.RelativeTo] returns.
func (s SourceID) Rel(dir CanonicalPath) (string, bool) {
	if s.cp.IsZero() || dir.IsZero() {
		return "", false
	}
	sv, srest := splitVolume(s.cp.path)
	dv, drest := splitVolume(dir.path)
	if sv != dv {
		return "", false
	}
	from, to := segments(drest), segments(srest)
	common := 0
	for common < len(from) && common < len(to) && from[common] == to[common] {
		common++
	}
	if common == len(to) {
		return "", false
	}
	parts := make([]string, 0, len(from)-common+len(to)-common)
	for range from[common:] {
		parts = append(parts, "..")
	}
	parts = append(parts, to[common:]...)
	return strings.Join(parts, "/"), true
}

// splitVolume splits an identity into its volume — a network share
// "//server/share", a drive "C:", or "" — and the rest of the path.
func splitVolume(p string) (volume, rest string) {
	if strings.HasPrefix(p, "//") {
		i := strings.IndexByte(p[2:], '/')
		if i < 0 {
			return p, ""
		}
		j := strings.IndexByte(p[2+i+1:], '/')
		if j < 0 {
			return p, ""
		}
		return p[:2+i+1+j], p[2+i+1+j:]
	}
	if len(p) >= 2 && isLetter(p[0]) && p[1] == ':' {
		return p[:2], p[2:]
	}
	return "", p
}

// segments splits a path's non-empty segments.
func segments(p string) []string {
	return strings.FieldsFunc(p, func(r rune) bool { return r == '/' })
}

// IsZero reports whether this is a zero-value SourceID.
// The zero value is invalid and should not be used.
func (s SourceID) IsZero() bool {
	return s.cp.IsZero() && s.synthetic == ""
}

// IsFilePath reports whether this SourceID represents a file-backed source.
func (s SourceID) IsFilePath() bool {
	return !s.cp.IsZero()
}

// CanonicalPath returns the underlying CanonicalPath if this is a file-backed
// source. Returns ok=false for synthetic sources.
//
// This method returns the actual stored CanonicalPath—no reconstruction from string.
func (s SourceID) CanonicalPath() (CanonicalPath, bool) {
	if s.cp.IsZero() {
		return CanonicalPath{}, false
	}
	return s.cp, true
}
