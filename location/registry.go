package location

// PositionRegistry provides byte-offset-to-position conversion.
//
// This interface is the bridge between format adapters (JSON, CSV) and source
// content registries that perform the actual conversion. It enables adapters
// to obtain accurate Position values from byte offsets captured during parsing.
//
// The primary implementation is schema.SourceRegistry, which enables unified
// source tracking for both schema and instance diagnostics.
//
// Design rationale:
//
//  1. Foundation tier placement: PositionRegistry is defined in location
//     (foundation tier) because the interface operates on location.Position and
//     location.SourceID — natural cohesion with the location package.
//
//  2. Decouples adapters from schema: Adapters can use any PositionRegistry
//     implementation, not just schema.SourceRegistry. This enables testing with
//     mock registries and supports alternative implementations.
//
//  3. Enables adapter independence: Adapters can be used in contexts where the
//     full schema machinery isn't needed.
type PositionRegistry interface {
	// PositionAt converts a byte offset to a Position for the given source.
	//
	// Returns a zero Position (check via IsZero()) if:
	//   - The source is not registered
	//   - The byte offset is out of range
	//   - The byte offset is negative
	//
	// The returned Position has:
	//   - Line: 1-based line number
	//   - Column: 1-based rune offset from line start
	//   - Byte: The input byteOffset (echoed back for convenience)
	PositionAt(source SourceID, byteOffset int) Position
}

// RuneOffsetConverter provides rune-to-byte offset conversion.
//
// ANTLR positions are rune-based (character indices), but the schema layer
// uses byte offsets for consistency with Go strings and UTF-8 handling.
// This interface enables the conversion between these coordinate systems.
//
// The primary implementation is internal/source.Registry.
type RuneOffsetConverter interface {
	// RuneToByteOffset converts a rune offset to a byte offset for the given source.
	//
	// Returns (byteOffset, true) on success.
	// Returns (0, false) if:
	//   - The source is not registered
	//   - The rune offset is out of range
	//   - The rune offset is negative
	RuneToByteOffset(source SourceID, runeOffset int) (byteOffset int, ok bool)
}
