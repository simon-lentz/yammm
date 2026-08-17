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

// add increments the counter for sev. It is the single Severity-to-counter
// mapping in the package — [Collector.collectLocked] (the live collection path)
// and [newResult] (recompute from a slice) both route through it, so the two
// cannot disagree on how a severity is counted and a new Severity is handled in
// exactly one switch.
func (c *SeverityCounts) add(sev Severity) {
	//exhaustive:enforce
	switch sev {
	case Fatal:
		c.Fatal++
	case Error:
		c.Errors++
	case Warning:
		c.Warnings++
	case Info:
		c.Info++
	case Hint:
		c.Hints++
	}
}

// sub decrements the counter for sev, the inverse of [SeverityCounts.add]. Only
// the collector's STORED tally shrinks (an evicted issue leaves storage but is
// still an issue the collector saw), so this must never be applied to the
// seen-severity counts that OK/HasErrors/ErrorCount read.
func (c *SeverityCounts) sub(sev Severity) {
	//exhaustive:enforce
	switch sev {
	case Fatal:
		c.Fatal--
	case Error:
		c.Errors--
	case Warning:
		c.Warnings--
	case Info:
		c.Info--
	case Hint:
		c.Hints--
	}
}

// leastSevere returns the least severe severity with a non-zero count, and
// whether any count is non-zero. Read against the collector's stored tally, it
// names the severity whose issues yield first when the issue limit forces a
// choice — see [Collector.storeLocked]. The scan runs from the least severe
// level down, so it is O(1) in the number of severities, not in issue count.
func (c SeverityCounts) leastSevere() (Severity, bool) {
	switch {
	case c.Hints > 0:
		return Hint, true
	case c.Info > 0:
		return Info, true
	case c.Warnings > 0:
		return Warning, true
	case c.Errors > 0:
		return Error, true
	case c.Fatal > 0:
		return Fatal, true
	default:
		return Fatal, false
	}
}

// addCounts folds o into c field-wise, letting [Collector.Merge] carry a source
// Result's whole seen-severity tally forward in one call.
func (c *SeverityCounts) addCounts(o SeverityCounts) {
	c.Fatal += o.Fatal
	c.Errors += o.Errors
	c.Warnings += o.Warnings
	c.Info += o.Info
	c.Hints += o.Hints
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

	// Precomputed severity counts, set at construction time. Holds the same
	// [SeverityCounts] the producing [Collector] carries (rather than mirroring
	// it into separate fields), so the two cannot drift. Under truncation these
	// reflect every issue seen, not only those stored (see
	// [Collector.collectLocked]).
	counts SeverityCounts
}

// newResult creates a Result, computing the severity counts from issues.
//
// The issues slice is owned by the Result and must not be modified after
// this call. Callers must pass a fresh slice (not shared with other code).
//
// Use [newResultWithCounts] directly when the counts must reflect issues that
// are not in the slice (e.g. a [Collector] carrying issues dropped past its
// limit, which must still count toward OK/HasErrors).
func newResult(issues []Issue, limit int, limitReached bool, droppedCount int) Result {
	var counts SeverityCounts
	for _, issue := range issues {
		counts.add(issue.Severity())
	}
	return newResultWithCounts(issues, limit, limitReached, droppedCount, counts)
}

// newResultWithCounts builds a Result from explicit precomputed severity counts.
// It is the single point that constructs the Result struct, so a new Result
// field is added in exactly one place. The issues slice is owned by the Result
// (see [newResult]'s contract).
func newResultWithCounts(issues []Issue, limit int, limitReached bool, droppedCount int, counts SeverityCounts) Result {
	return Result{
		issues:       issues,
		limit:        limit,
		limitReached: limitReached,
		droppedCount: droppedCount,
		counts:       counts,
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
	return r.counts.Fatal == 0 && r.counts.Errors == 0
}

// HasFatal reports whether any Fatal issue is present.
func (r Result) HasFatal() bool {
	return r.counts.Fatal > 0
}

// HasErrors reports whether any Fatal or Error issue is present.
func (r Result) HasErrors() bool {
	return r.counts.Fatal > 0 || r.counts.Errors > 0
}

// HasWarnings reports whether any Warning issue is present.
func (r Result) HasWarnings() bool {
	return r.counts.Warnings > 0
}

// HasCode reports whether any retained issue carries the given code, at any
// severity. Like [Result.Issues] and [Result.Len], it reflects the issues the
// result can enumerate: when the issue limit was reached ([Result.LimitReached]),
// a code present only among the dropped issues reads false here. Use the
// seen-based queries ([Result.HasErrors], [Result.SeverityCounts]) for gating
// that must stay truthful under truncation, and [Result.DroppedCount] to detect
// that the enumerable set is incomplete.
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
	// limit is the producing collector's own configured cap; after a Merge into
	// an unlimited collector it can be 0 (unlimited) even though issues were
	// dropped upstream. Name the cap only when it is a positive number;
	// droppedCount is the authoritative fact either way.
	if r.limit > 0 {
		return fmt.Sprintf("%d more issue(s) dropped after reaching the %d-issue limit; resolve issues and re-run to see the rest",
			r.droppedCount, r.limit)
	}
	return fmt.Sprintf("%d more issue(s) dropped; resolve issues and re-run to see the rest", r.droppedCount)
}

// Limit returns the configured issue limit (0 means unlimited).
// Use [LimitReached] to check if the limit was actually reached.
func (r Result) Limit() int {
	return r.limit
}

// SeverityCounts returns counts by severity level.
func (r Result) SeverityCounts() SeverityCounts {
	return r.counts
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
