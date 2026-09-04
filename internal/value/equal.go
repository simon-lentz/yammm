package value

import (
	"reflect"

	"github.com/simon-lentz/yammm/immutable"
)

// Equal reports whether a and b are the same value. It is structural and
// never errors, which is what `==`, `!=`, `in`, `Contains` and `Unique` need:
// two values [Order] can rank are equal when it ranks them together (so
// 1 == 1.0 and NaN == NaN); two maps — an instance among them — are equal
// when they hold the same keys and each value is equal by this rule; two
// lists when their elements are pairwise equal; and values of different
// kinds, or of a kind nothing here can read, are not equal. A nil is equal
// to nil alone.
func Equal(a, b any) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	as, bs := TypeStrata(a), TypeStrata(b)
	if as != InvalidStrata && bs != InvalidStrata {
		if as == SliceStrata && bs == SliceStrata {
			an, aAt := sliceAccessor(a)
			bn, bAt := sliceAccessor(b)
			if an != bn {
				return false
			}
			for i := range an {
				if !Equal(aAt(i), bAt(i)) {
					return false
				}
			}
			return true
		}
		cmp, err := Order(a, b)
		return err == nil && cmp == 0
	}
	// Only a value the order cannot rank reaches here: a map compares in
	// place, entry by entry, so an instance comparison allocates nothing.
	if !isMap(a) || !isMap(b) || mapLen(a) != mapLen(b) {
		return false
	}
	equal := true
	rangeMap(a, func(k string, av any) bool {
		bv, ok := mapLookup(b, k)
		if !ok || !Equal(av, bv) {
			equal = false
			return false
		}
		return true
	})
	return equal
}

// isMap reports whether v is a map shape [Equal] reads: an [immutable.Map],
// [immutable.Properties], or a string-keyed Go map.
func isMap(v any) bool {
	switch v.(type) {
	case immutable.Map[string], immutable.Properties, map[string]any:
		return true
	}
	rv := reflect.ValueOf(v)
	return rv.Kind() == reflect.Map && rv.Type().Key().Kind() == reflect.String
}

func mapLen(v any) int {
	switch m := v.(type) {
	case immutable.Map[string]:
		return m.Len()
	case immutable.Properties:
		return m.Len()
	case map[string]any:
		return len(m)
	}
	return reflect.ValueOf(v).Len()
}

// mapLookup reads one entry of a map shape, unwrapped.
func mapLookup(v any, key string) (any, bool) {
	switch m := v.(type) {
	case immutable.Map[string]:
		val, ok := m.Get(key)
		return val.Unwrap(), ok
	case immutable.Properties:
		val, ok := m.Get(key)
		return val.Unwrap(), ok
	case map[string]any:
		val, ok := m[key]
		return val, ok
	}
	rv := reflect.ValueOf(v).MapIndex(reflect.ValueOf(key))
	if !rv.IsValid() {
		return nil, false
	}
	return rv.Interface(), true
}

// rangeMap visits every entry of a map shape, unwrapped, until yield returns
// false.
func rangeMap(v any, yield func(string, any) bool) {
	switch m := v.(type) {
	case immutable.Map[string]:
		for k, val := range m.Range() {
			if !yield(k, val.Unwrap()) {
				return
			}
		}
	case immutable.Properties:
		for k, val := range m.Range() {
			if !yield(k, val.Unwrap()) {
				return
			}
		}
	case map[string]any:
		for k, val := range m {
			if !yield(k, val) {
				return
			}
		}
	default:
		for it := reflect.ValueOf(v).MapRange(); it.Next(); {
			if !yield(it.Key().String(), it.Value().Interface()) {
				return
			}
		}
	}
}
