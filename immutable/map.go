package immutable

import (
	"iter"
	"maps"
	"reflect"
	"slices"
	"sync"
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

	// folded memoises what [PropertiesOf] needs, once per map. Nil on a zero
	// Map and for any key type but string, which nothing reads it for.
	folded *foldedView
}

// foldedView is the sorted-key list and folded index of one string-keyed
// entry set, computed on first use and shared by every Properties view over
// it.
type foldedView struct {
	once        sync.Once
	sortedKeys  []string
	foldedIndex map[string]string
}

// get computes the view once for entries and returns it. Safe for
// concurrent first use.
func (f *foldedView) get(entries map[string]Value) ([]string, map[string]string) {
	f.once.Do(func() {
		f.sortedKeys = slices.Sorted(maps.Keys(entries))
		f.foldedIndex = computeFoldedIndex(f.sortedKeys)
	})
	return f.sortedKeys, f.foldedIndex
}

// WrapMap wraps a map with ownership transfer semantics.
//
// After calling WrapMap, the caller MUST NOT retain or use any reference
// to m or any mutable value reachable from m. Mutation after WrapMap is
// undefined behavior.
//
// Pass [WithClone] with true to deep-clone what would be stored as-is — a
// non-string-keyed map and its contents — so the caller can retain it.
func WrapMap[K comparable](m map[K]any, opts ...Option) Map[K] {
	if m == nil {
		return Map[K]{}
	}

	cfg := resolveConfig(opts)
	entries := make(map[K]Value, len(m))
	for k, v := range m {
		entries[k] = Value{val: wrapValue(v, cfg.clone)}
	}
	return Map[K]{entries: entries, folded: newFoldedView[K]()}
}

// newFoldedView allocates the memo only when K is string: PropertiesOf, its
// only reader, takes a Map[string], so a view on any other key type is dead.
func newFoldedView[K comparable]() *foldedView {
	var k K
	if _, ok := any(k).(string); ok {
		return &foldedView{}
	}
	return nil
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
	return maps.Keys(m.entries)
}

// Range returns an iterator over key-value pairs.
//
// The iteration order is not guaranteed to be consistent across calls.
func (m Map[K]) Range() iter.Seq2[K, Value] {
	return maps.All(m.entries)
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

// cloneValue returns a mutable deep copy of v's content: a map[string]any for a
// Map[string], an []any for a Slice, a deep copy of a map stored as given, and
// the value itself otherwise.
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
