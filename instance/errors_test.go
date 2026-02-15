package instance

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInternalErrorKind_String(t *testing.T) {
	tests := []struct {
		kind InternalErrorKind
		want string
	}{
		{KindNilValidator, "nil validator"},
		{KindCorruptedSchema, "corrupted schema"},
		{KindInvariantPanic, "invariant evaluation panic"},
		{KindConstraintPanic, "constraint checking panic"},
		{InternalErrorKind(999), "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.kind.String())
		})
	}
}

func TestInternalError_Error(t *testing.T) {
	t.Run("with cause", func(t *testing.T) {
		err := &InternalError{
			Kind:  KindInvariantPanic,
			Cause: errors.New("division by zero"),
		}
		assert.Equal(t, "invariant evaluation panic: division by zero", err.Error())
	})

	t.Run("without cause", func(t *testing.T) {
		err := &InternalError{
			Kind: KindNilValidator,
		}
		assert.Equal(t, "nil validator", err.Error())
	})

	t.Run("unknown kind without cause", func(t *testing.T) {
		err := &InternalError{
			Kind: InternalErrorKind(999),
		}
		assert.Equal(t, "unknown", err.Error())
	})
}

func TestInternalError_Unwrap(t *testing.T) {
	cause := errors.New("underlying error")
	err := &InternalError{
		Kind:  KindConstraintPanic,
		Cause: cause,
	}
	assert.Equal(t, cause, err.Unwrap())
}

func TestWrapPanicValue_WithError(t *testing.T) {
	var result *InternalError

	func() {
		defer func() {
			result = wrapPanicValue(recover(), KindInvariantPanic)
		}()
		panic(errors.New("test error"))
	}()

	require.NotNil(t, result)
	assert.Equal(t, KindInvariantPanic, result.Kind)
	assert.Equal(t, "test error", result.Cause.Error())
	assert.NotEmpty(t, result.Stack, "stack trace should be captured")
}

func TestWrapPanicValue_WithString(t *testing.T) {
	var result *InternalError

	func() {
		defer func() {
			result = wrapPanicValue(recover(), KindConstraintPanic)
		}()
		panic("string panic message")
	}()

	require.NotNil(t, result)
	assert.Equal(t, KindConstraintPanic, result.Kind)
	assert.Equal(t, "string panic message", result.Cause.Error())
	assert.NotEmpty(t, result.Stack)
}

func TestWrapPanicValue_WithOtherType(t *testing.T) {
	var result *InternalError

	func() {
		defer func() {
			result = wrapPanicValue(recover(), KindCorruptedSchema)
		}()
		panic(42)
	}()

	require.NotNil(t, result)
	assert.Equal(t, KindCorruptedSchema, result.Kind)
	assert.Equal(t, "panic: 42", result.Cause.Error())
	assert.NotEmpty(t, result.Stack)
}

func TestWrapPanicValue_NoPanic(t *testing.T) {
	var result *InternalError

	func() {
		defer func() {
			result = wrapPanicValue(recover(), KindNilValidator)
		}()
		// No panic
	}()

	assert.Nil(t, result)
}

func TestWrapPanicValue_NilInput(t *testing.T) {
	result := wrapPanicValue(nil, KindInvariantPanic)
	assert.Nil(t, result)
}

func TestErrCorruptedSchema_Is(t *testing.T) {
	t.Run("is ErrInternalFailure", func(t *testing.T) {
		assert.True(t, errors.Is(ErrCorruptedSchema, ErrInternalFailure))
	})

	t.Run("is not ErrNilValidator", func(t *testing.T) {
		assert.False(t, errors.Is(ErrCorruptedSchema, ErrNilValidator))
	})
}

func TestErrNilValidator_Is(t *testing.T) {
	assert.True(t, errors.Is(ErrNilValidator, ErrInternalFailure))
}

func TestInternalError_Is(t *testing.T) {
	t.Run("panic error is ErrInternalFailure", func(t *testing.T) {
		var panicErr *InternalError
		func() {
			defer func() {
				panicErr = wrapPanicValue(recover(), KindInvariantPanic)
			}()
			panic("test panic")
		}()

		require.NotNil(t, panicErr)
		assert.True(t, errors.Is(panicErr, ErrInternalFailure),
			"panic-derived InternalError should match ErrInternalFailure")
	})

	t.Run("constraint panic is ErrInternalFailure", func(t *testing.T) {
		var panicErr *InternalError
		func() {
			defer func() {
				panicErr = wrapPanicValue(recover(), KindConstraintPanic)
			}()
			panic("constraint panic")
		}()

		require.NotNil(t, panicErr)
		assert.True(t, errors.Is(panicErr, ErrInternalFailure),
			"KindConstraintPanic should match ErrInternalFailure")
	})

	t.Run("does not match unrelated errors", func(t *testing.T) {
		err := &InternalError{
			Kind:  KindConstraintPanic,
			Cause: errors.New("some cause"),
		}
		assert.False(t, errors.Is(err, errors.New("unrelated")))
	})

	t.Run("constructed InternalError is ErrInternalFailure", func(t *testing.T) {
		err := &InternalError{
			Kind:  KindCorruptedSchema,
			Cause: errors.New("schema corrupted"),
		}
		assert.True(t, errors.Is(err, ErrInternalFailure),
			"all InternalError should match ErrInternalFailure")
	})
}
