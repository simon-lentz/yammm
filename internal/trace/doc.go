// Package trace provides optional debug logging helpers for the YAMMM library.
//
// This package is an internal utility for developer observability. It is distinct
// from [diag.Result] (user-facing content issues) and error returns (system failures).
//
// # Internal Package
//
// This package is internal to the YAMMM module and is not importable by external
// consumers per Go's internal/ package semantics. It is used for coordination across
// library packages (schema, instance, graph, adapter).
//
// # Design Principles
//
// The trace package follows several key design principles:
//
//   - Near-zero cost when disabled: When the logger is nil, overhead is a single nil
//     check (~2ns). When the logger is non-nil but the level is disabled, overhead
//     includes the nil check plus a level test (~3-4ns). The Lazy variants guarantee
//     no allocation from attribute construction when disabled.
//   - Stdlib only: Uses [log/slog] (Go 1.21+), preserving dependency hygiene.
//   - Logger injection: Loggers are passed via options at API boundaries, not stored
//     in globals or read from environment variables.
//   - Foundation tier exclusion: This package is NOT at the foundation tier. It may
//     be imported by core library tier packages (schema, instance, graph) and adapters,
//     but NOT by foundation tier packages (diag, location, immutable).
//
// # Separation of Concerns
//
// The library uses three distinct mechanisms for different categories of information:
//
//   - [diag.Result]: User-facing content issues (schema syntax errors, validation failures,
//     constraint violations). These are structured diagnostics with error codes and locations.
//   - error returns: System failures (file I/O errors, nil arguments, impossible states).
//   - trace logging: Developer observability (inheritance linearization order, import
//     resolution sequence, validation decisions). This package.
//
// # Usage Patterns
//
// There are four patterns for logging, chosen based on attribute computation cost:
//
//   - [Begin]/[Op.End]: Operation boundaries (start/end of public API calls). Use for
//     wrapping top-level functions with automatic duration measurement.
//   - [Debug], [Info], [Warn], [Error]: Simple, pre-computed attributes. The variadic
//     args are evaluated at the call site even when logging is disabled.
//   - [DebugLazy], [InfoLazy], [WarnLazy], [ErrorLazy]: Computed attributes. The
//     function argument is not called when logging is disabled, guaranteeing no
//     allocation from attribute construction.
//   - [Enabled]: For complex control flow or multiple log calls at different levels.
//
// # Context Handling
//
// All logging functions accept a context parameter and pass it through to the
// underlying [log/slog.Logger]. This enables context-scoped behaviors such as:
//   - Request-scoped logging values stored in context
//   - Cancellation-aware log handlers
//
// The Op Runner ([Begin]/[Op.End]) additionally:
//   - Includes "request_id" if present in context (via [WithRequestID])
//   - Checks context cancellation for "ctx_err" attribute
//
// # Op Runner
//
// The [Op] type provides consistent operation boundary logging with automatic
// duration measurement and cancellation handling. [Begin] returns nil when
// logging is disabled (nil logger or level below Debug), achieving near-zero
// overhead (~1-2ns). All [Op] methods are safe to call on nil.
//
//	func Load(ctx context.Context, path string, opts ...LoadOption) (*Schema, diag.Result, error) {
//	    op := trace.Begin(ctx, cfg.logger, "yammm.schema.load", slog.String("source", path))
//	    defer op.End(nil)
//
//	    schema, result, err := loadInternal(ctx, path, cfg)
//	    if err != nil {
//	        op.End(err)
//	        return nil, result, err
//	    }
//
//	    op.End(nil, slog.Int("types_count", schema.TypeCount()))
//	    return schema, result, nil
//	}
//
// The Op runner automatically logs:
//   - "op": operation name
//   - "request_id": if present in context (via [WithRequestID])
//   - "elapsed_ms": elapsed time in milliseconds (int64, machine-parseable)
//   - "duration": elapsed time as [time.Duration] (human-readable)
//   - "ctx_err": context error message if cancelled
//   - "error": error message if err != nil
//
// # Operation Names
//
// Operation names follow the format yammm.<package>.<operation>:
//   - yammm.schema.load
//   - yammm.instance.validate
//   - yammm.graph.add
//
// Operation names are implementation details and may change without notice.
// Tests should not depend on the exact set of operation names.
package trace
