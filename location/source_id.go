package location

import (
	"fmt"
	"path/filepath"

	"golang.org/x/text/unicode/norm"
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
	if looksLikeAbsolutePath(identifier) {
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

// SourceIDFromCanonicalPath creates a SourceID from an already-canonical path.
//
// The CanonicalPath is stored directly—no conversion to string and back.
func SourceIDFromCanonicalPath(cp CanonicalPath) SourceID {
	return SourceID{cp: cp}
}

// SourceIDFromAbsolutePath creates a file-backed SourceID using
// filesystem-independent canonicalization.
//
// This applies path.Clean() to normalize . and .. segments, NFC normalization,
// and forward-slash conversion—but NO symlink resolution. Returns error if
// path is not absolute.
//
// Use for in-memory loading scenarios (LoadSources) where filesystem access
// is unavailable or undesirable.
//
// For paths without symlinks, this produces SourceIDs equal to those from
// SourceIDFromPath. When symlinks are involved, the results may differ—use
// CanonicalizePathForSourceID() before constructing LoadSources keys to ensure
// TypeID equality.
//
// # LoadSources Key Requirements
//
// The following transformations are applied automatically:
//   - path.Clean(): Normalizes . and .. segments (/a/../b → /b)
//   - NFC normalization: NFD é (e + combining accent) → NFC é
//   - Forward-slash conversion: \ → / on Windows
//
// The following are the caller's responsibility (NOT handled internally):
//   - Case normalization: On case-insensitive filesystems (macOS HFS+/APFS,
//     Windows NTFS), paths like /Users/Simon/file.yammm and /users/simon/file.yammm
//     produce DISTINCT SourceIDs. Callers reading from case-insensitive filesystems
//     should normalize case before building the sources map if TypeID equality
//     across different key casings matters.
//   - Symlink resolution: Use CanonicalizePathForSourceID() for symlink-resolved keys.
func SourceIDFromAbsolutePath(absPath string) (SourceID, error) {
	canonical, err := canonicalizeAbsolutePath(absPath)
	if err != nil {
		return SourceID{}, fmt.Errorf("create source ID from absolute path %q: %w", absPath, err)
	}
	// Create CanonicalPath directly from the cleaned path.
	// Since canonicalizeAbsolutePath already ensures it's absolute, clean,
	// NFC-normalized, and uses forward slashes, we can safely wrap it.
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

// CanonicalizePathForSourceID resolves symlinks and returns a path suitable
// for use as a LoadSources key when TypeID equality with Load() is required.
//
// Performs strict canonicalization: absolute, cleaned, NFC-normalized,
// forward-slashes, and symlink-resolved. Unlike NewCanonicalPath (which
// provides best-effort symlink resolution for general use), this function
// requires symlink resolution to succeed—guaranteeing the result matches
// what SourceIDFromPath would produce.
//
// Returns error if:
//   - The path does not exist
//   - Symlink resolution fails (e.g., broken symlink, permission error)
//   - The current directory cannot be determined (for relative paths)
//   - Path is a UNC path ([ErrUNCPath])
func CanonicalizePathForSourceID(path string) (string, error) {
	// Get absolute path
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("canonicalize path for source ID: %w", err)
	}

	// Strictly resolve symlinks - must succeed for TypeID equality guarantee
	resolved, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		return "", fmt.Errorf("canonicalize path for source ID: resolve symlinks: %w", err)
	}

	// Apply NFC normalization
	normalized := norm.NFC.String(resolved)

	// Convert to forward slashes
	slashed := filepath.ToSlash(normalized)

	// Reject UNC paths - path.Clean would corrupt // to / causing SourceID collisions.
	// This ensures consistency with NewCanonicalPath and SourceIDFromAbsolutePath.
	if len(slashed) >= 2 && slashed[0] == '/' && slashed[1] == '/' {
		return "", fmt.Errorf("%w: %q; use a local mount point", ErrUNCPath, path)
	}

	// Apply Windows drive-root fixup
	cleaned := fixWindowsClean(slashed)

	return cleaned, nil
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
