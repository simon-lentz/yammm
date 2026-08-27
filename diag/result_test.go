package diag

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

func TestResult_TruncationNote(t *testing.T) {
	c := NewCollector(2)
	c.Collect(NewIssue(Error, E_SYNTAX, "a").Build())
	c.Collect(NewIssue(Error, E_SYNTAX, "b").Build())
	c.Collect(NewIssue(Error, E_SYNTAX, "c").Build()) // dropped past the limit

	note := c.Result().TruncationNote()
	if !strings.Contains(note, "1 more issue(s) dropped after reaching the 2-issue limit") {
		t.Errorf("TruncationNote() = %q; want the dropped-count/limit summary", note)
	}

	if got := OK().TruncationNote(); got != "" {
		t.Errorf("TruncationNote() on a non-truncated result = %q; want empty", got)
	}
}

func TestResult_TruncationNote_MergedIntoUnlimited_OmitsBogusLimit(t *testing.T) {
	// Merging a truncated result into an unlimited collector carries the
	// truncation state forward but not a positive limit, so the note must
	// report the dropped count without naming a nonsensical "0-issue limit".
	src := NewCollector(1)
	src.Collect(NewIssue(Warning, E_SYNTAX, "fills the limit").Build())
	src.Collect(NewIssue(Error, E_SYNTAX, "dropped but real").Build())

	dst := NewCollectorUnlimited()
	dst.Merge(src.Result())
	r := dst.Result()

	if !r.LimitReached() || r.DroppedCount() != 1 {
		t.Fatalf("setup: LimitReached=%v DroppedCount=%d; want true/1", r.LimitReached(), r.DroppedCount())
	}
	note := r.TruncationNote()
	if strings.Contains(note, "0-issue") {
		t.Errorf("TruncationNote() = %q; must not name a 0-issue limit after merge into an unlimited collector", note)
	}
	if !strings.Contains(note, "1 more issue(s) dropped") {
		t.Errorf("TruncationNote() = %q; want the dropped-count summary", note)
	}
}

func TestOK(t *testing.T) {
	r := OK()

	if !r.OK() {
		t.Error("OK().OK() = false; want true")
	}
	if r.HasErrors() {
		t.Error("OK().HasErrors() = true; want false")
	}
	if r.Len() != 0 {
		t.Errorf("OK().Len() = %d; want 0", r.Len())
	}
	if r.LimitReached() {
		t.Error("OK().LimitReached() = true; want false")
	}
	if r.DroppedCount() != 0 {
		t.Errorf("OK().DroppedCount() = %d; want 0", r.DroppedCount())
	}
}

func TestResult_HasCode(t *testing.T) {
	issues := []Issue{
		NewIssue(Error, E_SYNTAX, "error").Build(),
		NewIssue(Warning, E_INVALID_NAME, "warning").Build(),
	}
	r := newResult(issues, 0, false, 0)

	if !r.HasCode(E_SYNTAX) {
		t.Error("HasCode(E_SYNTAX) = false; want true")
	}
	if !r.HasCode(E_INVALID_NAME) {
		t.Error("HasCode(E_INVALID_NAME) = false; want true (any severity matches)")
	}
	if r.HasCode(E_INTERNAL) {
		t.Error("HasCode(E_INTERNAL) = true; want false")
	}
	if OK().HasCode(E_SYNTAX) {
		t.Error("empty result HasCode(E_SYNTAX) = true; want false")
	}
}

func TestResult_HasCode_ReflectsRetainedIssuesUnderTruncation(t *testing.T) {
	// HasCode is an enumeration query (like Issues/Len): it reflects the issues
	// the result can enumerate, so a code present only among issues dropped past
	// the limit reads false. The seen-based severity gate stays truthful — a
	// dropped Error still fails OK()/HasErrors() — so the two families are
	// deliberately distinct, not in conflict.
	//
	// Both issues are Errors so the second is genuinely dropped: an incoming
	// issue only displaces a stored one when it is MORE severe (see
	// Collector.storeLocked), and equal severity does not displace.
	c := NewCollector(1)
	c.Collect(NewIssue(Error, E_SYNTAX, "fills the limit").Build())
	c.Collect(NewIssue(Error, E_INTERNAL, "dropped past the limit").Build())
	r := c.Result()

	if !r.LimitReached() || r.DroppedCount() != 1 {
		t.Fatalf("setup: LimitReached=%v DroppedCount=%d; want true/1", r.LimitReached(), r.DroppedCount())
	}
	// The dropped Error's code is not enumerable, so HasCode reports false...
	if r.HasCode(E_INTERNAL) {
		t.Error("HasCode(E_INTERNAL) = true; want false (the issue was dropped past the limit)")
	}
	// ...while the seen-based gate still counts it.
	if r.OK() || !r.HasErrors() {
		t.Errorf("OK=%v HasErrors=%v; want false/true — the dropped Error must still fail the gate", r.OK(), r.HasErrors())
	}
	if got := r.SeverityCounts().Errors; got != 2 {
		t.Errorf("SeverityCounts().Errors = %d; want 2 — the dropped Error is still an Error the collector saw", got)
	}
	// The retained issue's code is enumerable.
	if !r.HasCode(E_SYNTAX) {
		t.Error("HasCode(E_SYNTAX) = false; want true (the retained issue)")
	}
}

// A flood of warnings must not starve the errors that explain a failure: once
// the budget is full, an incoming Error displaces a stored Warning. Schema
// completion collects W_ANNOTATION_SHADOWED during linearization — ahead of
// every error-producing phase — so severity-blind truncation left a failing load
// reporting only warnings, with nothing naming what broke it.
func TestCollector_LimitRetainsMostSevere(t *testing.T) {
	c := NewCollector(3)
	for range 5 {
		c.Collect(NewIssue(Warning, E_SYNTAX, "shadowed annotation").Build())
	}
	c.Collect(NewIssue(Error, E_INTERNAL, "the error that explains the failure").Build())
	r := c.Result()

	if !r.HasCode(E_INTERNAL) {
		t.Error("the Error must be retained in place of a Warning, not dropped at the cap")
	}
	if got := r.Len(); got != 3 {
		t.Errorf("Len() = %d; want 3 — the cap still bounds storage", got)
	}
	// 6 issues seen, 3 retained: the 3 not retained are dropped, whether the
	// incoming one was rejected or a stored one was evicted for it.
	if got := r.DroppedCount(); got != 3 {
		t.Errorf("DroppedCount() = %d; want 3 (seen minus retained)", got)
	}
	if got := r.SeverityCounts(); got.Warnings != 5 || got.Errors != 1 {
		t.Errorf("SeverityCounts() = %+v; want 5 warnings / 1 error seen", got)
	}
}

// Eviction keeps the earliest-arrived issues of a given severity, so a caller
// that reads a truncated result sees the first errors reported, not the last.
func TestCollector_LimitEvictsLatestOfLeastSevere(t *testing.T) {
	c := NewCollector(2)
	c.Collect(NewIssue(Warning, E_SYNTAX, "first warning").Build())
	c.Collect(NewIssue(Warning, E_INVALID_NAME, "second warning").Build())
	c.Collect(NewIssue(Error, E_INTERNAL, "error displaces the LAST warning").Build())
	r := c.Result()

	if !r.HasCode(E_SYNTAX) {
		t.Error("the first warning should survive; eviction takes the latest of the least severe")
	}
	if r.HasCode(E_INVALID_NAME) {
		t.Error("the second warning should have been evicted")
	}
	if !r.HasCode(E_INTERNAL) {
		t.Error("the error should be retained")
	}
}

// Merge routes through the same storage path, so merging an error-bearing result
// into a warning-saturated collector retains the errors.
func TestCollector_MergeRetainsMostSevere(t *testing.T) {
	full := NewCollector(2)
	full.Collect(NewIssue(Warning, E_SYNTAX, "w1").Build())
	full.Collect(NewIssue(Warning, E_SYNTAX, "w2").Build())

	errs := NewCollectorUnlimited()
	errs.Collect(NewIssue(Error, E_INTERNAL, "the real failure").Build())

	full.Merge(errs.Result())
	r := full.Result()

	if !r.HasCode(E_INTERNAL) {
		t.Error("a merged Error must displace a stored Warning at the limit")
	}
}

func TestResult_SeverityQueries(t *testing.T) {
	issues := []Issue{
		NewIssue(Fatal, E_INTERNAL, "limit").Build(),
		NewIssue(Error, E_SYNTAX, "error").Build(),
		NewIssue(Warning, E_INVALID_NAME, "warning").Build(),
		NewIssue(Info, E_INTERNAL, "info").Build(),
		NewIssue(Hint, E_INTERNAL, "hint").Build(),
	}

	r := newResult(issues, 0, false, 0)

	if r.OK() {
		t.Error("OK() = true; want false (has fatal and error)")
	}
	if !r.HasFatal() {
		t.Error("HasFatal() = false; want true")
	}
	if !r.HasErrors() {
		t.Error("HasErrors() = false; want true")
	}
	if !r.HasWarnings() {
		t.Error("HasWarnings() = false; want true")
	}

	counts := r.SeverityCounts()
	if counts.Fatal != 1 {
		t.Errorf("SeverityCounts().Fatal = %d; want 1", counts.Fatal)
	}
	if counts.Errors != 1 {
		t.Errorf("SeverityCounts().Errors = %d; want 1", counts.Errors)
	}
	if counts.Warnings != 1 {
		t.Errorf("SeverityCounts().Warnings = %d; want 1", counts.Warnings)
	}
	if counts.Info != 1 {
		t.Errorf("SeverityCounts().Info = %d; want 1", counts.Info)
	}
	if counts.Hints != 1 {
		t.Errorf("SeverityCounts().Hints = %d; want 1", counts.Hints)
	}
}

func TestResult_OKWithWarnings(t *testing.T) {
	issues := []Issue{
		NewIssue(Warning, E_INVALID_NAME, "warning").Build(),
		NewIssue(Info, E_INTERNAL, "info").Build(),
	}

	r := newResult(issues, 0, false, 0)

	// Result should be OK because there are no Fatal or Error issues
	if !r.OK() {
		t.Error("OK() = false; want true (only warnings)")
	}
	if r.HasErrors() {
		t.Error("HasErrors() = true; want false (only warnings)")
	}
}

func TestResult_LimitTracking(t *testing.T) {
	issues := []Issue{
		NewIssue(Error, E_SYNTAX, "error").Build(),
	}

	r := newResult(issues, 10, true, 5)

	if !r.LimitReached() {
		t.Error("LimitReached() = false; want true")
	}
	if r.DroppedCount() != 5 {
		t.Errorf("DroppedCount() = %d; want 5", r.DroppedCount())
	}
}

func TestResult_Issues_Iterator(t *testing.T) {
	issues := []Issue{
		NewIssue(Error, E_SYNTAX, "first").Build(),
		NewIssue(Warning, E_INVALID_NAME, "second").Build(),
		NewIssue(Error, E_DUPLICATE_TYPE, "third").Build(),
	}

	r := newResult(issues, 0, false, 0)

	var count int
	var messages []string
	for issue := range r.Issues() {
		count++
		messages = append(messages, issue.Message())
	}

	if count != 3 {
		t.Errorf("Issues() yielded %d; want 3", count)
	}
	if messages[0] != "first" || messages[1] != "second" || messages[2] != "third" {
		t.Errorf("Issues() order wrong: %v", messages)
	}
}

func TestResult_Issues_EarlyBreak(t *testing.T) {
	issues := []Issue{
		NewIssue(Error, E_SYNTAX, "first").Build(),
		NewIssue(Error, E_SYNTAX, "second").Build(),
		NewIssue(Error, E_SYNTAX, "third").Build(),
	}

	r := newResult(issues, 0, false, 0)

	var count int
	for range r.Issues() {
		count++
		if count == 2 {
			break
		}
	}

	if count != 2 {
		t.Errorf("early break yielded %d; want 2", count)
	}
}

func TestResult_Errors(t *testing.T) {
	issues := []Issue{
		NewIssue(Fatal, E_INTERNAL, "fatal").Build(),
		NewIssue(Error, E_SYNTAX, "error").Build(),
		NewIssue(Warning, E_INVALID_NAME, "warning").Build(),
	}

	r := newResult(issues, 0, false, 0)

	var count int
	for issue := range r.Errors() {
		if !issue.Severity().IsFailure() {
			t.Errorf("Errors() yielded %s issue", issue.Severity())
		}
		count++
	}

	if count != 2 {
		t.Errorf("Errors() yielded %d; want 2", count)
	}
}

func TestResult_BySeverity(t *testing.T) {
	issues := []Issue{
		NewIssue(Fatal, E_INTERNAL, "fatal").Build(),
		NewIssue(Error, E_SYNTAX, "error").Build(),
		NewIssue(Warning, E_INVALID_NAME, "warning").Build(),
		NewIssue(Info, E_INTERNAL, "info").Build(),
		NewIssue(Hint, E_INTERNAL, "hint").Build(),
	}

	r := newResult(issues, 0, false, 0)

	for _, sev := range []Severity{Fatal, Error, Warning, Info, Hint} {
		var count int
		for issue := range r.BySeverity(sev) {
			if issue.Severity() != sev {
				t.Errorf("BySeverity(%s) yielded %s issue", sev, issue.Severity())
			}
			count++
		}
		if count != 1 {
			t.Errorf("BySeverity(%s) yielded %d; want 1", sev, count)
		}
	}
}

func TestResult_String_OK(t *testing.T) {
	r := OK()

	if s := r.String(); s != "OK" {
		t.Errorf("String() = %q; want %q", s, "OK")
	}
}

func TestResult_String_WithErrors(t *testing.T) {
	issues := []Issue{
		NewIssue(Error, E_SYNTAX, "syntax error").Build(),
		NewIssue(Error, E_DUPLICATE_TYPE, "type collision").Build(),
	}

	r := newResult(issues, 0, false, 0)

	s := r.String()
	if !strings.Contains(s, "2 error(s)") {
		t.Errorf("String() should contain error count: %q", s)
	}
	if !strings.Contains(s, "E_SYNTAX") {
		t.Errorf("String() should contain error code: %q", s)
	}
}

func TestResult_String_WithLimitReached(t *testing.T) {
	issues := []Issue{
		NewIssue(Error, E_SYNTAX, "error").Build(),
	}

	r := newResult(issues, 10, true, 5)

	s := r.String()
	if !strings.Contains(s, "limit reached") {
		t.Errorf("String() should contain limit info: %q", s)
	}
	if !strings.Contains(s, "5 dropped") {
		t.Errorf("String() should contain dropped count: %q", s)
	}
}

func TestResult_Immutability(t *testing.T) {
	// Result should not be constructable with arbitrary issues via public API
	// This is verified by the fact that newResult is unexported

	// The only public ways to get a Result are:
	// 1. OK() - returns empty result
	// 2. Collector.Result() - validates during collection

	r := OK()
	if !r.OK() {
		t.Error("OK() should return OK result")
	}

	// Verify collected slices are independent (each slices.Collect allocates).
	issues := []Issue{
		NewIssue(Error, E_SYNTAX, "test").Build(),
	}
	r = newResult(issues, 0, false, 0)

	slice1 := slices.Collect(r.Issues())
	slice2 := slices.Collect(r.Issues())

	if len(slice1) == 0 {
		t.Fatal("Issues() yielded nothing")
	}

	// The slices should be independent
	if &slice1[0] == &slice2[0] {
		t.Error("slices.Collect returned same backing array")
	}
}

func TestResult_Err_NilWhenOK(t *testing.T) {
	r := OK()
	if err := r.Err(); err != nil {
		t.Errorf("OK().Err() = %v; want nil", err)
	}
}

func TestResult_Err_NilWhenWarningsOnly(t *testing.T) {
	issues := []Issue{
		NewIssue(Warning, E_INVALID_NAME, "name looks odd").Build(),
	}
	r := newResult(issues, 0, false, 0)

	if !r.OK() {
		t.Fatal("result with only warnings should be OK")
	}
	if err := r.Err(); err != nil {
		t.Errorf("Err() = %v; want nil for warnings-only result", err)
	}
}

func TestResult_Err_NonNilOnError(t *testing.T) {
	issues := []Issue{
		NewIssue(Error, E_SYNTAX, "unexpected token").Build(),
	}
	r := newResult(issues, 0, false, 0)

	err := r.Err()
	if err == nil {
		t.Fatal("Err() = nil; want non-nil for result with errors")
	}

	// Error() string should contain the issue message
	if !strings.Contains(err.Error(), "unexpected token") {
		t.Errorf("Error() = %q; want it to contain %q", err.Error(), "unexpected token")
	}
}

func TestResult_Err_NonNilOnFatal(t *testing.T) {
	issues := []Issue{
		NewIssue(Fatal, E_INTERNAL, "limit reached").Build(),
	}
	r := newResult(issues, 0, false, 0)

	err := r.Err()
	if err == nil {
		t.Fatal("Err() = nil; want non-nil for result with fatal")
	}
}

func TestResult_Err_ErrorsAs(t *testing.T) {
	issues := []Issue{
		NewIssue(Error, E_DUPLICATE_TYPE, `type "Person" already defined`).Build(),
		NewIssue(Warning, E_INVALID_NAME, "name looks odd").Build(),
	}
	r := newResult(issues, 0, false, 0)

	err := r.Err()
	if err == nil {
		t.Fatal("Err() = nil; want non-nil")
	}

	re, ok := errors.AsType[*ResultError](err)
	if !ok {
		t.Fatal("errors.AsType[*ResultError] = false; want true")
	}

	// Verify the Result is accessible and intact
	if re.Result.Len() != 2 {
		t.Errorf("ResultError.Result.Len() = %d; want 2", re.Result.Len())
	}
	if re.Result.OK() {
		t.Error("ResultError.Result.OK() = true; want false")
	}
}

func TestResult_Err_WrappableWithFmtErrorf(t *testing.T) {
	issues := []Issue{
		NewIssue(Error, E_SYNTAX, "unexpected token").Build(),
	}
	r := newResult(issues, 0, false, 0)

	wrapped := errors.Join(errors.New("schema validation failed"), r.Err())

	if _, ok := errors.AsType[*ResultError](wrapped); !ok {
		t.Fatal("errors.AsType[*ResultError] through wrapped error = false; want true")
	}
}

func TestResult_Err_MatchesString(t *testing.T) {
	issues := []Issue{
		NewIssue(Error, E_SYNTAX, "unexpected token").Build(),
		NewIssue(Error, E_DUPLICATE_TYPE, "duplicate type").Build(),
	}
	r := newResult(issues, 0, false, 0)

	err := r.Err()
	if err == nil {
		t.Fatal("Err() = nil; want non-nil")
	}

	// Error() output should match Result.String()
	if err.Error() != r.String() {
		t.Errorf("Error() = %q; want %q (same as String())", err.Error(), r.String())
	}
}

// The retained set must follow arrival order across MORE than one eviction.
// After the first eviction the victim slot is overwritten in place, so slice
// position no longer says which issue arrived first; a victim chosen by slot
// position then evicts the wrong one. Two warnings, two errors and a fatal
// through a two-slot collector must keep the FIRST error, not the second.
func TestCollector_LimitEvictsLatestAcrossRepeatedEvictions(t *testing.T) {
	c := NewCollector(2)
	c.Collect(NewIssue(Warning, E_SYNTAX, "W1").Build())
	c.Collect(NewIssue(Warning, E_SYNTAX, "W2").Build())
	c.Collect(NewIssue(Error, E_INTERNAL, "E1").Build())
	c.Collect(NewIssue(Error, E_INTERNAL, "E2").Build())
	c.Collect(NewIssue(Fatal, E_INTERNAL, "F1").Build())

	got := retainedMessages(c)
	want := []string{"E1", "F1"}
	if !slices.Equal(got, want) {
		t.Errorf("retained messages = %v, want %v (earliest-arrived of each severity survives)", got, want)
	}
}

// The same rule must hold when the evicting issues arrive through Merge, which
// shares the storage path.
func TestCollector_MergeEvictsLatestAcrossRepeatedEvictions(t *testing.T) {
	c := NewCollector(2)
	c.Collect(NewIssue(Warning, E_SYNTAX, "W1").Build())
	c.Collect(NewIssue(Warning, E_SYNTAX, "W2").Build())

	errs := NewCollectorUnlimited()
	errs.Collect(NewIssue(Error, E_INTERNAL, "E1").Build())
	errs.Collect(NewIssue(Error, E_INTERNAL, "E2").Build())
	c.Merge(errs.Result())

	fatal := NewCollectorUnlimited()
	fatal.Collect(NewIssue(Fatal, E_INTERNAL, "F1").Build())
	c.Merge(fatal.Result())

	got := retainedMessages(c)
	want := []string{"E1", "F1"}
	if !slices.Equal(got, want) {
		t.Errorf("retained messages = %v, want %v", got, want)
	}
}

// retainedMessages returns the stored issues' messages sorted, so a test can
// assert the retained SET without depending on Result's severity ordering.
func retainedMessages(c *Collector) []string {
	var got []string
	for iss := range c.Result().Issues() {
		got = append(got, iss.Message())
	}
	slices.Sort(got)
	return got
}
