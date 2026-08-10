package yammmtest

import (
	"context"
	"log/slog"
	"slices"
	"sync"
)

// RecordHandler is a thread-safe slog.Handler that captures records for
// test inspection. Captured records are rebuilt with fresh attribute
// storage, so the handler never retains internal buffers that slog may
// reuse.
//
// Standard handler semantics are preserved: attributes bound via
// Logger.With precede call-site attributes in each captured record and are
// qualified by the groups open at bind time; call-site attributes are
// qualified by all open groups. Handlers derived via WithAttrs/WithGroup
// record into the same shared store, so one handler observes every logger
// derived from it.
//
// The zero value is unusable; construct with [NewRecordHandler].
type RecordHandler struct {
	store  *recordStore
	level  slog.Level
	attrs  []slog.Attr // bound attrs, already group-qualified
	groups []string    // groups opened by WithGroup, outermost first
}

// recordStore is the capture buffer shared by a handler and every handler
// derived from it.
type recordStore struct {
	mu      sync.Mutex
	records []slog.Record
}

// NewRecordHandler returns a handler capturing records at or above level.
func NewRecordHandler(level slog.Level) *RecordHandler {
	return &RecordHandler{store: &recordStore{}, level: level}
}

// Enabled implements slog.Handler.
func (h *RecordHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level
}

// Handle implements slog.Handler. The captured record carries the bound
// attributes first, then the call-site attributes qualified by the open
// groups.
func (h *RecordHandler) Handle(_ context.Context, r slog.Record) error {
	out := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)
	out.AddAttrs(h.attrs...)
	if r.NumAttrs() > 0 {
		callSite := make([]slog.Attr, 0, r.NumAttrs())
		r.Attrs(func(a slog.Attr) bool {
			callSite = append(callSite, a)
			return true
		})
		out.AddAttrs(qualify(h.groups, callSite)...)
	}

	h.store.mu.Lock()
	defer h.store.mu.Unlock()
	h.store.records = append(h.store.records, out)
	return nil
}

// WithAttrs implements slog.Handler: the derived handler records into the
// same store, with attrs qualified by the currently open groups.
func (h *RecordHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	child := *h
	child.attrs = append(slices.Clip(h.attrs), qualify(h.groups, attrs)...)
	return &child
}

// WithGroup implements slog.Handler: subsequent bound and call-site
// attributes nest under name.
func (h *RecordHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	child := *h
	child.groups = append(slices.Clip(h.groups), name)
	return &child
}

// qualify nests attrs inside the open groups, innermost group last.
func qualify(groups []string, attrs []slog.Attr) []slog.Attr {
	for _, group := range slices.Backward(groups) {
		args := make([]any, len(attrs))
		for j, a := range attrs {
			args[j] = a
		}
		attrs = []slog.Attr{slog.Group(group, args...)}
	}
	return attrs
}

// Records returns a copy of the captured records.
func (h *RecordHandler) Records() []slog.Record {
	h.store.mu.Lock()
	defer h.store.mu.Unlock()
	out := make([]slog.Record, len(h.store.records))
	copy(out, h.store.records)
	return out
}

// HasAttr reports whether any record carries an attribute with the given
// key and rendered value. Group nesting is searched recursively; keys match
// without group qualification.
func HasAttr(records []slog.Record, key, value string) bool {
	for _, r := range records {
		found := false
		r.Attrs(func(a slog.Attr) bool {
			if attrHas(a, key, value) {
				found = true
				return false
			}
			return true
		})
		if found {
			return true
		}
	}
	return false
}

// attrHas reports whether a — or any attribute nested in its groups —
// matches key and value.
func attrHas(a slog.Attr, key, value string) bool {
	if a.Value.Kind() == slog.KindGroup {
		for _, ga := range a.Value.Group() {
			if attrHas(ga, key, value) {
				return true
			}
		}
		return false
	}
	return a.Key == key && a.Value.String() == value
}

// HasMessage reports whether any record carries the given message.
func HasMessage(records []slog.Record, msg string) bool {
	for _, r := range records {
		if r.Message == msg {
			return true
		}
	}
	return false
}

// CountLevel returns how many records are at the given level.
func CountLevel(records []slog.Record, level slog.Level) int {
	count := 0
	for _, r := range records {
		if r.Level == level {
			count++
		}
	}
	return count
}
