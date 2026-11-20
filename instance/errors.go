package instance

import (
	"errors"
	"fmt"
	"runtime/debug"
)

// Internal error sentinels for programmatic detection via errors.Is().
// These represent system-level failures, not validation failures.
var (
	// ErrInternalFailure is the parent sentinel for all internal failures.
	// Use errors.Is(err, ErrInternalFailure) to detect any internal error.
	ErrInternalFailure = errors.New("instance: internal validation failure")
)

// InternalErrorKind classifies internal errors for programmatic handling. The
// zero InternalErrorKind names no kind.
type InternalErrorKind int

const (
	// KindInvariantPanic indicates a panic during invariant evaluation.
	KindInvariantPanic InternalErrorKind = iota + 1
	// KindConstraintPanic indicates a panic during constraint checking.
	KindConstraintPanic
)

// String returns a human-readable name for the error kind.
func (k InternalErrorKind) String() string {
	switch k {
	case KindInvariantPanic:
		return "invariant evaluation panic"
	case KindConstraintPanic:
		return "constraint checking panic"
	default:
		return "unknown"
	}
}

// InternalError wraps internal failures with context for debugging.
// Use errors.AsType[*InternalError](err) to extract debugging context.
type InternalError struct {
	Kind  InternalErrorKind
	Cause error
	Stack string // Stack trace from panic recovery, empty otherwise
}

func (e *InternalError) Error() string {
	kindStr := e.Kind.String()
	if e.Cause != nil {
		return kindStr + ": " + e.Cause.Error()
	}
	return kindStr
}

func (e *InternalError) Unwrap() error {
	return e.Cause
}

// Is reports whether the error matches target.
// InternalError always matches ErrInternalFailure, enabling
// errors.Is(err, ErrInternalFailure) to work for all internal errors
// including panic-derived ones.
func (e *InternalError) Is(target error) bool {
	return target == ErrInternalFailure
}

// wrapPanicValue wraps a recovered panic value into an InternalError with a
// stack trace. r is the non-nil value recover returned: every caller tests
// recover's result before calling, so a nil r is a programmer error.
func wrapPanicValue(r any, kind InternalErrorKind) *InternalError {
	var cause error
	switch v := r.(type) {
	case error:
		cause = v
	case string:
		cause = errors.New(v)
	default:
		cause = fmt.Errorf("panic: %v", v)
	}
	return &InternalError{
		Kind:  kind,
		Cause: cause,
		Stack: string(debug.Stack()),
	}
}
