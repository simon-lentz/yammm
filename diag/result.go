package diag

import (
	"cmp"
	"fmt"
	"iter"
	"log/slog"
	"maps"
	"slices"
	"strings"

	"github.com/simon-lentz/yammm/internal/termtext"
)

// SeverityCounts provides counts by severity level without map allocation.
type SeverityCounts struct {
	Fatal    int
	Errors   int
	Warnings int
	Info     int
	Hints    int
}

// add increments the counter for sev.
func (c *SeverityCounts) add(sev Severity) {
	c.addN(sev, 1)
}

// addN adds n to the counter for sev; [SeverityCounts.add] is its one-issue form.
func (c *SeverityCounts) addN(sev Severity, n int) {
	//exhaustive:enforce
	switch sev {
	case Fatal:
		c.Fatal += n
	case Error:
		c.Errors += n
	case Warning:
		c.Warnings += n
	case Info:
		c.Info += n
	case Hint:
		c.Hints += n
	}
}

// sub decrements the counter for sev, the inverse of [SeverityCounts.add]. Only
// the collector's STORED tally shrinks (an evicted issue leaves storage but is
// still an issue the collector saw), so this must never be applied to the
// seen-severity counts that [Collector.Err] and [Collector.ErrorCount] read.
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
// A Result is obtained from [Collector.Result], and a failure carries one as
// [ResultError.Result].
//
// Result satisfies [fmt.Stringer]; its [Result.String] method returns "OK" when
// there are no issues at all, or a summary line followed by one line per
// retained issue at every severity, suitable for test failures and log output.
//
// There is no public constructor accepting arbitrary issues; this ensures
// all issues in a Result are valid.
type Result struct {
	issues       []Issue
	limitReached bool
	droppedCount int

	// Precomputed severity counts, set at construction time. Holds the same
	// [SeverityCounts] the producing [Collector] carries (rather than mirroring
	// it into separate fields), so the two cannot drift. Under truncation these
	// reflect every issue seen, not only those stored (see
	// [Collector.collectLocked]).
	counts SeverityCounts

	// Per-severity, per-code counts over every issue seen — the code axis of
	// counts, truthful under truncation the same way. Read through
	// [Result.CodeCounts]; never handed out.
	codeCounts codeCounts

	// arrival[i] is the order issues[i] was collected in, which sorting
	// discards. A merge stores by it, so eviction keeps its meaning.
	arrival []uint64
}

// inArrivalOrder returns r's issues as they were collected, for a merge to
// store.
func (r Result) inArrivalOrder() []Issue {
	order := make([]int, len(r.issues))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int {
		return cmp.Compare(r.arrival[a], r.arrival[b])
	})
	out := make([]Issue, len(r.issues))
	for i, idx := range order {
		out[i] = r.issues[idx]
	}
	return out
}

// codeCounts counts issues seen by severity and code.
type codeCounts map[Severity]map[Code]int

// add increments the counter for one issue.
func (c *codeCounts) add(sev Severity, code Code) {
	c.addN(sev, code, 1)
}

// addN adds n issues of sev and code.
func (c *codeCounts) addN(sev Severity, code Code, n int) {
	if *c == nil {
		*c = make(codeCounts)
	}
	m := (*c)[sev]
	if m == nil {
		m = make(map[Code]int)
		(*c)[sev] = m
	}
	m[code] += n
}

// addCounts folds o into c, for [Collector.Merge].
func (c *codeCounts) addCounts(o codeCounts) {
	for sev, m := range o {
		for code, n := range m {
			if *c == nil {
				*c = make(codeCounts)
			}
			if (*c)[sev] == nil {
				(*c)[sev] = make(map[Code]int)
			}
			(*c)[sev][code] += n
		}
	}
}

// failures returns a deep copy of c's Fatal and Error counts alone.
func (c codeCounts) failures() codeCounts {
	var out codeCounts
	for sev, m := range c {
		if !sev.isFailure() {
			continue
		}
		for code, n := range m {
			out.addN(sev, code, n)
		}
	}
	return out
}

// clone deep-copies c, so a Result never shares a map with its Collector.
func (c codeCounts) clone() codeCounts {
	if c == nil {
		return nil
	}
	out := make(codeCounts, len(c))
	for sev, m := range c {
		out[sev] = maps.Clone(m)
	}
	return out
}

// newResult is the one constructor of a non-zero Result. The counts are the
// collector's, so they include issues dropped past its limit; arrival is the
// collection order that sorting issues discarded. The Result owns issues, so
// the caller passes a slice no other code holds.
func newResult(issues []Issue, limitReached bool, droppedCount int, counts SeverityCounts, codes codeCounts, arrival []uint64) Result {
	return Result{
		issues:       issues,
		limitReached: limitReached,
		droppedCount: droppedCount,
		counts:       counts,
		codeCounts:   codes,
		arrival:      arrival,
	}
}

// HasFatal reports whether the result saw a Fatal issue, stored or dropped.
func (r Result) HasFatal() bool {
	return r.counts.Fatal > 0
}

// HasWarnings reports whether the result saw a Warning issue, stored or
// dropped.
func (r Result) HasWarnings() bool {
	return r.counts.Warnings > 0
}

// HasCode reports whether any retained issue carries the given code, at any
// severity. Like [Result.Issues] and [Result.Len], it reflects the issues the
// result can enumerate: when the issue limit was reached ([Result.LimitReached]),
// a code present only among the dropped issues reads false here. Use the
// seen-based queries — [Result.CodeCounts] for a code, [Result.HasFatal] and
// [Result.SeverityCounts] for a severity — for gating that must stay truthful
// under truncation, and [Result.DroppedCount] to detect that the enumerable
// set is incomplete.
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
	return fmt.Sprintf("%d more issue(s) dropped at an issue limit; resolve issues and re-run to see the rest", r.droppedCount)
}

// SeverityCounts returns counts by severity level.
func (r Result) SeverityCounts() SeverityCounts {
	return r.counts
}

// CodeCounts returns, for the given severity, how many issues of each code
// were seen — stored and dropped alike, so it stays truthful under truncation
// where [Result.HasCode] does not. Severity is independent of code: one code
// can arrive at two severities and counts once under each. The map is a copy;
// a code never seen at that severity is absent. The total over severities is
// the number of issues seen with that code.
func (r Result) CodeCounts(sev Severity) map[Code]int {
	m := r.codeCounts[sev]
	if m == nil {
		return map[Code]int{}
	}
	return maps.Clone(m)
}

// Issues returns an iterator over all issues without copying.
//
// The yielded issues must not be mutated. Collect a mutable copy with
// slices.Collect(r.Issues()) if needed.
func (r Result) Issues() iter.Seq[Issue] {
	return slices.Values(r.issues)
}

// Errors returns an iterator over Fatal and Error issues.
func (r Result) Errors() iter.Seq[Issue] {
	return func(yield func(Issue) bool) {
		for _, issue := range r.issues {
			if issue.Severity().isFailure() {
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

// ResultError is a failure: the Fatal and Error issues a call drew, as one
// error value. [Collector.Err] builds it, and its Result holds failures alone.
// A caller reads it with errors.AsType[*diag.ResultError](err) and tests it by
// code with errors.Is(err, code).
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

// Is reports whether target is a [Code] of a Fatal or Error issue the failure
// saw, stored or dropped, so errors.Is(err, code) reads a failure by code. A
// Result built by hand reads by its failures too, never by a warning it holds.
func (e *ResultError) Is(target error) bool {
	code, ok := target.(Code)
	if !ok || code.IsZero() {
		return false
	}
	for sev, byCode := range e.Result.codeCounts {
		if sev.isFailure() && byCode[code] > 0 {
			return true
		}
	}
	return false
}

// LogValue implements [slog.LogValuer]. The group holds "code", the first
// failure's code when it has one; "counts", a group of "errors" (Fatal and
// Error) and "warnings", which is 0 for an error a [Collector] builds, since
// that holds failures alone; "limit_reached" and "dropped" when the Result was
// truncated; and "issues", one map per issue in [Issue.LogValue]'s shape. A
// release that changes the shape names the change, and a nil receiver returns
// an empty group.
func (e *ResultError) LogValue() slog.Value {
	if e == nil {
		return slog.GroupValue()
	}
	return slog.GroupValue(resultLogAttrs(e.Result)...)
}

// resultLogAttrs returns the attributes of r's log record, as
// [ResultError.LogValue] documents them. The issues are maps, not slog values,
// because a handler does not resolve a [slog.LogValuer] inside a slice and
// [slog.JSONHandler] then writes each map as a JSON object.
func resultLogAttrs(r Result) []slog.Attr {
	attrs := make([]slog.Attr, 0, 5)
	if code, ok := primaryErrorCode(r); ok {
		attrs = append(attrs, slog.String("code", code))
	}

	counts := r.SeverityCounts()
	attrs = append(attrs, slog.Group(
		"counts",
		slog.Int("errors", counts.Fatal+counts.Errors),
		slog.Int("warnings", counts.Warnings),
	))

	if r.LimitReached() {
		attrs = append(attrs,
			slog.Bool("limit_reached", true),
			slog.Int("dropped", r.DroppedCount()),
		)
	}

	issues := slices.Collect(r.Issues())
	issueMaps := make([]map[string]any, len(issues))
	for i, iss := range issues {
		issueMaps[i] = issueLogMap(iss)
	}
	return append(attrs, slog.Any("issues", issueMaps))
}

// primaryErrorCode returns the first failure's code string, or ("", false) when
// r holds no failure with a non-zero code.
func primaryErrorCode(r Result) (string, bool) {
	for issue := range r.Errors() {
		if !issue.Code().IsZero() {
			return issue.Code().String(), true
		}
	}
	return "", false
}

// String returns a minimal multi-line representation suitable for quick debugging.
//
// String returns "OK" only when the result holds no issue at all. Otherwise it
// returns a summary line — "OK" or the error count, then the warning, info and
// hint counts where non-zero, then the truncation note — followed by one line
// per retained issue at every severity (code and message, no excerpts). A control
// character other than a tab in a code or message is written as a visible glyph, as
// [Renderer.FormatResult] writes it, so each issue keeps its one line.
func (r Result) String() string {
	if len(r.issues) == 0 && !r.limitReached {
		return "OK"
	}

	var sb strings.Builder
	counts := r.SeverityCounts()

	// Summary line
	if failures := counts.Fatal + counts.Errors; failures == 0 {
		sb.WriteString("OK")
	} else {
		fmt.Fprintf(&sb, "%d error(s)", failures)
	}
	if counts.Warnings > 0 {
		fmt.Fprintf(&sb, ", %d warning(s)", counts.Warnings)
	}
	if counts.Info > 0 {
		fmt.Fprintf(&sb, ", %d info", counts.Info)
	}
	if counts.Hints > 0 {
		fmt.Fprintf(&sb, ", %d hint(s)", counts.Hints)
	}
	if r.limitReached {
		fmt.Fprintf(&sb, " [limit reached, %d dropped]", r.droppedCount)
	}
	for _, issue := range r.issues {
		fmt.Fprintf(&sb, "\n  %s: %s", termtext.String(issue.Code().String()), termtext.String(issue.Message()))
	}

	return sb.String()
}
