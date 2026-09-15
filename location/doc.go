// Package location provides source location tracking for diagnostics.
//
// This package defines the core types used by the YAMMM diagnostic system
// to track source locations. Besides the standard library it depends only on
// its location/path sub-package, golang.org/x/text/unicode/norm and, on
// Windows, golang.org/x/sys/windows (see Dependencies), so every other package
// can import it without an import cycle.
//
// # CanonicalPath
//
// CanonicalPath represents a canonicalized file system path that is always:
//   - Absolute (not relative)
//   - Clean (no . or .. segments)
//   - NFC-normalized (Unicode)
//   - Written with "/" as its separator, on every host
//   - Symlink-resolved, and spelled as the filesystem spells it
//   - Valid UTF-8
//
// Canonicalization follows the host's own path rules. On Windows a backslash
// is a separator and is written as "/", and a network share is canonicalized
// like any other path; on Unix a backslash is a file-name character and is
// kept, and a leading "//" names the root, as the kernel resolves it.
//
// A CanonicalPath is an identity, not a path to open. NFC can change its bytes,
// so on a filesystem that distinguishes normalization forms it may name no
// file; read a file through the host path it was derived from.
//
// Create via NewCanonicalPath or MustCanonicalPath. The type uses an unexported
// field to enforce construction through validated constructors only.
//
// # Host paths and identities
//
// [ResolveHostPath] answers one question — what does the filesystem call this
// file — and every file-backed identity is that answer, normalized. The
// constructors [SourceIDFromPath], [ResolveSourcePath], [NewCanonicalPath],
// [CanonicalPath.Join] and [CanonicalizePathForSourceID] all resolve, and none
// derives an identity from a path's text alone. Two spellings that name one
// file therefore give one identity, and it is the spelling the filesystem holds.
//
// On darwin the answer is realpath(3), called through libSystem. It reads each
// component's name from the directory the lookup passed through, so it answers
// in the volume's case and normalization; it opens nothing, so a FIFO, an
// unreadable file and a full descriptor table resolve like any other path.
// fcntl(F_GETPATH) is not used: it answers from a vnode's cached name, which a
// concurrent lookup of another hard link, or another firmlinked spelling, of
// the same file rewrites. A firmlink is not a symbolic link, so
// /System/Volumes/Data/Users and /Users stay two spellings, as a bind mount
// does on Linux. On Windows the answer is the final path of the file
// (GetFinalPathNameByHandleW), read from a handle opened with no access: the
// kernel resolves it through every symbolic link, junction and mount point and
// spells it in the volume's case with long names, so a path through a junction
// gives the junction target's identity. filepath.EvalSymlinks is not used
// there: it follows a link named inside a target before a ".." that follows
// it, where Windows evaluates the ".." first. On Linux filepath.EvalSymlinks
// is the answer: the host is case-sensitive, and a directory mounted with case
// folding (ext4 +F) keeps the typed case and is out of scope.
//
// A path that does not exist yet resolves as far as it exists: the deepest
// existing ancestor is spelled on disk and the missing tail is appended as
// typed, and a dangling symbolic link resolves to the path the kernel will
// reach through it. On Unix each ".." in its target takes the parent of the
// directory reached so far on disk, not a parent read from the target's text.
// On Windows the target replaces the link in the path's text and the text's
// ".." is evaluated before any further link is followed. One Windows case is
// not seen: a ".." that climbs out through a junction in the link's typed
// path, which the kernel takes from the typed text and the resolution from the
// junction's target; such a link's identity settles once its target exists. An
// identity minted for a file before it is written therefore equals the one
// minted after, which is what lets an in-memory source key match the file it
// will become. A component the process cannot traverse ends the resolution the
// same way. A path under a regular file can never exist and is refused, as is
// a cycle of dangling links, and so is an existing path whose resolved form is
// longer than the host allows a path to be (PATH_MAX, 1024 bytes on darwin).
//
// # SourceID
//
// SourceID identifies a source uniquely within a build. It supports two modes:
//   - File-backed: Created via SourceIDFromPath or ResolveSourcePath.
//     Stores a CanonicalPath directly.
//   - Synthetic: Created via NewSourceID or MustNewSourceID for non-file sources
//     like "<stdin>", "inline:test", or "test://unit/person.yammm".
//
// SourceID is comparable and safe for use as map keys. Case is not folded, it
// is read from the filesystem: where /Users/Simon/a.yammm and
// /users/simon/a.yammm name one file, both give the identity of the spelling
// the volume holds. A synthetic identifier is compared as written.
//
// # Position
//
// Position identifies a point in a UTF-8 encoded source file:
//   - Line: 1-based line number (0 = unknown)
//   - Column: 1-based column counting Unicode code points (runes), not bytes
//   - Byte: 0-based byte offset (-1 = unknown)
//
// Use IsZero() to check for unknown positions, IsKnown() to check for valid
// line/column, and HasByte() to check for known byte offsets.
//
// # Span
//
// Span represents a half-open range [Start, End) in a source file:
//   - Source: SourceID identifying the source
//   - Start: Inclusive start position
//   - End: Exclusive end position (equals Start for point spans)
//
// Create spans via Point, PointWithByte, Range, or RangeWithBytes. The Range
// constructors panic if end is before start by any order they are given: the
// line and column, and for RangeWithBytes the byte offsets too. A span whose
// two orders disagree is unsafe whichever one a reader takes, so it is refused
// at construction rather than carried.
//
// Use IsZero() to check for "no location", IsValid() to check for LSP
// compatibility, and IsGeometricallySafe() to validate spans from untrusted
// sources or from struct literals, which the constructors never saw. It asks
// the same question of both orders.
//
// # RelatedInfo
//
// RelatedInfo provides supplementary location context for diagnostics, such as
// "previous definition here" for duplicate type errors or showing edges of an
// import cycle.
//
// # Provenance
//
// [Provenance] tracks the origin of a parsed data element through the
// adapter → validator → graph pipeline. It carries the source name,
// a [github.com/simon-lentz/yammm/location/path] builder for JSONPath-like
// instance paths, and an optional [Span] for byte-level source location.
// Create via [NewProvenance], extending the path through its own
// [github.com/simon-lentz/yammm/location/path.Builder] while parsing.
//
// Every method is safe on a nil receiver: [Provenance.WithRawPath] turns nil
// into a new Provenance carrying that raw path and no source information, and
// [Provenance.SourceName], [Provenance.Path] and [Provenance.Span] return zero
// values. A nil receiver therefore keeps a diagnostic's path without claiming a
// source location it does not have.
//
// # Dependencies
//
//	location  ──imports──▶  location/path, golang.org/x/text/unicode/norm,
//	                        golang.org/x/sys/windows
//
// The sub-package carries provenance paths, the x/text import is NFC
// normalization, and the x/sys import, made on Windows alone, is the
// final-path call. It can be imported by all other packages without cycles.
package location
