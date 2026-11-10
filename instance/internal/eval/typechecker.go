package eval

import (
	"github.com/simon-lentz/yammm/internal/value"
	"github.com/simon-lentz/yammm/schema"
)

// TypeChecker is a function that checks if a value matches a type.
// Returns (true, "") if valid, or (false, message) with an error description.
type TypeChecker func(val any) (bool, string)

// checkerOf returns the TypeChecker of kind k. A datatype check (`=~ Kind`)
// applies the rule a property of that kind applies when it declares no
// constraint: no bounds, layout or member set. A value that holds none is of
// no kind.
func checkerOf(k schema.ConstraintKind) TypeChecker {
	return func(val any) (bool, string) {
		if err := value.CheckKind(val, k); err != nil {
			return false, err.Error()
		}
		return true, ""
	}
}

// IsString returns a TypeChecker that validates string values.
func IsString() TypeChecker {
	return checkerOf(schema.KindString)
}

// IsInteger returns a TypeChecker that validates integer values. A float is
// refused, whole or not.
func IsInteger() TypeChecker {
	return checkerOf(schema.KindInteger)
}

// IsFloat returns a TypeChecker that validates finite float and integer values.
func IsFloat() TypeChecker {
	return checkerOf(schema.KindFloat)
}

// IsBoolean returns a TypeChecker that validates boolean values.
func IsBoolean() TypeChecker {
	return checkerOf(schema.KindBoolean)
}

// IsUUID returns a TypeChecker that validates UUID values.
func IsUUID() TypeChecker {
	return checkerOf(schema.KindUUID)
}

// IsTimestamp returns a TypeChecker that validates timestamp values: a
// time.Time whose RFC 3339 text reads back, or a string in RFC 3339. It reads
// RFC 3339 whatever layout a property declares, so "2030-01-02", which a
// Timestamp["2006-01-02"] property holds, does not match.
func IsTimestamp() TypeChecker {
	return checkerOf(schema.KindTimestamp)
}

// IsDate returns a TypeChecker that validates date values.
func IsDate() TypeChecker {
	return checkerOf(schema.KindDate)
}
