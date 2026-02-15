package immutable

import (
	"math"
	"testing"
)

func TestValue_Wrap_Primitives(t *testing.T) {
	tests := []struct {
		name  string
		input any
		check func(t *testing.T, v Value)
	}{
		{
			name:  "nil",
			input: nil,
			check: func(t *testing.T, v Value) {
				t.Helper()
				if !v.IsNil() {
					t.Error("expected IsNil() to be true")
				}
				if v.Unwrap() != nil {
					t.Error("expected Unwrap() to be nil")
				}
			},
		},
		{
			name:  "bool true",
			input: true,
			check: func(t *testing.T, v Value) {
				t.Helper()
				b, ok := v.Bool()
				if !ok {
					t.Error("expected Bool() ok to be true")
				}
				if !b {
					t.Error("expected Bool() to be true")
				}
			},
		},
		{
			name:  "bool false",
			input: false,
			check: func(t *testing.T, v Value) {
				t.Helper()
				b, ok := v.Bool()
				if !ok {
					t.Error("expected Bool() ok to be true")
				}
				if b {
					t.Error("expected Bool() to be false")
				}
			},
		},
		{
			name:  "string",
			input: "hello",
			check: func(t *testing.T, v Value) {
				t.Helper()
				s, ok := v.String()
				if !ok {
					t.Error("expected String() ok to be true")
				}
				if s != "hello" {
					t.Errorf("expected String() to be 'hello', got %q", s)
				}
			},
		},
		{
			name:  "empty string",
			input: "",
			check: func(t *testing.T, v Value) {
				t.Helper()
				s, ok := v.String()
				if !ok {
					t.Error("expected String() ok to be true")
				}
				if s != "" {
					t.Errorf("expected String() to be empty, got %q", s)
				}
			},
		},
		{
			name:  "int",
			input: 42,
			check: func(t *testing.T, v Value) {
				t.Helper()
				n, ok := v.Int()
				if !ok {
					t.Error("expected Int() ok to be true")
				}
				if n != 42 {
					t.Errorf("expected Int() to be 42, got %d", n)
				}
			},
		},
		{
			name:  "int64",
			input: int64(9999999999),
			check: func(t *testing.T, v Value) {
				t.Helper()
				n, ok := v.Int()
				if !ok {
					t.Error("expected Int() ok to be true")
				}
				if n != 9999999999 {
					t.Errorf("expected Int() to be 9999999999, got %d", n)
				}
			},
		},
		{
			name:  "float64",
			input: 3.14,
			check: func(t *testing.T, v Value) {
				t.Helper()
				f, ok := v.Float()
				if !ok {
					t.Error("expected Float() ok to be true")
				}
				if f != 3.14 {
					t.Errorf("expected Float() to be 3.14, got %f", f)
				}
			},
		},
		{
			name:  "float64 whole number as int",
			input: float64(42),
			check: func(t *testing.T, v Value) {
				t.Helper()
				// Float64 whole numbers should work as Int()
				n, ok := v.Int()
				if !ok {
					t.Error("expected Int() ok to be true for whole float64")
				}
				if n != 42 {
					t.Errorf("expected Int() to be 42, got %d", n)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := Wrap(tt.input)
			tt.check(t, v)
		})
	}
}

func TestValue_TypeMismatch(t *testing.T) {
	// Test that type-safe accessors return (zero, false) for wrong types
	v := Wrap("hello")

	if _, ok := v.Bool(); ok {
		t.Error("expected Bool() ok to be false for string")
	}
	if _, ok := v.Int(); ok {
		t.Error("expected Int() ok to be false for string")
	}
	if _, ok := v.Float(); ok {
		t.Error("expected Float() ok to be false for string")
	}

	n := Wrap(42)
	if _, ok := n.String(); ok {
		t.Error("expected String() ok to be false for int")
	}
	if _, ok := n.Bool(); ok {
		t.Error("expected Bool() ok to be false for int")
	}
}

func TestValue_Map(t *testing.T) {
	input := map[string]any{
		"name": "Alice",
		"age":  30,
	}

	v := Wrap(input)

	m, ok := v.Map()
	if !ok {
		t.Fatal("expected Map() ok to be true")
	}

	if m.Len() != 2 {
		t.Errorf("expected Len() to be 2, got %d", m.Len())
	}

	name, ok := m.Get("name")
	if !ok {
		t.Fatal("expected Get('name') ok to be true")
	}
	if s, ok := name.String(); !ok || s != "Alice" {
		t.Errorf("expected name to be 'Alice', got %v", name.Unwrap())
	}

	age, ok := m.Get("age")
	if !ok {
		t.Fatal("expected Get('age') ok to be true")
	}
	if n, ok := age.Int(); !ok || n != 30 {
		t.Errorf("expected age to be 30, got %v", age.Unwrap())
	}
}

func TestValue_Slice(t *testing.T) {
	input := []any{"a", "b", "c"}

	v := Wrap(input)

	s, ok := v.Slice()
	if !ok {
		t.Fatal("expected Slice() ok to be true")
	}

	if s.Len() != 3 {
		t.Errorf("expected Len() to be 3, got %d", s.Len())
	}

	elem := s.Get(0)
	if str, ok := elem.String(); !ok || str != "a" {
		t.Errorf("expected first element to be 'a', got %v", elem.Unwrap())
	}
}

func TestValue_NestedStructures(t *testing.T) {
	input := map[string]any{
		"level1": map[string]any{
			"level2": map[string]any{
				"level3": []any{"deep", "value"},
			},
		},
	}

	v := Wrap(input)

	// Navigate to deeply nested value
	m1, ok := v.Map()
	if !ok {
		t.Fatal("expected top-level Map()")
	}

	level1Val, ok := m1.Get("level1")
	if !ok {
		t.Fatal("expected level1 key")
	}

	m2, ok := level1Val.Map()
	if !ok {
		t.Fatal("expected level1 to be Map")
	}

	level2Val, ok := m2.Get("level2")
	if !ok {
		t.Fatal("expected level2 key")
	}

	m3, ok := level2Val.Map()
	if !ok {
		t.Fatal("expected level2 to be Map")
	}

	level3Val, ok := m3.Get("level3")
	if !ok {
		t.Fatal("expected level3 key")
	}

	s, ok := level3Val.Slice()
	if !ok {
		t.Fatal("expected level3 to be Slice")
	}

	if s.Len() != 2 {
		t.Errorf("expected slice length 2, got %d", s.Len())
	}

	first := s.Get(0)
	if str, ok := first.String(); !ok || str != "deep" {
		t.Errorf("expected first element 'deep', got %v", first.Unwrap())
	}
}

func TestValue_WrapClone_Isolation(t *testing.T) {
	// Test that WrapClone creates an isolated copy
	nested := map[string]any{"key": "original"}
	outer := map[string]any{"nested": nested}

	wrapped := WrapClone(outer)

	// Mutate original after cloning
	nested["key"] = "mutated"
	outer["new"] = "added"

	// Wrapped value must be isolated
	m, ok := wrapped.Map()
	if !ok {
		t.Fatal("expected Map()")
	}

	// Check outer wasn't affected
	if _, ok := m.Get("new"); ok {
		t.Error("wrapped should not have 'new' key added after clone")
	}

	// Check nested wasn't affected
	nestedVal, ok := m.Get("nested")
	if !ok {
		t.Fatal("expected nested key")
	}
	nestedMap, ok := nestedVal.Map()
	if !ok {
		t.Fatal("expected nested to be Map")
	}
	keyVal, ok := nestedMap.Get("key")
	if !ok {
		t.Fatal("expected key in nested")
	}
	if str, ok := keyVal.String(); !ok || str != "original" {
		t.Errorf("expected nested key to be 'original', got %v", keyVal.Unwrap())
	}
}

func TestValue_IntTypes(t *testing.T) {
	// Test all integer types
	tests := []struct {
		name     string
		input    any
		expected int64
	}{
		{"int", int(10), 10},
		{"int8", int8(10), 10},
		{"int16", int16(10), 10},
		{"int32", int32(10), 10},
		{"int64", int64(10), 10},
		{"uint", uint(10), 10},
		{"uint8", uint8(10), 10},
		{"uint16", uint16(10), 10},
		{"uint32", uint32(10), 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := Wrap(tt.input)
			n, ok := v.Int()
			if !ok {
				t.Errorf("expected Int() ok for %s", tt.name)
			}
			if n != tt.expected {
				t.Errorf("expected %d, got %d", tt.expected, n)
			}
		})
	}
}

func TestValue_FloatTypes(t *testing.T) {
	// Test all numeric types with Float()
	tests := []struct {
		name     string
		input    any
		expected float64
	}{
		{"float64", float64(3.14), 3.14},
		{"float32", float32(3.14), float64(float32(3.14))},
		{"int", int(42), 42.0},
		{"int8", int8(42), 42.0},
		{"int16", int16(42), 42.0},
		{"int32", int32(42), 42.0},
		{"int64", int64(42), 42.0},
		{"uint", uint(42), 42.0},
		{"uint8", uint8(42), 42.0},
		{"uint16", uint16(42), 42.0},
		{"uint32", uint32(42), 42.0},
		{"uint64", uint64(42), 42.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := Wrap(tt.input)
			f, ok := v.Float()
			if !ok {
				t.Errorf("expected Float() ok for %s", tt.name)
			}
			if f != tt.expected {
				t.Errorf("expected %f, got %f", tt.expected, f)
			}
		})
	}
}

func TestDeepClone_NilInNonStringKeyedMap(t *testing.T) {
	// Non-string-keyed maps trigger deepCloneMap path which must handle nil values
	input := map[int]any{1: nil, 2: "value", 3: nil}

	// WrapClone with non-string-keyed map goes through deepCloneMap
	wrapped := WrapClone(input)

	// The underlying value should be the cloned map with nil values preserved
	cloned := wrapped.Unwrap()

	m, ok := cloned.(map[int]any)
	if !ok {
		t.Fatalf("expected map[int]any, got %T", cloned)
	}

	if len(m) != 3 {
		t.Errorf("expected len 3, got %d (nil values may have been dropped)", len(m))
	}

	// Check each key exists with correct value
	if val, ok := m[1]; !ok {
		t.Error("expected key 1 to exist")
	} else if val != nil {
		t.Errorf("expected nil value for key 1, got %v", val)
	}

	if val, ok := m[2]; !ok {
		t.Error("expected key 2 to exist")
	} else if val != "value" {
		t.Errorf("expected 'value' for key 2, got %v", val)
	}

	if val, ok := m[3]; !ok {
		t.Error("expected key 3 to exist")
	} else if val != nil {
		t.Errorf("expected nil value for key 3, got %v", val)
	}
}

func TestDeepClone_NilInNestedNonStringKeyedMap(t *testing.T) {
	// Nested structure with nil values in non-string-keyed map
	inner := map[int]any{1: nil, 2: "nested"}
	input := map[string]any{"inner": inner}

	wrapped := WrapClone(input)
	m, ok := wrapped.Map()
	if !ok {
		t.Fatal("expected Map")
	}

	innerVal, ok := m.Get("inner")
	if !ok {
		t.Fatal("expected 'inner' key")
	}

	// The inner map should be cloned (not wrapped as Map[string])
	innerCloned := innerVal.Unwrap()
	innerMap, ok := innerCloned.(map[int]any)
	if !ok {
		t.Fatalf("expected map[int]any, got %T", innerCloned)
	}

	if len(innerMap) != 2 {
		t.Errorf("expected len 2, got %d", len(innerMap))
	}

	if val, ok := innerMap[1]; !ok || val != nil {
		t.Errorf("expected key 1 with nil value, got %v", val)
	}
}

func TestDeepClone_NilInSliceWithinNonStringKeyedMap(t *testing.T) {
	// Test that deepCloneSlice handles nil elements correctly
	// deepCloneSlice is called for slices nested within non-string-keyed maps
	sliceWithNil := []any{nil, "hello", nil}
	input := map[int]any{1: sliceWithNil}

	wrapped := WrapClone(input)
	cloned := wrapped.Unwrap()

	m, ok := cloned.(map[int]any)
	if !ok {
		t.Fatalf("expected map[int]any, got %T", cloned)
	}

	// Get the nested slice
	val, ok := m[1]
	if !ok {
		t.Fatal("expected key 1 to exist")
	}

	s, ok := val.([]any)
	if !ok {
		t.Fatalf("expected []any, got %T", val)
	}

	if len(s) != 3 {
		t.Errorf("expected len 3, got %d", len(s))
	}
	if s[0] != nil {
		t.Errorf("expected nil at index 0, got %v", s[0])
	}
	if s[1] != "hello" {
		t.Errorf("expected 'hello' at index 1, got %v", s[1])
	}
	if s[2] != nil {
		t.Errorf("expected nil at index 2, got %v", s[2])
	}
}

func TestValue_Int_UintOverflow(t *testing.T) {
	tests := []struct {
		name    string
		input   any
		wantVal int64
		wantOK  bool
	}{
		// Valid uint values
		{"uint zero", uint(0), 0, true},
		{"uint small", uint(42), 42, true},
		{"uint at MaxInt64", uint(math.MaxInt64), math.MaxInt64, true},
		// Overflow uint values
		{"uint over MaxInt64", uint(math.MaxInt64) + 1, 0, false},
		{"uint large", uint(math.MaxUint64), 0, false},
		// Valid uint64 values
		{"uint64 zero", uint64(0), 0, true},
		{"uint64 small", uint64(42), 42, true},
		{"uint64 at MaxInt64", uint64(math.MaxInt64), math.MaxInt64, true},
		// Overflow uint64 values
		{"uint64 over MaxInt64", uint64(math.MaxInt64) + 1, 0, false},
		{"uint64 large", uint64(math.MaxUint64), 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := Wrap(tt.input)
			got, ok := v.Int()
			if ok != tt.wantOK {
				t.Errorf("Int() ok = %v, want %v", ok, tt.wantOK)
			}
			if got != tt.wantVal {
				t.Errorf("Int() = %d, want %d", got, tt.wantVal)
			}
			// Verify we never return negative for positive input
			if ok && got < 0 {
				t.Errorf("Int() returned negative %d for positive input", got)
			}
		})
	}
}

func TestValue_Int_FloatBoundary(t *testing.T) {
	tests := []struct {
		name   string
		input  float64
		wantOK bool
	}{
		{"whole number", 42.0, true},
		{"fraction", 42.5, false},
		{"negative whole", -42.0, true},
		{"large float", 1e100, false},
		{"negative large", -1e100, false},
		{"infinity", math.Inf(1), false},
		{"negative infinity", math.Inf(-1), false},
		{"NaN", math.NaN(), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := Wrap(tt.input)
			_, ok := v.Int()
			if ok != tt.wantOK {
				t.Errorf("Int() ok = %v, want %v for %v", ok, tt.wantOK, tt.input)
			}
		})
	}
}

func TestWrap_NilInStringKeyedMap(t *testing.T) {
	// Tests that nil values in map[string]any are correctly preserved through wrapping.
	// This exercises the wrapValue path (not deepClone) for the most common map type.
	input := map[string]any{"a": nil, "b": "value", "c": nil}

	// Test both Wrap and WrapClone
	for _, name := range []string{"Wrap", "WrapClone"} {
		t.Run(name, func(t *testing.T) {
			var wrapped Value
			if name == "Wrap" {
				// Make a copy since Wrap takes ownership
				inputCopy := map[string]any{"a": nil, "b": "value", "c": nil}
				wrapped = Wrap(inputCopy)
			} else {
				wrapped = WrapClone(input)
			}

			m, ok := wrapped.Map()
			if !ok {
				t.Fatal("expected Map")
			}

			// Check nil values are preserved
			aVal, ok := m.Get("a")
			if !ok {
				t.Error("expected key 'a' to exist")
			} else if !aVal.IsNil() {
				t.Errorf("expected nil value for key 'a', got %v", aVal.Unwrap())
			}

			// Check non-nil value
			bVal, ok := m.Get("b")
			if !ok {
				t.Error("expected key 'b' to exist")
			} else if s, ok := bVal.String(); !ok || s != "value" {
				t.Errorf("expected 'value' for key 'b', got %v", bVal.Unwrap())
			}

			// Check second nil value
			cVal, ok := m.Get("c")
			if !ok {
				t.Error("expected key 'c' to exist")
			} else if !cVal.IsNil() {
				t.Errorf("expected nil value for key 'c', got %v", cVal.Unwrap())
			}
		})
	}
}

func TestValue_IsNil_TypedNils(t *testing.T) {
	// Tests that typed nil values (pointers, chans, funcs) return IsNil() == true.
	// This is important because a typed nil interface value (e.g., (*int)(nil))
	// is not equal to nil when checked via == nil on the interface.

	var nilPtr *int
	var nilChan chan int
	var nilFunc func()
	var nilMap map[string]any
	var nilSlice []any

	tests := []struct {
		name  string
		input any
	}{
		{"nil pointer", nilPtr},
		{"nil channel", nilChan},
		{"nil function", nilFunc},
		{"nil map", nilMap},
		{"nil slice", nilSlice},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := Wrap(tt.input)
			if !v.IsNil() {
				t.Errorf("expected IsNil() to be true for %s, got false", tt.name)
			}
		})
	}

	// Non-nil values should not be nil
	nonNilTests := []struct {
		name  string
		input any
	}{
		{"non-nil pointer", new(int)},
		{"non-nil channel", make(chan int)},
		{"non-nil function", func() {}},
		{"non-nil map", map[string]any{}},
		{"non-nil slice", []any{}},
	}

	for _, tt := range nonNilTests {
		t.Run(tt.name, func(t *testing.T) {
			v := Wrap(tt.input)
			if v.IsNil() {
				t.Errorf("expected IsNil() to be false for %s, got true", tt.name)
			}
		})
	}
}

func TestValue_NilMapSlice_TypedWrapper(t *testing.T) {
	// Tests that nil maps/slices are wrapped as typed wrappers (not literal nil),
	// allowing callers to distinguish nil-typed values from literal nil.

	t.Run("nil map returns Map true", func(t *testing.T) {
		var m map[string]any // nil map
		v := Wrap(m)

		// Should be nil
		if !v.IsNil() {
			t.Error("expected IsNil() to be true for nil map")
		}

		// Should be recognized as a Map (distinguishes from Wrap(nil))
		_, ok := v.Map()
		if !ok {
			t.Error("expected Map() to return true for nil map")
		}
	})

	t.Run("nil slice returns Slice true", func(t *testing.T) {
		var s []any // nil slice
		v := Wrap(s)

		// Should be nil
		if !v.IsNil() {
			t.Error("expected IsNil() to be true for nil slice")
		}

		// Should be recognized as a Slice (distinguishes from Wrap(nil))
		_, ok := v.Slice()
		if !ok {
			t.Error("expected Slice() to return true for nil slice")
		}
	})

	t.Run("literal nil returns Map false", func(t *testing.T) {
		v := Wrap(nil)

		if !v.IsNil() {
			t.Error("expected IsNil() to be true for literal nil")
		}

		// Should NOT be recognized as a Map
		_, ok := v.Map()
		if ok {
			t.Error("expected Map() to return false for literal nil")
		}
	})

	t.Run("literal nil returns Slice false", func(t *testing.T) {
		v := Wrap(nil)

		if !v.IsNil() {
			t.Error("expected IsNil() to be true for literal nil")
		}

		// Should NOT be recognized as a Slice
		_, ok := v.Slice()
		if ok {
			t.Error("expected Slice() to return false for literal nil")
		}
	})

	t.Run("empty map is not nil", func(t *testing.T) {
		m := map[string]any{} // empty but non-nil
		v := Wrap(m)

		if v.IsNil() {
			t.Error("expected IsNil() to be false for empty non-nil map")
		}

		mp, ok := v.Map()
		if !ok {
			t.Error("expected Map() to return true for empty map")
		}
		if mp.Len() != 0 {
			t.Errorf("expected Len() to be 0, got %d", mp.Len())
		}
	})

	t.Run("empty slice is not nil", func(t *testing.T) {
		s := []any{} // empty but non-nil
		v := Wrap(s)

		if v.IsNil() {
			t.Error("expected IsNil() to be false for empty non-nil slice")
		}

		sl, ok := v.Slice()
		if !ok {
			t.Error("expected Slice() to return true for empty slice")
		}
		if sl.Len() != 0 {
			t.Errorf("expected Len() to be 0, got %d", sl.Len())
		}
	})
}

func TestValue_Int_Float32(t *testing.T) {
	// Tests that float32 values work with Int() accessor for whole numbers.
	tests := []struct {
		name     string
		input    float32
		expected int64
		ok       bool
	}{
		{"whole number", float32(42), 42, true},
		{"zero", float32(0), 0, true},
		{"negative whole", float32(-100), -100, true},
		{"large whole", float32(1000000), 1000000, true},
		{"non-whole", float32(3.14), 0, false},
		{"negative non-whole", float32(-2.5), 0, false},
		{"NaN", float32(math.NaN()), 0, false},
		{"positive infinity", float32(math.Inf(1)), 0, false},
		{"negative infinity", float32(math.Inf(-1)), 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := Wrap(tt.input)
			result, ok := v.Int()
			if ok != tt.ok {
				t.Errorf("expected ok=%v, got ok=%v", tt.ok, ok)
			}
			if result != tt.expected {
				t.Errorf("expected %d, got %d", tt.expected, result)
			}
		})
	}
}

func TestWrap_NilInSlice(t *testing.T) {
	// Tests that nil values in []any are correctly preserved through wrapping.
	// This exercises the wrapValue path (not deepClone) for slices.
	input := []any{nil, "hello", nil, 42, nil}

	// Test both Wrap and WrapClone
	for _, name := range []string{"Wrap", "WrapClone"} {
		t.Run(name, func(t *testing.T) {
			var wrapped Value
			if name == "Wrap" {
				// Make a copy since Wrap takes ownership
				inputCopy := []any{nil, "hello", nil, 42, nil}
				wrapped = Wrap(inputCopy)
			} else {
				wrapped = WrapClone(input)
			}

			s, ok := wrapped.Slice()
			if !ok {
				t.Fatal("expected Slice")
			}

			if s.Len() != 5 {
				t.Errorf("expected len 5, got %d", s.Len())
			}

			// Check nil at index 0
			if v := s.Get(0); !v.IsNil() {
				t.Errorf("expected nil at index 0, got %v", v.Unwrap())
			}

			// Check string at index 1
			if v := s.Get(1); v.IsNil() {
				t.Error("expected non-nil at index 1")
			} else if str, ok := v.String(); !ok || str != "hello" {
				t.Errorf("expected 'hello' at index 1, got %v", v.Unwrap())
			}

			// Check nil at index 2
			if v := s.Get(2); !v.IsNil() {
				t.Errorf("expected nil at index 2, got %v", v.Unwrap())
			}

			// Check int at index 3
			if v := s.Get(3); v.IsNil() {
				t.Error("expected non-nil at index 3")
			} else if n, ok := v.Int(); !ok || n != 42 {
				t.Errorf("expected 42 at index 3, got %v", v.Unwrap())
			}

			// Check nil at index 4
			if v := s.Get(4); !v.IsNil() {
				t.Errorf("expected nil at index 4, got %v", v.Unwrap())
			}
		})
	}
}
