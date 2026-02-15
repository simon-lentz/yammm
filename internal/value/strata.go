package value

import (
	"reflect"
	"regexp"
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

// TypeStrata returns the strata for a value's type. Use this to determine
// the canonical ordering category of a value before comparison.
//
// Returns InvalidStrata for unsupported types (maps, structs, channels, etc.).
// For regexp.Regexp pointers, returns StringStrata (compared via String()).
//
// IMPORTANT: Only predeclared scalar types are supported. Named scalar types
// (e.g., type MyInt int) return InvalidStrata. This is intentional for consistency
// with GetInt64, GetFloat64, and other value extraction functions that use type switches.
// All slices are supported structurally (via reflect), but their elements must be
// supported types.
func TypeStrata(a any) int {
	if a == nil {
		return NilStrata
	}
	// Use explicit type switches for predeclared types only.
	// Named types (e.g., type MyFloat float64) are not supported and return InvalidStrata.
	// This ensures consistency with GetInt64/GetFloat64/toStringComparable which also
	// use type switches and would fail on named types.
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
	}
	// Slices must use reflect because we can't enumerate all element types.
	// Element comparison will appropriately fail for unsupported element types.
	if t := reflect.TypeOf(a); t != nil && t.Kind() == reflect.Slice {
		return SliceStrata
	}
	return InvalidStrata
}
