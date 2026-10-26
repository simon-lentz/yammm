package value

import (
	"encoding/json"

	"github.com/simon-lentz/yammm/immutable"
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
// A predeclared type is answered by its type, and every other value by the
// form [Scalar] reads it in: through pointers and interfaces, a named type as
// its base value. A json.Number is classified by its spelling: an integer
// literal is IntKind, any other JSON number literal a finite float64 holds is
// FloatKind, whole-valued or not, and the rest is UnspecifiedKind, text that is
// no JSON number literal included. An integer literal outside the int64 range
// is IntKind and stays the json.Number: it is an integer no int64 holds, and
// its nearest float64 can be a different integer. A slice of any element type
// is UnspecifiedKind: a list's shape is the constraint's to judge, elementwise.
// So is a value of no form, a time.Time and a uuid.UUID; a nil, a nil pointer
// among them, is UnspecifiedKind with a nil value.
func Classify(val any) (Kind, any) {
	switch v := val.(type) {
	case nil:
		return UnspecifiedKind, nil
	case bool:
		return BoolKind, v
	case string:
		return StringKind, v
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, uintptr:
		return IntKind, v
	case float32, float64:
		return FloatKind, v
	}
	held, ok := Scalar(val)
	if !ok {
		return UnspecifiedKind, val
	}
	switch v := held.(type) {
	case nil:
		return UnspecifiedKind, nil
	case json.Number:
		if immutable.IsIntegerLiteral(v) {
			if n, err := v.Int64(); err == nil {
				return IntKind, n
			}
			return IntKind, v
		}
		if n, err := v.Float64(); err == nil {
			return FloatKind, n
		}
		return UnspecifiedKind, v
	case bool:
		return BoolKind, held
	case string:
		return StringKind, held
	case int64, uint64:
		return IntKind, held
	case float32, float64:
		return FloatKind, held
	}
	return UnspecifiedKind, val
}
