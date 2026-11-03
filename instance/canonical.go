package instance

import (
	"fmt"

	"github.com/simon-lentz/yammm/internal/value"
	"github.com/simon-lentz/yammm/schema"
)

// CanonicalValue returns val in the single stored representation c defines —
// the value [Validator] stores for the same input: an Integer as int64, a
// Float as float64, a Vector as []float64, a List with each element
// canonicalized through the element constraint, a Timestamp rendered through
// its declared format (RFC 3339 with fractional seconds otherwise), a UUID in
// canonical lowercase form, a Date as "2006-01-02" in the value's own
// location, and a String, Enum, Pattern or Boolean carried by a named Go type
// as the base value. A nil constraint passes val through untouched, and so
// does a constraint [schema.CheckConstraint] accepts a nil val; any other value
// that holds none, as [CheckValue] reads it, renders as nil. A non-nil
// constraint is judged first by [schema.CheckConstraint], and one it refuses
// is an error whatever the value, as it is for [CheckValue].
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
	if c == nil {
		return val, nil
	}
	if err := schema.CheckConstraint(c); err != nil {
		return val, fmt.Errorf("instance: %w", err)
	}
	if val == nil {
		return val, nil
	}
	out, err := coerceValueRecovering(val, c)
	if err != nil {
		return val, err
	}
	return out, nil
}

// IsAbsent reports whether v holds no value: nil, or a pointer, an interface, a
// map, a slice, a channel, a function or an unsafe pointer that is nil, bare or
// behind at most 64 pointers and interfaces, or a nil immutable container.
// [Validator] reads such a value at a property as a missing key: schema/gogen
// emits a typed nil pointer for an absent optional scalar, and a nil slice for
// an absent optional list. adapter/neo4j writes such a value as null.
func IsAbsent(v any) bool {
	return value.Absent(v)
}
