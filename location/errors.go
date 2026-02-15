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
//	_, err := location.NewCanonicalPath("//server/share")
//	if errors.Is(err, location.ErrUNCPath) {
//	    // Handle UNC path rejection specifically
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

// ErrUNCPath is returned when a UNC path (//server/share or \\server\share)
// is provided where a local filesystem path is required.
//
// UNC paths are rejected because path.Clean collapses "//" to "/", which would
// cause SourceID collisions between UNC paths and regular Unix paths.
// Use a local mount point instead.
//
// Returned by: NewCanonicalPath, SourceIDFromAbsolutePath, CanonicalizePathForSourceID.
var ErrUNCPath = errors.New("location: UNC paths are not supported")

// ErrNotAbsolute is returned when an absolute path is required but a
// relative path was provided.
//
// Returned by: SourceIDFromAbsolutePath (via canonicalizeAbsolutePath).
var ErrNotAbsolute = errors.New("location: path is not absolute")

// ErrAbsoluteJoinElement is returned when CanonicalPath.Join receives an
// element that looks like an absolute path (Unix "/path", Windows "C:/path",
// or UNC "//server").
//
// Passing absolute paths to Join is almost always a caller bug. Use
// NewCanonicalPath for absolute paths instead.
//
// Returned by: CanonicalPath.Join.
var ErrAbsoluteJoinElement = errors.New("location: join element is absolute")
