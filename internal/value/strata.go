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
// Returns InvalidStrata for types with no category: maps, channels, and structs
// other than [immutable.Slice]. A pointer takes the strata of what it points at
// and a named type the strata of its underlying kind, so the categories agree
// with [Order] and with the extraction functions.
func TypeStrata(a any) int {
	if a == nil {
		return NilStrata
	}
	// Predeclared types first: this is the whole of the hot path, and it
	// answers without reaching reflect.
	switch a.(type) {
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
		return StringStrata
	case json.Number:
		// Numeric here because Classify says numeric; a lexical number that
		// ordered as a string would compare against int64 by strata rank and
		// answer without erroring.
		return NumericStrata
	case immutable.Slice:
		// A List- or Vector-typed property carries this out of the
		// evaluator's scope; it is a struct, so no reflect kind names it.
		return SliceStrata
	}
	return strataOfKind(reflect.ValueOf(a))
}

// strataOfKind reports the strata of rv by its underlying kind, dereferencing
// pointers first. It is the fallback the predeclared type switch cannot answer:
// a pointer, or a named type over a predeclared base.
func strataOfKind(rv reflect.Value) int {
	for rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return NilStrata
		}
		rv = rv.Elem()
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
	default:
		return InvalidStrata
	}
}
