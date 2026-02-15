package location

import "fmt"

// Span represents a half-open range [Start, End) in a source file.
//
// Span is a value type with exported fields. Always pass by value.
// The zero value represents "no location"; use IsZero() to check.
type Span struct {
	// Source is the identity key for this span.
	Source SourceID

	// Start is the inclusive start position of the span.
	Start Position

	// End is the exclusive end position of the span.
	// For single-point spans, End equals Start.
	End Position
}

// Point creates a single-point Span where Start == End.
// This is the canonical way to create spans from parser token positions.
// The byte offset is set to -1 (unknown).
func Point(source SourceID, line, column int) Span {
	pos := Position{Line: line, Column: column, Byte: -1}
	return Span{Source: source, Start: pos, End: pos}
}

// PointWithByte creates a single-point Span with a known byte offset.
func PointWithByte(source SourceID, line, column, byteOffset int) Span {
	pos := Position{Line: line, Column: column, Byte: byteOffset}
	return Span{Source: source, Start: pos, End: pos}
}

// Range creates a Span from start to end positions (byte offsets unknown).
//
// Panics if end < start (geometric soundness invariant). This catches
// construction bugs early rather than allowing malformed spans to propagate
// through the system. For point spans where start == end, use Point() instead.
func Range(source SourceID, startLine, startCol, endLine, endCol int) Span {
	start := Position{Line: startLine, Column: startCol, Byte: -1}
	end := Position{Line: endLine, Column: endCol, Byte: -1}
	if positionBefore(end, start) {
		panic(fmt.Sprintf("location.Range: end %v before start %v", end, start))
	}
	return Span{Source: source, Start: start, End: end}
}

// RangeWithBytes creates a Span with known byte offsets.
//
// Panics if end < start (geometric soundness invariant). When byte offsets are
// present, the byte comparison takes precedence over line/column comparison.
// This means a span may be considered valid even if line/column ordering appears
// inverted, as long as byte ordering is correct. Use [Span.IsConsistent] to
// verify that both orderings agree before converting to LSP ranges.
func RangeWithBytes(source SourceID, startLine, startCol, startByte, endLine, endCol, endByte int) Span {
	start := Position{Line: startLine, Column: startCol, Byte: startByte}
	end := Position{Line: endLine, Column: endCol, Byte: endByte}

	// Use byte comparison when both have valid byte offsets
	if start.HasByte() && end.HasByte() {
		if end.Byte < start.Byte {
			panic(fmt.Sprintf("location.RangeWithBytes: end byte %d before start byte %d", endByte, startByte))
		}
	} else if positionBefore(end, start) {
		panic(fmt.Sprintf("location.RangeWithBytes: end %v before start %v", end, start))
	}
	return Span{Source: source, Start: start, End: end}
}

// IsZero reports whether the span is the zero value.
// A zero span has zero Source and zero Start/End positions.
func (s Span) IsZero() bool {
	return s.Source.IsZero() && s.Start.IsZero() && s.End.IsZero()
}

// IsPoint reports whether the span represents a single point (Start == End).
// Uses Go struct equality, comparing all Position fields.
func (s Span) IsPoint() bool {
	return s.Start == s.End
}

// IsValid reports whether the span has meaningful content for conversion to
// LSP ranges.
//
// A valid span has:
//   - Non-zero Source
//   - Known Start position (Line > 0 && Column > 0)
//   - Known End position OR is a point span
//
// IMPORTANT: IsValid() checks "convertible to LSP," NOT "geometrically sound."
// Use IsGeometricallySafe() to verify Start <= End.
func (s Span) IsValid() bool {
	if s.Source.IsZero() {
		return false
	}
	if !s.Start.IsKnown() {
		return false
	}
	// For non-point spans, End must also be known
	if !s.IsPoint() && !s.End.IsKnown() {
		return false
	}
	return true
}

// IsGeometricallySafe reports whether the span satisfies Start <= End.
//
// Returns true for:
//   - Zero spans
//   - Point spans (Start == End)
//   - Valid range spans where Start is at or before End
//
// Use this to validate spans constructed via struct literals or from
// untrusted sources.
func (s Span) IsGeometricallySafe() bool {
	if s.IsZero() || s.IsPoint() {
		return true
	}

	// If both positions have known bytes, use byte comparison
	if s.Start.HasByte() && s.End.HasByte() {
		return s.Start.Byte <= s.End.Byte
	}

	// Otherwise use line/column comparison
	return !positionBefore(s.End, s.Start)
}

// IsConsistent reports whether byte and line/column orderings agree.
//
// Returns true when:
//   - Both byte offsets are unknown (line/column is the only ordering)
//   - Both line/column are unknown (byte offset is the only ordering)
//   - Byte ordering and line/column ordering produce the same result
//
// This is useful before converting spans to LSP ranges, where inconsistent
// orderings could produce inverted ranges. A span can be geometrically safe
// (using byte precedence) but inconsistent if line/column ordering disagrees.
func (s Span) IsConsistent() bool {
	if s.IsZero() || s.IsPoint() {
		return true
	}

	hasByte := s.Start.HasByte() && s.End.HasByte()
	hasLineCol := s.Start.IsKnown() && s.End.IsKnown()

	// If only one ordering system is available, it's trivially consistent
	if !hasByte || !hasLineCol {
		return true
	}

	// Both systems available: check if they agree
	byteOrdered := s.Start.Byte <= s.End.Byte
	lineColOrdered := !positionBefore(s.End, s.Start)

	return byteOrdered == lineColOrdered
}

// String returns a human-readable representation of the span.
//
// Returns:
//   - "<no location>" for zero spans
//   - "source:line:column" for point spans
//   - "source:startLine:startCol-endLine:endCol" for range spans
func (s Span) String() string {
	if s.IsZero() {
		return "<no location>"
	}

	src := s.Source.String()
	if s.IsPoint() {
		return fmt.Sprintf("%s:%s", src, s.Start.String())
	}
	return fmt.Sprintf("%s:%d:%d-%d:%d", src, s.Start.Line, s.Start.Column, s.End.Line, s.End.Column)
}

// Contains reports whether position p is within this span.
//
// Uses byte-based comparison when available, falls back to line/column.
// The span is half-open: Start is inclusive, End is exclusive.
//
// Note: Point spans (where Start == End) contain no positions by definition
// of the half-open interval. For LSP-style operations where you need to match
// the exact position of a point span, use [Span.ContainsOrEquals] instead.
//
// Precondition: span must be geometrically sound (IsGeometricallySafe).
func (s Span) Contains(p Position) bool {
	if s.IsZero() || p.IsZero() {
		return false
	}

	// Use byte-based comparison if all three have known bytes
	if s.Start.HasByte() && s.End.HasByte() && p.HasByte() {
		return p.Byte >= s.Start.Byte && p.Byte < s.End.Byte
	}

	// Fall back to line/column comparison
	// p must be >= Start (inclusive) and < End (exclusive)
	if positionBefore(p, s.Start) {
		return false
	}
	// p must be strictly before End (half-open interval)
	if !positionBefore(p, s.End) {
		return false
	}
	return true
}

// ContainsOrEquals reports whether position p is within this span OR
// equals the location of a point span.
//
// This is equivalent to: s.Contains(p) || (s.IsPoint() && s.Start == p)
//
// Use this for LSP-style operations (like symbol lookup) where you need to
// match positions that fall exactly on a point span's location. For range
// spans, this behaves identically to Contains.
func (s Span) ContainsOrEquals(p Position) bool {
	if s.Contains(p) {
		return true
	}
	// For point spans, also match the exact position
	return s.IsPoint() && s.Start == p
}

// Overlaps reports whether the spans share any positions.
//
// Requires same Source. Precondition: both spans must be geometrically sound.
// Half-open intervals [a, b) and [c, d) overlap iff a < d AND c < b.
func (s Span) Overlaps(other Span) bool {
	// Different sources don't overlap
	if s.Source != other.Source {
		return false
	}

	if s.IsZero() || other.IsZero() {
		return false
	}

	// Use byte-based comparison if available
	if s.Start.HasByte() && s.End.HasByte() && other.Start.HasByte() && other.End.HasByte() {
		// Half-open intervals [a, b) and [c, d) overlap iff a < d AND c < b
		return s.Start.Byte < other.End.Byte && other.Start.Byte < s.End.Byte
	}

	// Fall back to line/column comparison
	// Half-open intervals [a, b) and [c, d) overlap iff a < d AND c < b
	// s.Start < other.End AND other.Start < s.End
	if !positionBefore(s.Start, other.End) {
		return false
	}
	if !positionBefore(other.Start, s.End) {
		return false
	}
	return true
}

// ContainsSpan reports whether this span fully contains other.
//
// Precondition: both spans must be geometrically sound.
func (s Span) ContainsSpan(other Span) bool {
	if s.Source != other.Source {
		return false
	}

	if s.IsZero() || other.IsZero() {
		return false
	}

	// Use byte-based comparison if available
	if s.Start.HasByte() && s.End.HasByte() && other.Start.HasByte() && other.End.HasByte() {
		return other.Start.Byte >= s.Start.Byte && other.End.Byte <= s.End.Byte
	}

	// Fall back to line/column comparison
	// other.Start >= s.Start AND other.End <= s.End
	if positionBefore(other.Start, s.Start) {
		return false
	}
	if positionBefore(s.End, other.End) {
		return false
	}
	return true
}

// Merge combines two spans into one covering both.
//
// REQUIRES trusted-provenance spans. Panics on:
//   - Different sources
//   - Invalid spans (IsValid returns false)
//   - Geometric violations
//
// For untrusted-provenance spans (from adapters or external sources), use
// MergeSafe instead.
func Merge(a, b Span) Span {
	if a.Source != b.Source {
		panic(fmt.Sprintf("location.Merge: source mismatch: %q vs %q", a.Source.String(), b.Source.String()))
	}
	if !a.IsValid() {
		panic(fmt.Sprintf("location.Merge: first span is invalid: %v", a))
	}
	if !b.IsValid() {
		panic(fmt.Sprintf("location.Merge: second span is invalid: %v", b))
	}

	return mergeSpans(a, b)
}

// MergeSafe is the safe variant of Merge for untrusted-provenance spans.
//
// Returns ok=false instead of panicking when:
//   - Sources differ
//   - Either span is invalid
//   - Either span is geometrically unsound
func MergeSafe(a, b Span) (Span, bool) {
	if a.Source != b.Source {
		return Span{}, false
	}
	if !a.IsValid() || !b.IsValid() {
		return Span{}, false
	}
	if !a.IsGeometricallySafe() || !b.IsGeometricallySafe() {
		return Span{}, false
	}

	return mergeSpans(a, b), true
}

// mergeSpans performs the actual merge. Caller must ensure preconditions.
func mergeSpans(a, b Span) Span {
	var start, end Position

	// Determine start (minimum of the two starts)
	if positionBefore(a.Start, b.Start) {
		start = a.Start
	} else {
		start = b.Start
	}

	// Determine end (maximum of the two ends)
	if positionBefore(a.End, b.End) {
		end = b.End
	} else {
		end = a.End
	}

	return Span{Source: a.Source, Start: start, End: end}
}

// Compare compares two spans for ordering.
//
// Comparison order:
//  1. Source (string comparison via [SourceID.String])
//  2. Start position (line, then column)
//  3. End position (line, then column)
//
// Source comparison uses string ordering of SourceID.String(). This means
// synthetic IDs that resemble file paths (e.g., "/absolute/path") will be
// interleaved with file-backed spans in the ordering. To ensure deterministic
// and non-colliding ordering, use [MustNewSourceID] which validates that
// synthetic identifiers don't resemble file paths.
//
// Returns:
//
//	-1 if a < b
//	 0 if a == b
//	+1 if a > b
func Compare(a, b Span) int {
	// Compare sources
	srcA, srcB := a.Source.String(), b.Source.String()
	if srcA < srcB {
		return -1
	}
	if srcA > srcB {
		return 1
	}

	// Compare start positions
	if cmp := comparePositions(a.Start, b.Start); cmp != 0 {
		return cmp
	}

	// Compare end positions
	return comparePositions(a.End, b.End)
}

// comparePositions compares two positions for ordering.
func comparePositions(a, b Position) int {
	if a.Line != b.Line {
		if a.Line < b.Line {
			return -1
		}
		return 1
	}
	if a.Column != b.Column {
		if a.Column < b.Column {
			return -1
		}
		return 1
	}
	return 0
}

// positionBefore reports whether a is strictly before b using line/column.
// Returns false if either position is not fully known (requires both Line > 0 and Column > 0).
func positionBefore(a, b Position) bool {
	if !a.IsKnown() || !b.IsKnown() {
		return false
	}
	if a.Line != b.Line {
		return a.Line < b.Line
	}
	return a.Column < b.Column
}
