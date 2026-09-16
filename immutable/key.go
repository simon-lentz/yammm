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
	// Always "[]" for empty/zero keys (both Key{} and WrapKey(nil)).
	str string
}

// WrapKey wraps key components with ownership transfer semantics.
//
// After calling WrapKey, the caller MUST NOT retain or use any reference
// to components or any mutable value reachable from components. Mutation
// after WrapKey is undefined behavior.
//
// WrapKey panics if any component cannot be JSON-marshaled. This includes:
//   - Channels and functions
//   - Cyclic data structures, which the wrap itself refuses
//   - NaN or Inf floating-point values
//   - Maps with unsupported key types (e.g., struct keys; int keys are converted to strings)
//
// This is a programmer error—key components must be JSON-compatible values.
// This behavior matches [graph.FormatKey].
//
// Pass [WithClone] with true to deep-clone what would be stored as-is — a
// non-string-keyed map and its contents — so the caller can retain it.
func WrapKey(components []any, opts ...Option) Key {
	if components == nil {
		return Key{str: "[]"}
	}

	cfg := resolveConfig(opts)
	wrapped := make([]Value, len(components))
	for i, c := range components {
		wrapped[i] = Value{val: wrapValue(c, cfg.clone)}
	}
	return Key{components: wrapped, str: computeKeyString(wrapped)}
}

// Get returns the key component at the given index.
//
// Panics if i is out of bounds, matching Go slice semantics.
func (k Key) Get(i int) Value {
	return k.components[i]
}

// Len returns the number of key components.
func (k Key) Len() int {
	return len(k.components)
}

// Iter returns an iterator over the key components.
func (k Key) Iter() iter.Seq[Value] {
	return slices.Values(k.components)
}

// Clone returns a deep copy of the key components as a mutable []any.
//
// This is the escape hatch for callers who need to modify values.
// The returned slice is independent of the immutable Key.
//
// Clone preserves Go slice semantics: it returns nil for keys constructed
// from nil input (WrapKey(nil) or Key{}), and an empty slice for keys
// constructed from empty input (WrapKey([]any{})). Both cases have
// String() == "[]", but Clone() distinguishes them for callers that need
// to differentiate nil vs empty slices.
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
// key built from components. It does not hold for a key built from none:
// [Key.Clone] returns nil there, and spreading a nil slice through a variadic
// parameter renders `null`, while String reports `[]`. A component whose type
// has its own JSON encoding and a slice or map kind, such as []byte, is wrapped
// by its kind, so String follows Clone and not that encoding.
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
