package instance

import (
	"github.com/simon-lentz/yammm/schema"
)

// CanonicalValue returns val in the single stored representation c defines —
// the value [Validator] stores for the same input: an Integer as int64, a
// Float as float64, a Vector as []float64, a List with each element
// canonicalized through the element constraint, a Timestamp rendered through
// its declared format (RFC 3339 with fractional seconds otherwise), a UUID in
// canonical lowercase form, a Date as "2006-01-02" in the value's own
// location, and a String, Enum, Pattern or Boolean carried by a named Go type
// as the base value. A nil value and a nil constraint pass through untouched.
//
// This is the render half of the boundary contract [CheckValue] checks the
// other half of, exported for a caller that renders schema-typed values at a
// boundary this library does not own — a hand-built export, a direct-Cypher
// parameter map. Values reaching a graph through [Validator] are already
// canonical.
//
// On error the returned value is val unchanged, so a caller that heals what it
// can and passes through what it cannot may ignore the error. A panic inside
// the coercion returns an [InternalError] that matches [ErrInternalFailure],
// as it does for the validator.
func CanonicalValue(val any, c schema.Constraint) (any, error) {
	if val == nil || c == nil {
		return val, nil
	}
	out, err := coerceValueRecovering(val, c)
	if err != nil {
		return val, err
	}
	return out, nil
}
