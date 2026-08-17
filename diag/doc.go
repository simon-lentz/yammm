// Package diag provides structured diagnostics for the YAMMM validation pipeline.
//
// This package provides the single diagnostic infrastructure used across schema
// loading, parsing, compilation, and instance validation. It depends only on
// [github.com/simon-lentz/yammm/location] and the standard library.
//
// # Design Principles
//
// The diag package follows several key design principles:
//
//   - Structured data, string-last presentation: Location is stored as data
//     ([location.Span], instance path strings), never embedded in message strings.
//   - Immutable results: [Result] stores issues in unexported fields and exposes
//     them only through iterators ([Result.Issues], [Result.Errors],
//     [Result.Warnings]); callers wanting a mutable slice take an explicit copy
//     via slices.Collect. The [Issue] accessors that return reference types
//     ([Issue.Related], [Issue.Details]) return defensive copies.
//   - Stable error codes: [Code] values are stable identifiers that tools can
//     match on, even when message text changes. The Code type uses an unexported
//     struct to enforce a closed set of valid codes.
//   - Deterministic ordering: [Collector.Result] sorts issues by source, position,
//     and code to ensure stable output across runs.
//   - Builder pattern: [IssueBuilder] is the only valid construction path for
//     [Issue] values, eliminating common construction mistakes.
//   - Precomputed counts: [Collector] maintains O(1) severity queries via
//     precomputed counts updated during collection.
//
// # Entry Point Pattern
//
// All YAMMM diagnostic-producing operations return (T, [Result]):
//
//   - [Result.HasFatal]: unrecoverable condition (I/O failure, context cancellation)
//   - [Result.HasErrors]: semantic failure represented as structured issues
//   - [Result.OK]: success (may still include warnings/info/hints)
//
// Pure transformations (serialization, query generation) return (T, error).
//
// # Severity Semantics
//
// [Severity] is an ordered enumeration where lower values are more severe:
//
//   - [Fatal]: Unrecoverable condition or collection limit reached sentinel
//   - [Error]: Validation failure but collection can continue
//   - [Warning], [Info], [Hint]: Non-blocking diagnostics
//
// The [Severity.IsFailure] method returns true for Fatal and Error severities,
// matching the [Result.Err] != nil check.
//
// # Issue Construction
//
// Issues must be constructed using [NewIssue] and [IssueBuilder]:
//
//	issue := diag.NewIssue(diag.Error, diag.E_DUPLICATE_TYPE, `type "Person" already defined`).
//	    WithSpan(span).
//	    WithHint("rename one of the types").
//	    WithRelated(location.RelatedInfo{Span: previousSpan, Message: "previous definition here"}).
//	    Build()
//
// Direct struct literal construction bypasses validity checks and will cause
// panics when the issue is collected.
//
// # Collection and Results
//
// For the terminal case — one or more already-built issues to return as a
// Result — use [Collect]:
//
//	return nil, diag.Collect(issue)
//
// For incremental or concurrent accumulation during validation, use a
// [Collector]:
//
//	collector := diag.NewCollector(100) // limit of 100 issues
//	collector.Collect(issue)
//	result := collector.Result()
//
//	if err := result.Err(); err != nil {
//	    // handle semantic failures
//	}
//
// [Collector] is thread-safe and provides O(1) severity queries via
// [Collector.HasErrors] and [Collector.ErrorCount]; the full query surface
// lives on [Result]. Both paths produce the same deterministic issue ordering.
//
// # Rendering
//
// The [Renderer] provides formatting for multiple output formats:
//
//   - Text output with optional source excerpts and ANSI colors
//   - JSON output with stable wire format
//
// Example:
//
//	renderer := diag.NewRenderer(
//	    diag.WithSourceProvider(provider),
//	    diag.WithExcerpts(true),
//	)
//	output := renderer.FormatResult(result)
//
// # Contextual Diagnostic Wrap
//
// At an error boundary, tag a [Result] with a human-readable context label
// via [Result.WithContext], which returns an error directly — nil when the
// result is OK, a [*ContextualError] otherwise:
//
//	if err := result.WithContext("schema_load"); err != nil {
//	    return fmt.Errorf("pipeline startup: %w", err)
//	}
//
// A [*ContextualError] participates in Go error chains — callers doing
// errors.As(err, &re) where re is *[ResultError] continue to work unchanged
// because [ContextualError.Unwrap] returns the underlying [*ResultError]. It
// also implements [slog.LogValuer], so the tagged diagnostic is handed
// directly to structured logging:
//
//	logger.Error("operation failed", slog.Any("diagnostic", err))
//
// The resulting group carries: "context" (the tag), an optional "code"
// (the first error-severity issue's code), "counts" (errors and warnings),
// and "issues" (a slice of per-issue objects matching [Issue.LogValue]'s
// shape). See [ContextualError.LogValue] for the full attribute tree.
//
// At the receiving end, [AsContextualError] recovers a [*ContextualError]
// from an arbitrarily-wrapped error. If the chain carries a
// [*ContextualError], the original tag survives; if it carries only a bare
// [*ResultError] (from [Result.Err] without a tag), a caller-supplied
// fallbackTag is synthesized. This unifies error handlers that receive both
// shapes.
//
// # Dependencies
//
// diag imports only stdlib and [github.com/simon-lentz/yammm/location]. It must not import schema, instance,
// graph, or adapter.
//
// # v0.3.0 Diagnostic Code Additions
//
// v0.3.0 adds three stable diagnostic codes under [CategorySnapshot],
// surfaced by the new primitives in the snapshot package. They land in
// this file ahead of the primitive PRs so every per-item PR has concrete
// codes to reference at merge time. The W_ prefix on the warning code
// inaugurates the convention for Warning-severity codes added from
// v0.3.0 onward; existing Warning-severity codes retain their E_
// identifiers for backwards compatibility.
//
//	Code                              Severity  Emitted by
//	--------------------------------  --------  -----------------------------------------------------
//	E_SNAPSHOT_IO                     Fatal     snapshot.ScanDir (per-file I/O failure on ScanEntry.Result)
//	E_UPDATE_METADATA_BODY_OFFSET     Fatal     snapshot.UpdateMetadata (body-offset tracker cannot resolve)
//	W_UPDATE_METADATA_FALLBACK        Warning   snapshot.UpdateMetadataOrReMarshal (fallback to Load+Marshal)
//
// See the Godoc on each individual [Code] for the failure-mode
// contract, detail-field conventions, and consumer recovery guidance.
package diag
