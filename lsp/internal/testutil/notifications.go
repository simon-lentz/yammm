package testutil

import (
	"log/slog"
	"sync"

	"github.com/simon-lentz/yammm/lsp/internal/protocol"
)

// NotificationCollector captures LSP notifications for test inspection.
// Safe for concurrent use; the zero value is ready.
type NotificationCollector struct {
	mu      sync.Mutex
	entries []NotificationEntry
}

// NotificationEntry is one captured notification.
type NotificationEntry struct {
	Method string
	Params any
}

// Notify records a notification; pass it as the server's notify callback.
func (c *NotificationCollector) Notify(method string, params any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = append(c.entries, NotificationEntry{Method: method, Params: params})
}

// DiagnosticsFor returns the most recently published diagnostics for uri,
// or nil if none were published.
func (c *NotificationCollector) DiagnosticsFor(uri string) []protocol.Diagnostic {
	c.mu.Lock()
	defer c.mu.Unlock()

	for i := len(c.entries) - 1; i >= 0; i-- {
		e := c.entries[i]
		if e.Method != protocol.ServerTextDocumentPublishDiagnostics {
			continue
		}
		p, ok := e.Params.(protocol.PublishDiagnosticsParams)
		if ok && p.URI == uri {
			return p.Diagnostics
		}
	}
	return nil
}

// Entries returns a copy of all captured notifications.
func (c *NotificationCollector) Entries() []NotificationEntry {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]NotificationEntry, len(c.entries))
	copy(out, c.entries)
	return out
}

// DiscardLogger returns a logger that drops everything — the default for
// tests that exercise behavior, not logging.
func DiscardLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}
