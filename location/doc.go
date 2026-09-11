// Package location provides source location tracking for diagnostics.
//
// This package defines the core types used by the YAMMM diagnostic system
// to track source locations. Besides the standard library it depends only on
// its location/path sub-package and golang.org/x/text/unicode/norm (see
// Dependencies), so every other package can import it without an import cycle.
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
// file — and every file-backed identity is that answer, normalized. Two
// spellings that name one file therefore give one identity, and it is the
// spelling the filesystem holds.
//
// Asking the filesystem costs an open on darwin. filepath.EvalSymlinks keeps
// the case as typed there, even through a symlink it resolved, so on a
// case-insensitive volume it cannot tell two spellings of one file apart;
// fcntl(F_GETPATH) on an open descriptor returns every component as the volume
// holds it, case and normalization both, and costs what EvalSymlinks costs.
// Everywhere else EvalSymlinks is already that answer: Linux is
// case-sensitive, and Go's Windows implementation spells each component
// through FindFirstFile. A Linux directory mounted with case folding (ext4 +F)
// keeps the typed case and is out of scope.
//
// The descriptor is why the file kind is checked before the open. Opening a
// FIFO blocks until a writer appears and opening a socket fails outright,
// while a caller canonicalizes a path before it knows what kind of file it
// names — the schema loader does exactly that — so anything but a directory or
// a regular file is spelled by EvalSymlinks, which opens nothing. An open also
// needs read permission where a resolution needs only traversal, so a
// mode-000 file and a directory that is traversable but not readable fall back
// to EvalSymlinks and keep the typed case, which is the best answer available
// for a path the caller cannot open.
//
// A path that does not exist yet resolves as far as it exists: the deepest
// existing ancestor is spelled on disk and the missing tail is appended as
// typed. An identity minted for a file before it is written therefore equals
// the one minted after, which is what lets an in-memory source key match the
// file it will become. A path under a regular file can never exist, and is
// refused rather than kept.
//
// # SourceID
//
// SourceID identifies a source uniquely within a build. It supports two modes:
//   - File-backed: Created via SourceIDFromPath or SourceIDFromAbsolutePath.
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
//	location  ──imports──▶  location/path, golang.org/x/text/unicode/norm
//
// The sub-package carries provenance paths and the x/text import is NFC
// normalization. It can be imported by all other packages without cycles.
package location
