package value_test

import (
	"encoding/json"
	"reflect"
	"slices"
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

func TestClassify_Vector(t *testing.T) {
	t.Run("typed float slices", func(t *testing.T) {
		var nilFloat64 []float64
		var nilFloat32 []float32

		tests := []struct {
			name      string
			input     any
			wantSlice any
		}{
			{"float64 slice", []float64{1.0, 2.5}, []float64{1.0, 2.5}},
			{"float32 slice", []float32{1.0, 2.5}, []float32{1.0, 2.5}},
			{"nil float64 slice", nilFloat64, nilFloat64},
			{"nil float32 slice", nilFloat32, nilFloat32},
			{"empty float64 slice", []float64{}, []float64{}},
			{"empty float32 slice", []float32{}, []float32{}},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				kind, norm := value.Classify(tt.input)
				if kind != value.VectorKind {
					t.Errorf("expected VectorKind, got %v", kind)
				}
				if !reflect.DeepEqual(norm, tt.wantSlice) {
					t.Errorf("expected %v, got %v", tt.wantSlice, norm)
				}
			})
		}
	})

	// []any always produces []float64 (JSON pathway normalization).
	// This matches the architecture spec: "each element is coerced to float64".
	t.Run("[]any float elements", func(t *testing.T) {
		tests := []struct {
			name      string
			input     []any
			wantSlice []float64 // Always []float64 for []any input
		}{
			{"float64 elements", []any{float64(1), float64(2.5)}, []float64{1, 2.5}},
			{"float32 elements", []any{float32(1.25), float32(2.5)}, []float64{1.25, 2.5}}, // float32 → float64
			{"mixed float widths", []any{float32(1.5), float64(2.25)}, []float64{1.5, 2.25}},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				kind, norm := value.Classify(tt.input)
				if kind != value.VectorKind {
					t.Errorf("expected VectorKind, got %v", kind)
				}
				if !reflect.DeepEqual(norm, tt.wantSlice) {
					t.Errorf("expected %v, got %v", tt.wantSlice, norm)
				}
			})
		}
	})

	// Integer elements are accepted in vectors (coerced to float64).
	t.Run("[]any integer elements", func(t *testing.T) {
		tests := []struct {
			name      string
			input     []any
			wantSlice []float64
		}{
			{"int elements", []any{1, 2, 3}, []float64{1, 2, 3}},
			{"int64 elements", []any{int64(10), int64(20)}, []float64{10, 20}},
			{"uint elements", []any{uint(1), uint(2)}, []float64{1, 2}},
			{"mixed int/float", []any{1, 2.5, 3}, []float64{1, 2.5, 3}},
			{"mixed int types", []any{int(1), int64(2), uint(3)}, []float64{1, 2, 3}},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				kind, norm := value.Classify(tt.input)
				if kind != value.VectorKind {
					t.Errorf("expected VectorKind, got %v", kind)
				}
				if !reflect.DeepEqual(norm, tt.wantSlice) {
					t.Errorf("expected %v, got %v", tt.wantSlice, norm)
				}
			})
		}
	})

	t.Run("json.Number elements in vectors", func(t *testing.T) {
		tests := []struct {
			name      string
			input     []json.Number
			wantSlice []float64
		}{
			{"float json.Numbers", []json.Number{json.Number("1.1"), json.Number("2.2")}, []float64{1.1, 2.2}},
			{"int json.Numbers", []json.Number{json.Number("1"), json.Number("2")}, []float64{1, 2}},
			{"mixed json.Numbers", []json.Number{json.Number("1"), json.Number("2.5")}, []float64{1, 2.5}},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				kind, norm := value.Classify(tt.input)
				if kind != value.VectorKind {
					t.Errorf("expected VectorKind, got %v", kind)
				}
				if !reflect.DeepEqual(norm, tt.wantSlice) {
					t.Errorf("expected %v, got %v", tt.wantSlice, norm)
				}
			})
		}
	})
}

func TestClassify_Unspecified(t *testing.T) {
	tests := []struct {
		name  string
		input any
	}{
		{"nil", nil},
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
		{value.VectorKind, "VectorKind"},
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

	t.Run("float64 in vector preserves value", func(t *testing.T) {
		input := []any{float64(3.14159265358979)}
		kind, norm := value.Classify(input)
		if kind != value.VectorKind {
			t.Errorf("expected VectorKind, got %v", kind)
		}
		slice := norm.([]float64)
		if slice[0] != 3.14159265358979 {
			t.Errorf("expected exact float64, got %v", slice[0])
		}
	})
}

// Custom numeric types for testing reflect fallback in toFloat64
type (
	customInt     int
	customInt64   int64
	customUint    uint
	customUint64  uint64
	customFloat32 float32
	customFloat64 float64
)

// TestClassify_VectorElementTypes covers element coercion to []float64 in an
// []any vector across every accepted element representation: each direct
// integer width, uintptr, custom (named) numeric types via the reflect
// fallback, and the rejection rows (nil element, non-numeric custom type).
// A nil want means the vector is rejected as UnspecifiedKind.
func TestClassify_VectorElementTypes(t *testing.T) {
	type customString string
	tests := []struct {
		name  string
		input []any
		want  []float64
	}{
		{"int8", []any{int8(1), int8(2), int8(3)}, []float64{1, 2, 3}},
		{"int16", []any{int16(10), int16(20)}, []float64{10, 20}},
		{"int32", []any{int32(100), int32(200)}, []float64{100, 200}},
		{"uint8", []any{uint8(5), uint8(10)}, []float64{5, 10}},
		{"uint16", []any{uint16(100), uint16(200)}, []float64{100, 200}},
		{"uint32", []any{uint32(1000), uint32(2000)}, []float64{1000, 2000}},
		{"uint64", []any{uint64(10000), uint64(20000)}, []float64{10000, 20000}},
		{"uintptr", []any{uintptr(1), uintptr(2)}, []float64{1, 2}},
		{"customInt", []any{customInt(1), customInt(2), customInt(3)}, []float64{1, 2, 3}},
		{"customInt64", []any{customInt64(10), customInt64(20)}, []float64{10, 20}},
		{"customUint", []any{customUint(5), customUint(10)}, []float64{5, 10}},
		{"customUint64", []any{customUint64(100), customUint64(200)}, []float64{100, 200}},
		{"customFloat32", []any{customFloat32(1.5), customFloat32(2.5)}, []float64{1.5, 2.5}},
		{"customFloat64", []any{customFloat64(3.14), customFloat64(2.71)}, []float64{3.14, 2.71}},
		{"mixed custom types", []any{customInt(1), customFloat64(2.5), customUint(3)}, []float64{1, 2.5, 3}},
		{"nil element rejected", []any{1.0, nil, 3.0}, nil},
		{"non-numeric custom type rejected", []any{customString("hello")}, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kind, norm := value.Classify(tt.input)
			if tt.want == nil {
				if kind != value.UnspecifiedKind {
					t.Errorf("Classify(%v) kind = %v, want UnspecifiedKind", tt.input, kind)
				}
				return
			}
			if kind != value.VectorKind {
				t.Fatalf("Classify(%v) kind = %v, want VectorKind", tt.input, kind)
			}
			got, ok := norm.([]float64)
			if !ok {
				t.Fatalf("normalized = %T, want []float64", norm)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("normalized = %v, want %v", got, tt.want)
			}
		})
	}
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

	t.Run("*[]float64 returns VectorKind", func(t *testing.T) {
		slice := []float64{1.0, 2.0, 3.0}
		input := &slice
		kind, norm := value.Classify(input)
		if kind != value.VectorKind {
			t.Errorf("expected VectorKind for *[]float64, got %v", kind)
		}
		if !reflect.DeepEqual(norm, slice) {
			t.Errorf("expected %v, got %v", slice, norm)
		}
	})
}

// Vec is a named slice type for testing registry hook
type Vec []float64
