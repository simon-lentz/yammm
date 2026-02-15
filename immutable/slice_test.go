package immutable

import (
	"slices"
	"testing"
)

func TestSlice_WrapSlice(t *testing.T) {
	input := []any{"a", "b", "c"}

	s := WrapSlice(input)

	if s.Len() != 3 {
		t.Errorf("expected Len() to be 3, got %d", s.Len())
	}

	elem := s.Get(0)
	if str, ok := elem.String(); !ok || str != "a" {
		t.Errorf("expected first element 'a', got %v", elem.Unwrap())
	}

	elem = s.Get(2)
	if str, ok := elem.String(); !ok || str != "c" {
		t.Errorf("expected third element 'c', got %v", elem.Unwrap())
	}
}

func TestSlice_WrapNil(t *testing.T) {
	s := WrapSlice(nil)

	if s.Len() != 0 {
		t.Errorf("expected Len() to be 0 for nil slice, got %d", s.Len())
	}
}

func TestSlice_WrapEmpty(t *testing.T) {
	s := WrapSlice([]any{})

	if s.Len() != 0 {
		t.Errorf("expected Len() to be 0 for empty slice, got %d", s.Len())
	}
}

func TestSlice_Get_Panic(t *testing.T) {
	s := WrapSlice([]any{"a", "b"})

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for out-of-bounds access")
		}
	}()

	_ = s.Get(5) // Should panic
}

func TestSlice_Get_NegativeIndex_Panic(t *testing.T) {
	s := WrapSlice([]any{"a", "b"})

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for negative index")
		}
	}()

	_ = s.Get(-1) // Should panic
}

func TestSlice_Iter(t *testing.T) {
	input := []any{1, 2, 3}

	s := WrapSlice(input)

	var values []int64
	for v := range s.Iter() {
		n, ok := v.Int()
		if !ok {
			t.Error("expected Int()")
		}
		values = append(values, n)
	}

	expected := []int64{1, 2, 3}
	if !slices.Equal(values, expected) {
		t.Errorf("expected %v, got %v", expected, values)
	}
}

func TestSlice_Clone(t *testing.T) {
	input := []any{
		map[string]any{"key": "value"},
		"string",
		42,
	}

	s := WrapSlice(input)
	cloned := s.Clone()

	// Verify cloned has correct structure
	if cloned == nil {
		t.Fatal("expected Clone() to return non-nil")
	}

	if len(cloned) != 3 {
		t.Errorf("expected 3 elements in clone, got %d", len(cloned))
	}

	// Verify first element is a map
	nested, ok := cloned[0].(map[string]any)
	if !ok {
		t.Fatal("expected first element to be map[string]any")
	}

	if nested["key"] != "value" {
		t.Errorf("expected value 'value', got %v", nested["key"])
	}

	// Verify clone is independent
	nested["key"] = "modified"

	// Original wrapped slice should be unchanged
	origFirst := s.Get(0)
	origMap, _ := origFirst.Map()
	origVal, _ := origMap.Get("key")
	if str, _ := origVal.String(); str != "value" {
		t.Error("clone modification affected original wrapped slice")
	}
}

func TestSlice_CloneNil(t *testing.T) {
	s := WrapSlice(nil)
	cloned := s.Clone()

	if cloned != nil {
		t.Error("expected Clone() of nil slice to return nil")
	}
}

func TestSlice_WrapSliceClone_Isolation(t *testing.T) {
	nested := map[string]any{"key": "original"}
	input := []any{nested}

	s := WrapSliceClone(input)

	// Mutate original after cloning
	nested["key"] = "mutated"
	_ = append(input, "new") // Would change original slice if shared

	// Wrapped slice should be isolated
	if s.Len() != 1 {
		t.Errorf("expected Len() 1, got %d", s.Len())
	}

	first := s.Get(0)
	firstMap, ok := first.Map()
	if !ok {
		t.Fatal("expected first element to be Map")
	}
	keyVal, ok := firstMap.Get("key")
	if !ok {
		t.Fatal("expected key in nested")
	}
	if str, ok := keyVal.String(); !ok || str != "original" {
		t.Errorf("expected 'original', got %v", keyVal.Unwrap())
	}
}

func TestSlice_IteratorEarlyExit(t *testing.T) {
	input := []any{1, 2, 3, 4, 5}

	s := WrapSlice(input)

	count := 0
	for range s.Iter() {
		count++
		if count == 2 {
			break
		}
	}
	if count != 2 {
		t.Errorf("expected early exit after 2, got %d", count)
	}
}

func TestSlice_NestedSlices(t *testing.T) {
	input := []any{
		[]any{1, 2},
		[]any{3, 4},
	}

	s := WrapSlice(input)

	first := s.Get(0)
	firstSlice, ok := first.Slice()
	if !ok {
		t.Fatal("expected first element to be Slice")
	}

	if firstSlice.Len() != 2 {
		t.Errorf("expected nested slice length 2, got %d", firstSlice.Len())
	}

	elem := firstSlice.Get(0)
	if n, ok := elem.Int(); !ok || n != 1 {
		t.Errorf("expected 1, got %v", elem.Unwrap())
	}
}

func TestSlice_MixedTypes(t *testing.T) {
	input := []any{
		"string",
		42,
		3.14,
		true,
		nil,
		map[string]any{"key": "value"},
		[]any{1, 2, 3},
	}

	s := WrapSlice(input)

	if s.Len() != 7 {
		t.Errorf("expected Len() 7, got %d", s.Len())
	}

	// Test string
	if str, ok := s.Get(0).String(); !ok || str != "string" {
		t.Error("expected string element")
	}

	// Test int
	if n, ok := s.Get(1).Int(); !ok || n != 42 {
		t.Error("expected int element")
	}

	// Test float
	if f, ok := s.Get(2).Float(); !ok || f != 3.14 {
		t.Error("expected float element")
	}

	// Test bool
	if b, ok := s.Get(3).Bool(); !ok || !b {
		t.Error("expected bool element")
	}

	// Test nil
	if !s.Get(4).IsNil() {
		t.Error("expected nil element")
	}

	// Test map
	if _, ok := s.Get(5).Map(); !ok {
		t.Error("expected map element")
	}

	// Test slice
	if _, ok := s.Get(6).Slice(); !ok {
		t.Error("expected slice element")
	}
}

func TestSlice_IteratorRepeatability(t *testing.T) {
	input := []any{1, 2, 3}

	s := WrapSlice(input)

	// First iteration
	var first []int64
	for v := range s.Iter() {
		n, _ := v.Int()
		first = append(first, n)
	}

	// Second iteration
	var second []int64
	for v := range s.Iter() {
		n, _ := v.Int()
		second = append(second, n)
	}

	if !slices.Equal(first, second) {
		t.Errorf("expected same results, got %v and %v", first, second)
	}
}

func TestSlice_Iter2(t *testing.T) {
	input := []any{"a", "b", "c"}

	s := WrapSlice(input)

	var indices []int
	var values []string
	for i, v := range s.Iter2() {
		indices = append(indices, i)
		str, _ := v.String()
		values = append(values, str)
	}

	expectedIndices := []int{0, 1, 2}
	if !slices.Equal(indices, expectedIndices) {
		t.Errorf("expected indices %v, got %v", expectedIndices, indices)
	}

	expectedValues := []string{"a", "b", "c"}
	if !slices.Equal(values, expectedValues) {
		t.Errorf("expected values %v, got %v", expectedValues, values)
	}
}

func TestSlice_Iter2_Empty(t *testing.T) {
	s := WrapSlice([]any{})

	count := 0
	for range s.Iter2() {
		count++
	}

	if count != 0 {
		t.Errorf("expected 0 iterations for empty slice, got %d", count)
	}
}

func TestSlice_Iter2_EarlyExit(t *testing.T) {
	input := []any{1, 2, 3, 4, 5}

	s := WrapSlice(input)

	lastIndex := -1
	for i := range s.Iter2() {
		lastIndex = i
		if i == 2 {
			break
		}
	}

	if lastIndex != 2 {
		t.Errorf("expected to stop at index 2, got %d", lastIndex)
	}
}

func TestSlice_Iter2_Repeatability(t *testing.T) {
	input := []any{1, 2, 3}

	s := WrapSlice(input)

	// First iteration
	var first []int
	for i := range s.Iter2() {
		first = append(first, i)
	}

	// Second iteration
	var second []int
	for i := range s.Iter2() {
		second = append(second, i)
	}

	if !slices.Equal(first, second) {
		t.Errorf("expected same order, got %v and %v", first, second)
	}
}

func TestSlice_Iter_ZeroValue(t *testing.T) {
	// A4: Verify iterating over literal zero-value Slice{} handles gracefully
	var s Slice

	count := 0
	for range s.Iter() {
		count++
	}

	if count != 0 {
		t.Errorf("expected 0 iterations for zero-value Slice{}, got %d", count)
	}
}

func TestSlice_Iter2_ZeroValue(t *testing.T) {
	// A4: Verify iterating over literal zero-value Slice{} with Iter2 handles gracefully
	var s Slice

	count := 0
	for range s.Iter2() {
		count++
	}

	if count != 0 {
		t.Errorf("expected 0 iterations for zero-value Slice{}, got %d", count)
	}
}

func TestSlice_GetOK(t *testing.T) {
	s := WrapSlice([]any{"a", "b", "c"})

	t.Run("valid index", func(t *testing.T) {
		v, ok := s.GetOK(0)
		if !ok {
			t.Error("expected ok to be true for valid index")
		}
		if str, strOk := v.String(); !strOk || str != "a" {
			t.Errorf("expected 'a', got %v", v.Unwrap())
		}
	})

	t.Run("last valid index", func(t *testing.T) {
		v, ok := s.GetOK(2)
		if !ok {
			t.Error("expected ok to be true for last valid index")
		}
		if str, strOk := v.String(); !strOk || str != "c" {
			t.Errorf("expected 'c', got %v", v.Unwrap())
		}
	})

	t.Run("negative index", func(t *testing.T) {
		v, ok := s.GetOK(-1)
		if ok {
			t.Error("expected ok to be false for negative index")
		}
		if !v.IsNil() {
			t.Errorf("expected zero Value for out of bounds, got %v", v.Unwrap())
		}
	})

	t.Run("out of bounds index", func(t *testing.T) {
		v, ok := s.GetOK(10)
		if ok {
			t.Error("expected ok to be false for out of bounds index")
		}
		if !v.IsNil() {
			t.Errorf("expected zero Value for out of bounds, got %v", v.Unwrap())
		}
	})

	t.Run("exactly at length", func(t *testing.T) {
		v, ok := s.GetOK(3) // len is 3, so index 3 is out of bounds
		if ok {
			t.Error("expected ok to be false for index at length")
		}
		if !v.IsNil() {
			t.Errorf("expected zero Value for out of bounds, got %v", v.Unwrap())
		}
	})

	t.Run("empty slice", func(t *testing.T) {
		empty := WrapSlice([]any{})
		v, ok := empty.GetOK(0)
		if ok {
			t.Error("expected ok to be false for empty slice")
		}
		if !v.IsNil() {
			t.Errorf("expected zero Value for empty slice, got %v", v.Unwrap())
		}
	})

	t.Run("nil slice", func(t *testing.T) {
		nilSlice := WrapSlice(nil)
		v, ok := nilSlice.GetOK(0)
		if ok {
			t.Error("expected ok to be false for nil slice")
		}
		if !v.IsNil() {
			t.Errorf("expected zero Value for nil slice, got %v", v.Unwrap())
		}
	})
}
