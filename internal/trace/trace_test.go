package trace

import (
	"context"
	"log/slog"
	"sync"
	"testing"
)

// recordHandler is a test handler that records log records for inspection.
type recordHandler struct {
	mu      sync.Mutex
	records []slog.Record
	level   slog.Level
}

func newRecordHandler(level slog.Level) *recordHandler {
	return &recordHandler{level: level}
}

func (h *recordHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level
}

func (h *recordHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	// Clone the record to avoid retaining internal buffers that slog may reuse.
	// This is a standard test handler pattern to avoid flaky tests.
	h.records = append(h.records, r.Clone())
	return nil
}

func (h *recordHandler) WithAttrs(_ []slog.Attr) slog.Handler {
	return h
}

func (h *recordHandler) WithGroup(_ string) slog.Handler {
	return h
}

func (h *recordHandler) Records() []slog.Record {
	h.mu.Lock()
	defer h.mu.Unlock()
	result := make([]slog.Record, len(h.records))
	copy(result, h.records)
	return result
}

func (h *recordHandler) Clear() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = nil
}

func TestEnabled_NilLogger(t *testing.T) {
	if Enabled(context.Background(), nil, slog.LevelDebug) {
		t.Error("Enabled should return false for nil logger")
	}
}

func TestEnabled_EnabledLevel(t *testing.T) {
	h := newRecordHandler(slog.LevelDebug)
	logger := slog.New(h)
	ctx := t.Context()

	if !Enabled(ctx, logger, slog.LevelDebug) {
		t.Error("Enabled should return true for enabled level")
	}
	if !Enabled(ctx, logger, slog.LevelInfo) {
		t.Error("Enabled should return true for Info when Debug is enabled")
	}
}

func TestEnabled_DisabledLevel(t *testing.T) {
	h := newRecordHandler(slog.LevelWarn) // only Warn and above enabled
	logger := slog.New(h)
	ctx := t.Context()

	if Enabled(ctx, logger, slog.LevelDebug) {
		t.Error("Enabled should return false for Debug when Warn is minimum")
	}
	if Enabled(ctx, logger, slog.LevelInfo) {
		t.Error("Enabled should return false for Info when Warn is minimum")
	}
	if !Enabled(ctx, logger, slog.LevelWarn) {
		t.Error("Enabled should return true for Warn when Warn is minimum")
	}
}

func TestDebug_NilLogger(t *testing.T) {
	// Should not panic
	Debug(context.Background(), nil, "test message", slog.String("key", "value"))
}

func TestDebug_EnabledLogger(t *testing.T) {
	h := newRecordHandler(slog.LevelDebug)
	logger := slog.New(h)
	ctx := t.Context()

	Debug(ctx, logger, "test message", slog.String("key", "value"))

	records := h.Records()
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}

	r := records[0]
	if r.Message != "test message" {
		t.Errorf("got message %q, want %q", r.Message, "test message")
	}
	if r.Level != slog.LevelDebug {
		t.Errorf("got level %v, want %v", r.Level, slog.LevelDebug)
	}

	// Check attributes
	var found bool
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == "key" && a.Value.String() == "value" {
			found = true
			return false
		}
		return true
	})
	if !found {
		t.Error("expected attribute key=value")
	}
}

func TestDebug_DisabledLevel(t *testing.T) {
	h := newRecordHandler(slog.LevelInfo) // Debug not enabled
	logger := slog.New(h)

	Debug(context.Background(), logger, "test message")

	records := h.Records()
	if len(records) != 0 {
		t.Fatalf("expected 0 records when level disabled, got %d", len(records))
	}
}

func TestDebugLazy_NilLogger(t *testing.T) {
	called := false
	DebugLazy(context.Background(), nil, "test", func() []slog.Attr {
		called = true
		return nil
	})

	if called {
		t.Error("fn should not be called when logger is nil")
	}
}

func TestDebugLazy_DisabledLevel(t *testing.T) {
	h := newRecordHandler(slog.LevelInfo) // Debug not enabled
	logger := slog.New(h)

	called := false
	DebugLazy(context.Background(), logger, "test", func() []slog.Attr {
		called = true
		return nil
	})

	if called {
		t.Error("fn should not be called when level is disabled")
	}
}

func TestDebugLazy_EnabledLevel(t *testing.T) {
	h := newRecordHandler(slog.LevelDebug)
	logger := slog.New(h)
	ctx := t.Context()

	called := false
	DebugLazy(ctx, logger, "test message", func() []slog.Attr {
		called = true
		return []slog.Attr{slog.String("computed", "attr")}
	})

	if !called {
		t.Error("fn should be called when level is enabled")
	}

	records := h.Records()
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}

	// Check computed attribute was included
	var found bool
	records[0].Attrs(func(a slog.Attr) bool {
		if a.Key == "computed" && a.Value.String() == "attr" {
			found = true
			return false
		}
		return true
	})
	if !found {
		t.Error("expected computed attribute")
	}
}

func TestInfo_EnabledLogger(t *testing.T) {
	h := newRecordHandler(slog.LevelInfo)
	logger := slog.New(h)
	ctx := t.Context()

	Info(ctx, logger, "info message", slog.Int("count", 42))

	records := h.Records()
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}

	r := records[0]
	if r.Level != slog.LevelInfo {
		t.Errorf("got level %v, want %v", r.Level, slog.LevelInfo)
	}
}

func TestInfoLazy_DisabledLevel(t *testing.T) {
	h := newRecordHandler(slog.LevelWarn) // Info not enabled
	logger := slog.New(h)

	called := false
	InfoLazy(context.Background(), logger, "test", func() []slog.Attr {
		called = true
		return nil
	})

	if called {
		t.Error("fn should not be called when level is disabled")
	}
}

// D3: Missing symmetric test for InfoLazy enabled level
func TestInfoLazy_EnabledLevel(t *testing.T) {
	h := newRecordHandler(slog.LevelInfo)
	logger := slog.New(h)
	ctx := t.Context()

	called := false
	InfoLazy(ctx, logger, "info message", func() []slog.Attr {
		called = true
		return []slog.Attr{slog.String("computed", "attr")}
	})

	if !called {
		t.Error("fn should be called when level is enabled")
	}

	records := h.Records()
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}

	// Check computed attribute was included
	var found bool
	records[0].Attrs(func(a slog.Attr) bool {
		if a.Key == "computed" && a.Value.String() == "attr" {
			found = true
			return false
		}
		return true
	})
	if !found {
		t.Error("expected computed attribute")
	}
}

func TestWarn_EnabledLogger(t *testing.T) {
	h := newRecordHandler(slog.LevelWarn)
	logger := slog.New(h)
	ctx := t.Context()

	Warn(ctx, logger, "warn message")

	records := h.Records()
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}

	r := records[0]
	if r.Level != slog.LevelWarn {
		t.Errorf("got level %v, want %v", r.Level, slog.LevelWarn)
	}
}

func TestWarnLazy_DisabledLevel(t *testing.T) {
	h := newRecordHandler(slog.LevelError) // Warn not enabled
	logger := slog.New(h)

	called := false
	WarnLazy(context.Background(), logger, "test", func() []slog.Attr {
		called = true
		return nil
	})

	if called {
		t.Error("fn should not be called when level is disabled")
	}
}

// D3: Missing symmetric test for WarnLazy enabled level
func TestWarnLazy_EnabledLevel(t *testing.T) {
	h := newRecordHandler(slog.LevelWarn)
	logger := slog.New(h)
	ctx := t.Context()

	called := false
	WarnLazy(ctx, logger, "warn message", func() []slog.Attr {
		called = true
		return []slog.Attr{slog.String("computed", "attr")}
	})

	if !called {
		t.Error("fn should be called when level is enabled")
	}

	records := h.Records()
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}

	// Check computed attribute was included
	var found bool
	records[0].Attrs(func(a slog.Attr) bool {
		if a.Key == "computed" && a.Value.String() == "attr" {
			found = true
			return false
		}
		return true
	})
	if !found {
		t.Error("expected computed attribute")
	}
}

func TestError_EnabledLogger(t *testing.T) {
	h := newRecordHandler(slog.LevelError)
	logger := slog.New(h)
	ctx := t.Context()

	Error(ctx, logger, "error message")

	records := h.Records()
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}

	r := records[0]
	if r.Level != slog.LevelError {
		t.Errorf("got level %v, want %v", r.Level, slog.LevelError)
	}
}

func TestErrorLazy_EnabledLevel(t *testing.T) {
	h := newRecordHandler(slog.LevelError)
	logger := slog.New(h)
	ctx := t.Context()

	called := false
	ErrorLazy(ctx, logger, "error message", func() []slog.Attr {
		called = true
		return []slog.Attr{slog.String("detail", "info")}
	})

	if !called {
		t.Error("fn should be called when level is enabled")
	}

	records := h.Records()
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
}

func TestAllFunctions_NilLoggerNoPanic(t *testing.T) {
	ctx := t.Context()
	// Verify none of these panic with nil logger
	Debug(ctx, nil, "msg")
	DebugLazy(ctx, nil, "msg", func() []slog.Attr { return nil })
	Info(ctx, nil, "msg")
	InfoLazy(ctx, nil, "msg", func() []slog.Attr { return nil })
	Warn(ctx, nil, "msg")
	WarnLazy(ctx, nil, "msg", func() []slog.Attr { return nil })
	Error(ctx, nil, "msg")
	ErrorLazy(ctx, nil, "msg", func() []slog.Attr { return nil })
}
