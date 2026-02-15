package immutable

import (
	"iter"
)

// Slice provides immutable access to a slice with pre-wrapped elements.
//
// Elements are wrapped at construction time, so Get operations are O(1)
// with no additional allocations.
//
// Slice is safe for concurrent read access.
type Slice struct {
	elements []Value
}

// WrapSlice wraps a slice with ownership transfer semantics.
//
// After calling WrapSlice, the caller MUST NOT retain or use any reference
// to s or any mutable value reachable from s. Mutation after WrapSlice is
// undefined behavior.
//
// Use [WrapSliceClone] when the slice comes from external sources or when
// ownership cannot be verified.
func WrapSlice(s []any) Slice {
	if s == nil {
		return Slice{}
	}

	elements := make([]Value, len(s))
	for i, v := range s {
		elements[i] = Value{val: wrapValue(v, false)}
	}
	return Slice{elements: elements}
}

// WrapSliceClone wraps a deep clone of the slice.
//
// The caller may freely retain and mutate the original slice after cloning.
// This is safe for slices from external sources or shared references.
func WrapSliceClone(s []any) Slice {
	if s == nil {
		return Slice{}
	}

	elements := make([]Value, len(s))
	for i, v := range s {
		elements[i] = Value{val: wrapValue(v, true)}
	}
	return Slice{elements: elements}
}

// Get returns the element at the given index.
//
// Panics if i is out of bounds, matching Go slice semantics.
// Use [Slice.GetOK] for bounds-checked access without panics.
func (s Slice) Get(i int) Value {
	return s.elements[i]
}

// GetOK returns the element at the given index and true if the index is valid.
// Returns (zero Value, false) if i is out of bounds.
//
// This is a safe alternative to [Slice.Get] for code paths where index
// validity is uncertain.
func (s Slice) GetOK(i int) (Value, bool) {
	if i < 0 || i >= len(s.elements) {
		return Value{}, false
	}
	return s.elements[i], true
}

// Len returns the number of elements in the slice.
func (s Slice) Len() int {
	return len(s.elements)
}

// Iter returns an iterator over the slice elements.
func (s Slice) Iter() iter.Seq[Value] {
	return func(yield func(Value) bool) {
		for _, v := range s.elements {
			if !yield(v) {
				return
			}
		}
	}
}

// Iter2 returns an iterator over index-value pairs.
func (s Slice) Iter2() iter.Seq2[int, Value] {
	return func(yield func(int, Value) bool) {
		for i, v := range s.elements {
			if !yield(i, v) {
				return
			}
		}
	}
}

// Clone returns a deep copy of the slice as a mutable []any.
//
// This is the escape hatch for callers who need to modify values.
// The returned slice is independent of the immutable Slice.
func (s Slice) Clone() []any {
	if s.elements == nil {
		return nil
	}

	result := make([]any, len(s.elements))
	for i, v := range s.elements {
		result[i] = cloneValue(v)
	}
	return result
}
