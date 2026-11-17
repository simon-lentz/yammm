package location

import (
	"path/filepath"
	"testing"
)

var testSource = SourceID{synthetic: "test://unit"}

// at returns the span on one line from column c1 at offset o1 to column c2 at
// offset o2.
func at(line, c1, o1, c2, o2 int) Span {
	return Span{
		Source: testSource,
		Start:  Position{Line: line, Column: c1, Offset: o1},
		End:    Position{Line: line, Column: c2, Offset: o2},
	}
}

func TestSpan_IsZero(t *testing.T) {
	t.Parallel()
	if !(Span{}).IsZero() {
		t.Error("the zero Span must report zero")
	}
	for name, s := range map[string]Span{
		"a source alone":   {Source: testSource},
		"a start alone":    {Start: Position{Line: 1, Column: 1}},
		"an offset alone":  {End: Position{Offset: 3}},
		"a complete point": at(1, 1, 0, 1, 0),
	} {
		if s.IsZero() {
			t.Errorf("%s: %v reports zero", name, s)
		}
	}
}

func TestSpan_String(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		span Span
		want string
	}{
		{"zero span", Span{}, "<no location>"},
		{"point span", at(10, 5, 40, 5, 40), "test://unit:10:5"},
		{"range span", at(10, 5, 40, 15, 50), "test://unit:10:5-10:15"},
		{"a range whose ends share a line and column", at(1, 1, 0, 1, 5), "test://unit:1:1-1:1"},
		{"range with an unknown end", Span{Source: testSource, Start: Position{Line: 10, Column: 5}}, "test://unit:10:5-<unknown>"},
		{"range with an unknown start", Span{Source: testSource, End: Position{Line: 10, Column: 15}}, "test://unit:<unknown>-10:15"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.span.String(); got != tt.want {
				t.Errorf("String() = %q; want %q", got, tt.want)
			}
		})
	}
}

// TestSpan_Contains holds Contains to the half-open byte range [Start, End):
// the offsets decide, and a span or position that is not valid contains, and
// is contained by, nothing. A point span is empty.
func TestSpan_Contains(t *testing.T) {
	t.Parallel()
	s := at(5, 10, 100, 20, 110)
	tests := []struct {
		name string
		span Span
		pos  Position
		want bool
	}{
		{"before the range", s, Position{Line: 5, Column: 5, Offset: 95}, false},
		{"at the start", s, Position{Line: 5, Column: 10, Offset: 100}, true},
		{"in the middle", s, Position{Line: 5, Column: 15, Offset: 105}, true},
		{"at the end, which is exclusive", s, Position{Line: 5, Column: 20, Offset: 110}, false},
		{"after the range", s, Position{Line: 5, Column: 25, Offset: 115}, false},
		{"the offset decides over the column", s, Position{Line: 5, Column: 99, Offset: 105}, true},
		{"an unknown position", s, Position{}, false},
		{"an unknown position whose offset falls inside", s, Position{Offset: 105}, false},
		{"a point span's own position", at(5, 10, 100, 10, 100), Position{Line: 5, Column: 10, Offset: 100}, false},
		{"a span with an unknown start", Span{Source: testSource, End: s.End}, Position{Line: 5, Column: 1, Offset: 0}, false},
		{"a span with an unknown end", Span{Source: testSource, Start: Position{Line: 1, Column: 1}}, Position{Line: 5, Column: 1, Offset: 3}, false},
		{"a span whose unknown end holds an offset past the position", Span{Source: testSource, Start: Position{Line: 1, Column: 1}, End: Position{Offset: 50}}, Position{Line: 1, Column: 5, Offset: 10}, false},
		{"the first byte of a span that starts there", at(1, 1, 0, 4, 3), Position{Line: 1, Column: 1, Offset: 0}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.span.Contains(tt.pos); got != tt.want {
				t.Errorf("%v.Contains(%#v) = %v; want %v", tt.span, tt.pos, got, tt.want)
			}
		})
	}
}

// TestSpan_Compare_ZeroOnlyForEqualSpans holds Compare to its contract: it
// returns 0 exactly when the two spans are ==, so a sort by it ties nothing
// distinct.
func TestSpan_Compare_ZeroOnlyForEqualSpans(t *testing.T) {
	t.Parallel()

	file, err := SourceIDFromPath(filepath.Join(t.TempDir(), "a.yammm"))
	if err != nil {
		t.Fatal(err)
	}
	pointIn := func(src SourceID) Span {
		return Span{Source: src, Start: Position{Line: 1, Column: 1}, End: Position{Line: 1, Column: 1}}
	}
	pairs := []struct {
		name string
		a, b Span
	}{
		{"two start offsets", at(1, 1, 0, 1, 0), at(1, 1, 4, 1, 4)},
		{"two end offsets", at(1, 1, 0, 5, 4), at(1, 1, 0, 5, 9)},
		{"two start offsets before one end offset", at(1, 1, 0, 5, 9), at(1, 1, 2, 5, 9)},
		{"a synthetic and a file-backed source", pointIn(testSource), pointIn(file)},
		{"the zero source and a synthetic one", pointIn(SourceID{}), pointIn(testSource)},
	}
	for _, p := range pairs {
		t.Run(p.name, func(t *testing.T) {
			t.Parallel()
			if p.a == p.b {
				t.Fatalf("the fixture's spans are equal: %v", p.a)
			}
			c := p.a.Compare(p.b)
			if c == 0 {
				t.Errorf("Compare(%v, %v) = 0 for spans that differ", p.a, p.b)
			}
			if back := p.b.Compare(p.a); back != -c {
				t.Errorf("Compare is not antisymmetric: %d one way, %d the other", c, back)
			}
			if self := p.a.Compare(p.a); self != 0 { //nolint:gocritic // a span compares equal to itself
				t.Errorf("Compare(a, a) = %d, want 0", self)
			}
		})
	}
}

func TestSpan_Compare(t *testing.T) {
	t.Parallel()
	in := func(src string, s Span) Span { s.Source = SourceID{synthetic: src}; return s }
	tests := []struct {
		name string
		a    Span
		b    Span
		want int
	}{
		{"equal", at(5, 10, 40, 20, 50), at(5, 10, 40, 20, 50), 0},
		{"a before b (line)", at(4, 10, 40, 20, 50), at(5, 10, 40, 20, 50), -1},
		{"a after b (line)", at(6, 10, 40, 20, 50), at(5, 10, 40, 20, 50), 1},
		{"a before b (column)", at(5, 5, 40, 15, 50), at(5, 10, 40, 20, 50), -1},
		{"same start, different end", at(5, 10, 40, 15, 50), at(5, 10, 40, 20, 50), -1},
		{"different source (alphabetic)", in("aaa://", at(5, 10, 40, 20, 50)), in("bbb://", at(5, 10, 40, 20, 50)), -1},
		{"the source decides before the position", in("aaa://", at(9, 10, 90, 20, 99)), in("bbb://", at(1, 1, 0, 2, 1)), -1},
		{"end position decides before the start offset", at(1, 1, 9, 5, 20), at(1, 1, 0, 6, 20), -1},
		{"start offset decides before the end offset", at(1, 1, 0, 5, 9), at(1, 1, 2, 5, 4), -1},
		{"smaller start offset first", at(1, 1, 0, 5, 9), at(1, 1, 4, 5, 9), -1},
		{"smaller end offset first", at(1, 1, 0, 5, 4), at(1, 1, 0, 5, 9), -1},
		{"an unknown start before a known one", Span{Source: testSource}, at(1, 1, 0, 1, 0), -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.a.Compare(tt.b); got != tt.want {
				t.Errorf("%v.Compare(%v) = %d; want %d", tt.a, tt.b, got, tt.want)
			}
			if got := tt.b.Compare(tt.a); got != -tt.want {
				t.Errorf("%v.Compare(%v) = %d; want %d", tt.b, tt.a, got, -tt.want)
			}
		})
	}
}

func TestSpan_MapKey(t *testing.T) {
	t.Parallel()
	m := map[Span]int{at(5, 10, 40, 20, 50): 42}
	if m[at(5, 10, 40, 20, 50)] != 42 {
		t.Error("equal spans should work as map keys")
	}
	if _, ok := m[at(5, 10, 40, 21, 51)]; ok {
		t.Error("a different span found the key")
	}
}
