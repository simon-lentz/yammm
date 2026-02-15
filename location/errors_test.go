package location

import (
	"errors"
	"testing"
)

// C2: Test errors.Is() works for each sentinel error.
// These tests verify that sentinel errors are usable for programmatic
// error handling via errors.Is().

func TestErrEmptySourceID_ErrorsIs(t *testing.T) {
	err := ErrEmptySourceID

	if !errors.Is(err, ErrEmptySourceID) {
		t.Error("errors.Is(ErrEmptySourceID, ErrEmptySourceID) = false; want true")
	}

	// Verify it doesn't match other sentinels
	if errors.Is(err, ErrAbsolutePathSourceID) {
		t.Error("ErrEmptySourceID should not match ErrAbsolutePathSourceID")
	}
}

func TestErrAbsolutePathSourceID_ErrorsIs(t *testing.T) {
	err := ErrAbsolutePathSourceID

	if !errors.Is(err, ErrAbsolutePathSourceID) {
		t.Error("errors.Is(ErrAbsolutePathSourceID, ErrAbsolutePathSourceID) = false; want true")
	}

	if errors.Is(err, ErrEmptySourceID) {
		t.Error("ErrAbsolutePathSourceID should not match ErrEmptySourceID")
	}
}

func TestErrUNCPath_ErrorsIs(t *testing.T) {
	err := ErrUNCPath

	if !errors.Is(err, ErrUNCPath) {
		t.Error("errors.Is(ErrUNCPath, ErrUNCPath) = false; want true")
	}

	if errors.Is(err, ErrNotAbsolute) {
		t.Error("ErrUNCPath should not match ErrNotAbsolute")
	}
}

func TestErrNotAbsolute_ErrorsIs(t *testing.T) {
	err := ErrNotAbsolute

	if !errors.Is(err, ErrNotAbsolute) {
		t.Error("errors.Is(ErrNotAbsolute, ErrNotAbsolute) = false; want true")
	}

	if errors.Is(err, ErrUNCPath) {
		t.Error("ErrNotAbsolute should not match ErrUNCPath")
	}
}

func TestErrAbsoluteJoinElement_ErrorsIs(t *testing.T) {
	err := ErrAbsoluteJoinElement

	if !errors.Is(err, ErrAbsoluteJoinElement) {
		t.Error("errors.Is(ErrAbsoluteJoinElement, ErrAbsoluteJoinElement) = false; want true")
	}

	if errors.Is(err, ErrNotAbsolute) {
		t.Error("ErrAbsoluteJoinElement should not match ErrNotAbsolute")
	}
}

// Test that wrapped errors still match via errors.Is
func TestSentinelErrors_WrappedMatchViaErrorsIs(t *testing.T) {
	tests := []struct {
		name     string
		sentinel error
	}{
		{"ErrEmptySourceID", ErrEmptySourceID},
		{"ErrAbsolutePathSourceID", ErrAbsolutePathSourceID},
		{"ErrUNCPath", ErrUNCPath},
		{"ErrNotAbsolute", ErrNotAbsolute},
		{"ErrAbsoluteJoinElement", ErrAbsoluteJoinElement},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate how errors are typically wrapped with context
			wrapped := wrapError(tt.sentinel, "additional context")

			if !errors.Is(wrapped, tt.sentinel) {
				t.Errorf("errors.Is(wrapped, %s) = false; want true", tt.name)
			}
		})
	}
}

// wrapError simulates error wrapping that occurs in production code.
// This tests that errors.Is still works through the wrapping.
type wrappedError struct {
	context string
	err     error
}

func (w *wrappedError) Error() string {
	return w.context + ": " + w.err.Error()
}

func (w *wrappedError) Unwrap() error {
	return w.err
}

func wrapError(err error, context string) error {
	return &wrappedError{context: context, err: err}
}
