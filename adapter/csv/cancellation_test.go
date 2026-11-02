package csv

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/internal/yammmtest"
)

// A context cancelled among the rows is refused at the per-instance guard, and
// the export returns no output.
func TestMarshalSnapshot_CancellationDuringRowsIsRefused(t *testing.T) {
	t.Parallel()
	s := loadTestSchema(t, "basic.yammm")
	snap := buildSnapshot(t, s, map[string][]map[string]any{
		"Entity": {{"id": "e1", "name": "Alice"}},
	})

	out, err := New().MarshalSnapshot(yammmtest.NewCheckContext(context.Background(), 2), snap)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error does not wrap context.Canceled: %v", err)
	}
	if !strings.Contains(err.Error(), "csv write:") {
		t.Errorf("a cancellation among the rows must report at the row guard, got %v", err)
	}
	if out != nil {
		t.Errorf("a cancelled export returned output: %q", out)
	}
}

// A context already cancelled is refused at the per-type guard.
func TestMarshalSnapshot_CancelledBeforeAnyType(t *testing.T) {
	t.Parallel()
	s := loadTestSchema(t, "basic.yammm")
	snap := buildSnapshot(t, s, map[string][]map[string]any{
		"Entity": {{"id": "e1", "name": "Alice"}},
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := New().MarshalSnapshot(ctx, snap)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error does not wrap context.Canceled: %v", err)
	}
	if !strings.Contains(err.Error(), "csv marshal snapshot:") {
		t.Errorf("want the per-type guard, got %v", err)
	}
}
