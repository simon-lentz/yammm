package diag

import (
	"errors"
	"fmt"
	"log/slog"
	"slices"
)

// ContextualError pairs a failed [Result] with a human-readable context tag.
//
// It is the single carrier produced by [Result.WithContext] at an error
// boundary: a *ContextualError implements error (participating in Go error
// chains via errors.As / errors.Is and fmt.Errorf("%w", ...)) and
// [slog.LogValuer] (uniform structured logging). It unwraps to the bare
// [*ResultError] that [Result.Err] produces, so consumers doing
// errors.As(err, &re) where re is *[ResultError] keep working; consumers that
// want the tag and issues read the exported fields or recover via
// [AsContextualError].
//
// The zero-value pointer (nil *ContextualError) is safe to call Error, Unwrap,
// and LogValue on: none panic.
type ContextualError struct {
	// Result contains the diagnostic issues that caused the failure.
	Result Result

	// Tag is the human-readable context label supplied to [Result.WithContext]
	// (e.g., "schema_load", "validation", "graph_check").
	Tag string
}

// WithContext tags r with the given context label, returning an error.
//
// It returns nil when r is OK (no Fatal or Error issues), and a
// [*ContextualError] otherwise. Intended for use at error boundaries where a
// top-level label identifies the operation that produced the diagnostic:
//
//	if err := result.WithContext("schema_load"); err != nil {
//	    return fmt.Errorf("pipeline startup: %w", err)
//	}
//
// tag is stored verbatim; yammm does not impose a naming convention. Common
// shapes are lower_snake_case identifiers ("schema_load", "label_collision")
// or short phrases ("county validation").
func (r Result) WithContext(tag string) error {
	if r.OK() {
		return nil
	}
	return &ContextualError{Result: r, Tag: tag}
}

// Error returns the formatted error string "<tag>: <result>".
//
// The result portion uses [Result.String], which lists the fatal/error count
// on the first line and each error-severity issue's code+message on subsequent
// lines. For rendered output with source excerpts, use a [Renderer] against the
// underlying Result.
//
// Nil-safe: a nil receiver returns a fixed diagnostic string rather than
// panicking.
func (e *ContextualError) Error() string {
	if e == nil {
		return "<nil *diag.ContextualError>"
	}
	return fmt.Sprintf("%s: %s", e.Tag, e.Result.String())
}

// Unwrap returns the bare [*ResultError] produced by [Result.Err], enabling
// errors.As recovery to the underlying diag error type.
//
// Consumers doing errors.As(err, &re) where re is *[ResultError] continue to
// work unchanged against tagged errors. Returns nil when the receiver is nil or
// when the underlying Result is OK.
func (e *ContextualError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Result.Err()
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
//     count. Always emitted. Info and Hint counts are deliberately omitted
//     — they are not observability signals consumers filter on in practice.
//   - "issues" (slice of maps): one entry per issue in the result, each
//     carrying the per-issue shape documented on [Issue.LogValue]. Always
//     emitted as a slice. Log consumers iterate the slice directly rather
//     than discovering positional keys.
//
// The shape is stable across future yammm releases so downstream log
// aggregators, alert queries, and dashboards can key off the attribute tree.
//
// Issues are materialized as []map[string]any rather than []slog.Value so that
// handlers that serialize via encoding/json (the canonical case is
// [slog.JSONHandler]) produce JSON arrays of objects. slog handlers do not
// recurse into [slog.LogValuer] within slice elements, so the materialization
// happens here rather than per-element.
//
// Nil-safe: a nil receiver returns an empty group.
func (e *ContextualError) LogValue() slog.Value {
	if e == nil {
		return slog.GroupValue()
	}

	attrs := make([]slog.Attr, 0, 4)
	attrs = append(attrs, slog.String("context", e.Tag))

	if code, ok := primaryErrorCode(e.Result); ok {
		attrs = append(attrs, slog.String("code", code))
	}

	counts := e.Result.SeverityCounts()
	attrs = append(attrs, slog.Group(
		"counts",
		slog.Int("errors", counts.Fatal+counts.Errors),
		slog.Int("warnings", counts.Warnings),
	))

	issues := slices.Collect(e.Result.Issues())
	issueMaps := make([]map[string]any, len(issues))
	for i, iss := range issues {
		issueMaps[i] = issueLogMap(iss)
	}
	attrs = append(attrs, slog.Any("issues", issueMaps))

	return slog.GroupValue(attrs...)
}

// primaryErrorCode returns the first error-severity (Fatal or Error) issue's
// code string, or ("", false) when no such issue exists or all such issues
// have zero codes.
func primaryErrorCode(r Result) (string, bool) {
	for issue := range r.Errors() {
		if !issue.Code().IsZero() {
			return issue.Code().String(), true
		}
	}
	return "", false
}

// AsContextualError recovers a [*ContextualError] from an error chain.
//
// The walk preserves tagged context when present and synthesizes a fallback tag
// when the error chain carries only a bare [*ResultError]:
//
//   - If the chain carries a [*ContextualError], returns it with its tag
//     preserved.
//   - Otherwise, if the chain carries a bare [*ResultError], returns a new
//     [*ContextualError] tagged with fallbackTag. This covers code paths that
//     surface a diag result without calling [Result.WithContext] — consumers
//     writing unified error handlers get a uniform shape across both patterns.
//   - Otherwise, returns (nil, false).
//
// errors.As walks the chain transparently including through wrapped errors
// (fmt.Errorf("...: %w", err), errors.Join, and any custom Unwrap chains), so
// this helper recovers context from arbitrarily nested wrappings.
//
// Nil-safe: AsContextualError(nil, tag) returns (nil, false) without consulting
// fallbackTag.
func AsContextualError(err error, fallbackTag string) (*ContextualError, bool) {
	if err == nil {
		return nil, false
	}
	var ce *ContextualError
	if errors.As(err, &ce) && ce != nil {
		return ce, true
	}
	var re *ResultError
	if errors.As(err, &re) && re != nil {
		return &ContextualError{Result: re.Result, Tag: fallbackTag}, true
	}
	return nil, false
}
