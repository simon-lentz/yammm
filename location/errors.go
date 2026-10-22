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
//	_, err := location.SourceIDFromPath("")
//	if errors.Is(err, location.ErrEmptyPath) {
//	    // Handle a caller that never set the file name
//	}

// ErrEmptySourceID is returned when a synthetic source ID is empty.
//
// Returned by: ValidateSyntheticSourceID. MustNewSourceID panics with its text.
var ErrEmptySourceID = errors.New("location: synthetic source ID cannot be empty")

// ErrAbsolutePathSourceID is returned when a synthetic source ID resembles
// an absolute file path (Unix "/path", Windows "C:/path", or UNC "//server").
//
// Synthetic source IDs that look like absolute paths would collide with
// file-backed SourceIDs, violating the String() injectivity invariant.
// A synthetic source ID takes a scheme prefix (e.g., test://, inline:,
// embedded://) instead; a source key under a synthetic root takes a relative
// path.
//
// Returned by: ValidateSyntheticSourceID, and MustNewSourceID panics with its
// text. NormalizeSyntheticKey returns it wrapped, for a key that looks absolute as
// given or once cleaned and in NFC. So do schema.SyntheticImportKey, for an
// importer key or an import that does, and adapter/gogen.Marshal, for a source
// whose embedded key does. A load under schema.WithSyntheticRoot reports such
// a source key as a Fatal E_LOAD_IO_FAILURE carrying the error's text, so
// errors.Is cannot match it there.
var ErrAbsolutePathSourceID = errors.New("location: synthetic source ID looks like absolute file path")

// ErrUnprintableSourceID is returned when a synthetic source ID holds a
// character that does not print: one strconv.IsPrint refuses, so a control
// character, a bidi or other format character, a space past ASCII, a private-use
// or unassigned code point. Such a character would let two identities read alike.
//
// Returned by: ValidateSyntheticSourceID, and MustNewSourceID panics with its
// text. NormalizeSyntheticKey, schema.SyntheticImportKey and adapter/gogen.Marshal return it
// wrapped.
var ErrUnprintableSourceID = errors.New("location: synthetic source ID holds a character that does not print")

// ErrEmptyPath is returned when a file-backed path is empty.
//
// An empty path is a caller that never set the file name. Without this rule it
// would name the working directory, because filepath.Abs("") does, and a
// directory would get a valid-looking identity for a file that was never named.
//
// Returned by: ResolveHostPath, NewCanonicalPath, SourceIDFromPath,
// ResolveSourcePath and CanonicalizePathForSourceID, and schema.FindModuleRoot
// returns it wrapped. Their Must forms panic with its text.
var ErrEmptyPath = errors.New("location: path is empty")

// ErrInvalidUTF8Path is returned for a path that is not valid UTF-8, or that
// resolves to one through the working directory, a name on disk or a dangling
// link's target.
//
// An identity is text that reaches JSON wires — a diagnostic under --format
// json, and a .ys document's schema_source and each instance's provenance
// source_name — and encoding/json writes
// an invalid byte as U+FFFD, which merges two names into one. NFC passes such
// bytes through unchanged, so nothing else refuses them.
//
// Returned by: ResolveHostPath, every file-backed constructor (NewCanonicalPath,
// SourceIDFromPath, ResolveSourcePath, CanonicalizePathForSourceID and
// CanonicalPath.Join), ValidateSyntheticSourceID and NormalizeSyntheticKey, and
// schema.SyntheticImportKey and schema.FindModuleRoot return it wrapped. The
// Must forms of the constructors and of ValidateSyntheticSourceID's
// MustNewSourceID panic with its text. A schema load reports it as a Fatal
// E_LOAD_IO_FAILURE for the entry path, an explicit module root, a LoadString
// source name and a LoadSourcesWithEntry source key, and as an E_IMPORT_RESOLVE
// error for an import, each carrying the error's text, so errors.Is cannot
// match it there.
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
