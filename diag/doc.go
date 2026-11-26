// Package diag provides structured diagnostics for the YAMMM validation pipeline.
//
// This package provides the single diagnostic infrastructure used across schema
// loading, parsing, compilation, and instance validation. It depends only on
// [github.com/simon-lentz/yammm/location], internal/termtext,
// golang.org/x/text/width and the standard library.
//
// # Design Principles
//
// The diag package follows several key design principles:
//
//   - Structured data, string-last presentation: Location is stored as data
//     ([location.Span], instance path strings), and a message can name a path or a line too.
//   - Immutable results: [Result] stores issues in unexported fields and exposes
//     them only through iterators ([Result.Issues], [Result.Errors],
//     [Result.BySeverity]); callers wanting a mutable slice take an explicit copy
//     via slices.Collect. The [Issue] accessors that return reference types
//     ([Issue.Related], [Issue.Details]) return defensive copies.
//   - Stable error codes: [Code] values are stable identifiers that tools can
//     match on, even when message text changes. The code set is open: the Neo4j
//     mapping and consumers register codes through [NewCode].
//   - Deterministic ordering: [Collector.Result] sorts issues by location, a span
//     in [location.Span.Compare] order, then by code, so output is stable across runs.
//   - Builder pattern: [IssueBuilder] is the only valid construction path for
//     [Issue] values, eliminating common construction mistakes.
//   - Precomputed counts: [Collector] maintains O(1) severity queries via
//     precomputed counts updated during collection.
//
// # Entry Point Pattern
//
// A YAMMM operation that can fail returns (T, error), and one that produces no
// value returns an error alone; one whose caller needs no reason, such as
// instance.Validator.PrimaryKeyOf, returns false as its last value. A failure is one *[ResultError] holding the
// Fatal and Error issues the call drew, and the call returns the zero T beside
// it unless its doc states a partial value. A call that can draw an issue below
// failure severity takes a WithWarnings option in its package's Option type: the
// callback receives each such issue on the calling goroutine, before the call
// returns, in the order found, and with no callback the issues are discarded.
// snapshot.ScanDir yields each file's failure as its entry's Err.
// schema.ModuleRootIssue returns a bare [Issue].
//
//	s, err := schema.Load(ctx, path, schema.WithWarnings(func(i diag.Issue) {
//	    log.Print(i)
//	}))
//	if re, ok := errors.AsType[*diag.ResultError](err); ok {
//	    for issue := range re.Result.Issues() { ... }
//	}
//	if errors.Is(err, diag.E_IMPORT_CYCLE) { ... }
//
// A [Result] reads its list:
//
//   - [Result.HasFatal]: any Fatal issue, such as an I/O failure, a cancellation,
//     an internal fault (E_INTERNAL, which every producer raises at Fatal), input
//     a reader cannot go past (a schema header that does not parse), an argument
//     or option value a loader refuses, or a finding that snapshot.WithRevalidation
//     raises at Fatal
//   - [Result.SeverityCounts] and [Result.CodeCounts]: every issue seen, stored
//     or dropped
//   - [Result.Issues], [Result.Errors] and [Result.BySeverity]: the stored issues
//
// An operation that can fail and reports no issues returns an error, alone or
// after its value, or false as its last value where the caller needs no reason.
//
// # Severity Semantics
//
// [Severity] is an ordered enumeration where lower values are more severe:
//
//   - [Fatal]: An unrecoverable condition. Reaching a collection limit marks
//     no severity; [Result.LimitReached] reports it
//   - [Error]: Validation failure but collection can continue
//   - [Warning], [Info], [Hint]: Non-blocking diagnostics
//
// A Fatal or Error issue is a failure, the issues a [ResultError] holds. The
// zero Severity names no severity. Only the zero [Issue] carries it, and a
// [Collector] refuses that Issue.
//
// # Issue Construction
//
// Issues must be constructed using [NewIssue] and [IssueBuilder]:
//
//	issue := diag.NewIssue(diag.Error, diag.E_DUPLICATE_TYPE, `type "Person" already defined`).
//	    WithSpan(span).
//	    WithHint("rename one of the types").
//	    WithRelated(diag.RelatedInfo{Span: previousSpan, Message: "previous definition here"}).
//	    Build()
//
// An Issue's fields are unexported, so diag.Issue{} is the one literal another
// package can write, and a zero Issue panics when collected.
//
// # Collection and Results
//
// One [Collector] serves both shapes. For the terminal case — one or more
// already-built issues to return as a failure — collect into an unlimited one
// and build its error immediately:
//
//	c := diag.NewCollector(0)
//	c.Collect(issue)
//	return nil, c.Err()
//
// For incremental or concurrent accumulation during validation, hold the
// [Collector] across the operation and build the failure at its end:
//
//	collector := diag.NewCollector(100) // limit of 100 issues
//	collector.Collect(issue)
//	if err := collector.Err(); err != nil {
//	    return fmt.Errorf("validate: %w", err)
//	}
//
// [Collector] is thread-safe and provides O(1) severity queries via
// [Collector.HasErrors] and [Collector.ErrorCount]; the full query surface
// lives on [Result]. Both paths produce the same deterministic issue ordering.
//
// [Collector.Err] builds a failure as one error value: nil when the collector
// has counted no Fatal or Error issue, dropped ones included, else a
// *[ResultError] whose Result holds the failures alone. A caller reads it with errors.AsType[*diag.ResultError] and
// tests it by code with errors.Is(err, code): a [Code] is the target of
// errors.Is, and no call returns a Code as an error. A collector given a route
// by [Collector.RouteWarnings] hands each issue below failure severity to the
// route as it arrives and stores and counts failures alone. Only the collector
// a warning reaches last may cap it, so a routed merge panics on a Result that
// dropped an issue the merge would hand the route: one below failure severity
// after [Collector.MergeRetag]'s retag, of a code the merge keeps.
//
// # Rendering
//
// The [Renderer] provides formatting for multiple output formats:
//
//   - Text output with optional source excerpts and ANSI colors
//   - JSON output with stable wire format
//
// Text output, and [Result.String], write a C0 control other than a tab, DEL,
// a C1 control or a bidirectional control in any text (a location, a code, a
// message, a hint, a note or an excerpt) as a visible glyph: a C0 control as
// its Control Pictures glyph, DEL as U+2421, the rest as U+FFFD. A byte that is
// not valid UTF-8 shows as U+FFFD. A tab is kept. So schema or document text
// cannot forge a second diagnostic line or act on the terminal.
// [Issue.Message] keeps the text's own bytes, and so does the JSON output, but
// encoding/json writes a byte that is not valid UTF-8 as U+FFFD.
//
// Example:
//
//	renderer := diag.NewRenderer(
//	    diag.WithSourceProvider(provider),
//	    diag.WithExcerpts(true),
//	)
//	output := renderer.FormatResult(result)
//
// # Logging a Failure
//
// A *[ResultError] implements [slog.LogValuer], so a failure is handed
// directly to structured logging, through any fmt.Errorf tag:
//
//	if re, ok := errors.AsType[*diag.ResultError](err); ok {
//	    logger.Error("schema load failed", slog.Any("diagnostic", re))
//	}
//
// The group carries an optional "code" (the first failure's code), "counts"
// (errors, and warnings, 0 for an error a Collector builds), "limit_reached" and "dropped" when the failure hit its
// issue limit, and "issues" (a slice of per-issue objects matching
// [Issue.LogValue]'s shape). See [ResultError.LogValue].
//
// # Dependencies
//
//	diag  ──imports──▶  location, internal/termtext, golang.org/x/text/width
//
// It must not import schema, instance, graph, schema/cypher, or neo4j.
//
// # Snapshot I/O Code
//
// E_SNAPSHOT_IO is the stable code for an I/O failure reading a snapshot; the
// table below names its emitters. Severity lives on the Issue, so a code's
// prefix is a convention and not a guarantee: W_ marks a code raised at Warning, and E_ every other code, one raised at
// more than one severity such as E_SNAPSHOT_UNSUPPORTED_HASH_ALGORITHM among
// them, save W_SNAPSHOT_UNRESOLVED_REQUIRED, raised at whatever severity
// snapshot.WithRevalidation was given.
//
//	Code                              Severity  Emitted by
//	--------------------------------  --------  -----------------------------------------------------
//	E_SNAPSHOT_IO                     Fatal     snapshot.ScanDir (a file that fails to open, on ScanEntry.Err)
//	                                            snapshot.ReadHeader (a reader that fails, not a truncation)
//	                                            yammm snapshot info --dir (a directory that fails to read)
//
// The Godoc on each code states its failure mode.
package diag
