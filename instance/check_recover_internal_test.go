package instance

import (
	"errors"
	"testing"

	"github.com/simon-lentz/yammm/schema"
)

// TestCheckValueRecovering_RecoversAPanicIntoAnInternalError pins the recovery
// the validator and CheckValue share. CheckValue judges a constraint first, so
// no constraint it passes on makes the checker panic; the lever is a List with
// no element handed to the shared recovery directly, whose element check
// dereferences it.
func TestCheckValueRecovering_RecoversAPanicIntoAnInternalError(t *testing.T) {
	t.Parallel()
	for name, run := range map[string]func() error{
		"check":  func() error { return checkValueRecovering([]any{"x"}, schema.ListConstraint{}) },
		"coerce": func() error { _, err := coerceValueRecovering([]any{"x"}, schema.ListConstraint{}); return err },
	} {
		err := run()
		if !errors.Is(err, ErrInternalFailure) {
			t.Errorf("%s: error %v does not match ErrInternalFailure", name, err)
		}
		internal, ok := errors.AsType[*InternalError](err)
		if !ok {
			t.Errorf("%s: error is %T, want *InternalError", name, err)
			continue
		}
		if internal.Kind != KindConstraintPanic {
			t.Errorf("%s: kind = %v, want KindConstraintPanic", name, internal.Kind)
		}
	}
}
