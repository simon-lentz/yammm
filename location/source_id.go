package location

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// SourceID identifies a source uniquely within a build.
//
// A SourceID can represent:
//   - File-backed source: Created via SourceIDFromPath or ResolveSourcePath
//   - Synthetic source: Created via NewSourceID or MustNewSourceID, such as
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
// WARNING: Prefer [MustNewSourceID] for new code. NewSourceID bypasses validation,
// which can lead to subtle bugs:
//   - Empty string: Returns a zero-value SourceID (IsZero() returns true),
//     which is invalid and may cause map key anomalies.
//   - Absolute paths: Creates collisions with file-backed SourceIDs, breaking
//     the String() injectivity invariant.
//
// NewSourceID is appropriate for internal use where the identifier is known-valid
// at compile time (e.g., string literals in test code).
//
// Recommended synthetic identifier patterns:
//   - test://unit/person.yammm (unit tests)
//   - inline:fixture_schema (inline schemas)
//   - embedded://app/builtin.yammm (embedded content)
//   - <stdin> (standard input)
func NewSourceID(identifier string) SourceID {
	return SourceID{synthetic: identifier}
}

// MustNewSourceID creates a synthetic SourceID with validation.
//
// Panics if the identifier resembles an absolute file path (Unix or Windows),
// which would violate the String() injectivity invariant and cause collision
// hazards with file-backed SourceIDs.
//
// Use in application code, tests, and high-level APIs.
func MustNewSourceID(identifier string) SourceID {
	if err := ValidateSyntheticSourceID(identifier); err != nil {
		panic("location.MustNewSourceID: " + err.Error())
	}
	return SourceID{synthetic: identifier}
}

// ValidateSyntheticSourceID reports whether an identifier is safe as a
// synthetic SourceID: not empty ([ErrEmptySourceID]), not shaped like an
// absolute file path ([ErrAbsolutePathSourceID]), and valid UTF-8
// ([ErrInvalidUTF8Path]). MustNewSourceID calls it.
func ValidateSyntheticSourceID(identifier string) error {
	if identifier == "" {
		return ErrEmptySourceID
	}
	if !utf8.ValidString(identifier) {
		return fmt.Errorf("%w: %q", ErrInvalidUTF8Path, identifier)
	}
	if looksLikeAbsolute(identifier) {
		return fmt.Errorf("%w: %q; use a scheme prefix (e.g., test://, inline:) to avoid collision with file-backed sources", ErrAbsolutePathSourceID, identifier)
	}
	return nil
}

// SourceIDFromPath returns the file-backed identity of path, derived as
// [NewCanonicalPath] derives it. Every file-backed identity is minted through
// it, [ResolveSourcePath] or [CanonicalizePathForSourceID], so two spellings of
// one file give one identity. A path that does not exist yet is allowed.
func SourceIDFromPath(path string) (SourceID, error) {
	cp, err := NewCanonicalPath(path)
	if err != nil {
		return SourceID{}, fmt.Errorf("create source ID from path %q: %w", path, err)
	}
	return SourceID{cp: cp}, nil
}

// MustSourceIDFromPath is like SourceIDFromPath but panics on error.
func MustSourceIDFromPath(path string) SourceID {
	sid, err := SourceIDFromPath(path)
	if err != nil {
		panic("location.MustSourceIDFromPath: " + err.Error())
	}
	return sid
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

// CanonicalizePathForSourceID returns the canonical form of an existing path,
// for a Sources key whose TypeIDs must equal a Load of the same file. Unlike
// NewCanonicalPath it requires the path itself to exist, so it fails for one
// that does not; its result is what SourceIDFromPath produces.
func CanonicalizePathForSourceID(path string) (string, error) {
	host, err := resolveHostPath(path, true)
	if err != nil {
		return "", fmt.Errorf("canonicalize path for source ID: %w", err)
	}
	return identityOf(host).String(), nil
}

// MustCanonicalizePathForSourceID is like CanonicalizePathForSourceID but
// panics on error.
//
// Use only in initialization code where paths are known-good.
func MustCanonicalizePathForSourceID(path string) string {
	s, err := CanonicalizePathForSourceID(path)
	if err != nil {
		panic("location.MustCanonicalizePathForSourceID: " + err.Error())
	}
	return s
}
