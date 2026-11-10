package value

import (
	"encoding/json"
	"reflect"
	"time"

	"github.com/google/uuid"

	"github.com/simon-lentz/yammm/immutable"
)

// Scalar returns v in the one form the validator reads a scalar in ([Classify]):
// a string, bool, int64, uint64, float32 or float64 for a value of that kind, a
// named type as its base value; a json.Number that is a JSON number literal
// ([immutable.IsNumberLiteral]), a time.Time or a uuid.UUID as itself; nil for a
// value that holds none ([Absent]). It reports false for every other value: a
// json.Number holding other text, a pointer, nil or not, a struct, an array, a
// map, a slice, a channel, a function and a complex number.
func Scalar(v any) (any, bool) {
	held, ok := form(v)
	if n, isNumber := held.(json.Number); isNumber && !immutable.IsNumberLiteral(n) {
		return nil, false
	}
	return held, ok
}

// UnreadNumber returns v when it is a json.Number whose text is no JSON number
// literal: the value Scalar refuses for its text alone, which a refusal names.
func UnreadNumber(v any) (json.Number, bool) {
	n, ok := v.(json.Number)
	return n, ok && !immutable.IsNumberLiteral(n)
}

var numberType = reflect.TypeFor[json.Number]()

// literal returns rv as a json.Number when it is one whose text is a JSON number
// literal.
func literal(rv reflect.Value) (json.Number, bool) {
	if !rv.IsValid() || rv.Type() != numberType {
		return "", false
	}
	n := json.Number(rv.String())
	return n, immutable.IsNumberLiteral(n)
}

// NumberLiteral returns v when it is a json.Number whose text is a JSON number
// literal.
func NumberLiteral(v any) (json.Number, bool) {
	n, ok := v.(json.Number)
	return n, ok && immutable.IsNumberLiteral(n)
}

// Absent reports whether v holds no value: nil, a nil map, a nil slice or a nil
// [immutable] container ([immutable.IsNilContainer]). A nil slice or map is no
// empty list or map: encoding/json writes it as null. A pointer is a value of
// no form, a nil one too, so it is not absent.
func Absent(v any) bool {
	if v == nil || immutable.IsNilContainer(v) {
		return true
	}
	return isNil(reflect.ValueOf(v))
}

// isNil reports whether rv is a nil map or a nil slice.
func isNil(rv reflect.Value) bool {
	switch rv.Kind() {
	case reflect.Map, reflect.Slice:
		return rv.IsNil()
	}
	return false
}

// form is [Scalar] without the number literal rule.
func form(v any) (any, bool) {
	switch x := v.(type) {
	case nil:
		return nil, true
	case string, bool, int64, uint64, float64, float32, json.Number, time.Time, uuid.UUID:
		return x, true
	}
	if immutable.IsNilContainer(v) {
		return nil, true
	}
	rv := reflect.ValueOf(v)
	if isNil(rv) {
		return nil, true
	}
	switch rv.Kind() {
	case reflect.String:
		return rv.String(), true
	case reflect.Bool:
		return rv.Bool(), true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return rv.Int(), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return rv.Uint(), true
	case reflect.Float32:
		return float32(rv.Float()), true
	case reflect.Float64:
		return rv.Float(), true
	}
	return nil, false
}
