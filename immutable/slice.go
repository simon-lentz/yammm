package immutable

import (
	"iter"
	"slices"
)

// Slice holds a slice immutably, each element wrapped as [Wrap] wraps it: a
// List or Vector value.
//
// Slice is safe for concurrent read access.
type Slice struct {
	elements []Value
}

// WrapSlice returns s held immutably. It copies the slice, and wraps each
// element as [Wrap] does, so a later change to s reaches nothing it returned,
// save through a value Wrap stores as given, such as a struct, a pointer or an
// array. A nil slice gives the zero Slice, which is nil.
func WrapSlice(s []any) Slice {
	if s == nil {
		return Slice{}
	}
	elements := make([]Value, len(s))
	for i, v := range s {
		elements[i] = Value{val: wrapValue(v)}
	}
	return Slice{elements: elements}
}

// At returns the element at index i. It panics if i is out of range, as a
// slice index does.
func (s Slice) At(i int) Value {
	return s.elements[i]
}

// Len returns the number of elements in the slice.
func (s Slice) Len() int {
	return len(s.elements)
}

// Values returns an iterator over the elements in order.
func (s Slice) Values() iter.Seq[Value] {
	return slices.Values(s.elements)
}

// Clone returns the elements as a new []any, each as [Value.Clone] returns it:
// a container at every depth is copied, and what [Value.Clone] shares is shared
// with the Slice. The zero Slice clones to nil.
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
