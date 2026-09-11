// Package location provides source location tracking for diagnostics.
//
// This package defines the core types used by the YAMMM diagnostic system
// to track source locations. It depends only on the standard library and can be
// imported by all other packages without introducing circular dependencies.
//
// # CanonicalPath
//
// CanonicalPath represents a canonicalized file system path that is always:
//   - Absolute (not relative)
//   - Clean (no . or .. segments)
//   - NFC-normalized (Unicode)
//   - Written with "/" as its separator, on every host
//   - Symlink-resolved (best-effort)
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
// # SourceID
//
// SourceID identifies a source uniquely within a build. It supports two modes:
//   - File-backed: Created via SourceIDFromPath, SourceIDFromCanonicalPath, or
//     SourceIDFromAbsolutePath. Stores a CanonicalPath directly.
//   - Synthetic: Created via NewSourceID or MustNewSourceID for non-file sources
//     like "<stdin>", "inline:test", or "test://unit/person.yammm".
//
// SourceID is comparable and safe for use as map keys. Case is not normalized:
// on a case-insensitive filesystem, /Users/Simon/a.yammm and /users/simon/a.yammm
// are distinct SourceIDs.
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
// constructors panic if end < start (geometric soundness invariant).
//
// Use IsZero() to check for "no location", IsValid() to check for LSP
// compatibility, and IsGeometricallySafe() to validate spans from untrusted
// sources.
//
// # RelatedInfo
//
// RelatedInfo provides supplementary location context for diagnostics, such as
// "previous definition here" for duplicate type errors or showing edges of an
// import cycle. Use the Msg* constants for consistent message formatting.
//
// # Provenance
//
// [Provenance] tracks the origin of a parsed data element through the
// adapter → validator → graph pipeline. It carries the source name,
// a [github.com/simon-lentz/yammm/location/path] builder for JSONPath-like
// instance paths, and an optional [Span] for byte-level source location.
// Create via [NewProvenance]; use [Provenance.AtKey] to extend the path during
// recursive parsing.
//
// # Dependencies
//
//	location  ──imports──▶  location/path, golang.org/x/text/unicode/norm
//
// The sub-package carries provenance paths and the x/text import is NFC
// normalization. It can be imported by all other packages without cycles.
package location
