package location

import "testing"

func TestNewPosition(t *testing.T) {
	p := NewPosition(10, 5, 42)
	if p.Line != 10 {
		t.Errorf("Line = %d; want 10", p.Line)
	}
	if p.Column != 5 {
		t.Errorf("Column = %d; want 5", p.Column)
	}
	if p.Byte != 42 {
		t.Errorf("Byte = %d; want 42", p.Byte)
	}
}

func TestUnknownPosition(t *testing.T) {
	p := UnknownPosition()
	if p.Line != 0 {
		t.Errorf("Line = %d; want 0", p.Line)
	}
	if p.Column != 0 {
		t.Errorf("Column = %d; want 0", p.Column)
	}
	if p.Byte != -1 {
		t.Errorf("Byte = %d; want -1", p.Byte)
	}
	if !p.IsZero() {
		t.Error("UnknownPosition should be zero")
	}
}

func TestPosition_IsZero(t *testing.T) {
	tests := []struct {
		name string
		pos  Position
		want bool
	}{
		{
			name: "zero value",
			pos:  Position{},
			want: true,
		},
		{
			name: "unknown position",
			pos:  UnknownPosition(),
			want: true,
		},
		{
			name: "zero line and column with byte 0",
			pos:  Position{Line: 0, Column: 0, Byte: 0},
			want: true,
		},
		{
			name: "known position at start of file",
			pos:  Position{Line: 1, Column: 1, Byte: 0},
			want: false,
		},
		{
			name: "known position without byte",
			pos:  Position{Line: 5, Column: 10, Byte: -1},
			want: false,
		},
		{
			name: "only line set",
			pos:  Position{Line: 1, Column: 0, Byte: -1},
			want: false, // Line != 0, but this is a partial position
		},
		{
			name: "only column set",
			pos:  Position{Line: 0, Column: 1, Byte: -1},
			want: false, // Column != 0, but this is a partial position
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.pos.IsZero(); got != tt.want {
				t.Errorf("IsZero() = %v; want %v", got, tt.want)
			}
		})
	}
}

func TestPosition_IsKnown(t *testing.T) {
	tests := []struct {
		name string
		pos  Position
		want bool
	}{
		{
			name: "zero value",
			pos:  Position{},
			want: false,
		},
		{
			name: "unknown position",
			pos:  UnknownPosition(),
			want: false,
		},
		{
			name: "known position at start",
			pos:  Position{Line: 1, Column: 1, Byte: 0},
			want: true,
		},
		{
			name: "known position without byte",
			pos:  Position{Line: 5, Column: 10, Byte: -1},
			want: true,
		},
		{
			name: "only line set",
			pos:  Position{Line: 1, Column: 0, Byte: -1},
			want: false,
		},
		{
			name: "only column set",
			pos:  Position{Line: 0, Column: 1, Byte: -1},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.pos.IsKnown(); got != tt.want {
				t.Errorf("IsKnown() = %v; want %v", got, tt.want)
			}
		})
	}
}

func TestPosition_HasByte(t *testing.T) {
	tests := []struct {
		name string
		pos  Position
		want bool
	}{
		{
			name: "zero value - byte is 0 but position is zero",
			pos:  Position{},
			want: false, // Critical: zero position with Byte=0 should NOT have byte
		},
		{
			name: "unknown position",
			pos:  UnknownPosition(),
			want: false,
		},
		{
			name: "known position with byte 0 (start of file)",
			pos:  Position{Line: 1, Column: 1, Byte: 0},
			want: true,
		},
		{
			name: "known position with positive byte",
			pos:  Position{Line: 5, Column: 10, Byte: 42},
			want: true,
		},
		{
			name: "known position without byte",
			pos:  Position{Line: 5, Column: 10, Byte: -1},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.pos.HasByte(); got != tt.want {
				t.Errorf("HasByte() = %v; want %v", got, tt.want)
			}
		})
	}
}

func TestPosition_String(t *testing.T) {
	tests := []struct {
		name string
		pos  Position
		want string
	}{
		{
			name: "zero value",
			pos:  Position{},
			want: "<unknown>",
		},
		{
			name: "unknown position",
			pos:  UnknownPosition(),
			want: "<unknown>",
		},
		{
			name: "known position",
			pos:  Position{Line: 10, Column: 5, Byte: 42},
			want: "10:5",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.pos.String(); got != tt.want {
				t.Errorf("String() = %q; want %q", got, tt.want)
			}
		})
	}
}

func TestPosition_Before(t *testing.T) {
	tests := []struct {
		name  string
		p     Position
		other Position
		want  bool
	}{
		{
			name:  "same position",
			p:     Position{Line: 5, Column: 10, Byte: -1},
			other: Position{Line: 5, Column: 10, Byte: -1},
			want:  false,
		},
		{
			name:  "earlier line",
			p:     Position{Line: 4, Column: 10, Byte: -1},
			other: Position{Line: 5, Column: 1, Byte: -1},
			want:  true,
		},
		{
			name:  "later line",
			p:     Position{Line: 6, Column: 1, Byte: -1},
			other: Position{Line: 5, Column: 10, Byte: -1},
			want:  false,
		},
		{
			name:  "same line earlier column",
			p:     Position{Line: 5, Column: 5, Byte: -1},
			other: Position{Line: 5, Column: 10, Byte: -1},
			want:  true,
		},
		{
			name:  "same line later column",
			p:     Position{Line: 5, Column: 15, Byte: -1},
			other: Position{Line: 5, Column: 10, Byte: -1},
			want:  false,
		},
		{
			name:  "unknown first position",
			p:     UnknownPosition(),
			other: Position{Line: 5, Column: 10, Byte: -1},
			want:  false,
		},
		{
			name:  "unknown second position",
			p:     Position{Line: 5, Column: 10, Byte: -1},
			other: UnknownPosition(),
			want:  false,
		},
		{
			name:  "both unknown",
			p:     UnknownPosition(),
			other: UnknownPosition(),
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.p.Before(tt.other); got != tt.want {
				t.Errorf("Before() = %v; want %v", got, tt.want)
			}
		})
	}
}

func TestPosition_After(t *testing.T) {
	tests := []struct {
		name  string
		p     Position
		other Position
		want  bool
	}{
		{
			name:  "same position",
			p:     Position{Line: 5, Column: 10, Byte: -1},
			other: Position{Line: 5, Column: 10, Byte: -1},
			want:  false,
		},
		{
			name:  "earlier line",
			p:     Position{Line: 4, Column: 10, Byte: -1},
			other: Position{Line: 5, Column: 1, Byte: -1},
			want:  false,
		},
		{
			name:  "later line",
			p:     Position{Line: 6, Column: 1, Byte: -1},
			other: Position{Line: 5, Column: 10, Byte: -1},
			want:  true,
		},
		{
			name:  "same line earlier column",
			p:     Position{Line: 5, Column: 5, Byte: -1},
			other: Position{Line: 5, Column: 10, Byte: -1},
			want:  false,
		},
		{
			name:  "same line later column",
			p:     Position{Line: 5, Column: 15, Byte: -1},
			other: Position{Line: 5, Column: 10, Byte: -1},
			want:  true,
		},
		{
			name:  "unknown first position",
			p:     UnknownPosition(),
			other: Position{Line: 5, Column: 10, Byte: -1},
			want:  false,
		},
		{
			name:  "unknown second position",
			p:     Position{Line: 5, Column: 10, Byte: -1},
			other: UnknownPosition(),
			want:  false,
		},
		{
			name:  "both unknown",
			p:     UnknownPosition(),
			other: UnknownPosition(),
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.p.After(tt.other); got != tt.want {
				t.Errorf("After() = %v; want %v", got, tt.want)
			}
		})
	}
}

func TestPosition_BeforeAfter_Relationship(t *testing.T) {
	// When p.Before(other) is true, p.After(other) should be false
	// and other.Before(p) should be false, and other.After(p) should be true
	p := Position{Line: 3, Column: 5, Byte: -1}
	other := Position{Line: 5, Column: 10, Byte: -1}

	if !p.Before(other) {
		t.Error("p should be before other")
	}
	if p.After(other) {
		t.Error("p should not be after other")
	}
	if other.Before(p) {
		t.Error("other should not be before p")
	}
	if !other.After(p) {
		t.Error("other should be after p")
	}
}

func TestPosition_Equality(t *testing.T) {
	// Go struct equality should work as expected
	p1 := Position{Line: 5, Column: 10, Byte: 42}
	p2 := Position{Line: 5, Column: 10, Byte: 42}
	p3 := Position{Line: 5, Column: 10, Byte: 43}

	if p1 != p2 {
		t.Error("identical positions should be equal")
	}
	if p1 == p3 {
		t.Error("positions with different bytes should not be equal")
	}
}

// TestPosition_RuneBasedColumnSemantics documents the expected column semantics
// for multi-byte characters. The Column field counts Unicode code points (runes),
// not bytes.
//
// This test demonstrates the expected relationship between byte offsets and
// rune-based columns for strings containing multi-byte UTF-8 characters.
func TestPosition_RuneBasedColumnSemantics(t *testing.T) {
	// Example: "héllo" where 'é' is a 2-byte UTF-8 character (U+00E9)
	// Byte offsets:  h=0, é=1-2, l=3, l=4, o=5
	// Rune columns:  h=1, é=2,   l=3, l=4, o=5
	//
	// Position.Column should be the rune column (1-based), not the byte offset.

	tests := []struct {
		name       string
		line       int
		column     int // rune-based column (1-based)
		byteOffset int // 0-based byte offset
		desc       string
	}{
		{
			name:       "ASCII character 'h'",
			line:       1,
			column:     1, // First rune
			byteOffset: 0, // First byte
			desc:       "ASCII: rune column equals byte offset + 1",
		},
		{
			name:       "2-byte character 'é' (U+00E9)",
			line:       1,
			column:     2, // Second rune
			byteOffset: 1, // Bytes 1-2 for this character
			desc:       "Multi-byte: column is rune position, not byte position",
		},
		{
			name:       "ASCII after multi-byte 'l'",
			line:       1,
			column:     3, // Third rune
			byteOffset: 3, // Byte offset diverges from column
			desc:       "After multi-byte: column 3 but byte offset 3",
		},
		{
			name:       "3-byte character '日' (U+65E5)",
			line:       1,
			column:     1, // First rune
			byteOffset: 0, // Bytes 0-2 for this character
			desc:       "CJK: 3-byte character still counts as 1 column",
		},
		{
			name:       "4-byte emoji '😀' (U+1F600)",
			line:       1,
			column:     1, // First rune
			byteOffset: 0, // Bytes 0-3 for this character
			desc:       "Emoji: 4-byte character still counts as 1 column",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a position with the expected rune-based column
			p := NewPosition(tt.line, tt.column, tt.byteOffset)

			// Verify the position stores values correctly
			if p.Line != tt.line {
				t.Errorf("Line = %d; want %d", p.Line, tt.line)
			}
			if p.Column != tt.column {
				t.Errorf("Column = %d; want %d (%s)", p.Column, tt.column, tt.desc)
			}
			if p.Byte != tt.byteOffset {
				t.Errorf("Byte = %d; want %d", p.Byte, tt.byteOffset)
			}

			// Verify position is known
			if !p.IsKnown() {
				t.Error("position should be known")
			}
		})
	}
}

// TestPosition_ColumnVsByteOffset demonstrates the distinction between
// rune-based columns and byte offsets using concrete examples.
func TestPosition_ColumnVsByteOffset(t *testing.T) {
	// Consider the string "日本語" (Japanese for "Japanese language")
	// Each character is 3 bytes in UTF-8:
	//   日 = bytes 0-2, column 1
	//   本 = bytes 3-5, column 2
	//   語 = bytes 6-8, column 3
	//
	// The key insight: Column counts runes (1, 2, 3), not bytes (0, 3, 6).

	positions := []struct {
		char   string
		column int // 1-based rune position
		byte   int // 0-based byte offset
	}{
		{"日", 1, 0},
		{"本", 2, 3},
		{"語", 3, 6},
	}

	for _, pos := range positions {
		p := NewPosition(1, pos.column, pos.byte)

		// The Column field should reflect rune position, not byte offset
		if p.Column != pos.column {
			t.Errorf("Character %q: Column = %d; want rune position %d", pos.char, p.Column, pos.column)
		}

		// The Byte field captures the actual byte offset
		if p.Byte != pos.byte {
			t.Errorf("Character %q: Byte = %d; want byte offset %d", pos.char, p.Byte, pos.byte)
		}
	}
}

// TestNewPosition_NegativeValues documents that NewPosition stores values as-is
// without clamping. Negative values are stored directly.
func TestNewPosition_NegativeValues(t *testing.T) {
	tests := []struct {
		name   string
		line   int
		column int
		byte   int
	}{
		{"negative line", -1, 5, 10},
		{"negative column", 5, -1, 10},
		{"negative both", -1, -1, 10},
		{"negative byte", 5, 5, -1},
		{"all negative", -1, -1, -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewPosition(tt.line, tt.column, tt.byte)

			// Values should be stored as-is
			if p.Line != tt.line {
				t.Errorf("Line = %d; want %d", p.Line, tt.line)
			}
			if p.Column != tt.column {
				t.Errorf("Column = %d; want %d", p.Column, tt.column)
			}
			if p.Byte != tt.byte {
				t.Errorf("Byte = %d; want %d", p.Byte, tt.byte)
			}

			// Negative line/column: IsZero returns false (not 0,0), IsKnown returns false (not > 0)
			if tt.line < 0 || tt.column < 0 {
				if p.IsKnown() {
					t.Error("negative position should not be known")
				}
			}
			if tt.line < 0 && tt.column < 0 {
				if p.IsZero() {
					t.Error("negative position should not be zero (zero requires Line==0 && Column==0)")
				}
			}
		})
	}
}

// TestPosition_PartialPositions_Before documents that partial positions
// (Line > 0, Column == 0 or vice versa) return false from Before/After
// because they are not fully known (IsKnown returns false).
func TestPosition_PartialPositions_Before(t *testing.T) {
	known := Position{Line: 5, Column: 10, Byte: -1}

	tests := []struct {
		name  string
		pos   Position
		other Position
		want  bool
	}{
		{
			name:  "partial line only vs known",
			pos:   Position{Line: 1, Column: 0, Byte: -1},
			other: known,
			want:  false, // partial position is not known
		},
		{
			name:  "partial column only vs known",
			pos:   Position{Line: 0, Column: 1, Byte: -1},
			other: known,
			want:  false, // partial position is not known
		},
		{
			name:  "known vs partial line only",
			pos:   known,
			other: Position{Line: 10, Column: 0, Byte: -1},
			want:  false, // partial position is not known
		},
		{
			name:  "known vs partial column only",
			pos:   known,
			other: Position{Line: 0, Column: 10, Byte: -1},
			want:  false, // partial position is not known
		},
		{
			name:  "both partial positions",
			pos:   Position{Line: 1, Column: 0, Byte: -1},
			other: Position{Line: 0, Column: 1, Byte: -1},
			want:  false, // neither is known
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.pos.Before(tt.other); got != tt.want {
				t.Errorf("Before() = %v; want %v", got, tt.want)
			}
		})
	}
}

// TestPosition_PartialPositions_After documents that partial positions
// return false from After because they are not fully known.
func TestPosition_PartialPositions_After(t *testing.T) {
	known := Position{Line: 5, Column: 10, Byte: -1}

	tests := []struct {
		name  string
		pos   Position
		other Position
		want  bool
	}{
		{
			name:  "partial line only vs known",
			pos:   Position{Line: 10, Column: 0, Byte: -1},
			other: known,
			want:  false, // partial position is not known
		},
		{
			name:  "known vs partial line only",
			pos:   known,
			other: Position{Line: 1, Column: 0, Byte: -1},
			want:  false, // partial position is not known
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.pos.After(tt.other); got != tt.want {
				t.Errorf("After() = %v; want %v", got, tt.want)
			}
		})
	}
}

// TestPosition_ByteOnlyPosition documents HasByte behavior for various position states.
// HasByte checks `Byte >= 0 && !IsZero()`. Note that partial positions (Line > 0 but
// Column == 0, or vice versa) are NOT zero, so HasByte returns true for them.
// This is the current design - the byte offset is considered valid as long as the
// position is not the zero value.
func TestPosition_ByteOnlyPosition(t *testing.T) {
	tests := []struct {
		name    string
		pos     Position
		hasByte bool
	}{
		{
			name:    "true zero position - byte 100",
			pos:     Position{Line: 0, Column: 0, Byte: 100},
			hasByte: false, // IsZero is true, so HasByte is false
		},
		{
			name:    "true zero position - byte 0",
			pos:     Position{Line: 0, Column: 0, Byte: 0},
			hasByte: false, // IsZero is true, so HasByte is false (prevents Go zero value trap)
		},
		{
			name:    "partial line - not zero so HasByte is true",
			pos:     Position{Line: 1, Column: 0, Byte: 100},
			hasByte: true, // IsZero is false (Line != 0), so HasByte is true
		},
		{
			name:    "partial column - not zero so HasByte is true",
			pos:     Position{Line: 0, Column: 1, Byte: 100},
			hasByte: true, // IsZero is false (Column != 0), so HasByte is true
		},
		{
			name:    "fully known with byte",
			pos:     Position{Line: 1, Column: 1, Byte: 100},
			hasByte: true, // Both line/column known and byte >= 0
		},
		{
			name:    "fully known without byte",
			pos:     Position{Line: 1, Column: 1, Byte: -1},
			hasByte: false, // Byte is -1 (unknown)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.pos.HasByte(); got != tt.hasByte {
				t.Errorf("HasByte() = %v; want %v", got, tt.hasByte)
			}
		})
	}
}
