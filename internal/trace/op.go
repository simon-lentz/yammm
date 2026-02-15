package trace

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"
)

// Op represents a running operation with automatic start/end logging.
//
// Op provides consistent operation boundary logging with automatic duration
// measurement and cancellation handling. It enforces the operation naming
// convention and prevents "forgot to log end" bugs.
//
// Create via [Begin]. It is safe to call methods on a nil *Op.
type Op struct {
	// ctx is stored to check for cancellation at End() time and to extract
	// request ID. This is an intentional exception to the "don't store context"
	// guideline because Op represents an operation boundary that needs to:
	// 1. Log context cancellation state when the operation ends
	// 2. Include request ID from context in both start and end logs
	ctx       context.Context //nolint:containedctx // See comment above
	logger    *slog.Logger
	name      string
	startTime time.Time
	ended     atomic.Bool
}

// Begin starts a new operation and logs at Debug level.
//
// Returns *Op (pointer) so nil checks are cheap. When logging is disabled
// (logger is nil or level is below Debug), Begin returns nil to achieve
// near-zero overhead (~1-2ns). It is safe to call methods on a nil *Op.
//
// Operation names should follow the format yammm.<package>.<operation>:
//   - yammm.schema.load
//   - yammm.instance.validate
//   - yammm.graph.add
//
// The start log includes:
//   - "op": operation name
//   - "request_id": if present in context
//   - All additional attrs passed to Begin
func Begin(ctx context.Context, logger *slog.Logger, name string, attrs ...slog.Attr) *Op {
	// Fast path: return nil when logging is disabled to avoid allocation.
	// All *Op methods are safe to call on nil.
	if logger == nil {
		return nil
	}
	if !logger.Enabled(ctx, slog.LevelDebug) {
		return nil
	}

	// Slow path: logging is enabled - allocate and log
	op := &Op{
		ctx:       ctx,
		logger:    logger,
		name:      name,
		startTime: time.Now(),
	}

	// Build start log attributes
	logAttrs := make([]slog.Attr, 0, len(attrs)+2)
	logAttrs = append(logAttrs, slog.String("op", name))
	if reqID, ok := RequestIDFrom(ctx); ok {
		logAttrs = append(logAttrs, slog.String("request_id", reqID))
	}
	logAttrs = append(logAttrs, attrs...)

	logger.LogAttrs(ctx, slog.LevelDebug, "operation started", logAttrs...)

	return op
}

// End logs the operation completion. Safe to call multiple times.
//
// The first call logs at Debug level; subsequent calls are silently ignored
// (no log output). This prevents double-logging if End is called explicitly
// and also via defer.
//
// The end log includes:
//   - "op": operation name
//   - "request_id": if present in context
//   - "elapsed_ms": int64 milliseconds (machine-parseable)
//   - "duration": time.Duration (human-readable)
//   - "ctx_err": context error message if cancelled
//   - "error": error message if err != nil
//   - All additional attrs passed to End
func (o *Op) End(err error, attrs ...slog.Attr) {
	// Safe to call on nil
	if o == nil {
		return
	}

	// Prevent double-logging
	if o.ended.Swap(true) {
		return
	}

	if o.logger == nil {
		return
	}
	if !o.logger.Enabled(o.ctx, slog.LevelDebug) {
		return
	}

	elapsed := time.Since(o.startTime)

	// Build end log attributes
	// Estimate capacity: op, request_id (optional), elapsed_ms, duration,
	// ctx_err (optional), error (optional), plus user attrs
	logAttrs := make([]slog.Attr, 0, len(attrs)+6)
	logAttrs = append(logAttrs, slog.String("op", o.name))
	if reqID, ok := RequestIDFrom(o.ctx); ok {
		logAttrs = append(logAttrs, slog.String("request_id", reqID))
	}
	logAttrs = append(logAttrs,
		slog.Int64("elapsed_ms", elapsed.Milliseconds()),
		slog.Duration("duration", elapsed),
	)

	// Add context error if present
	if ctxErr := o.ctx.Err(); ctxErr != nil {
		logAttrs = append(logAttrs, slog.String("ctx_err", ctxErr.Error()))
	}

	// Add error if present
	if err != nil {
		logAttrs = append(logAttrs, slog.String("error", err.Error()))
	}

	logAttrs = append(logAttrs, attrs...)

	o.logger.LogAttrs(o.ctx, slog.LevelDebug, "operation ended", logAttrs...)
}
