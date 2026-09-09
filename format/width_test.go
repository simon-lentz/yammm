package format

import (
	"testing"
	"unicode/utf8"
)

// TestDisplayWidth_ThreeUnitsForOneQuantity measures each string three ways —
// bytes, runes, cells — with the cell count written by hand rather than
// computed. The three disagreeing is the defect this function exists to end: the
// wrap threshold once counted runes, the alignment columns counted bytes, and
// the thing being measured was always cells.
func TestDisplayWidth_ThreeUnitsForOneQuantity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		s     string
		bytes int
		runes int
		cells int
	}{
		{"ascii", "abc", 3, 3, 3},
		{"tab", "\tabc", 4, 4, 7},
		{"cjk wide", "記録", 6, 2, 4},
		{"fullwidth", "ＡＢ", 6, 2, 4},
		{"ambiguous is narrow", "é", 2, 1, 1},
		{"mixed", "id 記録", 9, 5, 7},
		{"ideographic space", "　", 3, 1, 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := len(tt.s); got != tt.bytes {
				t.Errorf("bytes = %d, want %d", got, tt.bytes)
			}
			if got := utf8.RuneCountInString(tt.s); got != tt.runes {
				t.Errorf("runes = %d, want %d", got, tt.runes)
			}
			if got := DisplayWidth(tt.s); got != tt.cells {
				t.Errorf("cells = %d, want %d", got, tt.cells)
			}
		})
	}
}

// TestDisplayWidth_WideRunesAreNotCountedAsOne pins the property the old
// implementation got wrong, independently of any table: a wide rune must not
// measure the same as an ASCII one.
func TestDisplayWidth_WideRunesAreNotCountedAsOne(t *testing.T) {
	t.Parallel()

	if DisplayWidth("記") == DisplayWidth("a") {
		t.Error("a wide rune measures the same as an ASCII rune; the threshold and the columns are counting runes again")
	}
	if DisplayWidth("é") != DisplayWidth("a") {
		t.Error("East Asian Ambiguous measured wide; it is one cell where this output is read")
	}
}
