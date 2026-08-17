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
