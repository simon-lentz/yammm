package location

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"golang.org/x/text/unicode/norm"
)

// CanonicalPath represents a canonicalized file system path.
//
// A valid CanonicalPath is always:
//   - Absolute (not relative)
//   - Clean (no . or .. segments, no redundant slashes)
//   - NFC-normalized (Unicode Normalization Form C)
//   - Forward-slash normalized (uses "/" on all platforms)
//   - Symlink-resolved (best-effort: resolved when path exists at canonicalization time)
//
// The "best-effort symlink resolution" invariant reflects reality: NewCanonicalPath
// cannot resolve symlinks for paths that don't exist yet. Code that receives a
// CanonicalPath should not assume symlinks have been resolved. However, the Clean
// invariant is always guaranteed.
//
// CanonicalPath is a value type with an unexported field. Always pass by value.
// The zero value is invalid; use IsZero() to check.
type CanonicalPath struct {
	path string
}

// NewCanonicalPath canonicalizes the input path.
//
// Canonicalization includes:
//   - Converting to absolute path (via filepath.Abs, which calls filepath.Clean)
//   - Resolving symlinks (if the path exists)
//   - Applying NFC Unicode normalization
//   - Normalizing to forward slashes
//
// Returns an error if:
//   - filepath.Abs fails (e.g., current directory cannot be determined)
//   - Symlink resolution fails due to permission errors, symlink loops, or
//     other filesystem errors (NOT including non-existence)
//   - Path is a UNC path ([ErrUNCPath])
//
// If the path does not exist, the absolute path is used without error—this
// supports new file creation scenarios. Other EvalSymlinks errors are returned
// to the caller because they indicate real problems (permission denied, symlink
// loops) that should not be silently masked.
func NewCanonicalPath(p string) (CanonicalPath, error) {
	// Get absolute path (this also cleans . and .. segments)
	absPath, err := filepath.Abs(p)
	if err != nil {
		return CanonicalPath{}, fmt.Errorf("canonicalize path %q: %w", p, err)
	}

	// Attempt symlink resolution
	resolved, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Path doesn't exist - use absolute path (supports new file creation)
			resolved = absPath
		} else {
			// Permission denied, symlink loop, or other filesystem error
			return CanonicalPath{}, fmt.Errorf("canonicalize path %q: %w", p, err)
		}
	}

	// Apply NFC normalization
	normalized := norm.NFC.String(resolved)

	// Convert to forward slashes for cross-platform stability.
	// filepath.ToSlash only converts the native separator, which on Unix is
	// already '/'. We also need to normalize any literal backslashes that may
	// appear in path names (rare but possible on Unix) to maintain the
	// forward-slash invariant consistently.
	canonical := filepath.ToSlash(normalized)
	canonical = strings.ReplaceAll(canonical, "\\", "/")

	// Reject UNC paths - path.Clean would corrupt // to / causing SourceID collisions.
	// Example: //server/share and /server/share would both become /server/share.
	if len(canonical) >= 2 && canonical[0] == '/' && canonical[1] == '/' {
		return CanonicalPath{}, fmt.Errorf("%w: %q; use a local mount point", ErrUNCPath, p)
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

// Base returns the last element of the path (the file name).
// For the zero value, returns an empty string.
func (c CanonicalPath) Base() string {
	if c.IsZero() {
		return ""
	}
	return path.Base(c.path)
}

// Dir returns the directory portion as a CanonicalPath.
// The result maintains all CanonicalPath invariants (absolute, clean, NFC, forward slashes).
//
// The input is cleaned before taking the directory, ensuring semantic correctness
// even for non-canonical inputs (symmetric with Join's behavior).
//
// For the zero value, returns zero. For Windows paths at the drive root,
// returns the drive root (e.g., Dir("C:/") returns "C:/").
func (c CanonicalPath) Dir() CanonicalPath {
	if c.IsZero() {
		return CanonicalPath{}
	}
	// Clean input first for robustness (symmetric with Join)
	cleaned := fixWindowsClean(c.path)
	// Take directory of cleaned path
	dir := path.Dir(cleaned)
	// Apply Windows drive-root fixup (use cleaned path for volume reference)
	dir = fixWindowsPath(cleaned, dir)
	// Apply NFC normalization
	normalized := norm.NFC.String(dir)
	return CanonicalPath{path: normalized}
}

// Join appends path elements and returns a new CanonicalPath.
// The joined path is re-canonicalized to handle ".." and "." segments
// that may be introduced by the elements.
//
// IMPORTANT: Join performs purely lexical joining without symlink resolution,
// even if the joined path exists on the filesystem. If the result may contain
// symlinks that need resolution, use NewCanonicalPath(result.String()) afterward.
//
// Backslashes in elements are normalized to forward slashes to maintain
// the forward-slash invariant across all platforms.
//
// For Windows paths, ".." segments that would escape the drive root are
// clamped to the root (e.g., Join("C:/a", "..") returns "C:/").
//
// Returns [ErrAbsoluteJoinElement] if any element looks like an absolute path
// (starts with "/" or contains a Windows volume like "C:/"). Passing absolute
// paths to Join is almost always a caller bug—use NewCanonicalPath instead.
func (c CanonicalPath) Join(elem ...string) (CanonicalPath, error) {
	if c.IsZero() {
		return CanonicalPath{}, nil
	}

	// Join elements using forward slashes, normalizing backslashes in each element
	joined := c.path
	for _, e := range elem {
		// Reject absolute elements - this is almost always a caller bug.
		// If the caller has an absolute path, they should use NewCanonicalPath.
		if looksLikeAbsoluteElement(e) {
			return CanonicalPath{}, fmt.Errorf("%w: %s; use relative path or NewCanonicalPath for absolute paths", ErrAbsoluteJoinElement, e)
		}
		e = strings.ReplaceAll(e, "\\", "/")
		joined = joined + "/" + e
	}

	// Clean to handle any . or .. segments, with Windows drive-root fixup
	cleaned := fixWindowsClean(joined)

	// Apply NFC normalization to any new path elements
	normalized := norm.NFC.String(cleaned)

	return CanonicalPath{path: normalized}, nil
}

// looksLikeAbsoluteElement checks if a path element looks like an absolute path.
// Used by Join to reject elements that would produce nonsensical paths.
//
// Examples of absolute elements:
//   - "/etc/passwd" (Unix absolute)
//   - "C:/Windows" or "C:\Windows" (Windows volume)
//   - "//server/share" or "\\server\share" (UNC)
func looksLikeAbsoluteElement(e string) bool {
	if len(e) == 0 {
		return false
	}
	// Unix absolute or UNC with forward slashes
	if e[0] == '/' {
		return true
	}
	// UNC with backslashes
	if len(e) >= 2 && e[0] == '\\' && e[1] == '\\' {
		return true
	}
	// Windows volume: C:/ or C:\
	if len(e) >= 3 && isLetter(e[0]) && e[1] == ':' && (e[2] == '/' || e[2] == '\\') {
		return true
	}
	return false
}

// canonicalizeAbsolutePath performs filesystem-independent canonicalization
// of an absolute path: path.Clean() + NFC normalization + forward slashes.
// No symlink resolution is performed. Returns error if path is not absolute
// or is a UNC path.
//
// For Windows paths, this ensures the drive root is preserved (e.g., "C:/"
// stays "C:/", not "C:").
//
// UNC paths (//server/share or \\server\share) are explicitly rejected because
// path.Clean would collapse // to /, causing collisions with regular Unix paths.
// Use a local mount point instead.
//
// This is used by SourceIDFromAbsolutePath for LoadSources scenarios.
func canonicalizeAbsolutePath(absPath string) (string, error) {
	// Convert all backslashes to forward slashes for consistent handling.
	// We do this manually because filepath.ToSlash only converts the native
	// separator, which on Unix is already '/'.
	slashed := strings.ReplaceAll(absPath, "\\", "/")

	// Reject UNC paths - path.Clean would corrupt // to / causing SourceID collisions.
	// Example: //server/share and /server/share would both become /server/share.
	if len(slashed) >= 2 && slashed[0] == '/' && slashed[1] == '/' {
		return "", fmt.Errorf("%w: %q; use a local mount point", ErrUNCPath, absPath)
	}

	// Check if absolute (works for both Unix and Windows paths)
	if !isAbsolutePath(slashed) {
		return "", fmt.Errorf("%w: %q", ErrNotAbsolute, absPath)
	}

	// Clean the path with Windows drive-root fixup
	cleaned := fixWindowsClean(slashed)

	// Apply NFC normalization
	normalized := norm.NFC.String(cleaned)

	return normalized, nil
}

// isAbsolutePath checks if a forward-slash normalized path is absolute.
// Handles both Unix (/path) and Windows (C:/path) conventions.
func isAbsolutePath(p string) bool {
	if len(p) == 0 {
		return false
	}
	// Unix absolute path
	if p[0] == '/' {
		return true
	}
	// Windows absolute path: C:/ or similar
	if len(p) >= 3 && isLetter(p[0]) && p[1] == ':' && p[2] == '/' {
		return true
	}
	return false
}

// isLetter reports whether c is an ASCII letter.
func isLetter(c byte) bool {
	return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
}

// fixWindowsClean applies path.Clean and fixes Windows drive-root edge cases.
// For Windows paths (C:/...), this ensures the result is always absolute.
//
// Handles two cases:
//   - Bare drive letter: "C:" -> "C:/"
//   - Root escape: path.Clean("C:/..") = "." -> "C:/"
func fixWindowsClean(p string) string {
	cleaned := path.Clean(p)
	return fixWindowsPath(p, cleaned)
}

// fixWindowsPath corrects Windows drive-root issues after path.Clean or path.Dir.
// The input parameter is needed to recover volume information if path.Clean/Dir
// escapes the root entirely (e.g., path.Clean("C:/..") = ".").
//
// This ensures Windows paths maintain the "always absolute" invariant, matching
// Unix semantics where path.Clean("/..") = "/" (root is the ceiling).
func fixWindowsPath(input, output string) string {
	// Check if input was a Windows path (has volume prefix like "C:/")
	if len(input) < 3 || !isLetter(input[0]) || input[1] != ':' || input[2] != '/' {
		return output // Not a Windows path, no fixup needed
	}

	drive := input[0]

	// Case 1: Bare drive letter "C:" -> "C:/"
	if len(output) == 2 && output[0] == drive && output[1] == ':' {
		return output + "/"
	}

	// Case 2: Completely escaped the volume (e.g., "." or relative path)
	// Clamp to volume root (matches Unix behavior: path.Clean("/..") = "/")
	if len(output) < 3 || output[0] != drive || output[1] != ':' || output[2] != '/' {
		return string(drive) + ":/"
	}

	return output
}

// looksLikeAbsolutePath checks if an identifier looks like an absolute file path.
// Used by ValidateSyntheticSourceID to reject synthetic identifiers that could
// collide with file-backed SourceIDs.
func looksLikeAbsolutePath(identifier string) bool {
	if len(identifier) == 0 {
		return false
	}
	// Unix absolute path
	if identifier[0] == '/' {
		return true
	}
	// Windows absolute path with forward or back slashes: C:\ or C:/
	if len(identifier) >= 3 && isLetter(identifier[0]) && identifier[1] == ':' {
		if identifier[2] == '/' || identifier[2] == '\\' {
			return true
		}
	}
	// Windows UNC path: \\server or //server
	if len(identifier) >= 2 {
		if (identifier[0] == '\\' && identifier[1] == '\\') ||
			(identifier[0] == '/' && identifier[1] == '/') {
			return true
		}
	}
	return false
}
