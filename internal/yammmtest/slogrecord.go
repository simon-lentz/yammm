package yammmtest

import (
	"context"
	"log/slog"
	"sync"
)

// RecordHandler is a thread-safe slog.Handler that captures records for
// test inspection. Records are cloned on capture so the handler never
// retains internal buffers that slog may reuse.
//
// Attributes attached at the log call site are captured in the record;
// attributes bound via Logger.With and group scoping are dropped — the
// handler exists to assert that records were emitted, at what level, and
// with what message and call-site attrs.
//
// The zero value is unusable; construct with [NewRecordHandler].
type RecordHandler struct {
	mu      sync.Mutex
	records []slog.Record
	level   slog.Level
}

// NewRecordHandler returns a handler capturing records at or above level.
func NewRecordHandler(level slog.Level) *RecordHandler {
	return &RecordHandler{level: level}
}

// Enabled implements slog.Handler.
func (h *RecordHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level
}

// Handle implements slog.Handler, capturing a clone of r.
func (h *RecordHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r.Clone())
	return nil
}

// WithAttrs implements slog.Handler; bound attributes are dropped (see the
// type documentation).
func (h *RecordHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }

// WithGroup implements slog.Handler; group scoping is dropped (see the
// type documentation).
func (h *RecordHandler) WithGroup(_ string) slog.Handler { return h }

// Records returns a copy of the captured records.
func (h *RecordHandler) Records() []slog.Record {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]slog.Record, len(h.records))
	copy(out, h.records)
	return out
}

// Clear discards all captured records.
func (h *RecordHandler) Clear() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = nil
}
