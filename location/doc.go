// Package location provides source location tracking for diagnostics.
//
// This package defines the core types used by the YAMMM diagnostic system
// to track source locations. It sits at the foundation tier and can be imported
// by all other packages without introducing circular dependencies.
//
// # CanonicalPath
//
// CanonicalPath represents a canonicalized file system path that is always:
//   - Absolute (not relative)
//   - Clean (no . or .. segments)
//   - NFC-normalized (Unicode)
//   - Forward-slash normalized (uses "/" on all platforms)
//   - Symlink-resolved (best-effort)
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
// SourceID is comparable and safe for use as map keys.
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
// # PositionRegistry
//
// PositionRegistry is an interface for byte-offset-to-position conversion,
// bridging format adapters (JSON, CSV) and source content registries. The
// primary implementation is schema.SourceRegistry.
//
// # Dependencies
//
// This package depends only on the standard library and golang.org/x/text/unicode/norm
// (for NFC normalization). It does not import any other packages, enabling it
// to be imported by all other packages without cycles.
package location
