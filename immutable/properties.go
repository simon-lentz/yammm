package immutable

import (
	"iter"
	"maps"
	"slices"
	"sync"
)

// Properties holds a string-keyed map immutably, such as an instance's or an
// edge's properties. [Wrap] makes one of each string-keyed map it reaches, save
// inside a map with a non-string key. Its iterators visit the names in sorted
// order, and [Properties.GetFold] reads a name with its ASCII letters folded.
//
// Properties is safe for concurrent read access.
type Properties struct {
	entries map[string]Value

	// view holds the sorted names and the folded index, each computed on first
	// use and shared by every copy. It is nil exactly when entries is.
	view *propertiesView
}

// propertiesView is the sorted-name list and folded index of one entry set,
// each computed once, so building a Properties pays for neither until it is
// read, and a sorted read pays for no folded index.
type propertiesView struct {
	sortOnce    sync.Once
	sortedKeys  []string
	foldOnce    sync.Once
	foldedIndex map[string]string
}

// newProperties returns a Properties over entries, which it takes.
func newProperties(entries map[string]Value) Properties {
	return Properties{entries: entries, view: &propertiesView{}}
}

// sorted returns the names in sorted order. Safe for concurrent first use.
func (p Properties) sorted() []string {
	if p.view == nil {
		return nil
	}
	p.view.sortOnce.Do(func() {
		p.view.sortedKeys = slices.Sorted(maps.Keys(p.entries))
	})
	return p.view.sortedKeys
}

// folded returns the folded index. Safe for concurrent first use.
func (p Properties) folded() map[string]string {
	if p.view == nil {
		return nil
	}
	p.view.foldOnce.Do(func() {
		p.view.foldedIndex = computeFoldedIndex(p.sorted())
	})
	return p.view.foldedIndex
}

// WrapProperties returns props held immutably. It copies the map, and wraps
// each value as [Wrap] does, so a later change to props reaches nothing it
// returned, save through a value Wrap stores as given, such as a struct, a
// pointer or an array. A nil map gives the zero Properties.
func WrapProperties(props map[string]any) Properties {
	if props == nil {
		return Properties{}
	}
	entries := make(map[string]Value, len(props))
	for k, v := range props {
		entries[k] = Value{val: wrapValue(v)}
	}
	return newProperties(entries)
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
// A key that matches name exactly wins. Otherwise, of the keys that fold to name
// (e.g., "Name" and "NAME"), the alphabetically first wins, so the result is
// deterministic.
//
// Returns (zero Value, false) if no matching property exists.
func (p Properties) GetFold(name string) (Value, bool) {
	if v, ok := p.entries[name]; ok {
		return v, ok
	}
	if originalKey, ok := lookupFolded(p.folded(), name); ok {
		return p.entries[originalKey], true
	}
	return Value{}, false
}

// lookupFolded reads index at name with its ASCII capitals lowered. A name of
// up to 64 bytes is lowered in a stack buffer and read without a copy, so the
// lookup allocates nothing.
func lookupFolded(index map[string]string, name string) (string, bool) {
	var buf [64]byte
	b := append(buf[:0], name...)
	lowerASCII(b)
	k, ok := index[string(b)]
	return k, ok
}

// Len returns the number of properties.
func (p Properties) Len() int {
	return len(p.entries)
}

// Keys returns an iterator over the property names in sorted order.
func (p Properties) Keys() iter.Seq[string] {
	// One closure and no other statement keeps Keys inlinable, so a range over
	// it allocates nothing; the closure reads the sorted names when it runs.
	return func(yield func(string) bool) {
		for _, k := range p.sorted() {
			if !yield(k) {
				return
			}
		}
	}
}

// All returns an iterator over the name-value pairs in sorted name order.
func (p Properties) All() iter.Seq2[string, Value] {
	// One closure and no other statement keeps All inlinable, as Keys.
	return func(yield func(string, Value) bool) {
		for _, k := range p.sorted() {
			if !yield(k, p.entries[k]) {
				return
			}
		}
	}
}

// Clone returns the properties as a new map[string]any, each value as
// [Value.Clone] returns it: a container at every depth is copied, and what
// [Value.Clone] shares is shared with the Properties. The zero Properties
// clones to nil, and any other Properties to a non-nil map.
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
	lowerASCII(b)
	return string(b)
}

// lowerASCII lowers the ASCII capitals of b in place.
func lowerASCII(b []byte) {
	for i, c := range b {
		if 'A' <= c && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
}
