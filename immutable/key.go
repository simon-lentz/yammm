package immutable

import (
	"encoding/json"
	"fmt"
	"iter"
	"slices"
)

// Key provides immutable access to primary or foreign key components.
//
// Key wraps a slice of key components (such as ["us", 12345] for a composite key).
// It is NOT comparable and cannot be used directly as a map key. For map-keyed
// lookups, use [Key.String] to produce a canonical string representation.
//
// Key is safe for concurrent read access.
type Key struct {
	components []Value

	// str is the canonical string representation, precomputed at construction.
	// It is empty only for the zero Key, which WrapKey(nil) returns, and String
	// reports "[]" for it.
	str string
}

// WrapKey returns components held immutably as a key. It copies the slice, and
// wraps each component as [Wrap] does, so a later change to components reaches
// nothing it returned, save through a struct, a pointer or an array, which Wrap
// stores as given.
//
// WrapKey panics if encoding/json refuses what [Key.Clone] returns for a
// component, such as a channel, a function, a complex number, a cyclic value, a
// NaN or infinite float, or a time.Time outside years 0 to 9999. A key
// component is a programmer's value.
func WrapKey(components []any) Key {
	if components == nil {
		return Key{}
	}

	wrapped := make([]Value, len(components))
	for i, c := range components {
		wrapped[i] = Value{val: wrapValue(c)}
	}
	return Key{components: wrapped, str: computeKeyString(wrapped)}
}

// At returns the component at index i. It panics if i is out of range, as a
// slice index does.
func (k Key) At(i int) Value {
	return k.components[i]
}

// Len returns the number of key components.
func (k Key) Len() int {
	return len(k.components)
}

// Values returns an iterator over the components in order.
func (k Key) Values() iter.Seq[Value] {
	return slices.Values(k.components)
}

// Clone returns the components as a new []any, each as [Value.Clone] returns
// it: a container at every depth is copied, and what [Value.Clone] shares is
// shared with the Key. It returns nil for a key built from nil (WrapKey(nil)
// or Key{}) and an empty slice for one built from none (WrapKey([]any{})); both
// keys print "[]".
func (k Key) Clone() []any {
	if k.components == nil {
		return nil
	}

	result := make([]any, len(k.components))
	for i, c := range k.components {
		result[i] = cloneValue(c)
	}
	return result
}

// String returns the canonical JSON array representation of the key.
//
// This format is suitable for use as a map key and is compatible with
// [graph.FormatKey] output. The string is precomputed at construction time.
//
// The invariant key.String() == graph.FormatKey(key.Clone()...) holds for every
// key built from a non-nil slice, save one holding a pointer, struct or array,
// at any depth, that refers to data changed after the wrap: such a value is
// stored as given, and String is computed at the wrap. It does not hold for a
// key built from nil (WrapKey(nil) or Key{}): [Key.Clone] returns nil there, and
// spreading a nil slice through a variadic parameter renders `null`, while
// String reports `[]`. A component of a
// slice kind or a string-keyed map kind with its own JSON encoding, such as
// []byte, is wrapped by its kind, so String follows Clone and not that encoding;
// a map with another key type keeps its type, and with it that encoding.
//
// Examples:
//
//	Key with ["us", 12345] -> `["us",12345]`
//	Key with [42] -> `[42]`
//	WrapKey([]any{}) -> `[]`
//	Key{} or WrapKey(nil) -> `[]`
func (k Key) String() string {
	if k.str == "" {
		return "[]" // Handle literal Key{} zero value
	}
	return k.str
}

// computeKeyString computes the canonical JSON array string for wrapped components.
// Panics if any component cannot be JSON-marshaled.
func computeKeyString(wrapped []Value) string {
	if len(wrapped) == 0 {
		return "[]"
	}

	// Marshal what Clone returns, so String equals FormatKey(Clone()...) by
	// construction, nil maps and slices included.
	raw := make([]any, len(wrapped))
	for i, c := range wrapped {
		raw[i] = cloneValue(c)
	}

	data, err := json.Marshal(raw)
	if err != nil {
		panic(fmt.Sprintf("immutable: key component is not JSON-marshalable: %v", err))
	}
	return string(data)
}
