package value

import (
	"encoding/json"
	"reflect"
	"time"

	"github.com/google/uuid"

	"github.com/simon-lentz/yammm/immutable"
)

// Scalar returns v in the one form every reader and writer of the module reads
// a scalar in, through pointers and interfaces: a string, bool, int64, uint64,
// float32 or float64 for a value of that kind, a named type as its base value;
// a json.Number that is a JSON number literal ([immutable.IsNumberLiteral]), a
// time.Time or a uuid.UUID as itself; nil for a value that holds none
// ([Absent]). It reports false for every other value: a json.Number holding
// other text, a struct, an array, a map, a slice, a channel, a function, a
// complex number, and a chain of more than [maxPointerDepth] pointers and
// interfaces.
//
// A list or a map reaches a writer wrapped, as an immutable.Slice or Map,
// which the writer walks; one behind a pointer is no list, as [ListElems]
// reads one. A writer writes a time.Time or a uuid.UUID only where its
// constraint renders it.
func Scalar(v any) (any, bool) {
	held, ok := form(v)
	if n, isNumber := held.(json.Number); isNumber && !immutable.IsNumberLiteral(n) {
		return nil, false
	}
	return held, ok
}

// UnreadNumber returns the json.Number v holds, read as [Scalar] reads it, when
// its text is no JSON number literal: the value Scalar refuses for its text
// alone, which a refusal names.
func UnreadNumber(v any) (json.Number, bool) {
	rv, ok := walk(v)
	if !ok || !rv.IsValid() || rv.Type() != numberType {
		return "", false
	}
	n := json.Number(rv.String())
	return n, !immutable.IsNumberLiteral(n)
}

var (
	numberType = reflect.TypeFor[json.Number]()
	timeType   = reflect.TypeFor[time.Time]()
	uuidType   = reflect.TypeFor[uuid.UUID]()
)

// walk follows v through at most [maxPointerDepth] pointers and interfaces to
// the value they hold, or the zero Value for a nil at any depth. It reports
// false for a longer chain. It boxes nothing, so a getter that reads the
// returned Value allocates nothing; [Scalar] boxes only the form it returns.
func walk(v any) (reflect.Value, bool) {
	rv := reflect.ValueOf(v)
	for depth := 0; rv.Kind() == reflect.Pointer || rv.Kind() == reflect.Interface; depth++ {
		if rv.IsNil() {
			return reflect.Value{}, true
		}
		if depth == maxPointerDepth {
			return reflect.Value{}, false
		}
		rv = rv.Elem()
	}
	return rv, true
}

// literal returns rv as a json.Number when it is one whose text is a JSON number
// literal.
func literal(rv reflect.Value) (json.Number, bool) {
	if !rv.IsValid() || rv.Type() != numberType {
		return "", false
	}
	n := json.Number(rv.String())
	return n, immutable.IsNumberLiteral(n)
}

// NumberLiteral returns the json.Number v holds, read as [Scalar] reads it, when
// its text is a JSON number literal.
func NumberLiteral(v any) (json.Number, bool) {
	if n, ok := v.(json.Number); ok {
		return n, immutable.IsNumberLiteral(n)
	}
	rv, ok := walk(v)
	if !ok {
		return "", false
	}
	return literal(rv)
}

// Absent reports whether v holds no value, as [Scalar] reads it and every
// writer writes it: nil, a pointer or an interface that is nil at some depth,
// or a nil map, slice, channel, function or unsafe pointer. A bare slice or
// string-keyed map is a value even when nil, an empty list or map to the
// validator, and [ListElems] reads a nil slice as an empty list.
func Absent(v any) bool {
	if v == nil {
		return true
	}
	if container(reflect.ValueOf(v)) {
		return false
	}
	rv, ok := walk(v)
	return ok && isNil(rv)
}

// container reports whether rv is a slice or a string-keyed map, which the
// validator reads as a list or a map, nil or not.
func container(rv reflect.Value) bool {
	switch rv.Kind() {
	case reflect.Slice:
		return true
	case reflect.Map:
		return rv.Type().Key().Kind() == reflect.String
	}
	return false
}

// isNil reports whether rv, a value [walk] reached, holds no value.
func isNil(rv reflect.Value) bool {
	if !rv.IsValid() {
		return true
	}
	switch rv.Kind() {
	case reflect.Map, reflect.Slice, reflect.Chan, reflect.Func, reflect.UnsafePointer:
		return rv.IsNil()
	}
	return false
}

// GetTime returns the time.Time v holds, read as [Scalar] reads it.
func GetTime(v any) (time.Time, bool) {
	return held[time.Time](v)
}

// GetUUID returns the uuid.UUID v holds, read as [Scalar] reads it.
func GetUUID(v any) (uuid.UUID, bool) {
	return held[uuid.UUID](v)
}

// held returns the T that v holds through its carriers, boxing nothing.
func held[T any](v any) (T, bool) {
	if x, ok := v.(T); ok {
		return x, true
	}
	rv, ok := walk(v)
	if !ok || !rv.IsValid() {
		var zero T
		return zero, false
	}
	return reflect.TypeAssert[T](rv)
}

// form is [Scalar] without the number literal rule.
func form(v any) (any, bool) {
	switch x := v.(type) {
	case nil:
		return nil, true
	case string, bool, int64, uint64, float64, float32, json.Number, time.Time, uuid.UUID:
		return x, true
	}
	if container(reflect.ValueOf(v)) {
		return nil, false
	}
	rv, ok := walk(v)
	if !ok {
		return nil, false
	}
	if isNil(rv) {
		return nil, true
	}
	switch rv.Kind() {
	case reflect.Map, reflect.Slice, reflect.Chan, reflect.Func, reflect.UnsafePointer:
		return nil, false
	}
	switch rv.Type() {
	case numberType:
		return json.Number(rv.String()), true
	case timeType, uuidType:
		return rv.Interface(), true
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
