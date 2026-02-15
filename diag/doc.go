// Package diag provides structured diagnostics for the YAMMM validation pipeline.
//
// This package sits at the foundation tier alongside [location], providing the
// single diagnostic infrastructure used across schema loading, parsing,
// compilation, and instance validation.
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
// All YAMMM public entry points follow a consistent pattern:
//
//   - err != nil: catastrophic failure (I/O, internal corruption, runtime failures)
//   - err == nil and !result.OK(): semantic failure represented as structured issues
//   - err == nil and result.OK(): success (may still include warnings/info/hints)
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
// matching the !result.OK() check.
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
//	if !result.OK() {
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
//   - LSP-compatible diagnostics with UTF-16 character offsets
//
// Example:
//
//	renderer := diag.NewRenderer(
//	    diag.WithSourceProvider(provider),
//	    diag.WithExcerpts(true),
//	)
//	output := renderer.FormatResult(result)
//
// # Package Dependencies
//
// Per the Foundation Rule, diag imports only stdlib and [location]. It must not
// import higher-level packages like schema, instance, graph, or adapter.
package diag
