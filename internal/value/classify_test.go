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

	// V2 CHANGE: Integer elements are now accepted in vectors
	t.Run("[]any integer elements (v2 change)", func(t *testing.T) {
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
		{"empty interface slice", []any{}},
		{"empty json.Number slice", []json.Number{}},
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

func TestClassifyWithRegistry(t *testing.T) {
	// Custom type for testing
	type CustomFloat float64

	t.Run("zero-value registry falls back to builtin", func(t *testing.T) {
		kind, _ := value.ClassifyWithRegistry(value.Registry{}, int(42))
		if kind != value.IntKind {
			t.Errorf("expected IntKind, got %v", kind)
		}
	})

	t.Run("registry with custom type hook", func(t *testing.T) {
		customFloatType := reflect.TypeFor[CustomFloat]()

		registry := value.Registry{
			BaseKindOfReflectType: func(t reflect.Type) value.Kind {
				if t == customFloatType {
					return value.FloatKind
				}
				return value.UnspecifiedKind
			},
		}

		kind, _ := value.ClassifyWithRegistry(registry, CustomFloat(3.14))
		if kind != value.FloatKind {
			t.Errorf("expected FloatKind for CustomFloat, got %v", kind)
		}
	})

	t.Run("registry unrecognized type returns unspecified", func(t *testing.T) {
		registry := value.Registry{
			BaseKindOfReflectType: func(t reflect.Type) value.Kind {
				return value.UnspecifiedKind
			},
		}

		// struct{} should still be unspecified even with registry
		kind, _ := value.ClassifyWithRegistry(registry, struct{}{})
		if kind != value.UnspecifiedKind {
			t.Errorf("expected UnspecifiedKind, got %v", kind)
		}
	})
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

// Spec test vectors from architecture document
func TestClassify_SpecVectors(t *testing.T) {
	t.Run("json.Number coercion table", func(t *testing.T) {
		// JSON Numeric Coercion specification
		tests := []struct {
			input    json.Number
			wantKind value.Kind
			wantVal  any
		}{
			{json.Number("42"), value.IntKind, int64(42)},
			{json.Number("9007199254740993"), value.IntKind, int64(9007199254740993)},
			{json.Number("3.14"), value.FloatKind, float64(3.14)},
			{json.Number("3.0"), value.FloatKind, float64(3.0)}, // Has decimal -> Float, NOT Int
		}

		for _, tt := range tests {
			kind, norm := value.Classify(tt.input)
			if kind != tt.wantKind {
				t.Errorf("Classify(%q): kind = %v, want %v", tt.input, kind, tt.wantKind)
			}
			if !reflect.DeepEqual(norm, tt.wantVal) {
				t.Errorf("Classify(%q): value = %v (%T), want %v (%T)",
					tt.input, norm, norm, tt.wantVal, tt.wantVal)
			}
		}
	})

	t.Run("vector coercion table", func(t *testing.T) {
		// Vector coercion rules
		tests := []struct {
			name     string
			input    any
			wantKind value.Kind
			wantVal  any
		}{
			{"integers to float64", []any{1, 2, 3}, value.VectorKind, []float64{1, 2, 3}},
			{"floats preserved", []any{1.5, 2.5}, value.VectorKind, []float64{1.5, 2.5}},
			{"mixed numeric", []any{1, 2.5, 3}, value.VectorKind, []float64{1, 2.5, 3}},
			{"non-numeric rejected", []any{1, "x", 3}, value.UnspecifiedKind, nil},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				kind, norm := value.Classify(tt.input)
				if kind != tt.wantKind {
					t.Errorf("kind = %v, want %v", kind, tt.wantKind)
				}
				if tt.wantKind == value.VectorKind && !reflect.DeepEqual(norm, tt.wantVal) {
					t.Errorf("value = %v, want %v", norm, tt.wantVal)
				}
			})
		}
	})
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

func TestClassify_VectorWithCustomNumericTypes(t *testing.T) {
	// These tests exercise the reflect fallback path in toFloat64 (classify.go:232-246)
	// which handles custom numeric types that don't match the direct type switch cases.

	t.Run("[]any with customInt elements", func(t *testing.T) {
		input := []any{customInt(1), customInt(2), customInt(3)}
		kind, norm := value.Classify(input)
		if kind != value.VectorKind {
			t.Errorf("expected VectorKind, got %v", kind)
		}
		expected := []float64{1, 2, 3}
		if !reflect.DeepEqual(norm, expected) {
			t.Errorf("expected %v, got %v", expected, norm)
		}
	})

	t.Run("[]any with customInt64 elements", func(t *testing.T) {
		input := []any{customInt64(10), customInt64(20)}
		kind, norm := value.Classify(input)
		if kind != value.VectorKind {
			t.Errorf("expected VectorKind, got %v", kind)
		}
		expected := []float64{10, 20}
		if !reflect.DeepEqual(norm, expected) {
			t.Errorf("expected %v, got %v", expected, norm)
		}
	})

	t.Run("[]any with customUint elements", func(t *testing.T) {
		input := []any{customUint(5), customUint(10)}
		kind, norm := value.Classify(input)
		if kind != value.VectorKind {
			t.Errorf("expected VectorKind, got %v", kind)
		}
		expected := []float64{5, 10}
		if !reflect.DeepEqual(norm, expected) {
			t.Errorf("expected %v, got %v", expected, norm)
		}
	})

	t.Run("[]any with customUint64 elements", func(t *testing.T) {
		input := []any{customUint64(100), customUint64(200)}
		kind, norm := value.Classify(input)
		if kind != value.VectorKind {
			t.Errorf("expected VectorKind, got %v", kind)
		}
		expected := []float64{100, 200}
		if !reflect.DeepEqual(norm, expected) {
			t.Errorf("expected %v, got %v", expected, norm)
		}
	})

	t.Run("[]any with customFloat32 elements", func(t *testing.T) {
		input := []any{customFloat32(1.5), customFloat32(2.5)}
		kind, norm := value.Classify(input)
		if kind != value.VectorKind {
			t.Errorf("expected VectorKind, got %v", kind)
		}
		expected := []float64{1.5, 2.5}
		if !reflect.DeepEqual(norm, expected) {
			t.Errorf("expected %v, got %v", expected, norm)
		}
	})

	t.Run("[]any with customFloat64 elements", func(t *testing.T) {
		input := []any{customFloat64(3.14), customFloat64(2.71)}
		kind, norm := value.Classify(input)
		if kind != value.VectorKind {
			t.Errorf("expected VectorKind, got %v", kind)
		}
		expected := []float64{3.14, 2.71}
		if !reflect.DeepEqual(norm, expected) {
			t.Errorf("expected %v, got %v", expected, norm)
		}
	})

	t.Run("[]any with mixed custom numeric types", func(t *testing.T) {
		input := []any{customInt(1), customFloat64(2.5), customUint(3)}
		kind, norm := value.Classify(input)
		if kind != value.VectorKind {
			t.Errorf("expected VectorKind, got %v", kind)
		}
		expected := []float64{1, 2.5, 3}
		if !reflect.DeepEqual(norm, expected) {
			t.Errorf("expected %v, got %v", expected, norm)
		}
	})

	t.Run("[]any with nil element fails", func(t *testing.T) {
		input := []any{1.0, nil, 3.0}
		kind, _ := value.Classify(input)
		if kind != value.UnspecifiedKind {
			t.Errorf("expected UnspecifiedKind for nil element, got %v", kind)
		}
	})

	t.Run("[]any with non-numeric custom type fails", func(t *testing.T) {
		type customString string
		input := []any{customString("hello")}
		kind, _ := value.Classify(input)
		if kind != value.UnspecifiedKind {
			t.Errorf("expected UnspecifiedKind for non-numeric custom type, got %v", kind)
		}
	})
}

func TestClassify_SliceEdgeCases(t *testing.T) {
	// Test typed slices that should NOT be coerced to VectorKind
	t.Run("typed int slice returns UnspecifiedKind", func(t *testing.T) {
		input := []int{1, 2, 3}
		kind, _ := value.Classify(input)
		if kind != value.UnspecifiedKind {
			t.Errorf("expected UnspecifiedKind for []int, got %v", kind)
		}
	})

	t.Run("typed uint slice returns UnspecifiedKind", func(t *testing.T) {
		input := []uint{1, 2, 3}
		kind, _ := value.Classify(input)
		if kind != value.UnspecifiedKind {
			t.Errorf("expected UnspecifiedKind for []uint, got %v", kind)
		}
	})

	t.Run("nil int slice returns UnspecifiedKind", func(t *testing.T) {
		var input []int
		kind, _ := value.Classify(input)
		if kind != value.UnspecifiedKind {
			t.Errorf("expected UnspecifiedKind for nil []int, got %v", kind)
		}
	})

	t.Run("empty int slice returns UnspecifiedKind", func(t *testing.T) {
		input := []int{}
		kind, _ := value.Classify(input)
		if kind != value.UnspecifiedKind {
			t.Errorf("expected UnspecifiedKind for empty []int, got %v", kind)
		}
	})
}

func TestClassify_VectorWithDirectIntTypes(t *testing.T) {
	// Test toFloat64 direct type switch cases for all integer types in []any
	t.Run("[]any with int8 elements", func(t *testing.T) {
		input := []any{int8(1), int8(2), int8(3)}
		kind, norm := value.Classify(input)
		if kind != value.VectorKind {
			t.Errorf("expected VectorKind, got %v", kind)
		}
		expected := []float64{1, 2, 3}
		if !reflect.DeepEqual(norm, expected) {
			t.Errorf("expected %v, got %v", expected, norm)
		}
	})

	t.Run("[]any with int16 elements", func(t *testing.T) {
		input := []any{int16(10), int16(20)}
		kind, norm := value.Classify(input)
		if kind != value.VectorKind {
			t.Errorf("expected VectorKind, got %v", kind)
		}
		expected := []float64{10, 20}
		if !reflect.DeepEqual(norm, expected) {
			t.Errorf("expected %v, got %v", expected, norm)
		}
	})

	t.Run("[]any with int32 elements", func(t *testing.T) {
		input := []any{int32(100), int32(200)}
		kind, norm := value.Classify(input)
		if kind != value.VectorKind {
			t.Errorf("expected VectorKind, got %v", kind)
		}
		expected := []float64{100, 200}
		if !reflect.DeepEqual(norm, expected) {
			t.Errorf("expected %v, got %v", expected, norm)
		}
	})

	t.Run("[]any with uint8 elements", func(t *testing.T) {
		input := []any{uint8(5), uint8(10)}
		kind, norm := value.Classify(input)
		if kind != value.VectorKind {
			t.Errorf("expected VectorKind, got %v", kind)
		}
		expected := []float64{5, 10}
		if !reflect.DeepEqual(norm, expected) {
			t.Errorf("expected %v, got %v", expected, norm)
		}
	})

	t.Run("[]any with uint16 elements", func(t *testing.T) {
		input := []any{uint16(100), uint16(200)}
		kind, norm := value.Classify(input)
		if kind != value.VectorKind {
			t.Errorf("expected VectorKind, got %v", kind)
		}
		expected := []float64{100, 200}
		if !reflect.DeepEqual(norm, expected) {
			t.Errorf("expected %v, got %v", expected, norm)
		}
	})

	t.Run("[]any with uint32 elements", func(t *testing.T) {
		input := []any{uint32(1000), uint32(2000)}
		kind, norm := value.Classify(input)
		if kind != value.VectorKind {
			t.Errorf("expected VectorKind, got %v", kind)
		}
		expected := []float64{1000, 2000}
		if !reflect.DeepEqual(norm, expected) {
			t.Errorf("expected %v, got %v", expected, norm)
		}
	})

	t.Run("[]any with uint64 elements", func(t *testing.T) {
		input := []any{uint64(10000), uint64(20000)}
		kind, norm := value.Classify(input)
		if kind != value.VectorKind {
			t.Errorf("expected VectorKind, got %v", kind)
		}
		expected := []float64{10000, 20000}
		if !reflect.DeepEqual(norm, expected) {
			t.Errorf("expected %v, got %v", expected, norm)
		}
	})
}

func TestClassify_VectorWithInvalidJSONNumber(t *testing.T) {
	// Test json.Number that fails Float64() conversion in vector context
	t.Run("[]json.Number with invalid number fails", func(t *testing.T) {
		input := []json.Number{json.Number("1.0"), json.Number("invalid"), json.Number("3.0")}
		kind, _ := value.Classify(input)
		if kind != value.UnspecifiedKind {
			t.Errorf("expected UnspecifiedKind for invalid json.Number in vector, got %v", kind)
		}
	})
}

func TestClassify_EdgeCases(t *testing.T) {
	// []any always produces []float64, even with all float32 elements.
	// This is the "JSON pathway" normalization that matches the architecture spec.
	t.Run("[]any with float32 elements produces []float64", func(t *testing.T) {
		input := []any{float32(1.5), float32(2.5), float32(3.5)}
		kind, norm := value.Classify(input)
		if kind != value.VectorKind {
			t.Errorf("expected VectorKind, got %v", kind)
		}
		f64slice, ok := norm.([]float64)
		if !ok {
			t.Errorf("expected []float64, got %T", norm)
		}
		expected := []float64{1.5, 2.5, 3.5}
		if !reflect.DeepEqual(f64slice, expected) {
			t.Errorf("expected %v, got %v", expected, f64slice)
		}
	})

	t.Run("[]any with mixed floats produces []float64", func(t *testing.T) {
		input := []any{float32(1.5), float64(2.5)}
		kind, norm := value.Classify(input)
		if kind != value.VectorKind {
			t.Errorf("expected VectorKind, got %v", kind)
		}
		f64slice, ok := norm.([]float64)
		if !ok {
			t.Errorf("expected []float64, got %T", norm)
		}
		expected := []float64{1.5, 2.5}
		if !reflect.DeepEqual(f64slice, expected) {
			t.Errorf("expected %v, got %v", expected, f64slice)
		}
	})

	t.Run("integer widens to float64", func(t *testing.T) {
		// Integer in vector should produce float64 (64-bit)
		input := []any{1, 2, 3}
		kind, norm := value.Classify(input)
		if kind != value.VectorKind {
			t.Errorf("expected VectorKind, got %v", kind)
		}
		_, ok := norm.([]float64)
		if !ok {
			t.Errorf("expected []float64, got %T", norm)
		}
	})

	t.Run("uintptr in vector", func(t *testing.T) {
		input := []any{uintptr(1), uintptr(2)}
		kind, norm := value.Classify(input)
		if kind != value.VectorKind {
			t.Errorf("expected VectorKind, got %v", kind)
		}
		expected := []float64{1, 2}
		if !reflect.DeepEqual(norm, expected) {
			t.Errorf("expected %v, got %v", expected, norm)
		}
	})
}

// Helper functions for pointer tests
func ptr[T any](v T) *T { return &v }

func ptrptr[T any](v T) **T {
	p := &v
	return &p
}

func TestClassify_PointerDereferencing(t *testing.T) {
	// ClassifyWithRegistry should automatically dereference pointers
	t.Run("*int returns IntKind", func(t *testing.T) {
		input := ptr(42)
		kind, norm := value.Classify(input)
		if kind != value.IntKind {
			t.Errorf("expected IntKind for *int, got %v", kind)
		}
		if norm != 42 {
			t.Errorf("expected 42, got %v", norm)
		}
	})

	t.Run("*string returns StringKind", func(t *testing.T) {
		input := ptr("hello")
		kind, norm := value.Classify(input)
		if kind != value.StringKind {
			t.Errorf("expected StringKind for *string, got %v", kind)
		}
		if norm != "hello" {
			t.Errorf("expected hello, got %v", norm)
		}
	})

	t.Run("*float64 returns FloatKind", func(t *testing.T) {
		input := ptr(3.14)
		kind, norm := value.Classify(input)
		if kind != value.FloatKind {
			t.Errorf("expected FloatKind for *float64, got %v", kind)
		}
		if norm != 3.14 {
			t.Errorf("expected 3.14, got %v", norm)
		}
	})

	t.Run("*bool returns BoolKind", func(t *testing.T) {
		input := ptr(true)
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

func TestClassifyWithRegistry_NamedSliceType(t *testing.T) {
	// Test that registry hooks can recognize custom slice types
	// before built-in slice handling kicks in
	vecType := reflect.TypeFor[Vec]()

	registry := value.Registry{
		BaseKindOfReflectType: func(t reflect.Type) value.Kind {
			if t == vecType {
				return value.VectorKind
			}
			return value.UnspecifiedKind
		},
	}

	t.Run("named slice type recognized by registry", func(t *testing.T) {
		input := Vec{1.0, 2.0, 3.0}
		kind, norm := value.ClassifyWithRegistry(registry, input)
		if kind != value.VectorKind {
			t.Errorf("expected VectorKind for Vec, got %v", kind)
		}
		// Registry returns the original value unchanged
		if !reflect.DeepEqual(norm, input) {
			t.Errorf("expected %v, got %v", input, norm)
		}
	})

	t.Run("built-in float64 slice still works", func(t *testing.T) {
		input := []float64{1.0, 2.0, 3.0}
		kind, norm := value.ClassifyWithRegistry(registry, input)
		if kind != value.VectorKind {
			t.Errorf("expected VectorKind for []float64, got %v", kind)
		}
		if !reflect.DeepEqual(norm, input) {
			t.Errorf("expected %v, got %v", input, norm)
		}
	})

	t.Run("registry can override for non-slice types too", func(t *testing.T) {
		type CustomInt int
		customIntType := reflect.TypeFor[CustomInt]()

		customRegistry := value.Registry{
			BaseKindOfReflectType: func(t reflect.Type) value.Kind {
				if t == customIntType {
					return value.IntKind
				}
				return value.UnspecifiedKind
			},
		}

		kind, _ := value.ClassifyWithRegistry(customRegistry, CustomInt(42))
		if kind != value.IntKind {
			t.Errorf("expected IntKind for CustomInt, got %v", kind)
		}
	})
}
