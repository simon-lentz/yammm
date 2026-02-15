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
// The yielded issues must not be mutated. Use [IssuesSlice] if you need
// a mutable slice.
func (r Result) Issues() iter.Seq[Issue] {
	return func(yield func(Issue) bool) {
		for _, issue := range r.issues {
			if !yield(issue) {
				return
			}
		}
	}
}

// IssuesSlice returns a deep copy of all issues.
//
// Prefer [Issues] for read-only iteration to avoid allocation.
func (r Result) IssuesSlice() []Issue {
	if len(r.issues) == 0 {
		return nil
	}
	result := make([]Issue, len(r.issues))
	for i, issue := range r.issues {
		result[i] = issue.Clone()
	}
	return result
}

// Errors returns an iterator over Fatal and Error issues.
//
// Use [ErrorsSlice] if you need a slice.
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

// ErrorsSlice returns only Fatal and Error issues (deep copy).
func (r Result) ErrorsSlice() []Issue {
	if r.fatalCount+r.errorCount == 0 {
		return nil
	}
	result := make([]Issue, 0, r.fatalCount+r.errorCount)
	for _, issue := range r.issues {
		if issue.Severity().IsFailure() {
			result = append(result, issue.Clone())
		}
	}
	return result
}

// Warnings returns an iterator over Warning issues.
//
// Use [WarningsSlice] if you need a slice.
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

// WarningsSlice returns only Warning issues (deep copy).
func (r Result) WarningsSlice() []Issue {
	if r.warningCount == 0 {
		return nil
	}
	result := make([]Issue, 0, r.warningCount)
	for _, issue := range r.issues {
		if issue.Severity() == Warning {
			result = append(result, issue.Clone())
		}
	}
	return result
}

// BySeverity returns an iterator over issues at exactly the given severity.
//
// Use [BySeveritySlice] if you need a slice.
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

// BySeveritySlice returns issues at exactly the given severity (deep copy).
func (r Result) BySeveritySlice(severity Severity) []Issue {
	count := r.countBySeverity(severity)
	if count == 0 {
		return nil
	}
	result := make([]Issue, 0, count)
	for _, issue := range r.issues {
		if issue.Severity() == severity {
			result = append(result, issue.Clone())
		}
	}
	return result
}

func (r Result) countBySeverity(severity Severity) int {
	switch severity {
	case Fatal:
		return r.fatalCount
	case Error:
		return r.errorCount
	case Warning:
		return r.warningCount
	case Info:
		return r.infoCount
	case Hint:
		return r.hintCount
	default:
		return 0
	}
}

// IssuesAtLeastAsSevereAs returns an iterator over issues at least as severe
// as the threshold.
//
// This uses the same semantics as [Severity.IsAtLeastAsSevereAs].
// Example: IssuesAtLeastAsSevereAs(Warning) yields Fatal, Error, and Warning issues.
//
// Use [IssuesAtLeastAsSevereAsSlice] if you need a slice.
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

// IssuesAtLeastAsSevereAsSlice returns issues at least as severe as the
// threshold (deep copy).
//
// See [IssuesAtLeastAsSevereAs] for semantics.
func (r Result) IssuesAtLeastAsSevereAsSlice(threshold Severity) []Issue {
	var count int
	switch {
	case threshold > Hint:
		// Invalid threshold: all valid severities (0-4) are "at least as severe"
		// because severity uses lower numeric values for higher severity.
		// This matches the iterator's behavior via IsAtLeastAsSevereAs.
		count = len(r.issues)
	case threshold == Fatal:
		count = r.fatalCount
	case threshold == Error:
		count = r.fatalCount + r.errorCount
	case threshold == Warning:
		count = r.fatalCount + r.errorCount + r.warningCount
	case threshold == Info:
		count = r.fatalCount + r.errorCount + r.warningCount + r.infoCount
	case threshold == Hint:
		count = len(r.issues)
	}

	if count == 0 {
		return nil
	}

	result := make([]Issue, 0, count)
	for _, issue := range r.issues {
		if issue.Severity().IsAtLeastAsSevereAs(threshold) {
			result = append(result, issue.Clone())
		}
	}
	return result
}

// Messages returns message strings from Fatal and Error issues.
//
// This is a convenience helper, not a collection accessor; no iterator variant.
func (r Result) Messages() []string {
	if r.fatalCount+r.errorCount == 0 {
		return nil
	}
	result := make([]string, 0, r.fatalCount+r.errorCount)
	for _, issue := range r.issues {
		if issue.Severity().IsFailure() {
			result = append(result, issue.Message())
		}
	}
	return result
}

// MessagesAtOrAbove returns message strings from issues at or above the
// specified severity threshold.
//
// "Above" means more severe, not higher numeric value (severity ordering:
// Fatal < Error < Warning < Info < Hint).
//
// Example: MessagesAtOrAbove(Warning) returns Fatal, Error, and Warning messages.
//
// This is a convenience helper for log/error output; for iteration over Issue
// values, use [IssuesAtLeastAsSevereAs] or [Issues] with filtering.
func (r Result) MessagesAtOrAbove(threshold Severity) []string {
	var result []string
	for _, issue := range r.issues {
		if issue.Severity().IsAtLeastAsSevereAs(threshold) {
			result = append(result, issue.Message())
		}
	}
	return result
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
