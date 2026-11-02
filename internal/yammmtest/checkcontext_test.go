package yammmtest

import (
	"context"
	"errors"
	"testing"
)

func TestCheckContext_AnswersWithItsParentBeforeTheChosenCall(t *testing.T) {
	t.Parallel()

	parent, cancel := context.WithCancel(t.Context())
	cancel()
	ctx := NewCheckContext(parent, 0)
	if err := ctx.Err(); !errors.Is(err, context.Canceled) {
		t.Errorf("Err() over a cancelled parent = %v, want %v", err, context.Canceled)
	}
	if got := ctx.Checks(); got != 1 {
		t.Errorf("Checks() = %d, want 1", got)
	}
}
