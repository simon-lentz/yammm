package immutable

import (
	"iter"
	"maps"
	"slices"
	"sync"
)

// Properties holds a string-keyed map immutably, such as an instance's or an
// edge's properties. [Wrap] makes one of each string-keyed map it reaches, save
// inside a map with a non-string key. [Properties.Get] reads a name exactly,
// and its iterators visit the names in sorted order.
//
// Properties is safe for concurrent read access.
type Properties struct {
	entries map[string]Value

	// view holds the sorted names, computed on first use and shared by every
	// copy. It is nil exactly when entries is.
	view *propertiesView
}

// propertiesView is the sorted-name list of one entry set, computed once, so
// building a Properties pays for no sort until a sorted read.
type propertiesView struct {
	sortOnce   sync.Once
	sortedKeys []string
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
// The match is exact.
func (p Properties) Get(name string) (Value, bool) {
	v, ok := p.entries[name]
	return v, ok
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
