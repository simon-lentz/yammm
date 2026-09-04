package instance

import (
	"errors"
	"fmt"
	"runtime/debug"

	"github.com/simon-lentz/yammm/diag"
)

// Error codes for validation failures.
// These are aliases to the canonical codes in the diag package.
var (
	// ErrTypeNotFound indicates the type name was not found in the schema.
	ErrTypeNotFound = diag.E_INSTANCE_TYPE_NOT_FOUND

	// ErrAbstractType indicates an attempt to instantiate an abstract type.
	ErrAbstractType = diag.E_ABSTRACT_TYPE

	// ErrPartTypeDirect indicates a part type was instantiated outside composition.
	ErrPartTypeDirect = diag.E_PART_TYPE_DIRECT

	// ErrMissingRequired indicates a required property was absent.
	ErrMissingRequired = diag.E_MISSING_REQUIRED

	// ErrUnknownField indicates an undeclared property was present.
	ErrUnknownField = diag.E_UNKNOWN_FIELD

	// ErrTypeMismatch indicates a property value failed type validation.
	ErrTypeMismatch = diag.E_TYPE_MISMATCH

	// ErrConstraintFail indicates a property constraint was not satisfied.
	ErrConstraintFail = diag.E_CONSTRAINT_FAIL

	// ErrInvariantFail indicates a type invariant expression failed.
	ErrInvariantFail = diag.E_INVARIANT_FAIL

	// ErrEdgeShapeMismatch indicates an edge object has an invalid shape.
	ErrEdgeShapeMismatch = diag.E_EDGE_SHAPE_MISMATCH

	// ErrMissingFKTarget indicates a _target_* field is missing from an edge.
	ErrMissingFKTarget = diag.E_MISSING_FK_TARGET

	// ErrPartialCompositeFK indicates an incomplete composite foreign key.
	ErrPartialCompositeFK = diag.E_PARTIAL_COMPOSITE_FK

	// ErrUnknownEdgeField indicates an unknown field in an edge object.
	ErrUnknownEdgeField = diag.E_UNKNOWN_EDGE_FIELD

	// ErrUnresolvedRequiredComposition indicates a required composition is absent/empty.
	ErrUnresolvedRequiredComposition = diag.E_UNRESOLVED_REQUIRED_COMPOSITION

	// ErrCompositionNotFound indicates a composition relation was not found on the parent type.
	ErrCompositionNotFound = diag.E_COMPOSITION_NOT_FOUND

	// ErrCompositionDepthExceeded indicates composed nesting deeper than [MaxComposedDepth].
	ErrCompositionDepthExceeded = diag.E_COMPOSITION_DEPTH_EXCEEDED

	// ErrDuplicateComposedPK indicates duplicate primary keys in composed children.
	ErrDuplicateComposedPK = diag.E_DUPLICATE_COMPOSED_PK

	// ErrEvalError indicates an error during expression evaluation.
	ErrEvalError = diag.E_EVAL_ERROR

	// ErrCaseFoldCollision indicates multiple input fields fold to the same schema property.
	// This occurs when non-strict mode is enabled and the input contains multiple
	// field names that differ only in case (e.g., "Name" and "name").
	ErrCaseFoldCollision = diag.E_CASE_FOLD_COLLISION
)

// Internal error sentinels for programmatic detection via errors.Is().
// These represent system-level failures, not validation failures.
var (
	// ErrInternalFailure is the parent sentinel for all internal failures.
	// Use errors.Is(err, ErrInternalFailure) to detect any internal error.
	ErrInternalFailure = errors.New("internal validation failure")
)

// InternalErrorKind classifies internal errors for programmatic handling.
type InternalErrorKind int

const (
	// KindInvariantPanic indicates a panic during invariant evaluation.
	KindInvariantPanic InternalErrorKind = iota
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
