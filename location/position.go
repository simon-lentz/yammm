package location

import "fmt"

// Position identifies a point in a UTF-8 encoded source file, as
// [go/token.Position] does: the zero value is an unknown position, and
// [Position.IsValid] reports a known one.
//
// Line and Column are 1-based. Column counts Unicode code points (runes),
// not bytes or grapheme clusters. Offset is the 0-based byte offset in the
// UTF-8 source content; every producer in this module sets it beside the line
// and column, so a valid position's Offset is known, and an Offset of 0 is the
// first byte.
//
// Position is a value type and should be passed by value.
type Position struct {
	// Line is the 1-based line number. Zero means unknown.
	Line int

	// Column is the 1-based column number, counting runes from line start.
	Column int

	// Offset is the 0-based byte offset in the source content.
	Offset int
}

// IsValid reports whether the position is known: Line > 0.
func (p Position) IsValid() bool {
	return p.Line > 0
}

// String returns "line:column" for a valid position, or "<unknown>".
func (p Position) String() string {
	if !p.IsValid() {
		return "<unknown>"
	}
	return fmt.Sprintf("%d:%d", p.Line, p.Column)
}
