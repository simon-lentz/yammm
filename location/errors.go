package location

import "errors"

// Sentinel errors for programmatic error handling.
//
// These errors enable callers to distinguish between different failure modes
// using errors.Is(). Error messages may include additional context (e.g., the
// offending path), but the sentinel error is always the root cause and can be
// matched with errors.Is().
//
// Example usage:
//
//	_, err := location.SourceIDFromAbsolutePath("schemas/main.yammm")
//	if errors.Is(err, location.ErrNotAbsolute) {
//	    // Handle a relative path specifically
//	}

// ErrEmptySourceID is returned when a synthetic source ID is empty.
//
// Returned by: ValidateSyntheticSourceID (and transitively by MustNewSourceID).
var ErrEmptySourceID = errors.New("location: synthetic source ID cannot be empty")

// ErrAbsolutePathSourceID is returned when a synthetic source ID resembles
// an absolute file path (Unix "/path", Windows "C:/path", or UNC "//server").
//
// Synthetic source IDs that look like absolute paths would collide with
// file-backed SourceIDs, violating the String() injectivity invariant.
// Use a scheme prefix (e.g., test://, inline:, embedded://) instead.
//
// Returned by: ValidateSyntheticSourceID (and transitively by MustNewSourceID).
var ErrAbsolutePathSourceID = errors.New("location: synthetic source ID looks like absolute file path")

// ErrNotAbsolute is returned when an absolute path is required but a
// relative path was provided.
//
// Returned by: SourceIDFromAbsolutePath.
var ErrNotAbsolute = errors.New("location: path is not absolute")

// ErrEmptyPath is returned when a file-backed path is empty.
//
// An empty path is a caller that never set the file name. Without this rule it
// would name the working directory, because filepath.Abs("") does, and a
// directory would get a valid-looking identity for a file that was never named.
//
// Returned by: ResolveHostPath, NewCanonicalPath, SourceIDFromPath and
// CanonicalizePathForSourceID (and transitively by their Must forms).
var ErrEmptyPath = errors.New("location: path is empty")

// ErrInvalidUTF8Path is returned for a path that is not valid UTF-8.
//
// An identity is text that reaches two JSON wires — a diagnostic under
// --format json, and the .ys header's schema_source — and encoding/json writes
// an invalid byte as U+FFFD, which merges two names into one. NFC passes such
// bytes through unchanged, so nothing else refuses them.
//
// Returned by: every file-backed constructor, and ValidateSyntheticSourceID.
var ErrInvalidUTF8Path = errors.New("location: path is not valid UTF-8")

// ErrAbsoluteJoinElement is returned when CanonicalPath.Join receives an
// element that looks like an absolute path (Unix "/path", Windows "C:/path",
// or UNC "//server").
//
// Passing absolute paths to Join is almost always a caller bug. Use
// NewCanonicalPath for absolute paths instead.
//
// Returned by: CanonicalPath.Join.
var ErrAbsoluteJoinElement = errors.New("location: join element is absolute")
