package immutable

import (
	"math"
	"reflect"
)

// Value wraps an arbitrary Go value and provides immutable access.
//
// For primitive types (string, int, float64, bool, nil), the underlying value
// is returned directly via type-safe accessors. For mutable types (map, slice),
// the value is recursively wrapped at construction time.
//
// Value is safe for concurrent read access.
type Value struct {
	// val holds the wrapped value. For primitives, this is the value itself.
	// For maps and slices, this is a wrapped Map[string] or Slice.
	val any
}

// Wrap wraps a value with ownership transfer semantics.
//
// After calling Wrap, the caller MUST NOT retain or use any reference to v
// or any mutable value reachable from v. Mutation after Wrap is undefined behavior.
//
// Use [WrapClone] when the value comes from external sources or when ownership
// cannot be verified.
func Wrap(v any) Value {
	return Value{val: wrapValue(v, false)}
}

// WrapClone wraps a deep clone of the value.
//
// The caller may freely retain and mutate the original value after cloning.
// This is safe for values from external sources or shared references.
func WrapClone(v any) Value {
	return Value{val: wrapValue(v, true)}
}

// Unwrap returns the underlying value.
//
// For primitives, this returns the value directly. For maps and slices,
// this returns the wrapped [Map] or [Slice] type, not the raw map/slice.
// Use [Value.Map] or [Value.Slice] for type-safe access to collections,
// or use the Clone() method on the returned wrapper to get a mutable copy.
func (v Value) Unwrap() any {
	return v.val
}

// IsNil reports whether the wrapped value is nil.
//
// This returns true for:
//   - Literal nil passed to [Wrap]
//   - Typed nil pointers, channels, functions, interfaces
//   - Nil maps and slices (wrapped as typed [Map] or [Slice])
func (v Value) IsNil() bool {
	if v.val == nil {
		return true
	}
	// Check for wrapped nil maps/slices (entries/elements are nil)
	switch inner := v.val.(type) {
	case Map[string]:
		return inner.entries == nil
	case Slice:
		return inner.elements == nil
	}
	// Check for typed nils (e.g., var p *int; Wrap(p))
	rv := reflect.ValueOf(v.val)
	switch rv.Kind() {
	case reflect.Ptr, reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Slice:
		return rv.IsNil()
	}
	return false
}

// Bool returns the value as a bool and true if the value is a bool.
// Returns (false, false) if the value is not a bool.
func (v Value) Bool() (bool, bool) {
	b, ok := v.val.(bool)
	return b, ok
}

// Int returns the value as an int64 and true if the value is an integer type.
// Handles int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64,
// and float32/float64 values that represent whole numbers.
// Returns (0, false) if the value is not a numeric type, if the value
// exceeds the int64 range, or if a floating-point value is not a whole number.
//
// For unsigned values that may exceed int64 range, use [Value.Unwrap] for direct access.
func (v Value) Int() (int64, bool) {
	switch n := v.val.(type) {
	case int:
		return int64(n), true
	case int8:
		return int64(n), true
	case int16:
		return int64(n), true
	case int32:
		return int64(n), true
	case int64:
		return n, true
	case uint:
		// Use uint64 comparison for 32-bit architecture portability.
		// Convert to uint64 first to satisfy gosec G115 (integer overflow check).
		n64 := uint64(n)
		if n64 > uint64(math.MaxInt64) {
			return 0, false // Exceeds int64 range
		}
		return int64(n64), true
	case uint8:
		return int64(n), true
	case uint16:
		return int64(n), true
	case uint32:
		return int64(n), true
	case uint64:
		if n > math.MaxInt64 {
			return 0, false // Exceeds int64 range
		}
		return int64(n), true
	case float64:
		// JSON numbers are float64; check if it's a representable whole number.
		// Guard before converting to avoid implementation-dependent behavior
		// for NaN, Inf, and out-of-range values per Go spec.
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return 0, false
		}
		if n < float64(math.MinInt64) || n > float64(math.MaxInt64) {
			return 0, false
		}
		if n != math.Trunc(n) {
			return 0, false // Not a whole number
		}
		return int64(n), true
	case float32:
		// Handle float32 with same whole-number checks as float64.
		n64 := float64(n)
		if math.IsNaN(n64) || math.IsInf(n64, 0) {
			return 0, false
		}
		if n64 < float64(math.MinInt64) || n64 > float64(math.MaxInt64) {
			return 0, false
		}
		if n64 != math.Trunc(n64) {
			return 0, false // Not a whole number
		}
		return int64(n64), true
	default:
		return 0, false
	}
}

// Float returns the value as a float64 and true if the value is a numeric type.
// Handles all integer types and float32/float64.
// Returns (0, false) if the value is not a numeric type.
//
// Note: Integer values larger than 2^53 may lose precision when converted
// to float64. For exact access to large integers, use [Value.Int] or [Value.Unwrap].
func (v Value) Float() (float64, bool) {
	switch n := v.val.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int8:
		return float64(n), true
	case int16:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint:
		return float64(n), true
	case uint8:
		return float64(n), true
	case uint16:
		return float64(n), true
	case uint32:
		return float64(n), true
	case uint64:
		return float64(n), true
	default:
		return 0, false
	}
}

// String returns the value as a string and true if the value is a string.
// Returns ("", false) if the value is not a string.
func (v Value) String() (string, bool) {
	s, ok := v.val.(string)
	return s, ok
}

// Map returns the value as an immutable Map[string] and true if the value
// is a wrapped map with string keys.
//
// Only string-keyed maps (map[string]T) are wrapped as Map[string] during
// construction. Maps with other key types (e.g., map[int]any) are stored
// as-is without typed wrapping. For such maps, Map() returns (zero Map, false)
// and the original map can be accessed via [Value.Unwrap] with a type assertion.
//
// Returns (zero Map, false) if the value is not a string-keyed map.
func (v Value) Map() (Map[string], bool) {
	m, ok := v.val.(Map[string])
	return m, ok
}

// Slice returns the value as an immutable Slice and true if the value
// is a wrapped slice. Returns (zero Slice, false) if the value is not a slice.
func (v Value) Slice() (Slice, bool) {
	s, ok := v.val.(Slice)
	return s, ok
}

// wrapValue recursively wraps a value.
// If clone is true, mutable values are deep-cloned before wrapping.
func wrapValue(v any, clone bool) any {
	if v == nil {
		return nil
	}

	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Map:
		return wrapMapValue(rv, clone)
	case reflect.Slice:
		return wrapSliceValue(rv, clone)
	default:
		// Primitives are inherently immutable
		return v
	}
}

// wrapMapValue wraps a reflect.Value of kind Map into a Map[string].
// Only string-keyed maps are supported; other key types are stored as-is.
func wrapMapValue(rv reflect.Value, clone bool) any {
	// Only wrap string-keyed maps as Map[string]
	if rv.Type().Key().Kind() == reflect.String {
		if rv.IsNil() {
			// Return typed nil Map (entries: nil) to distinguish from literal nil.
			// This allows Value.Map() to return (zero Map, true) for nil maps.
			return Map[string]{}
		}
		m := make(map[string]Value, rv.Len())
		iter := rv.MapRange()
		for iter.Next() {
			key := iter.Key().String()
			val := iter.Value().Interface()
			m[key] = Value{val: wrapValue(val, clone)}
		}
		return Map[string]{entries: m}
	}

	// For non-string-keyed maps, store as-is (unusual case)
	// This maintains the value but doesn't provide typed access
	if rv.IsNil() {
		return nil
	}
	if clone {
		return deepCloneMap(rv)
	}
	return rv.Interface()
}

// wrapSliceValue wraps a reflect.Value of kind Slice into a Slice.
func wrapSliceValue(rv reflect.Value, clone bool) any {
	if rv.IsNil() {
		// Return typed nil Slice (elements: nil) to distinguish from literal nil.
		// This allows Value.Slice() to return (zero Slice, true) for nil slices.
		return Slice{}
	}

	elements := make([]Value, rv.Len())
	for i := range rv.Len() {
		val := rv.Index(i).Interface()
		elements[i] = Value{val: wrapValue(val, clone)}
	}
	return Slice{elements: elements}
}

// deepClone performs a deep clone of any value.
func deepClone(v any) any {
	if v == nil {
		return nil
	}

	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Map:
		return deepCloneMap(rv)
	case reflect.Slice:
		return deepCloneSlice(rv)
	default:
		return v
	}
}

// deepCloneMap deep-clones a map.
// Handles nil element values correctly using reflect.Zero for interface-typed maps.
func deepCloneMap(rv reflect.Value) any {
	if rv.IsNil() {
		return nil
	}

	newMap := reflect.MakeMapWithSize(rv.Type(), rv.Len())
	elemType := rv.Type().Elem()
	iter := rv.MapRange()
	for iter.Next() {
		key := iter.Key()
		val := iter.Value().Interface()
		cloned := deepClone(val)
		if cloned == nil {
			// reflect.ValueOf(nil) is invalid; use Zero for nil interface values
			newMap.SetMapIndex(key, reflect.Zero(elemType))
		} else {
			newMap.SetMapIndex(key, reflect.ValueOf(cloned))
		}
	}
	return newMap.Interface()
}

// deepCloneSlice deep-clones a slice.
// Handles nil element values correctly using reflect.Zero for interface-typed slices.
func deepCloneSlice(rv reflect.Value) any {
	if rv.IsNil() {
		return nil
	}

	newSlice := reflect.MakeSlice(rv.Type(), rv.Len(), rv.Len())
	elemType := rv.Type().Elem()
	for i := range rv.Len() {
		val := rv.Index(i).Interface()
		cloned := deepClone(val)
		if cloned == nil {
			// reflect.ValueOf(nil) is invalid; use Zero for nil interface values
			newSlice.Index(i).Set(reflect.Zero(elemType))
		} else {
			newSlice.Index(i).Set(reflect.ValueOf(cloned))
		}
	}
	return newSlice.Interface()
}
