package diag

import (
	"fmt"
	"iter"
	"strings"
)

// SeverityCounts provides counts by severity level without map allocation.
type SeverityCounts struct {
	Fatal    int
	Errors   int
	Warnings int
	Info     int
	Hints    int
}

// Result is an immutable snapshot of diagnostic issues with precomputed counts.
//
// Result provides O(1) severity queries and iterator-based access to issues.
// Results are obtained via [Collector.Result] or the [OK] function for empty
// success results.
//
// Result satisfies [fmt.Stringer]; its [Result.String] method returns "OK" when
// there are no errors, or a multi-line summary of error messages suitable for
// test failures and log output.
//
// There is no public constructor accepting arbitrary issues; this ensures
// all issues in a Result are valid.
type Result struct {
	issues       []Issue
	limit        int
	limitReached bool
	droppedCount int

	// Precomputed counts (set at construction time)
	fatalCount   int
	errorCount   int
	warningCount int
	infoCount    int
	hintCount    int
}

// newResult creates a Result with precomputed counts.
//
// The issues slice is owned by the Result and must not be modified after
// this call. Callers must pass a fresh slice (not shared with other code).
func newResult(issues []Issue, limit int, limitReached bool, droppedCount int) Result {
	var fatalCount, errorCount, warningCount, infoCount, hintCount int

	for _, issue := range issues {
		switch issue.Severity() {
		case Fatal:
			fatalCount++
		case Error:
			errorCount++
		case Warning:
			warningCount++
		case Info:
			infoCount++
		case Hint:
			hintCount++
		}
	}

	return Result{
		issues:       issues,
		limit:        limit,
		limitReached: limitReached,
		droppedCount: droppedCount,
		fatalCount:   fatalCount,
		errorCount:   errorCount,
		warningCount: warningCount,
		infoCount:    infoCount,
		hintCount:    hintCount,
	}
}

// OK returns a Result representing success (no issues).
//
// This is the canonical way to construct a success Result in return statements.
// The returned Result has:
//   - OK() == true
//   - HasErrors() == false
//   - Len() == 0
//   - LimitReached() == false
func OK() Result {
	return newResult(nil, 0, false, 0)
}

// OK reports whether no Fatal or Error issues are present.
func (r Result) OK() bool {
	return r.fatalCount == 0 && r.errorCount == 0
}

// HasFatal reports whether any Fatal issue is present.
func (r Result) HasFatal() bool {
	return r.fatalCount > 0
}

// HasErrors reports whether any Fatal or Error issue is present.
func (r Result) HasErrors() bool {
	return r.fatalCount > 0 || r.errorCount > 0
}

// HasWarnings reports whether any Warning issue is present.
func (r Result) HasWarnings() bool {
	return r.warningCount > 0
}

// HasInfo reports whether any Info issue is present.
func (r Result) HasInfo() bool {
	return r.infoCount > 0
}

// HasHints reports whether any Hint issue is present.
func (r Result) HasHints() bool {
	return r.hintCount > 0
}

// HasCode reports whether any issue carries the given code, at any
// severity.
func (r Result) HasCode(code Code) bool {
	for _, issue := range r.issues {
		if issue.Code() == code {
			return true
		}
	}
	return false
}

// Len returns the number of issues.
func (r Result) Len() int {
	return len(r.issues)
}

// LimitReached reports whether the collection limit was reached.
func (r Result) LimitReached() bool {
	return r.limitReached
}

// DroppedCount returns how many issues were dropped after hitting the limit.
func (r Result) DroppedCount() int {
	return r.droppedCount
}

// TruncationNote returns a one-line, human-readable summary of how many issues
// were dropped at the collection limit, or "" when the limit was not reached.
// It is the single source of the dropped-issues wording for text-rendering
// consumers (e.g. the CLI); the structured LSP log and the compact
// [Result.String] suffix surface the same fact in formats suited to their own
// audiences.
func (r Result) TruncationNote() string {
	if !r.limitReached {
		return ""
	}
	return fmt.Sprintf("%d more issue(s) dropped after reaching the %d-issue limit; resolve issues and re-run to see the rest",
		r.droppedCount, r.limit)
}

// Limit returns the configured issue limit (0 means unlimited).
// Use [LimitReached] to check if the limit was actually reached.
func (r Result) Limit() int {
	return r.limit
}

// SeverityCounts returns counts by severity level.
func (r Result) SeverityCounts() SeverityCounts {
	return SeverityCounts{
		Fatal:    r.fatalCount,
		Errors:   r.errorCount,
		Warnings: r.warningCount,
		Info:     r.infoCount,
		Hints:    r.hintCount,
	}
}

// Issues returns an iterator over all issues without copying.
//
// The yielded issues must not be mutated. Collect a mutable copy with
// slices.Collect(r.Issues()) if needed.
func (r Result) Issues() iter.Seq[Issue] {
	return func(yield func(Issue) bool) {
		for _, issue := range r.issues {
			if !yield(issue) {
				return
			}
		}
	}
}

// Errors returns an iterator over Fatal and Error issues.
func (r Result) Errors() iter.Seq[Issue] {
	return func(yield func(Issue) bool) {
		for _, issue := range r.issues {
			if issue.Severity().IsFailure() {
				if !yield(issue) {
					return
				}
			}
		}
	}
}

// Warnings returns an iterator over Warning issues.
func (r Result) Warnings() iter.Seq[Issue] {
	return func(yield func(Issue) bool) {
		for _, issue := range r.issues {
			if issue.Severity() == Warning {
				if !yield(issue) {
					return
				}
			}
		}
	}
}

// BySeverity returns an iterator over issues at exactly the given severity.
func (r Result) BySeverity(severity Severity) iter.Seq[Issue] {
	return func(yield func(Issue) bool) {
		for _, issue := range r.issues {
			if issue.Severity() == severity {
				if !yield(issue) {
					return
				}
			}
		}
	}
}

// IssuesAtLeastAsSevereAs returns an iterator over issues at least as severe
// as the threshold.
//
// This uses the same semantics as [Severity.IsAtLeastAsSevereAs].
// Example: IssuesAtLeastAsSevereAs(Warning) yields Fatal, Error, and Warning issues.
func (r Result) IssuesAtLeastAsSevereAs(threshold Severity) iter.Seq[Issue] {
	return func(yield func(Issue) bool) {
		for _, issue := range r.issues {
			if issue.Severity().IsAtLeastAsSevereAs(threshold) {
				if !yield(issue) {
					return
				}
			}
		}
	}
}

// ResultError is an error wrapping a failed [Result].
//
// ResultError is returned by [Result.Err] when the result contains Fatal or
// Error issues. Use [errors.As] to extract the underlying Result for structured
// access to diagnostic issues:
//
//	var re *diag.ResultError
//	if errors.As(err, &re) {
//	    for issue := range re.Result.Issues() { ... }
//	}
//
// This follows the pattern of [context.Context.Err] and [sql.Rows.Err]:
// nil means success, non-nil means failure with structured details available.
type ResultError struct {
	// Result contains the diagnostic issues that caused the failure.
	Result Result
}

// Error returns a summary of the diagnostic issues.
//
// The format matches [Result.String]: a count line followed by per-issue
// code and message lines.
func (e *ResultError) Error() string {
	return e.Result.String()
}

// Err returns nil if the result is OK, or a *[ResultError] if it contains
// Fatal or Error issues.
//
// This provides idiomatic error conversion for use in error-returning functions:
//
//	schema, result, err := load.Load(ctx, path)
//	if err != nil { return err }
//	if err := result.Err(); err != nil {
//	    return fmt.Errorf("schema validation: %w", err)
//	}
//
// The returned error supports [errors.As] with *[ResultError] for access to
// the full [Result].
func (r Result) Err() error {
	if r.OK() {
		return nil
	}
	return &ResultError{Result: r}
}

// String returns a minimal multi-line representation suitable for quick debugging.
//
// String returns "OK" when OK() is true (no Fatal/Error issues), regardless of
// warnings or hints. Each error/fatal issue is printed on its own line (message
// only, no excerpts). Use [SeverityCounts] for full severity breakdown.
// For formatted output with excerpts, use [Renderer.FormatResult].
func (r Result) String() string {
	if r.OK() {
		return "OK"
	}

	var sb strings.Builder
	counts := r.SeverityCounts()

	// Summary line
	fmt.Fprintf(&sb, "%d error(s)", counts.Fatal+counts.Errors)
	if counts.Warnings > 0 {
		fmt.Fprintf(&sb, ", %d warning(s)", counts.Warnings)
	}
	if r.limitReached {
		fmt.Fprintf(&sb, " [limit reached, %d dropped]", r.droppedCount)
	}
	sb.WriteString("\n")

	// Error messages
	for _, issue := range r.issues {
		if issue.Severity().IsFailure() {
			fmt.Fprintf(&sb, "  %s: %s\n", issue.Code(), issue.Message())
		}
	}

	return sb.String()
}
