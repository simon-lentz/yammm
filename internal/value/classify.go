package value

import (
	"encoding/json"
	"reflect"
)

// Kind identifies the semantic type of a runtime value.
type Kind int

const (
	// UnspecifiedKind indicates an unknown or unsupported type.
	UnspecifiedKind Kind = iota
	// StringKind indicates a string value.
	StringKind
	// IntKind indicates an integer value.
	IntKind
	// FloatKind indicates a floating-point value.
	FloatKind
	// BoolKind indicates a boolean value.
	BoolKind
)

// String returns the string representation of a Kind.
func (k Kind) String() string {
	switch k {
	case UnspecifiedKind:
		return "UnspecifiedKind"
	case StringKind:
		return "StringKind"
	case IntKind:
		return "IntKind"
	case FloatKind:
		return "FloatKind"
	case BoolKind:
		return "BoolKind"
	default:
		return "UnknownKind"
	}
}

// Classify normalizes a runtime scalar into a Kind and possibly transformed
// value. It is used by runtime validation to stay aligned with type checker
// expectations.
//
// For json.Number: attempts Int64() first, then Float64() to determine kind.
// A slice of any element type is UnspecifiedKind: a list's shape is the
// constraint's to judge, elementwise.
//
// Pointers are dereferenced before classification, so *int and int return the
// same Kind; a nil pointer returns UnspecifiedKind. A named type over a basic
// kind classifies as its base kind.
func Classify(val any) (Kind, any) {
	if val == nil {
		return UnspecifiedKind, val
	}

	rv := reflect.ValueOf(val)
	for rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return UnspecifiedKind, nil
		}
		rv = rv.Elem()
		val = rv.Interface()
	}

	switch v := val.(type) {
	case json.Number:
		if n, err := v.Int64(); err == nil {
			return IntKind, n
		}
		if n, err := v.Float64(); err == nil {
			return FloatKind, n
		}
		return UnspecifiedKind, val
	case bool:
		return BoolKind, val
	case string:
		return StringKind, val
	case int, int8, int16, int32, int64:
		return IntKind, val
	case uint, uint8, uint16, uint32, uint64, uintptr:
		return IntKind, val
	case float32, float64:
		return FloatKind, val
	}

	if kind, base, ok := classifyNamedBase(rv); ok {
		return kind, base
	}

	return UnspecifiedKind, val
}

// classifyNamedBase classifies a named type over a predeclared base, which the
// concrete type switch cannot match, and returns the BASE value rather than the
// named one. A caller that classified and then extracted the named value would
// get a Kind it cannot read — the shape adapter/gogen's emitted carriers hit.
func classifyNamedBase(rv reflect.Value) (Kind, any, bool) {
	switch rv.Kind() {
	case reflect.Bool:
		return BoolKind, rv.Bool(), true
	case reflect.String:
		return StringKind, rv.String(), true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return IntKind, rv.Int(), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return IntKind, rv.Uint(), true
	case reflect.Float32, reflect.Float64:
		return FloatKind, rv.Float(), true
	default:
		return UnspecifiedKind, nil, false
	}
}
