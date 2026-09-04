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
	am, aIsMap := mapEntries(a)
	bm, bIsMap := mapEntries(b)
	if aIsMap || bIsMap {
		if !aIsMap || !bIsMap || len(am) != len(bm) {
			return false
		}
		for k, av := range am {
			bv, ok := bm[k]
			if !ok || !Equal(av, bv) {
				return false
			}
		}
		return true
	}
	if TypeStrata(a) == SliceStrata && TypeStrata(b) == SliceStrata {
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

// mapEntries reads a map value as its unwrapped entries: an [immutable.Map],
// [immutable.Properties], or a string-keyed Go map. Anything else is not a
// map here.
func mapEntries(v any) (map[string]any, bool) {
	switch m := v.(type) {
	case immutable.Map[string]:
		out := make(map[string]any, m.Len())
		for k, val := range m.Range() {
			out[k] = val.Unwrap()
		}
		return out, true
	case immutable.Properties:
		out := make(map[string]any, m.Len())
		for k, val := range m.Range() {
			out[k] = val.Unwrap()
		}
		return out, true
	case map[string]any:
		return m, true
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Map && rv.Type().Key().Kind() == reflect.String {
		out := make(map[string]any, rv.Len())
		for it := rv.MapRange(); it.Next(); {
			out[it.Key().String()] = it.Value().Interface()
		}
		return out, true
	}
	return nil, false
}
