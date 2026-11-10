package instance

import (
	"fmt"

	"github.com/simon-lentz/yammm/internal/value"
	"github.com/simon-lentz/yammm/schema"
)

// CheckValue reports whether val conforms to c, by the rule [Validator]
// applies to every property value: Go kind and form, bounds, enum membership,
// pattern, list length and element rule and a Vector's dimension, with an
// alias resolved to its DataType. A nil
// constraint is valid, and so is a value that holds none ([IsAbsent]) under a
// constraint [schema.CheckConstraint] accepts — presence is the
// caller's rule, as it is the validator's. A non-nil constraint is judged first by
// [schema.CheckConstraint], and one it refuses is an error whatever the value.
//
// It is exported for a caller that admits schema-typed values without a
// [Validator], such as the snapshot reader. A violation returns the checker's
// message; a panic inside the check returns an [InternalError] that matches
// [ErrInternalFailure], as it does for the validator.
func CheckValue(val any, c schema.Constraint) error {
	if c == nil {
		return nil
	}
	if err := schema.CheckConstraint(c); err != nil {
		return fmt.Errorf("instance: %w", err)
	}
	return checkValueRecovering(val, c)
}

// checkValueRecovering runs value.Check and converts a panic into an
// InternalError of KindConstraintPanic, so the validator and CheckValue share
// one recovery rule.
func checkValueRecovering(val any, c schema.Constraint) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = wrapPanicValue(r, KindConstraintPanic)
		}
	}()
	//nolint:wrapcheck // the checker's error is the contract; callers classify CheckError vs InternalError
	return value.Check(val, c)
}

// coerceValueRecovering runs value.Coerce under the same recovery rule as
// [checkValueRecovering].
func coerceValueRecovering(val any, c schema.Constraint) (result any, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = wrapPanicValue(r, KindConstraintPanic)
		}
	}()
	//nolint:wrapcheck // the checker's error is the contract; callers classify CheckError vs InternalError
	return value.Coerce(val, c)
}
