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

func TestResult_SeverityQueries(t *testing.T) {
	issues := []Issue{
		NewIssue(Fatal, E_LIMIT_REACHED, "limit").Build(),
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
	if !r.HasInfo() {
		t.Error("HasInfo() = false; want true")
	}
	if !r.HasHints() {
		t.Error("HasHints() = false; want true")
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
		NewIssue(Error, E_TYPE_COLLISION, "third").Build(),
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
		NewIssue(Fatal, E_LIMIT_REACHED, "fatal").Build(),
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

func TestResult_Warnings(t *testing.T) {
	issues := []Issue{
		NewIssue(Error, E_SYNTAX, "error").Build(),
		NewIssue(Warning, E_INVALID_NAME, "warning1").Build(),
		NewIssue(Warning, E_RESERVED_PREFIX, "warning2").Build(),
	}

	r := newResult(issues, 0, false, 0)

	var count int
	for issue := range r.Warnings() {
		if issue.Severity() != Warning {
			t.Errorf("Warnings() yielded %s issue", issue.Severity())
		}
		count++
	}

	if count != 2 {
		t.Errorf("Warnings() yielded %d; want 2", count)
	}
}

func TestResult_BySeverity(t *testing.T) {
	issues := []Issue{
		NewIssue(Fatal, E_LIMIT_REACHED, "fatal").Build(),
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

func TestResult_IssuesAtLeastAsSevereAs(t *testing.T) {
	issues := []Issue{
		NewIssue(Fatal, E_LIMIT_REACHED, "fatal").Build(),
		NewIssue(Error, E_SYNTAX, "error").Build(),
		NewIssue(Warning, E_INVALID_NAME, "warning").Build(),
		NewIssue(Info, E_INTERNAL, "info").Build(),
		NewIssue(Hint, E_INTERNAL, "hint").Build(),
	}

	r := newResult(issues, 0, false, 0)

	tests := []struct {
		threshold Severity
		wantCount int
	}{
		{Fatal, 1},   // Only Fatal
		{Error, 2},   // Fatal + Error
		{Warning, 3}, // Fatal + Error + Warning
		{Info, 4},    // Fatal + Error + Warning + Info
		{Hint, 5},    // All
	}

	for _, tt := range tests {
		t.Run(tt.threshold.String(), func(t *testing.T) {
			var count int
			for issue := range r.IssuesAtLeastAsSevereAs(tt.threshold) {
				if !issue.Severity().IsAtLeastAsSevereAs(tt.threshold) {
					t.Errorf("IssuesAtLeastAsSevereAs(%s) yielded %s issue",
						tt.threshold, issue.Severity())
				}
				count++
			}
			if count != tt.wantCount {
				t.Errorf("IssuesAtLeastAsSevereAs(%s) yielded %d; want %d",
					tt.threshold, count, tt.wantCount)
			}
		})
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
		NewIssue(Error, E_TYPE_COLLISION, "type collision").Build(),
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

// TestResult_IssuesAtLeastAsSevereAs_InvalidThreshold verifies that
// IssuesAtLeastAsSevereAs yields all issues when given an invalid severity
// threshold (> Hint).
func TestResult_IssuesAtLeastAsSevereAs_InvalidThreshold(t *testing.T) {
	issues := []Issue{
		NewIssue(Error, E_SYNTAX, "error").Build(),
		NewIssue(Warning, E_INVALID_NAME, "warning").Build(),
		NewIssue(Hint, E_INTERNAL, "hint").Build(),
	}
	r := newResult(issues, 0, false, 0)

	// Invalid threshold (Severity(255) is > Hint)
	invalidThreshold := Severity(255)

	iteratorCount := 0
	for range r.IssuesAtLeastAsSevereAs(invalidThreshold) {
		iteratorCount++
	}

	// All issues are "at least as severe" as an invalid threshold because
	// severity uses lower numeric values for higher severity.
	if iteratorCount != len(issues) {
		t.Errorf("iterator count = %d; want %d (all issues)", iteratorCount, len(issues))
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
		NewIssue(Fatal, E_LIMIT_REACHED, "limit reached").Build(),
	}
	r := newResult(issues, 0, false, 0)

	err := r.Err()
	if err == nil {
		t.Fatal("Err() = nil; want non-nil for result with fatal")
	}
}

func TestResult_Err_ErrorsAs(t *testing.T) {
	issues := []Issue{
		NewIssue(Error, E_TYPE_COLLISION, `type "Person" already defined`).Build(),
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
		NewIssue(Error, E_TYPE_COLLISION, "duplicate type").Build(),
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
