// Package immutable provides immutable wrapper types for Go values.
//
// This package provides compile-time immutability guarantees for values that
// appear in public API signatures throughout the YAMMM library. It depends only
// on the standard library.
//
// # Design Principles
//
// The immutable package follows several key design principles:
//
//   - Zero-cost reads for primitives: Accessing a string or number incurs no
//     allocation. The underlying value is returned directly via type-safe accessors.
//   - Recursive wrapping for nested structures: Maps and slices are recursively
//     wrapped at construction time, not access time. This is a one-time cost.
//   - Type-safe API: Generic types where appropriate ([Map]), specialized types
//     where semantics matter ([Key], [Properties]).
//   - Iterator-first access: Collections expose [iter.Seq] and [iter.Seq2] iterators
//     as the primary API for zero-allocation iteration.
//
// # Core Types
//
// [Value] wraps an arbitrary Go value and provides immutable access.
// [Wrap] handles typed maps (e.g., map[string]int) and typed slices
// (e.g., []string) via reflection — callers do not need to convert to
// map[K]any or []any before wrapping:
//
//	val := immutable.Wrap(someData)
//	if s, ok := val.String(); ok {
//	    fmt.Println(s)
//	}
//
// [Map] provides immutable access to a map with pre-wrapped values.
//
//	m := immutable.WrapMap(data)
//	for k, v := range m.Range() {
//	    fmt.Printf("%v: %v\n", k, v.Unwrap())
//	}
//
// [Slice] provides immutable access to a slice with pre-wrapped elements:
//
//	s := immutable.WrapSlice(items)
//	for v := range s.Iter() {
//	    fmt.Println(v.Unwrap())
//	}
//
// [Properties] is a specialized wrapper for instance property maps, providing
// case-insensitive lookup via [Properties.GetFold]:
//
//	props := immutable.WrapProperties(data)
//	if v, ok := props.GetFold("UserName"); ok {
//	    // matches "username", "USERNAME", "UserName", etc. (ASCII only)
//	}
//
// [Key] wraps primary or foreign key components and provides a canonical string
// representation via [Key.String]:
//
//	key := immutable.WrapKey([]any{"us", 12345})
//	fmt.Println(key.String()) // ["us",12345]
//
// The specialized [WrapMap] and [WrapSlice] functions require map[K]any and
// []any respectively.
//
// # Ownership Semantics
//
// By default, Wrap constructors (Wrap, WrapMap, WrapSlice, WrapProperties, WrapKey)
// implement whole-graph ownership transfer. After calling Wrap(v), the caller MUST NOT
// retain or use any reference to v or any mutable value reachable from v.
// Mutation after Wrap is undefined behavior.
//
//	data := map[string]any{"key": "value"}
//	wrapped := immutable.WrapMap(data)
//	// data must not be used after this point
//
// Pass [WithClone] to deep-clone mutable values before wrapping. The caller may
// freely retain and mutate the original value after construction:
//
//	data := map[string]any{"key": "value"}
//	wrapped := immutable.WrapMap(data, immutable.WithClone())
//	data["key"] = "modified" // safe: wrapped is isolated
//
// Use bare Wrap when you control the value's origin (e.g., freshly constructed in the
// same scope). Use WithClone when the value comes from external sources, is shared,
// or when ownership cannot be verified.
//
// Note: Deep cloning only applies to maps and slices. Struct values and pointer
// values are stored as-is (shallow copy). For full isolation of struct-based data,
// ensure the original struct is not mutated after calling Wrap with WithClone, or pass
// a map/slice representation of the data.
//
// # Nil Semantics
//
// [Value.IsNil] returns true for:
//   - Literal nil passed to [Wrap]
//   - Typed nil pointers, channels, functions, interfaces
//   - Nil maps and slices
//
// When wrapping nil maps or slices, the resulting Value still identifies as a
// [Map] or [Slice] via [Value.Map] and [Value.Slice], allowing callers to distinguish
// nil-typed values from literal nil:
//
//	var m map[string]any // nil map
//	v := immutable.Wrap(m)
//	v.IsNil()     // true (nil map)
//	v.Map()       // (zero Map, true) - IS a map, just nil
//
// For literal nil:
//
//	v := immutable.Wrap(nil)
//	v.IsNil()     // true (literal nil)
//	v.Map()       // (zero Map, false) - NOT a map
//
// # Thread Safety
//
// All immutable types are safe for concurrent read access. The underlying data
// structures are never modified after construction. Multiple goroutines can
// simultaneously call Get, Iter, Keys, Range, and other read methods.
//
// # Performance Characteristics
//
// | Operation | Cost | Notes |
// | --------- | ---- | ----- |
// | Wrap(primitive) | O(1), no allocation | Primitives stored directly |
// | Wrap(map) | O(n) | Iterates map once to wrap values |
// | Wrap(slice) | O(n) | Iterates slice once to wrap elements |
// | Wrap(any, WithClone()) | O(n) deep | Clones maps/slices recursively; structs/pointers stored as-is |
// | Get(key) / Get(i) | O(1) | Map/slice lookup |
// | Keys() / Iter() | O(1) start | Iterator creation is cheap |
// | Clone() | O(n) deep | Full recursive clone for escape hatch |
//
// # Escape Hatch
//
// Each collection type provides a Clone() method that returns a mutable copy
// of the underlying data. This is the intentional escape hatch for callers
// who need to modify values:
//
//	props := instance.Properties()
//	mutable := props.Clone()
//	mutable["newKey"] = "newValue"
//
// # Dependencies
//
// immutable imports only stdlib packages (cmp, encoding/json, fmt, iter, math,
// reflect, slices, strconv, strings).
// It must not import schema, instance, graph, or adapter.
package immutable
