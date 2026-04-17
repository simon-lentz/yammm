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
//     accessor methods that return defensive copies.
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
//	issue := diag.NewIssue(diag.Error, diag.E_TYPE_COLLISION, `type "Person" already defined`).
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
// Use [Collector] to aggregate issues during validation:
//
//	collector := diag.NewCollector(100) // limit of 100 issues
//	collector.Collect(issue)
//	result := collector.Result()
//
//	if err := result.Err(); err != nil {
//	    // handle semantic failures
//	}
//
// [Collector] is thread-safe and provides O(1) severity queries via [Collector.OK],
// [Collector.HasErrors], and [Collector.HasFatal].
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
