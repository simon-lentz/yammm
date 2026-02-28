package trace

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"
)

func TestBegin_NilLogger(t *testing.T) {
	ctx := t.Context()

	op := Begin(ctx, nil, "test.op")

	// Begin returns nil when logging is disabled (for near-zero overhead)
	if op != nil {
		t.Fatal("Begin should return nil when logger is nil")
	}

	// End should not panic on nil *Op
	op.End(nil)
}

func TestEnd_NilOp(t *testing.T) {
	// Should not panic
	var op *Op
	op.End(nil)
}

func TestBeginEnd_EnabledLogger(t *testing.T) {
	h := newRecordHandler(slog.LevelDebug)
	logger := slog.New(h)
	ctx := t.Context()

	op := Begin(ctx, logger, "yammm.test.op", slog.String("source", "/test/path"))
	// Set startTime to 25ms ago for deterministic duration testing (avoids time.Sleep).
	// We use 25ms with a >= 20ms assertion to provide 5ms slack for CI timing variance.
	op.startTime = time.Now().Add(-25 * time.Millisecond)
	op.End(nil, slog.Int("result_count", 5))

	records := h.Records()
	if len(records) != 2 {
		t.Fatalf("expected 2 records (start + end), got %d", len(records))
	}

	// Check start record
	start := records[0]
	if start.Message != "operation started" {
		t.Errorf("start message: got %q, want %q", start.Message, "operation started")
	}
	assertAttr(t, start, "op", "yammm.test.op")
	assertAttr(t, start, "source", "/test/path")

	// Check end record
	end := records[1]
	if end.Message != "operation ended" {
		t.Errorf("end message: got %q, want %q", end.Message, "operation ended")
	}
	assertAttr(t, end, "op", "yammm.test.op")
	assertAttr(t, end, "result_count", int64(5))

	// Check duration attributes exist
	assertHasAttr(t, end, "elapsed_ms")
	assertHasAttr(t, end, "duration")

	// Check elapsed_ms reflects the 25ms offset we set (with 5ms slack for CI variance)
	var elapsedMS int64
	end.Attrs(func(a slog.Attr) bool {
		if a.Key == "elapsed_ms" {
			elapsedMS = a.Value.Int64()
			return false
		}
		return true
	})
	if elapsedMS < 20 {
		t.Errorf("elapsed_ms should be >= 20 (we set startTime 25ms ago with 5ms slack), got %d", elapsedMS)
	}
}

func TestBeginEnd_WithRequestID(t *testing.T) {
	h := newRecordHandler(slog.LevelDebug)
	logger := slog.New(h)
	ctx := WithRequestID(context.Background(), "req-456")

	op := Begin(ctx, logger, "test.op")
	op.End(nil)

	records := h.Records()
	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}

	// Both start and end should have request_id
	assertAttr(t, records[0], "request_id", "req-456")
	assertAttr(t, records[1], "request_id", "req-456")
}

func TestBeginEnd_NoRequestID(t *testing.T) {
	h := newRecordHandler(slog.LevelDebug)
	logger := slog.New(h)
	ctx := t.Context()

	op := Begin(ctx, logger, "test.op")
	op.End(nil)

	records := h.Records()
	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}

	// Neither should have request_id
	assertNoAttr(t, records[0], "request_id")
	assertNoAttr(t, records[1], "request_id")
}

func TestEnd_WithError(t *testing.T) {
	h := newRecordHandler(slog.LevelDebug)
	logger := slog.New(h)
	ctx := t.Context()

	op := Begin(ctx, logger, "test.op")
	op.End(errors.New("something failed"))

	records := h.Records()
	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}

	assertAttr(t, records[1], "error", "something failed")
}

func TestEnd_NoError(t *testing.T) {
	h := newRecordHandler(slog.LevelDebug)
	logger := slog.New(h)
	ctx := t.Context()

	op := Begin(ctx, logger, "test.op")
	op.End(nil)

	records := h.Records()
	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}

	// Should not have error attribute when err is nil
	assertNoAttr(t, records[1], "error")
}

func TestEnd_ContextCancelled(t *testing.T) {
	h := newRecordHandler(slog.LevelDebug)
	logger := slog.New(h)
	ctx, cancel := context.WithCancel(context.Background())

	op := Begin(ctx, logger, "test.op")
	cancel() // Cancel the context
	op.End(nil)

	records := h.Records()
	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}

	assertAttr(t, records[1], "ctx_err", "context canceled")
}

func TestEnd_DoubleCalling(t *testing.T) {
	h := newRecordHandler(slog.LevelDebug)
	logger := slog.New(h)
	ctx := t.Context()

	op := Begin(ctx, logger, "test.op")
	op.End(nil)
	op.End(nil) // Second call should be ignored
	op.End(nil) // Third call should be ignored

	records := h.Records()
	if len(records) != 2 {
		t.Fatalf("expected 2 records (double calls ignored), got %d", len(records))
	}
}

func TestBeginEnd_DisabledLevel(t *testing.T) {
	h := newRecordHandler(slog.LevelInfo) // Debug not enabled
	logger := slog.New(h)
	ctx := t.Context()

	op := Begin(ctx, logger, "test.op")

	// Begin returns nil when level is disabled (for near-zero overhead)
	if op != nil {
		t.Fatal("Begin should return nil when level is disabled")
	}

	// End should not panic on nil *Op
	op.End(nil)

	records := h.Records()
	if len(records) != 0 {
		t.Fatalf("expected 0 records when level disabled, got %d", len(records))
	}
}

func TestEnd_ContextDeadlineExceeded(t *testing.T) {
	h := newRecordHandler(slog.LevelDebug)
	logger := slog.New(h)
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-1*time.Second))
	defer cancel()

	op := Begin(ctx, logger, "test.op")
	op.End(nil)

	records := h.Records()
	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}

	assertAttr(t, records[1], "ctx_err", "context deadline exceeded")
}

func TestEnd_BothErrorAndContextError(t *testing.T) {
	h := newRecordHandler(slog.LevelDebug)
	logger := slog.New(h)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	op := Begin(ctx, logger, "test.op")
	op.End(errors.New("operation error"))

	records := h.Records()
	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}

	// Both should be present
	assertAttr(t, records[1], "ctx_err", "context canceled")
	assertAttr(t, records[1], "error", "operation error")
}

// Helper functions

func assertAttr(t *testing.T, r slog.Record, key string, wantValue any) {
	t.Helper()
	var found bool
	var gotValue any
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == key {
			found = true
			switch v := wantValue.(type) {
			case string:
				gotValue = a.Value.String()
			case int64:
				gotValue = a.Value.Int64()
			case int:
				gotValue = a.Value.Int64()
			default:
				t.Fatalf("unsupported type for assertion: %T", v)
			}
			return false
		}
		return true
	})
	if !found {
		t.Errorf("expected attribute %q to be present", key)
		return
	}
	// Compare based on type
	switch w := wantValue.(type) {
	case int:
		if gotValue != int64(w) {
			t.Errorf("attribute %q: got %v, want %v", key, gotValue, wantValue)
		}
	default:
		if gotValue != wantValue {
			t.Errorf("attribute %q: got %v, want %v", key, gotValue, wantValue)
		}
	}
}

func assertHasAttr(t *testing.T, r slog.Record, key string) {
	t.Helper()
	var found bool
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == key {
			found = true
			return false
		}
		return true
	})
	if !found {
		t.Errorf("expected attribute %q to be present", key)
	}
}

func assertNoAttr(t *testing.T, r slog.Record, key string) {
	t.Helper()
	var found bool
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == key {
			found = true
			return false
		}
		return true
	})
	if found {
		t.Errorf("expected attribute %q to NOT be present", key)
	}
}
