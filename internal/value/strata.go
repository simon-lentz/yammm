package value

import (
	"encoding/json"
	"reflect"
	"regexp"

	"github.com/simon-lentz/yammm/immutable"
)

// Strata constants define the ordering of value types for canonical comparison.
// The order from lowest to highest is: Nil < Bool < Numeric < String < Slice.
// InvalidStrata indicates an unsupported type.
const (
	InvalidStrata = iota
	NilStrata
	BoolStrata
	NumericStrata
	StringStrata
	SliceStrata
)

// TypeStrata returns the strata for a value's type — its ordering category.
//
// Returns InvalidStrata for types with no category: a pointer, nil or not, a
// non-nil map, a channel, a function, a struct other than an [immutable.Slice]
// and a nil [immutable] container, and a json.Number that is no JSON number
// literal. The one pointer with a category is a non-nil *regexp.Regexp, the
// evaluator's pattern literal, which is StringStrata. A value that holds none
// ([Absent]) is NilStrata, and a scalar takes
// the strata of the form [Scalar] reads it in, so the categories agree with
// [Order] and with the extraction functions.
func TypeStrata(a any) int {
	if a == nil {
		return NilStrata
	}
	// Predeclared types first: this is the whole of the hot path, and it
	// answers without reaching reflect.
	switch x := a.(type) {
	case bool:
		return BoolStrata
	// Signed integers
	case int, int8, int16, int32, int64:
		return NumericStrata
	// Unsigned integers
	case uint, uint8, uint16, uint32, uint64, uintptr:
		return NumericStrata
	// Floats
	case float32, float64:
		return NumericStrata
	case string:
		return StringStrata
	case *regexp.Regexp:
		// The evaluator's pattern literal. A nil one is a pointer like any other.
		if x == nil {
			return InvalidStrata
		}
		return StringStrata
	case json.Number:
		// Numeric here because Classify says numeric; a lexical number that
		// ordered as a string would compare against int64 by strata rank and
		// answer without erroring.
		if !immutable.IsNumberLiteral(x) {
			return InvalidStrata
		}
		return NumericStrata
	case immutable.Slice:
		// A List- or Vector-typed property carries this out of the
		// evaluator's scope; it is a struct, so no reflect kind names it.
		if immutable.IsNilContainer(a) {
			return NilStrata
		}
		return SliceStrata
	}
	return strataOfKind(a)
}

// strataOfKind reports the strata of a value the predeclared type switch
// cannot answer: nil for a value [Absent] reads as none, a named scalar by its
// kind, a non-nil slice as a list, and any other value as none.
func strataOfKind(a any) int {
	if immutable.IsNilContainer(a) {
		return NilStrata
	}
	rv := reflect.ValueOf(a)
	if isNil(rv) {
		return NilStrata
	}
	switch rv.Kind() {
	case reflect.Bool:
		return BoolStrata
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64:
		return NumericStrata
	case reflect.String:
		return StringStrata
	case reflect.Slice:
		// Elements are not inspected here; an unsupported element fails at
		// the comparison that reaches it.
		return SliceStrata
	}
	return InvalidStrata
}
