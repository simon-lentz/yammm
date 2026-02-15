package diag

import (
	"strings"
	"testing"
)

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

func TestResult_IssuesSlice_DeepCopy(t *testing.T) {
	issues := []Issue{
		NewIssue(Error, E_SYNTAX, "original").
			WithDetails(Detail{Key: DetailKeyTypeName, Value: "original"}).
			Build(),
	}

	r := newResult(issues, 0, false, 0)

	slice := r.IssuesSlice()

	// Modify returned slice's details (via the clone)
	details := slice[0].Details()
	details[0].Value = "modified"

	// Original should be unchanged
	for issue := range r.Issues() {
		issueDetails := issue.Details()
		if issueDetails[0].Value == "modified" {
			t.Error("IssuesSlice returned reference, not deep copy")
		}
	}
}

func TestResult_IssuesSlice_NilForEmpty(t *testing.T) {
	r := OK()

	if slice := r.IssuesSlice(); slice != nil {
		t.Error("IssuesSlice() should be nil for empty result")
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

func TestResult_ErrorsSlice(t *testing.T) {
	issues := []Issue{
		NewIssue(Fatal, E_LIMIT_REACHED, "fatal").Build(),
		NewIssue(Error, E_SYNTAX, "error").Build(),
		NewIssue(Warning, E_INVALID_NAME, "warning").Build(),
	}

	r := newResult(issues, 0, false, 0)

	slice := r.ErrorsSlice()
	if len(slice) != 2 {
		t.Fatalf("ErrorsSlice() len = %d; want 2", len(slice))
	}
}

func TestResult_ErrorsSlice_NilForEmpty(t *testing.T) {
	issues := []Issue{
		NewIssue(Warning, E_INVALID_NAME, "warning").Build(),
	}

	r := newResult(issues, 0, false, 0)

	if slice := r.ErrorsSlice(); slice != nil {
		t.Error("ErrorsSlice() should be nil when no errors")
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

func TestResult_WarningsSlice(t *testing.T) {
	issues := []Issue{
		NewIssue(Warning, E_INVALID_NAME, "warning1").Build(),
		NewIssue(Warning, E_RESERVED_PREFIX, "warning2").Build(),
	}

	r := newResult(issues, 0, false, 0)

	slice := r.WarningsSlice()
	if len(slice) != 2 {
		t.Fatalf("WarningsSlice() len = %d; want 2", len(slice))
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

func TestResult_BySeveritySlice(t *testing.T) {
	issues := []Issue{
		NewIssue(Error, E_SYNTAX, "error1").Build(),
		NewIssue(Error, E_TYPE_COLLISION, "error2").Build(),
	}

	r := newResult(issues, 0, false, 0)

	slice := r.BySeveritySlice(Error)
	if len(slice) != 2 {
		t.Fatalf("BySeveritySlice(Error) len = %d; want 2", len(slice))
	}

	// Warning slice should be nil
	if slice := r.BySeveritySlice(Warning); slice != nil {
		t.Error("BySeveritySlice(Warning) should be nil when no warnings")
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

func TestResult_IssuesAtLeastAsSevereAsSlice(t *testing.T) {
	issues := []Issue{
		NewIssue(Error, E_SYNTAX, "error").Build(),
		NewIssue(Warning, E_INVALID_NAME, "warning").Build(),
		NewIssue(Info, E_INTERNAL, "info").Build(),
	}

	r := newResult(issues, 0, false, 0)

	slice := r.IssuesAtLeastAsSevereAsSlice(Warning)
	if len(slice) != 2 {
		t.Fatalf("IssuesAtLeastAsSevereAsSlice(Warning) len = %d; want 2", len(slice))
	}

	// Fatal threshold with no fatal issues
	if slice := r.IssuesAtLeastAsSevereAsSlice(Fatal); slice != nil {
		t.Errorf("IssuesAtLeastAsSevereAsSlice(Fatal) = %v; want nil", slice)
	}
}

func TestResult_Messages(t *testing.T) {
	issues := []Issue{
		NewIssue(Fatal, E_LIMIT_REACHED, "fatal message").Build(),
		NewIssue(Error, E_SYNTAX, "error message").Build(),
		NewIssue(Warning, E_INVALID_NAME, "warning message").Build(),
	}

	r := newResult(issues, 0, false, 0)

	messages := r.Messages()
	if len(messages) != 2 {
		t.Fatalf("Messages() len = %d; want 2", len(messages))
	}
	if messages[0] != "fatal message" {
		t.Errorf("Messages()[0] = %q; want %q", messages[0], "fatal message")
	}
	if messages[1] != "error message" {
		t.Errorf("Messages()[1] = %q; want %q", messages[1], "error message")
	}
}

func TestResult_Messages_NilForEmpty(t *testing.T) {
	issues := []Issue{
		NewIssue(Warning, E_INVALID_NAME, "warning").Build(),
	}

	r := newResult(issues, 0, false, 0)

	if messages := r.Messages(); messages != nil {
		t.Error("Messages() should be nil when no errors")
	}
}

func TestResult_MessagesAtOrAbove(t *testing.T) {
	issues := []Issue{
		NewIssue(Error, E_SYNTAX, "error").Build(),
		NewIssue(Warning, E_INVALID_NAME, "warning").Build(),
		NewIssue(Info, E_INTERNAL, "info").Build(),
	}

	r := newResult(issues, 0, false, 0)

	messages := r.MessagesAtOrAbove(Warning)
	if len(messages) != 2 {
		t.Fatalf("MessagesAtOrAbove(Warning) len = %d; want 2", len(messages))
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

	// Verify returned slices are independent
	issues := []Issue{
		NewIssue(Error, E_SYNTAX, "test").Build(),
	}
	r = newResult(issues, 0, false, 0)

	slice1 := r.IssuesSlice()
	slice2 := r.IssuesSlice()

	if len(slice1) == 0 {
		t.Fatal("IssuesSlice returned empty")
	}

	// The slices should be independent
	if &slice1[0] == &slice2[0] {
		t.Error("IssuesSlice returned same backing array")
	}
}

// TestResult_IssuesAtLeastAsSevereAs_InvalidThreshold verifies that
// IssuesAtLeastAsSevereAs and IssuesAtLeastAsSevereAsSlice behave consistently
// when given an invalid severity threshold (> Hint).
func TestResult_IssuesAtLeastAsSevereAs_InvalidThreshold(t *testing.T) {
	issues := []Issue{
		NewIssue(Error, E_SYNTAX, "error").Build(),
		NewIssue(Warning, E_INVALID_NAME, "warning").Build(),
		NewIssue(Hint, E_INTERNAL, "hint").Build(),
	}
	r := newResult(issues, 0, false, 0)

	// Invalid threshold (Severity(255) is > Hint)
	invalidThreshold := Severity(255)

	// Count via iterator
	iteratorCount := 0
	for range r.IssuesAtLeastAsSevereAs(invalidThreshold) {
		iteratorCount++
	}

	// Count via slice
	slice := r.IssuesAtLeastAsSevereAsSlice(invalidThreshold)
	sliceCount := len(slice)

	// Both should return all issues (any valid severity is "at least as severe"
	// as an invalid threshold because severity uses lower numeric values for
	// higher severity)
	if iteratorCount != len(issues) {
		t.Errorf("iterator count = %d; want %d (all issues)", iteratorCount, len(issues))
	}
	if sliceCount != len(issues) {
		t.Errorf("slice count = %d; want %d (all issues)", sliceCount, len(issues))
	}

	// Iterator and slice should match
	if iteratorCount != sliceCount {
		t.Errorf("iterator count (%d) != slice count (%d); should be consistent",
			iteratorCount, sliceCount)
	}
}
