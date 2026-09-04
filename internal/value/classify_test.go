package value_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/simon-lentz/yammm/internal/value"
)

func TestClassify_BaseKinds(t *testing.T) {
	tests := []struct {
		name      string
		input     any
		wantKind  value.Kind
		wantNorm  any
		checkNorm bool
	}{
		// nil
		{"nil", nil, value.UnspecifiedKind, nil, true},

		// Booleans
		{"bool true", true, value.BoolKind, true, true},
		{"bool false", false, value.BoolKind, false, true},

		// Strings
		{"string", "hello", value.StringKind, "hello", true},
		{"string empty", "", value.StringKind, "", true},

		// Signed integers
		{"int", int(42), value.IntKind, int(42), true},
		{"int8", int8(42), value.IntKind, int8(42), true},
		{"int16", int16(42), value.IntKind, int16(42), true},
		{"int32", int32(42), value.IntKind, int32(42), true},
		{"int64", int64(42), value.IntKind, int64(42), true},

		// Unsigned integers
		{"uint", uint(42), value.IntKind, uint(42), true},
		{"uint8", uint8(42), value.IntKind, uint8(42), true},
		{"uint16", uint16(42), value.IntKind, uint16(42), true},
		{"uint32", uint32(42), value.IntKind, uint32(42), true},
		{"uint64", uint64(42), value.IntKind, uint64(42), true},

		// Floats
		{"float32", float32(3.14), value.FloatKind, float32(3.14), true},
		{"float64", float64(3.14), value.FloatKind, float64(3.14), true},

		// Unsupported types
		{"struct", struct{}{}, value.UnspecifiedKind, struct{}{}, true},
		{"map", map[string]int{}, value.UnspecifiedKind, map[string]int{}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kind, norm := value.Classify(tt.input)
			if kind != tt.wantKind {
				t.Errorf("Classify(%v) kind = %v, want %v", tt.input, kind, tt.wantKind)
			}
			if tt.checkNorm && !reflect.DeepEqual(norm, tt.wantNorm) {
				t.Errorf("Classify(%v) normalized = %v, want %v", tt.input, norm, tt.wantNorm)
			}
		})
	}
}

func TestClassify_JSONNumber(t *testing.T) {
	tests := []struct {
		name     string
		input    json.Number
		wantKind value.Kind
		wantNorm any
	}{
		// Integer json.Numbers (no decimal) -> IntKind
		{"integer", json.Number("42"), value.IntKind, int64(42)},
		{"integer negative", json.Number("-10"), value.IntKind, int64(-10)},
		{"integer zero", json.Number("0"), value.IntKind, int64(0)},
		{"integer large", json.Number("9007199254740993"), value.IntKind, int64(9007199254740993)},

		// Float json.Numbers (has decimal) -> FloatKind
		{"float", json.Number("3.14"), value.FloatKind, float64(3.14)},
		{"float whole number", json.Number("3.0"), value.FloatKind, float64(3.0)}, // Has decimal -> Float
		{"float negative", json.Number("-2.5"), value.FloatKind, float64(-2.5)},
		{"float scientific", json.Number("1.5e10"), value.FloatKind, float64(1.5e10)},

		// Invalid json.Numbers -> UnspecifiedKind
		{"invalid", json.Number("invalid"), value.UnspecifiedKind, json.Number("invalid")},
		{"empty", json.Number(""), value.UnspecifiedKind, json.Number("")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kind, norm := value.Classify(tt.input)
			if kind != tt.wantKind {
				t.Errorf("Classify(%q) kind = %v, want %v", tt.input, kind, tt.wantKind)
			}
			if !reflect.DeepEqual(norm, tt.wantNorm) {
				t.Errorf("Classify(%q) normalized = %v (%T), want %v (%T)",
					tt.input, norm, norm, tt.wantNorm, tt.wantNorm)
			}
		})
	}
}

func TestClassify_Unspecified(t *testing.T) {
	tests := []struct {
		name  string
		input any
	}{
		{"nil", nil},
		{"float64 slice", []float64{1}},
		{"numeric interface slice", []any{1.5, 2}},
		{"json.Number slice", []json.Number{json.Number("1")}},
		{"string slice", []any{"x", "y"}},
		{"int slice (typed)", []int{1, 2}}, // typed int slice, not []any
		{"uint slice (typed)", []uint{1, 2, 3}},
		{"nil int slice", []int(nil)},
		{"empty int slice", []int{}},
		{"empty interface slice", []any{}},
		{"empty json.Number slice", []json.Number{}},
		{"invalid json.Number in slice", []json.Number{json.Number("1.0"), json.Number("invalid"), json.Number("3.0")}},
		{"empty string slice", []string{}},
		{"nil string slice", []string(nil)},
		{"mixed non-numeric", []any{1, "x", 3}},
		{"struct", struct{}{}},
		{"map", map[string]int{"a": 1}},
		{"channel", make(chan int)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kind, _ := value.Classify(tt.input)
			if kind != value.UnspecifiedKind {
				t.Errorf("expected UnspecifiedKind, got %v", kind)
			}
		})
	}
}

func TestKind_String(t *testing.T) {
	tests := []struct {
		kind value.Kind
		want string
	}{
		{value.UnspecifiedKind, "UnspecifiedKind"},
		{value.StringKind, "StringKind"},
		{value.IntKind, "IntKind"},
		{value.FloatKind, "FloatKind"},
		{value.BoolKind, "BoolKind"},
		{value.Kind(99), "UnknownKind"}, // Unknown kind
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.kind.String(); got != tt.want {
				t.Errorf("Kind(%d).String() = %q, want %q", tt.kind, got, tt.want)
			}
		})
	}
}

func TestClassify_FloatPrecision(t *testing.T) {
	// Test float precision edge cases (> 2^53)
	// Per architecture doc: "Precision warning for Float: When a large integer (> 2^53)
	// is used as a Float, precision may be lost during the Float64() conversion."

	t.Run("large integer as IntKind preserves precision", func(t *testing.T) {
		// 9007199254740993 = 2^53 + 1 (just above MAX_SAFE_INTEGER)
		input := json.Number("9007199254740993")
		kind, norm := value.Classify(input)
		if kind != value.IntKind {
			t.Errorf("expected IntKind, got %v", kind)
		}
		// Should preserve exact value as int64
		if norm != int64(9007199254740993) {
			t.Errorf("expected exact int64, got %v", norm)
		}
	})

	t.Run("large integer as float loses precision", func(t *testing.T) {
		// When same value is treated as float, precision is lost
		// This documents the expected behavior per spec
		largeInt := int64(9007199254740993)
		asFloat := float64(largeInt)
		// Due to float64 precision limits, this will not equal the original
		if int64(asFloat) == largeInt {
			t.Skip("platform preserves precision - cannot test precision loss")
		}
		// This test documents that precision loss occurs
		t.Logf("large int %d as float64 becomes %f (precision lost)", largeInt, asFloat)
	})
}

// Helper functions for pointer tests

func ptrptr[T any](v T) **T {
	p := &v
	return &p
}

func TestClassify_PointerDereferencing(t *testing.T) {
	// Classify dereferences pointers
	t.Run("*int returns IntKind", func(t *testing.T) {
		input := new(42)
		kind, norm := value.Classify(input)
		if kind != value.IntKind {
			t.Errorf("expected IntKind for *int, got %v", kind)
		}
		if norm != 42 {
			t.Errorf("expected 42, got %v", norm)
		}
	})

	t.Run("*string returns StringKind", func(t *testing.T) {
		input := new("hello")
		kind, norm := value.Classify(input)
		if kind != value.StringKind {
			t.Errorf("expected StringKind for *string, got %v", kind)
		}
		if norm != "hello" {
			t.Errorf("expected hello, got %v", norm)
		}
	})

	t.Run("*float64 returns FloatKind", func(t *testing.T) {
		input := new(3.14)
		kind, norm := value.Classify(input)
		if kind != value.FloatKind {
			t.Errorf("expected FloatKind for *float64, got %v", kind)
		}
		if norm != 3.14 {
			t.Errorf("expected 3.14, got %v", norm)
		}
	})

	t.Run("*bool returns BoolKind", func(t *testing.T) {
		input := new(true)
		kind, norm := value.Classify(input)
		if kind != value.BoolKind {
			t.Errorf("expected BoolKind for *bool, got %v", kind)
		}
		if norm != true {
			t.Errorf("expected true, got %v", norm)
		}
	})

	t.Run("**int returns IntKind", func(t *testing.T) {
		input := ptrptr(42)
		kind, norm := value.Classify(input)
		if kind != value.IntKind {
			t.Errorf("expected IntKind for **int, got %v", kind)
		}
		if norm != 42 {
			t.Errorf("expected 42, got %v", norm)
		}
	})

	t.Run("nil *int returns UnspecifiedKind", func(t *testing.T) {
		var input *int
		kind, norm := value.Classify(input)
		if kind != value.UnspecifiedKind {
			t.Errorf("expected UnspecifiedKind for nil *int, got %v", kind)
		}
		if norm != nil {
			t.Errorf("expected nil, got %v", norm)
		}
	})

	t.Run("nil **int returns UnspecifiedKind", func(t *testing.T) {
		var input **int
		kind, norm := value.Classify(input)
		if kind != value.UnspecifiedKind {
			t.Errorf("expected UnspecifiedKind for nil **int, got %v", kind)
		}
		if norm != nil {
			t.Errorf("expected nil, got %v", norm)
		}
	})

	t.Run("*int where value is nil pointer", func(t *testing.T) {
		var inner *int
		input := &inner // **int where inner is nil
		kind, norm := value.Classify(input)
		if kind != value.UnspecifiedKind {
			t.Errorf("expected UnspecifiedKind for **int with nil inner, got %v", kind)
		}
		if norm != nil {
			t.Errorf("expected nil, got %v", norm)
		}
	})

	t.Run("*[]float64 follows its target to the slice rule", func(t *testing.T) {
		slice := []float64{1.0, 2.0, 3.0}
		kind, _ := value.Classify(&slice)
		if kind != value.UnspecifiedKind {
			t.Errorf("expected UnspecifiedKind for *[]float64, got %v", kind)
		}
	})
}
