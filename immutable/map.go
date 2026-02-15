package immutable

import (
	"iter"
	"reflect"
)

// Map provides immutable access to a map with pre-wrapped values.
//
// Map is a generic type parameterized by the key type K. Values are
// wrapped at construction time, so Get operations are O(1) with no
// additional allocations.
//
// Map is safe for concurrent read access.
type Map[K comparable] struct {
	entries map[K]Value
}

// WrapMap wraps a map with ownership transfer semantics.
//
// After calling WrapMap, the caller MUST NOT retain or use any reference
// to m or any mutable value reachable from m. Mutation after WrapMap is
// undefined behavior.
//
// Use [WrapMapClone] when the map comes from external sources or when
// ownership cannot be verified.
func WrapMap[K comparable](m map[K]any) Map[K] {
	if m == nil {
		return Map[K]{}
	}

	entries := make(map[K]Value, len(m))
	for k, v := range m {
		entries[k] = Value{val: wrapValue(v, false)}
	}
	return Map[K]{entries: entries}
}

// WrapMapClone wraps a deep clone of the map.
//
// The caller may freely retain and mutate the original map after cloning.
// This is safe for maps from external sources or shared references.
func WrapMapClone[K comparable](m map[K]any) Map[K] {
	if m == nil {
		return Map[K]{}
	}

	entries := make(map[K]Value, len(m))
	for k, v := range m {
		entries[k] = Value{val: wrapValue(v, true)}
	}
	return Map[K]{entries: entries}
}

// Get returns the value for the given key and true if the key exists.
// Returns (zero Value, false) if the key does not exist.
func (m Map[K]) Get(key K) (Value, bool) {
	v, ok := m.entries[key]
	return v, ok
}

// Len returns the number of entries in the map.
func (m Map[K]) Len() int {
	return len(m.entries)
}

// Keys returns an iterator over the map keys.
//
// The iteration order is not guaranteed to be consistent across calls.
// Use this for iteration without needing values.
func (m Map[K]) Keys() iter.Seq[K] {
	return func(yield func(K) bool) {
		for k := range m.entries {
			if !yield(k) {
				return
			}
		}
	}
}

// Range returns an iterator over key-value pairs.
//
// The iteration order is not guaranteed to be consistent across calls.
func (m Map[K]) Range() iter.Seq2[K, Value] {
	return func(yield func(K, Value) bool) {
		for k, v := range m.entries {
			if !yield(k, v) {
				return
			}
		}
	}
}

// Clone returns a deep copy of the map as a mutable map[K]any.
//
// This is the escape hatch for callers who need to modify values.
// The returned map is independent of the immutable Map.
func (m Map[K]) Clone() map[K]any {
	if m.entries == nil {
		return nil
	}

	result := make(map[K]any, len(m.entries))
	for k, v := range m.entries {
		result[k] = cloneValue(v)
	}
	return result
}

// cloneValue recursively clones a Value back to its original type.
func cloneValue(v Value) any {
	if v.val == nil {
		return nil
	}

	switch inner := v.val.(type) {
	case Map[string]:
		return inner.Clone()
	case Slice:
		return inner.Clone()
	default:
		// Primitives and other types
		rv := reflect.ValueOf(inner)
		if rv.Kind() == reflect.Map {
			return deepCloneMap(rv)
		}
		if rv.Kind() == reflect.Slice {
			return deepCloneSlice(rv)
		}
		return inner
	}
}
