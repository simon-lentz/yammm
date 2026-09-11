package location

import (
	"fmt"
)

// SourceID identifies a source uniquely within a build.
//
// A SourceID can represent:
//   - File-backed source: Created via SourceIDFromPath, SourceIDFromCanonicalPath,
//     or SourceIDFromAbsolutePath
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

// ValidateSyntheticSourceID validates that an identifier is safe for use as
// a synthetic SourceID.
//
// Returns an error if the identifier:
//   - Is empty ([ErrEmptySourceID])
//   - Resembles an absolute file path ([ErrAbsolutePathSourceID])
//
// This is called automatically by MustNewSourceID.
func ValidateSyntheticSourceID(identifier string) error {
	if identifier == "" {
		return ErrEmptySourceID
	}
	if looksLikeAbsolute(identifier) {
		return fmt.Errorf("%w: %q; use a scheme prefix (e.g., test://, inline:) to avoid collision with file-backed sources", ErrAbsolutePathSourceID, identifier)
	}
	return nil
}

// SourceIDFromPath canonicalizes the path via NewCanonicalPath (including
// symlink resolution) and creates a file-backed SourceID.
//
// Use for normal file loading scenarios.
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

// SourceIDFromAbsolutePath creates a file-backed SourceID from an absolute
// path without touching the filesystem: it cleans the path by the host's rules,
// applies NFC and writes forward slashes, but resolves no symlink. It returns
// [ErrNotAbsolute] for a path the host does not call absolute. Use it for
// in-memory Sources keys; for a path with symlinks, derive the key with
// CanonicalizePathForSourceID so it matches what Load produces.
func SourceIDFromAbsolutePath(absPath string) (SourceID, error) {
	canonical, err := canonicalize(absPath, true, symlinksNone)
	if err != nil {
		return SourceID{}, fmt.Errorf("create source ID from absolute path %q: %w", absPath, err)
	}
	return SourceID{cp: CanonicalPath{path: canonical}}, nil
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
// NewCanonicalPath it requires symlink resolution to succeed, so it fails for
// a path that does not exist; its result is what SourceIDFromPath produces.
func CanonicalizePathForSourceID(path string) (string, error) {
	canonical, err := canonicalize(path, false, symlinksStrict)
	if err != nil {
		return "", fmt.Errorf("canonicalize path for source ID: %w", err)
	}
	return canonical, nil
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
