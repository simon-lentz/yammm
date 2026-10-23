package value

import (
	"encoding/json"
	"reflect"
	"time"

	"github.com/google/uuid"
)

// Scalar returns v in the one form every writer writes, read through pointers
// and interfaces: a string, bool, int64, uint64, float32 or float64 for a value
// of that kind, a named type as its base value; a json.Number, time.Time or
// uuid.UUID as itself; nil for any nil. It reports false for every other value:
// a struct, an array, a map, a slice, a channel, a function, a complex number,
// and more than [maxPointerDepth] pointers. A list or a map reaches a writer
// wrapped, as an immutable.Slice or Map, which the writer walks; one behind a
// pointer is no list, as [ListElems] reads one. A writer writes a time.Time
// or a uuid.UUID only where its constraint renders it.
func Scalar(v any) (any, bool) {
	switch x := v.(type) {
	case nil:
		return nil, true
	case string, bool, int64, uint64, float64, float32, json.Number, time.Time, uuid.UUID:
		return x, true
	}
	rv := reflect.ValueOf(v)
	for depth := 0; rv.Kind() == reflect.Pointer || rv.Kind() == reflect.Interface; depth++ {
		if rv.IsNil() {
			return nil, true
		}
		if depth == maxPointerDepth {
			return nil, false
		}
		rv = rv.Elem()
	}
	switch rv.Kind() {
	case reflect.Map, reflect.Slice, reflect.Chan, reflect.Func, reflect.UnsafePointer:
		if rv.IsNil() {
			return nil, true
		}
		return nil, false
	}
	if rv.CanInterface() {
		switch x := rv.Interface().(type) {
		case json.Number, time.Time, uuid.UUID:
			return x, true
		}
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
