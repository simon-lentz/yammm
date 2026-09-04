package instance

import (
	"github.com/simon-lentz/yammm/instance/internal/eval"
	"github.com/simon-lentz/yammm/schema"
)

// CheckValue reports whether val conforms to c, by the rule [Validator]
// applies to every property value: Go kind, bounds, enum membership, pattern,
// list length and element rule, with an alias resolved to its DataType. A nil
// value and a nil constraint are both valid — presence is the caller's rule,
// as it is the validator's.
//
// This is the check half of the boundary contract [CanonicalValue] renders the
// other half of, exported for a caller that admits schema-typed values at a
// boundary this library does not own. A violation returns the checker's
// message; a panic inside the check returns an [InternalError] that matches
// [ErrInternalFailure], as it does for the validator.
func CheckValue(val any, c schema.Constraint) error {
	if c == nil {
		return nil
	}
	return checkValueRecovering(val, c)
}

// checkValueRecovering runs eval.CheckValue and converts a panic into an
// InternalError of KindConstraintPanic, so the validator and CheckValue share
// one recovery rule.
func checkValueRecovering(val any, c schema.Constraint) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = wrapPanicValue(r, KindConstraintPanic)
		}
	}()
	//nolint:wrapcheck // the checker's error is the contract; callers classify CheckError vs InternalError
	return eval.CheckValue(val, c)
}

// coerceValueRecovering runs eval.CoerceValue under the same recovery rule as
// [checkValueRecovering], so the validator and CanonicalValue share it.
func coerceValueRecovering(val any, c schema.Constraint) (result any, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = wrapPanicValue(r, KindConstraintPanic)
		}
	}()
	//nolint:wrapcheck // the checker's error is the contract; callers classify CheckError vs InternalError
	return eval.CoerceValue(val, c)
}
