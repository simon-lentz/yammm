package location

import "fmt"

// Position identifies a point in a UTF-8 encoded source file.
//
// Line and Column are 1-based. Column counts Unicode code points (runes),
// not bytes or grapheme clusters. Byte is a 0-based byte offset in the
// UTF-8 source content; -1 means the byte offset is unknown.
//
// Position is a value type and should be passed by value.
type Position struct {
	// Line is the 1-based line number. Zero means unknown.
	Line int

	// Column is the 1-based column number, counting runes from line start.
	// Zero means unknown.
	Column int

	// Byte is the 0-based byte offset in the source content.
	// A value of -1 indicates the byte offset is unknown.
	Byte int
}

// NewPosition creates a Position with the specified line, column, and byte offset.
// Use -1 for byte to indicate an unknown byte offset.
//
// Values are stored as-is without validation or clamping. Negative values are
// permitted but may produce unexpected results with [IsZero] and [IsKnown]:
//   - Negative line/column: IsZero returns false (not 0,0), IsKnown returns false (not > 0)
//   - Zero line or column: the position is considered partial/incomplete
//
// For explicit unknown position construction, use [UnknownPosition] instead.
func NewPosition(line, column, byteOffset int) Position {
	return Position{Line: line, Column: column, Byte: byteOffset}
}

// UnknownPosition returns a Position representing an unknown location.
// This is the canonical way to construct a position when location is not available.
// The returned position has Line=0, Column=0, Byte=-1, for which IsZero() returns true.
func UnknownPosition() Position {
	return Position{Line: 0, Column: 0, Byte: -1}
}

// IsZero reports whether the position represents an unknown location.
// A position is zero/unknown when Line == 0 && Column == 0.
// The byte offset is ignored for this determination.
func (p Position) IsZero() bool {
	return p.Line == 0 && p.Column == 0
}

// IsKnown reports whether line and column are known (both > 0).
// This is distinct from !IsZero(): a position with Line=1, Column=1, Byte=0
// is both "not zero" and "known".
func (p Position) IsKnown() bool {
	return p.Line > 0 && p.Column > 0
}

// HasByte reports whether the byte offset is known and meaningful.
// Returns true only when Byte >= 0 AND the position is not zero/unknown.
//
// This prevents Contains() from accidentally treating an unknown end position
// (with Byte=0 from Go's zero value) as having a meaningful byte offset of 0.
//
// Note: Byte-only positions (where byte offset is known but line/column are zero)
// return false. This is intentional: the package design requires line/column for
// geometric operations like Before, After, and Contains. Adapters should always
// compute line/column coordinates alongside byte offsets.
func (p Position) HasByte() bool {
	return p.Byte >= 0 && !p.IsZero()
}

// String returns a human-readable representation of the position.
// Returns "line:column" for known positions, or "<unknown>" for zero positions.
func (p Position) String() string {
	if p.IsZero() {
		return "<unknown>"
	}
	return fmt.Sprintf("%d:%d", p.Line, p.Column)
}

// Before reports whether p is strictly before other.
// Comparison is by Line first, then by Column. Byte offset is ignored.
// Returns false if either position is not fully known (requires both Line > 0 and Column > 0).
//
// Byte offset is intentionally ignored because Position.Before/After are
// designed for human-readable ordering (line/column), not byte-level precision.
// For byte-level comparison, use [Span.Contains] or compare Byte fields directly.
func (p Position) Before(other Position) bool {
	if !p.IsKnown() || !other.IsKnown() {
		return false
	}
	if p.Line != other.Line {
		return p.Line < other.Line
	}
	return p.Column < other.Column
}

// After reports whether p is strictly after other.
// Comparison is by Line first, then by Column. Byte offset is ignored.
// Returns false if either position is not fully known (requires both Line > 0 and Column > 0).
func (p Position) After(other Position) bool {
	if !p.IsKnown() || !other.IsKnown() {
		return false
	}
	if p.Line != other.Line {
		return p.Line > other.Line
	}
	return p.Column > other.Column
}
