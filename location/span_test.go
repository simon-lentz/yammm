package location

import (
	"testing"
)

var testSource = NewSourceID("test://unit")

func TestPoint(t *testing.T) {
	s := Point(testSource, 10, 5)

	if s.Source != testSource {
		t.Error("Source mismatch")
	}
	if s.Start.Line != 10 || s.Start.Column != 5 {
		t.Errorf("Start = %v; want {10, 5, -1}", s.Start)
	}
	if s.Start.Byte != -1 {
		t.Error("Point should have Byte = -1")
	}
	if !s.IsPoint() {
		t.Error("Point should report IsPoint() == true")
	}
}

func TestPointWithByte(t *testing.T) {
	s := PointWithByte(testSource, 10, 5, 42)

	if s.Start.Byte != 42 {
		t.Errorf("Start.Byte = %d; want 42", s.Start.Byte)
	}
	if !s.IsPoint() {
		t.Error("PointWithByte should report IsPoint() == true")
	}
}

func TestRange(t *testing.T) {
	s := Range(testSource, 10, 5, 10, 15)

	if s.Start.Line != 10 || s.Start.Column != 5 {
		t.Errorf("Start = %v; want {10, 5, -1}", s.Start)
	}
	if s.End.Line != 10 || s.End.Column != 15 {
		t.Errorf("End = %v; want {10, 15, -1}", s.End)
	}
	if s.IsPoint() {
		t.Error("Range should not be a point")
	}
}

func TestRange_Panics_EndBeforeStart(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("Range with end before start should panic")
		}
	}()

	Range(testSource, 10, 15, 10, 5) // End column before start column
}

func TestRangeWithBytes(t *testing.T) {
	s := RangeWithBytes(testSource, 10, 5, 100, 10, 15, 110)

	if s.Start.Byte != 100 {
		t.Errorf("Start.Byte = %d; want 100", s.Start.Byte)
	}
	if s.End.Byte != 110 {
		t.Errorf("End.Byte = %d; want 110", s.End.Byte)
	}
}

func TestRangeWithBytes_Panics_EndByteBeforeStartByte(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("RangeWithBytes with end byte before start byte should panic")
		}
	}()

	RangeWithBytes(testSource, 10, 5, 110, 10, 15, 100) // End byte before start byte
}

func TestSpan_IsZero(t *testing.T) {
	var zeroSpan Span
	if !zeroSpan.IsZero() {
		t.Error("zero value should report IsZero() == true")
	}

	s := Point(testSource, 1, 1)
	if s.IsZero() {
		t.Error("valid span should not be zero")
	}
}

func TestSpan_IsValid(t *testing.T) {
	tests := []struct {
		name string
		span Span
		want bool
	}{
		{
			name: "zero span",
			span: Span{},
			want: false,
		},
		{
			name: "valid point",
			span: Point(testSource, 1, 1),
			want: true,
		},
		{
			name: "valid range",
			span: Range(testSource, 1, 1, 2, 10),
			want: true,
		},
		{
			name: "no source",
			span: Span{Start: Position{Line: 1, Column: 1}, End: Position{Line: 1, Column: 1}},
			want: false,
		},
		{
			name: "unknown start",
			span: Span{Source: testSource, Start: Position{}, End: Position{Line: 1, Column: 1}},
			want: false,
		},
		{
			name: "unknown end (non-point)",
			span: Span{Source: testSource, Start: Position{Line: 1, Column: 1}, End: Position{}},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.span.IsValid(); got != tt.want {
				t.Errorf("IsValid() = %v; want %v", got, tt.want)
			}
		})
	}
}

func TestSpan_IsGeometricallySafe(t *testing.T) {
	tests := []struct {
		name string
		span Span
		want bool
	}{
		{
			name: "zero span",
			span: Span{},
			want: true,
		},
		{
			name: "point span",
			span: Point(testSource, 5, 10),
			want: true,
		},
		{
			name: "valid range",
			span: Range(testSource, 1, 1, 2, 10),
			want: true,
		},
		{
			name: "inverted range via struct literal",
			span: Span{
				Source: testSource,
				Start:  Position{Line: 2, Column: 10, Byte: -1},
				End:    Position{Line: 1, Column: 1, Byte: -1},
			},
			want: false,
		},
		{
			name: "inverted bytes via struct literal",
			span: Span{
				Source: testSource,
				Start:  Position{Line: 1, Column: 1, Byte: 100},
				End:    Position{Line: 1, Column: 10, Byte: 50},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.span.IsGeometricallySafe(); got != tt.want {
				t.Errorf("IsGeometricallySafe() = %v; want %v", got, tt.want)
			}
		})
	}
}

func TestSpan_String(t *testing.T) {
	tests := []struct {
		name string
		span Span
		want string
	}{
		{
			name: "zero span",
			span: Span{},
			want: "<no location>",
		},
		{
			name: "point span",
			span: Point(testSource, 10, 5),
			want: "test://unit:10:5",
		},
		{
			name: "range span",
			span: Range(testSource, 10, 5, 10, 15),
			want: "test://unit:10:5-10:15",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.span.String(); got != tt.want {
				t.Errorf("String() = %q; want %q", got, tt.want)
			}
		})
	}
}

func TestSpan_Contains(t *testing.T) {
	// Range from line 5, column 10 to line 5, column 20
	s := Range(testSource, 5, 10, 5, 20)

	tests := []struct {
		name string
		pos  Position
		want bool
	}{
		{
			name: "before range",
			pos:  Position{Line: 5, Column: 5, Byte: -1},
			want: false,
		},
		{
			name: "at start",
			pos:  Position{Line: 5, Column: 10, Byte: -1},
			want: true,
		},
		{
			name: "in middle",
			pos:  Position{Line: 5, Column: 15, Byte: -1},
			want: true,
		},
		{
			name: "at end (exclusive)",
			pos:  Position{Line: 5, Column: 20, Byte: -1},
			want: false,
		},
		{
			name: "after range",
			pos:  Position{Line: 5, Column: 25, Byte: -1},
			want: false,
		},
		{
			name: "unknown position",
			pos:  Position{},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := s.Contains(tt.pos); got != tt.want {
				t.Errorf("Contains() = %v; want %v", got, tt.want)
			}
		})
	}
}

func TestSpan_Contains_WithBytes(t *testing.T) {
	// Range with byte offsets: 100 to 110 (half-open)
	s := RangeWithBytes(testSource, 5, 10, 100, 5, 20, 110)

	tests := []struct {
		name string
		pos  Position
		want bool
	}{
		{
			name: "before range (byte)",
			pos:  Position{Line: 5, Column: 5, Byte: 95},
			want: false,
		},
		{
			name: "at start (byte)",
			pos:  Position{Line: 5, Column: 10, Byte: 100},
			want: true,
		},
		{
			name: "in middle (byte)",
			pos:  Position{Line: 5, Column: 15, Byte: 105},
			want: true,
		},
		{
			name: "at end (byte, exclusive)",
			pos:  Position{Line: 5, Column: 20, Byte: 110},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := s.Contains(tt.pos); got != tt.want {
				t.Errorf("Contains() = %v; want %v", got, tt.want)
			}
		})
	}
}

func TestCompare(t *testing.T) {
	tests := []struct {
		name string
		a    Span
		b    Span
		want int
	}{
		{
			name: "equal",
			a:    Range(testSource, 5, 10, 5, 20),
			b:    Range(testSource, 5, 10, 5, 20),
			want: 0,
		},
		{
			name: "a before b (line)",
			a:    Range(testSource, 4, 10, 4, 20),
			b:    Range(testSource, 5, 10, 5, 20),
			want: -1,
		},
		{
			name: "a after b (line)",
			a:    Range(testSource, 6, 10, 6, 20),
			b:    Range(testSource, 5, 10, 5, 20),
			want: 1,
		},
		{
			name: "a before b (column)",
			a:    Range(testSource, 5, 5, 5, 15),
			b:    Range(testSource, 5, 10, 5, 20),
			want: -1,
		},
		{
			name: "same start, different end",
			a:    Range(testSource, 5, 10, 5, 15),
			b:    Range(testSource, 5, 10, 5, 20),
			want: -1,
		},
		{
			name: "different source (alphabetic)",
			a:    Range(NewSourceID("aaa://"), 5, 10, 5, 20),
			b:    Range(NewSourceID("bbb://"), 5, 10, 5, 20),
			want: -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Compare(tt.a, tt.b); got != tt.want {
				t.Errorf("Compare() = %d; want %d", got, tt.want)
			}
		})
	}
}

func TestSpan_Equality(t *testing.T) {
	// Go struct equality should work
	s1 := Range(testSource, 5, 10, 5, 20)
	s2 := Range(testSource, 5, 10, 5, 20)
	s3 := Range(testSource, 5, 10, 5, 21)

	if s1 != s2 {
		t.Error("equal spans should be equal")
	}
	if s1 == s3 {
		t.Error("different spans should not be equal")
	}
}

func TestSpan_MapKey(t *testing.T) {
	s1 := Range(testSource, 5, 10, 5, 20)
	s2 := Range(testSource, 5, 10, 5, 20)

	m := make(map[Span]int)
	m[s1] = 42

	if m[s2] != 42 {
		t.Error("equal spans should work as map keys")
	}
}

// TestSpan_PointContains documents that point spans contain no positions by definition
// of the half-open interval [Start, End) where Start == End.
func TestSpan_PointContains(t *testing.T) {
	point := Point(testSource, 5, 10)
	pos := Position{Line: 5, Column: 10, Byte: -1}

	// Point spans use half-open interval [Start, End) where Start == End
	// Therefore they contain no positions (empty interval)
	if point.Contains(pos) {
		t.Error("point span should not contain its own position (half-open interval [Start, End) is empty when Start == End)")
	}

	// The correct way to check if a position matches a point span's location
	// is to compare against Start directly
	if point.Start != pos {
		t.Error("use Start equality for point span position checks")
	}
}

// TestSpan_PointContains_WithBytes tests point span behavior with byte offsets.
func TestSpan_PointContains_WithBytes(t *testing.T) {
	point := PointWithByte(testSource, 5, 10, 100)
	pos := Position{Line: 5, Column: 10, Byte: 100}

	// Even with matching byte offsets, point span contains nothing
	if point.Contains(pos) {
		t.Error("point span with bytes should not contain its own position")
	}

	// Verify position equality still works
	if point.Start != pos {
		t.Error("position equality should work for point span start")
	}
}

// TestSpan_PartialPositions_Contains documents that spans with partial positions
// behave defensively - positionBefore returns false for partial positions.
func TestSpan_PartialPositions_Contains(t *testing.T) {
	// A span where End has a partial position (Line > 0, Column == 0)
	span := Span{
		Source: testSource,
		Start:  Position{Line: 1, Column: 1, Byte: -1},
		End:    Position{Line: 5, Column: 0, Byte: -1}, // partial - column is 0
	}

	pos := Position{Line: 3, Column: 5, Byte: -1}

	// Contains should return false because End.IsKnown() is false
	// and positionBefore will return false
	if span.Contains(pos) {
		t.Error("Contains should return false when End position is partial")
	}
}
