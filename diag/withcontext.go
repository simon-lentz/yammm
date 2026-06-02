package diag

import (
	"errors"
	"fmt"
	"log/slog"
)

// ResultWithContext pairs a [Result] with a human-readable context tag.
//
// ResultWithContext is the structured-access carrier produced by
// [Result.WithContext]. It implements [slog.LogValuer] for uniform
// structured logging at error boundaries, and its [ResultWithContext.Err]
// method returns a [*ErrorWithContext] that participates in Go error chains
// (errors.As / errors.Is, transparent unwrapping through
// fmt.Errorf("%w", ...)).
//
// Use ResultWithContext when you want structured access to the tag and
// issues (direct field reads, iteration via Result). Use the pointer error
// type via Err() when you want to return an error from a function. The
// split mirrors yammm's existing [Result] / [*ResultError] shape.
//
// The zero value is well-defined: Tag is empty, Result is OK, and Err()
// returns nil. Callers constructing via Result.WithContext(tag) get a
// populated value.
type ResultWithContext struct {
	// Result carries the diagnostic issues.
	Result Result

	// Tag is the human-readable context label identifying the operation
	// that produced the diagnostic (e.g., "schema_load", "validation",
	// "graph_check").
	Tag string
}

// WithContext returns a [ResultWithContext] tagging r with the given
// context label.
//
// Intended for use at error boundaries where a top-level label identifies
// the operation that produced the diagnostic. The label flows through
// [ResultWithContext.Err]'s error message, [ResultWithContext.LogValue]'s
// "context" attribute, and [AsResultWithContext]'s recovery path.
//
// tag is stored verbatim; yammm does not impose a naming convention.
// Common shapes across consumers are lower_snake_case identifiers
// ("schema_load", "label_collision") or short phrases ("county validation").
func (r Result) WithContext(tag string) ResultWithContext {
	return ResultWithContext{Result: r, Tag: tag}
}

// Err returns nil when the underlying [Result] is OK, or a
// [*ErrorWithContext] when it carries Fatal or Error issues.
//
// The returned error unwraps to the bare [*ResultError] that
// [Result.Err] would produce, so consumers performing
// errors.As(err, &re) where re is *[ResultError] continue to work
// unchanged. Consumers that want the tag use errors.As(err, &ewc)
// where ewc is *[ErrorWithContext], or [AsResultWithContext] for a
// uniform dispatch across tagged and untagged diag errors.
func (c ResultWithContext) Err() error {
	if c.Result.OK() {
		return nil
	}
	return &ErrorWithContext{Result: c.Result, Tag: c.Tag}
}

// HasErrors delegates to the underlying [Result.HasErrors].
func (c ResultWithContext) HasErrors() bool {
	return c.Result.HasErrors()
}

// OK delegates to the underlying [Result.OK].
func (c ResultWithContext) OK() bool {
	return c.Result.OK()
}

// LogValue implements [slog.LogValuer] for structured logging.
//
// Returns a GroupValue with the following attributes:
//
//   - "context" (string): the tag. Always emitted.
//   - "code" (string): the primary error-severity issue's stable code.
//     Omitted when the result carries no error-severity issue with a
//     non-zero code.
//   - "counts" (group): {errors: int, warnings: int}. "errors" is the
//     sum of Fatal and Error severity counts; "warnings" is the Warning
//     count. Always emitted (0/0 on OK). Info and Hint counts are
//     deliberately omitted — they are not observability signals
//     consumers filter on in practice.
//   - "issues" (slice of maps): one entry per issue in the result, each
//     carrying the per-issue shape documented on [Issue.LogValue].
//     Always emitted as a slice; empty when the result is OK. Log
//     consumers iterate the slice directly rather than discovering
//     positional keys.
//
// The shape is stable across future yammm releases so downstream log
// aggregators, alert queries, and dashboards can key off the attribute
// tree.
//
// Issues are materialized as []map[string]any rather than []slog.Value
// so that handlers that serialize via encoding/json (the canonical case
// is [slog.JSONHandler]) produce JSON arrays of objects. slog handlers
// do not recurse into [slog.LogValuer] within slice elements, so the
// materialization happens here rather than per-element.
func (c ResultWithContext) LogValue() slog.Value {
	attrs := make([]slog.Attr, 0, 4)
	attrs = append(attrs, slog.String("context", c.Tag))

	if code, ok := primaryErrorCode(c.Result); ok {
		attrs = append(attrs, slog.String("code", code))
	}

	counts := c.Result.SeverityCounts()
	attrs = append(attrs, slog.Group(
		"counts",
		slog.Int("errors", counts.Fatal+counts.Errors),
		slog.Int("warnings", counts.Warnings),
	))

	issues := c.Result.IssuesSlice()
	issueMaps := make([]map[string]any, len(issues))
	for i, iss := range issues {
		issueMaps[i] = issueLogMap(iss)
	}
	attrs = append(attrs, slog.Any("issues", issueMaps))

	return slog.GroupValue(attrs...)
}

// primaryErrorCode returns the first error-severity (Fatal or Error)
// issue's code string, or ("", false) when no such issue exists or all
// such issues have zero codes.
func primaryErrorCode(r Result) (string, bool) {
	for issue := range r.Errors() {
		if !issue.Code().IsZero() {
			return issue.Code().String(), true
		}
	}
	return "", false
}

// ErrorWithContext is the error type returned by [ResultWithContext.Err].
//
// ErrorWithContext wraps a [Result] with a context tag and participates in
// Go error chains. Consumers recover the tag via errors.As or the
// [AsResultWithContext] helper; consumers that only need the underlying
// [Result] can unwrap to the bare [*ResultError].
//
// The zero-value pointer (nil *ErrorWithContext) is safe to call Error
// and Unwrap on: neither panics, Error returns a fixed diagnostic string,
// Unwrap returns nil.
type ErrorWithContext struct {
	// Result contains the diagnostic issues that caused the failure.
	Result Result

	// Tag is the context label supplied to [Result.WithContext].
	Tag string
}

// Error returns the formatted error string "<tag>: <result>".
//
// The result portion uses [Result.String], which lists the fatal/error
// count on the first line and each error-severity issue's code+message
// on subsequent lines. For rendered output with source excerpts, use a
// [Renderer] against the underlying Result.
//
// Nil-safe: a nil receiver returns a fixed diagnostic string rather than
// panicking.
func (e *ErrorWithContext) Error() string {
	if e == nil {
		return "<nil *diag.ErrorWithContext>"
	}
	return fmt.Sprintf("%s: %s", e.Tag, e.Result.String())
}

// Unwrap returns the bare [*ResultError] produced by [Result.Err],
// enabling errors.As recovery to the existing diag error type.
//
// Consumers doing errors.As(err, &re) where re is *[ResultError] continue
// to work unchanged against tagged errors. Returns nil when the receiver
// is nil or when the underlying Result is OK.
func (e *ErrorWithContext) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Result.Err()
}

// AsResultWithContext recovers a [ResultWithContext] from an error chain.
//
// The walk preserves tagged context when present and synthesizes a
// fallback tag when the error chain carries only a bare [*ResultError]:
//
//   - If the chain carries a [*ErrorWithContext], returns a
//     ResultWithContext with its tag preserved.
//   - Otherwise, if the chain carries a bare [*ResultError], returns a
//     ResultWithContext tagged with fallbackTag. This covers code paths
//     that surface a diag result without calling [Result.WithContext]
//     — consumers writing unified error handlers get a uniform shape
//     across both patterns.
//   - Otherwise, returns the zero [ResultWithContext] and false.
//
// errors.As walks the chain transparently including through wrapped
// errors (fmt.Errorf("...: %w", err), errors.Join, and any custom
// Unwrap chains), so this helper recovers context from arbitrarily
// nested wrappings.
//
// Nil-safe: AsResultWithContext(nil, tag) returns the zero value and
// false without consulting fallbackTag.
func AsResultWithContext(err error, fallbackTag string) (ResultWithContext, bool) {
	if err == nil {
		return ResultWithContext{}, false
	}
	var ewc *ErrorWithContext
	if errors.As(err, &ewc) && ewc != nil {
		return ResultWithContext{Result: ewc.Result, Tag: ewc.Tag}, true
	}
	var re *ResultError
	if errors.As(err, &re) && re != nil {
		return ResultWithContext{Result: re.Result, Tag: fallbackTag}, true
	}
	return ResultWithContext{}, false
}
