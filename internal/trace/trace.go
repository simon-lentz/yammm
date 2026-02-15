package trace

import (
	"context"
	"log/slog"
)

// Enabled reports whether logging at the given level is enabled.
// Returns false if logger is nil.
//
// Use this for complex control flow or when mixing log calls at different
// levels. For simple cases, prefer the convenience wrappers ([Debug], [Info], etc.)
// or lazy variants ([DebugLazy], etc.).
func Enabled(ctx context.Context, logger *slog.Logger, level slog.Level) bool {
	if logger == nil {
		return false
	}
	return logger.Enabled(ctx, level)
}

// Debug logs a message at Debug level if the logger is non-nil and enabled.
//
// Use for simple, pre-computed attributes only. The variadic attrs are
// evaluated at the call site even when logging is disabled. For computed
// attributes (function calls, fmt.Sprintf, slice ops), use [DebugLazy].
func Debug(ctx context.Context, logger *slog.Logger, msg string, attrs ...slog.Attr) {
	if logger == nil {
		return
	}
	if !logger.Enabled(ctx, slog.LevelDebug) {
		return
	}
	logger.LogAttrs(ctx, slog.LevelDebug, msg, attrs...)
}

// DebugLazy logs at Debug level with lazily-computed attributes.
//
// The fn is not called if logging is disabled, guaranteeing no allocation
// from attribute construction. Use this for any computed attributes:
// function calls, fmt.Sprintf, slice operations, struct construction.
func DebugLazy(ctx context.Context, logger *slog.Logger, msg string, fn func() []slog.Attr) {
	if logger == nil {
		return
	}
	if !logger.Enabled(ctx, slog.LevelDebug) {
		return
	}
	logger.LogAttrs(ctx, slog.LevelDebug, msg, fn()...)
}

// Info logs a message at Info level if the logger is non-nil and enabled.
//
// Use for simple, pre-computed attributes only. For computed attributes,
// use [InfoLazy].
func Info(ctx context.Context, logger *slog.Logger, msg string, attrs ...slog.Attr) {
	if logger == nil {
		return
	}
	if !logger.Enabled(ctx, slog.LevelInfo) {
		return
	}
	logger.LogAttrs(ctx, slog.LevelInfo, msg, attrs...)
}

// InfoLazy logs at Info level with lazily-computed attributes.
//
// The fn is not called if logging is disabled.
func InfoLazy(ctx context.Context, logger *slog.Logger, msg string, fn func() []slog.Attr) {
	if logger == nil {
		return
	}
	if !logger.Enabled(ctx, slog.LevelInfo) {
		return
	}
	logger.LogAttrs(ctx, slog.LevelInfo, msg, fn()...)
}

// Warn logs a message at Warn level if the logger is non-nil and enabled.
//
// Use for simple, pre-computed attributes only. For computed attributes,
// use [WarnLazy].
func Warn(ctx context.Context, logger *slog.Logger, msg string, attrs ...slog.Attr) {
	if logger == nil {
		return
	}
	if !logger.Enabled(ctx, slog.LevelWarn) {
		return
	}
	logger.LogAttrs(ctx, slog.LevelWarn, msg, attrs...)
}

// WarnLazy logs at Warn level with lazily-computed attributes.
//
// The fn is not called if logging is disabled.
func WarnLazy(ctx context.Context, logger *slog.Logger, msg string, fn func() []slog.Attr) {
	if logger == nil {
		return
	}
	if !logger.Enabled(ctx, slog.LevelWarn) {
		return
	}
	logger.LogAttrs(ctx, slog.LevelWarn, msg, fn()...)
}

// Error logs a message at Error level if the logger is non-nil and enabled.
//
// Use for simple, pre-computed attributes only. For computed attributes,
// use [ErrorLazy].
//
// Note: In the YAMMM library, errors are typically returned rather than
// logged. This function is provided for API completeness with slog levels.
func Error(ctx context.Context, logger *slog.Logger, msg string, attrs ...slog.Attr) {
	if logger == nil {
		return
	}
	if !logger.Enabled(ctx, slog.LevelError) {
		return
	}
	logger.LogAttrs(ctx, slog.LevelError, msg, attrs...)
}

// ErrorLazy logs at Error level with lazily-computed attributes.
//
// The fn is not called if logging is disabled.
func ErrorLazy(ctx context.Context, logger *slog.Logger, msg string, fn func() []slog.Attr) {
	if logger == nil {
		return
	}
	if !logger.Enabled(ctx, slog.LevelError) {
		return
	}
	logger.LogAttrs(ctx, slog.LevelError, msg, fn()...)
}
