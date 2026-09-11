package immutable

import (
	"math"
	"reflect"
)

// Option configures the behavior of Wrap constructors.
type Option func(*wrapConfig)

type wrapConfig struct {
	clone bool
}

// WithClone selects whether a value the constructor would store as-is — a map
// with a non-string key, and everything reachable through it — is deep-cloned
// first. String-keyed maps and slices are copied into immutable containers
// whether or not it is set; structs and pointers are stored as-is whether or
// not it is set. With true, the caller can retain and mutate such a map after
// construction. With false — the default — the constructor takes ownership and
// the caller must not touch the value again.
func WithClone(clone bool) Option {
	return func(c *wrapConfig) { c.clone = clone }
}

func resolveConfig(opts []Option) wrapConfig {
	var cfg wrapConfig
	for _, o := range opts {
		o(&cfg)
	}
	return cfg
}

// Value wraps an arbitrary Go value and provides immutable access.
//
// For primitive types (string, int, float64, bool, nil), the underlying value
// is returned directly via type-safe accessors. For mutable types (map, slice),
// the value is recursively wrapped at construction time.
//
// Value is safe for concurrent read access.
type Value struct {
	// val holds the wrapped value. For primitives, this is the value itself.
	// For a string-keyed map it is a Map[string], for a slice a Slice; any
	// other map is stored as given.
	val any
}

// Wrap wraps a value with ownership transfer semantics.
//
// After calling Wrap, the caller MUST NOT retain or use any reference to v
// or any mutable value reachable from v. Mutation after Wrap is undefined behavior.
//
// Pass [WithClone] with true to deep-clone what would be stored as-is — a
// non-string-keyed map and its contents — so the caller can retain it.
func Wrap(v any, opts ...Option) Value {
	cfg := resolveConfig(opts)
	return Value{val: wrapValue(v, cfg.clone)}
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
	// A wrapper is a struct, so it answers for itself; a reflect kind cannot.
	if w, ok := v.val.(wrapper); ok {
		return w.isNil()
	}
	// Check for typed nils (e.g., var p *int; Wrap(p))
	rv := reflect.ValueOf(v.val)
	switch rv.Kind() {
	case reflect.Pointer, reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Slice:
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
		return wholeFloatToInt64(n)
	case float32:
		return wholeFloatToInt64(float64(n))
	default:
		return 0, false
	}
}

// wholeFloatToInt64 returns f as an int64 when f is a whole number int64 can
// hold. float64(math.MaxInt64) rounds up to 2^63, one past the range, so the
// upper bound is exclusive; converting an out-of-range float is
// implementation-defined, so every guard runs before the conversion.
func wholeFloatToInt64(f float64) (int64, bool) {
	if math.IsNaN(f) || math.IsInf(f, 0) || f != math.Trunc(f) {
		return 0, false
	}
	if f < math.MinInt64 || f >= 0x1p63 {
		return 0, false
	}
	return int64(f), true
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
// If clone is true, a value that would be stored as-is (a non-string-keyed
// map) is deep-cloned first.
func wrapValue(v any, clone bool) any {
	var g cycleGuard
	return wrapValueAt(v, clone, &g)
}

// wrapValueAt is wrapValue on one walk's state. A Value contributes its
// content, so no Value ever holds a Value; any other wrapper is already
// immutable and is stored as itself.
func wrapValueAt(v any, clone bool, g *cycleGuard) any {
	if v == nil {
		return nil
	}
	switch w := v.(type) {
	case Value:
		return w.val
	case wrapper:
		return w
	}

	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Map:
		return wrapMapValue(rv, clone, g)
	case reflect.Slice:
		return wrapSliceValue(rv, clone, g)
	default:
		// Primitives, structs, pointers and arrays are stored as they are.
		return v
	}
}

// wrapMapValue wraps a reflect.Value of kind Map into a Map[string].
// Only string-keyed maps are supported; other key types are stored as-is.
func wrapMapValue(rv reflect.Value, clone bool, g *cycleGuard) any {
	// Only wrap string-keyed maps as Map[string]
	if rv.Type().Key().Kind() == reflect.String {
		if rv.IsNil() {
			// Return typed nil Map (entries: nil) to distinguish from literal nil.
			// This allows Value.Map() to return (zero Map, true) for nil maps.
			return Map[string]{}
		}
		defer g.push(rv)()
		m := make(map[string]Value, rv.Len())
		iter := rv.MapRange()
		for iter.Next() {
			key := iter.Key().String()
			val := iter.Value().Interface()
			m[key] = Value{val: wrapValueAt(val, clone, g)}
		}
		return Map[string]{entries: m, folded: &foldedView{}}
	}

	// For non-string-keyed maps, store as-is (unusual case)
	// This maintains the value but doesn't provide typed access
	if rv.IsNil() {
		return rv.Interface() // the typed nil, so the value keeps its type
	}
	if clone {
		return deepCloneMap(rv, g)
	}
	return rv.Interface()
}

// wrapSliceValue wraps a reflect.Value of kind Slice into a Slice.
func wrapSliceValue(rv reflect.Value, clone bool, g *cycleGuard) any {
	if rv.IsNil() {
		// Return typed nil Slice (elements: nil) to distinguish from literal nil.
		// This allows Value.Slice() to return (zero Slice, true) for nil slices.
		return Slice{}
	}
	defer g.push(rv)()

	elements := make([]Value, rv.Len())
	for i := range rv.Len() {
		val := rv.Index(i).Interface()
		elements[i] = Value{val: wrapValueAt(val, clone, g)}
	}
	return Slice{elements: elements}
}

// deepCloneAt performs a deep clone of any value on one walk's state.
func deepCloneAt(v any, g *cycleGuard) any {
	if v == nil {
		return nil
	}

	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Map:
		return deepCloneMap(rv, g)
	case reflect.Slice:
		return deepCloneSlice(rv, g)
	default:
		return v
	}
}

// deepCloneMap deep-clones a map.
// Handles nil element values correctly using reflect.Zero for interface-typed maps.
func deepCloneMap(rv reflect.Value, g *cycleGuard) any {
	if rv.IsNil() {
		return rv.Interface() // the typed nil, which an untyped nil would lose
	}
	defer g.push(rv)()

	newMap := reflect.MakeMapWithSize(rv.Type(), rv.Len())
	elemType := rv.Type().Elem()
	iter := rv.MapRange()
	for iter.Next() {
		key := iter.Key()
		val := iter.Value().Interface()
		cloned := deepCloneAt(val, g)
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
func deepCloneSlice(rv reflect.Value, g *cycleGuard) any {
	if rv.IsNil() {
		return rv.Interface() // the typed nil, which an untyped nil would lose
	}
	defer g.push(rv)()

	newSlice := reflect.MakeSlice(rv.Type(), rv.Len(), rv.Len())
	elemType := rv.Type().Elem()
	for i := range rv.Len() {
		val := rv.Index(i).Interface()
		cloned := deepCloneAt(val, g)
		if cloned == nil {
			// reflect.ValueOf(nil) is invalid; use Zero for nil interface values
			newSlice.Index(i).Set(reflect.Zero(elemType))
		} else {
			newSlice.Index(i).Set(reflect.ValueOf(cloned))
		}
	}
	return newSlice.Interface()
}
