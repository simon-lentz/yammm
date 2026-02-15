package immutable

import (
	"cmp"
	"iter"
	"slices"
)

// Properties provides immutable access to instance property values.
//
// Properties is a specialized wrapper for string-keyed maps that appear
// in instance property access patterns. It provides case-insensitive lookup
// via [Properties.GetFold] and deterministic iteration via [Properties.SortedKeys].
//
// Properties is safe for concurrent read access.
type Properties struct {
	entries map[string]Value

	// sortedKeys is the precomputed sorted key list, computed at construction.
	// Nil for zero-value Properties or empty properties.
	sortedKeys []string

	// foldedIndex maps lowercase (ASCII-folded) keys to original keys.
	// Used for O(1) case-insensitive lookup in GetFold.
	// When multiple keys collide under folding, the alphabetically first wins.
	foldedIndex map[string]string
}

// WrapProperties wraps a property map with ownership transfer semantics.
//
// After calling WrapProperties, the caller MUST NOT retain or use any reference
// to props or any mutable value reachable from props. Mutation after WrapProperties
// is undefined behavior.
//
// Use [WrapPropertiesClone] when the map comes from external sources or when
// ownership cannot be verified.
func WrapProperties(props map[string]any) Properties {
	if props == nil {
		return Properties{}
	}

	entries := make(map[string]Value, len(props))
	for k, v := range props {
		entries[k] = Value{val: wrapValue(v, false)}
	}
	sortedKeys := computeSortedKeys(entries)
	return Properties{
		entries:     entries,
		sortedKeys:  sortedKeys,
		foldedIndex: computeFoldedIndex(sortedKeys),
	}
}

// WrapPropertiesClone wraps a deep clone of the property map.
//
// The caller may freely retain and mutate the original map after cloning.
// This is safe for maps from external sources or shared references.
func WrapPropertiesClone(props map[string]any) Properties {
	if props == nil {
		return Properties{}
	}

	entries := make(map[string]Value, len(props))
	for k, v := range props {
		entries[k] = Value{val: wrapValue(v, true)}
	}
	sortedKeys := computeSortedKeys(entries)
	return Properties{
		entries:     entries,
		sortedKeys:  sortedKeys,
		foldedIndex: computeFoldedIndex(sortedKeys),
	}
}

// Get returns the value for the given property name and true if it exists.
// Returns (zero Value, false) if the property does not exist.
//
// This performs an exact, case-sensitive match. Use [Properties.GetFold]
// for case-insensitive lookup.
func (p Properties) Get(name string) (Value, bool) {
	v, ok := p.entries[name]
	return v, ok
}

// GetFold returns the value for a property name using ASCII case-insensitive matching.
//
// Only ASCII letters (a-z, A-Z) are folded; other characters must match exactly.
// If multiple keys match when folded (e.g., "Name" and "NAME"), the alphabetically
// first key wins. This provides deterministic behavior.
//
// Returns (zero Value, false) if no matching property exists.
func (p Properties) GetFold(name string) (Value, bool) {
	// First try exact match (common case, O(1))
	if v, ok := p.entries[name]; ok {
		return v, ok
	}

	// Use folded index for O(1) case-insensitive lookup
	if originalKey, ok := p.foldedIndex[toLowerASCII(name)]; ok {
		return p.entries[originalKey], true
	}
	return Value{}, false
}

// Len returns the number of properties.
func (p Properties) Len() int {
	return len(p.entries)
}

// Keys returns an iterator over property names.
//
// The iteration order is not guaranteed to be consistent across calls.
// Use [Properties.SortedKeys] for deterministic ordering.
func (p Properties) Keys() iter.Seq[string] {
	return func(yield func(string) bool) {
		for k := range p.entries {
			if !yield(k) {
				return
			}
		}
	}
}

// SortedKeys returns an iterator over property names in sorted order.
//
// This provides deterministic iteration order for stable output.
// The sorted keys are precomputed at construction time.
func (p Properties) SortedKeys() iter.Seq[string] {
	return func(yield func(string) bool) {
		for _, k := range p.sortedKeys {
			if !yield(k) {
				return
			}
		}
	}
}

// computeSortedKeys computes the sorted key list for a property map.
func computeSortedKeys(entries map[string]Value) []string {
	if len(entries) == 0 {
		return nil
	}

	keys := make([]string, 0, len(entries))
	for k := range entries {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, cmp.Compare[string])
	return keys
}

// Range returns an iterator over property name-value pairs.
//
// The iteration order is not guaranteed to be consistent across calls.
func (p Properties) Range() iter.Seq2[string, Value] {
	return func(yield func(string, Value) bool) {
		for k, v := range p.entries {
			if !yield(k, v) {
				return
			}
		}
	}
}

// SortedRange returns an iterator over property name-value pairs in sorted key order.
//
// This provides deterministic iteration order for stable output.
// The sorted keys are precomputed at construction time.
func (p Properties) SortedRange() iter.Seq2[string, Value] {
	return func(yield func(string, Value) bool) {
		for _, k := range p.sortedKeys {
			if !yield(k, p.entries[k]) {
				return
			}
		}
	}
}

// Clone returns a deep copy of the properties as a mutable map[string]any.
//
// This is the escape hatch for callers who need to modify values.
// The returned map is independent of the immutable Properties.
func (p Properties) Clone() map[string]any {
	if p.entries == nil {
		return nil
	}

	result := make(map[string]any, len(p.entries))
	for k, v := range p.entries {
		result[k] = cloneValue(v)
	}
	return result
}

// computeFoldedIndex builds a map from lowercase (ASCII-folded) keys to original keys.
// Keys are processed in sorted order, so collisions resolve to the alphabetically first key.
func computeFoldedIndex(sortedKeys []string) map[string]string {
	if len(sortedKeys) == 0 {
		return nil
	}

	index := make(map[string]string, len(sortedKeys))
	for _, k := range sortedKeys {
		folded := toLowerASCII(k)
		// First key (alphabetically) wins on collision
		if _, exists := index[folded]; !exists {
			index[folded] = k
		}
	}
	return index
}

// toLowerASCII converts ASCII letters (A-Z) to lowercase (a-z).
// Non-ASCII characters are left unchanged.
func toLowerASCII(s string) string {
	// Fast path: check if any conversion is needed
	needsConversion := false
	for i := range len(s) {
		if 'A' <= s[i] && s[i] <= 'Z' {
			needsConversion = true
			break
		}
	}
	if !needsConversion {
		return s
	}

	b := []byte(s)
	for i, c := range b {
		if 'A' <= c && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}
