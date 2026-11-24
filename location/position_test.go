package location

import "testing"

// TestPosition_IsValid holds go/token.Position's model: a position is known
// when its line is, whatever its column and offset, and an offset of 0 is the
// first byte, not an unknown one.
func TestPosition_IsValid(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		pos  Position
		want bool
	}{
		{"the zero value", Position{}, false},
		{"an offset with no line", Position{Offset: 12}, false},
		{"a column with no line", Position{Column: 3}, false},
		{"a negative line", Position{Line: -1, Column: 1}, false},
		{"the first byte", Position{Line: 1, Column: 1, Offset: 0}, true},
		{"a later position", Position{Line: 10, Column: 5, Offset: 42}, true},
		{"a line with no column", Position{Line: 2}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.pos.IsValid(); got != tt.want {
				t.Errorf("%#v.IsValid() = %v; want %v", tt.pos, got, tt.want)
			}
		})
	}
}

func TestPosition_String(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		pos  Position
		want string
	}{
		{"the zero value", Position{}, "<unknown>"},
		{"an offset with no line", Position{Offset: 7}, "<unknown>"},
		{"the first byte", Position{Line: 1, Column: 1}, "1:1"},
		{"a later position", Position{Line: 10, Column: 5, Offset: 42}, "10:5"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.pos.String(); got != tt.want {
				t.Errorf("String() = %q; want %q", got, tt.want)
			}
		})
	}
}
