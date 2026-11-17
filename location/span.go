package location

import (
	"cmp"
	"fmt"
)

// Span represents a half-open range [Start, End) in a source file.
//
// Span is a value type with exported fields, written as a literal; nothing
// checks that Start is not after End. The zero value represents "no location";
// use IsZero to check.
type Span struct {
	// Source is the identity key for this span.
	Source SourceID

	// Start is the inclusive start position of the span.
	Start Position

	// End is the exclusive end position of the span.
	// For single-point spans, End equals Start.
	End Position
}

// IsZero reports whether the span is the zero value.
func (s Span) IsZero() bool {
	return s == Span{}
}

// String returns a human-readable representation of the span.
//
// Returns:
//   - "<no location>" for zero spans
//   - "source:line:column" for point spans
//   - "source:startLine:startCol-endLine:endCol" for range spans
//
// An unknown position renders as "<unknown>", as [Position.String] writes it.
func (s Span) String() string {
	if s.IsZero() {
		return "<no location>"
	}

	src := s.Source.String()
	if s.Start == s.End {
		return fmt.Sprintf("%s:%s", src, s.Start.String())
	}
	return fmt.Sprintf("%s:%s-%s", src, s.Start.String(), s.End.String())
}

// Contains reports whether position p is within this span, by byte offset.
// The span is half-open: Start is inclusive, End is exclusive, so a point span
// contains no position. A span or a position that is not valid contains, and
// is contained by, nothing.
func (s Span) Contains(p Position) bool {
	if !s.Start.IsValid() || !s.End.IsValid() || !p.IsValid() {
		return false
	}
	return p.Offset >= s.Start.Offset && p.Offset < s.End.Offset
}

// Compare compares s with t for ordering, as [time.Time.Compare] does: -1 if
// s sorts before t, +1 if after, and 0 exactly when s == t, so a sort by it
// ties nothing distinct. Two sources are equal when their texts are: a
// synthetic name never takes the form of a file-backed identity.
//
// Comparison order:
//  1. Source (string comparison via [SourceID.String])
//  2. Start position (line, then column)
//  3. End position (line, then column)
//  4. Start, then End, byte offset
func (s Span) Compare(t Span) int {
	return cmp.Or(
		cmp.Compare(s.Source.String(), t.Source.String()),
		comparePositions(s.Start, t.Start),
		comparePositions(s.End, t.End),
		cmp.Compare(s.Start.Offset, t.Start.Offset),
		cmp.Compare(s.End.Offset, t.End.Offset),
	)
}

// comparePositions compares two positions by line, then column.
func comparePositions(a, b Position) int {
	if c := cmp.Compare(a.Line, b.Line); c != 0 {
		return c
	}
	return cmp.Compare(a.Column, b.Column)
}
