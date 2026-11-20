package instance

import (
	"github.com/simon-lentz/yammm/internal/value"
	"github.com/simon-lentz/yammm/schema"
)

// checkValueRecovering runs value.Check and converts a panic into an
// InternalError of KindConstraintPanic.
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
