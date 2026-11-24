package diag_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/internal/yammmtest/sourcetest"
	"github.com/simon-lentz/yammm/location"
)

// issueAt builds an issue of sev and code at line, so the sorted order of a
// Result differs from the order the issues arrive in.
func issueAt(sev diag.Severity, code diag.Code, line int, msg string) diag.Issue {
	pos := location.Position{Line: line, Column: 1}
	return diag.NewIssue(sev, code, msg).
		WithSpan(location.Span{Source: sourcetest.ID("test://route.yammm"), Start: pos, End: pos}).
		Build()
}

func messages(issues []diag.Issue) []string {
	out := make([]string, len(issues))
	for i, issue := range issues {
		out[i] = issue.Message()
	}
	return out
}

func TestCollectorErr_NilWithoutAFailure(t *testing.T) {
	t.Parallel()
	if err := diag.NewCollector(diag.NoLimit).Err(); err != nil {
		t.Errorf("empty collector: Err() = %v; want nil", err)
	}
	c := diag.NewCollector(diag.NoLimit)
	c.Collect(diag.NewIssue(diag.Warning, testCodeWarning, "a warning").Build())
	c.Collect(diag.NewIssue(diag.Info, testCodeWarning, "a note").Build())
	c.Collect(diag.NewIssue(diag.Hint, testCodeWarning, "a hint").Build())
	if err := c.Err(); err != nil {
		t.Errorf("warnings only: Err() = %v; want nil", err)
	}
}

func TestCollectorErr_HoldsFailuresAlone(t *testing.T) {
	t.Parallel()
	// A limit of 3 keeps three of the four errors and evicts both warnings, so
	// the collector drops three issues and the failure drops one.
	c := diag.NewCollector(3)
	c.Collect(issueAt(diag.Warning, testCodeWarning, 1, "w1"))
	c.Collect(issueAt(diag.Warning, testCodeWarning, 2, "w2"))
	c.Collect(issueAt(diag.Error, diag.E_SYNTAX, 3, "e1"))
	c.Collect(issueAt(diag.Fatal, diag.E_INTERNAL, 4, "f1"))
	c.Collect(issueAt(diag.Error, diag.E_SYNTAX, 5, "e2"))
	c.Collect(issueAt(diag.Error, diag.E_UNKNOWN_TYPE, 6, "e3"))
	if got := c.Result().DroppedCount(); got != 3 {
		t.Fatalf("collector DroppedCount() = %d; want 3", got)
	}

	re, ok := errors.AsType[*diag.ResultError](c.Err())
	if !ok {
		t.Fatalf("Err() = %v; want a *diag.ResultError", c.Err())
	}
	res := re.Result
	if got, want := messages(slices.Collect(res.Issues())), []string{"e1", "f1", "e2"}; !slices.Equal(got, want) {
		t.Errorf("issues = %q; want %q", got, want)
	}
	if got, want := res.SeverityCounts(), (diag.SeverityCounts{Fatal: 1, Errors: 3}); got != want {
		t.Errorf("SeverityCounts() = %+v; want %+v", got, want)
	}
	if !res.LimitReached() || res.DroppedCount() != 1 {
		t.Errorf("LimitReached() = %t, DroppedCount() = %d; want true, 1 (four failures seen, three stored)",
			res.LimitReached(), res.DroppedCount())
	}
	if got := res.CodeCounts(diag.Warning); len(got) != 0 {
		t.Errorf("CodeCounts(Warning) = %v; want none", got)
	}
	if got, want := res.CodeCounts(diag.Error), map[diag.Code]int{diag.E_SYNTAX: 2, diag.E_UNKNOWN_TYPE: 1}; !maps.Equal(got, want) {
		t.Errorf("CodeCounts(Error) = %v; want %v", got, want)
	}
	if res.TruncationNote() == "" {
		t.Error("TruncationNote() is empty for a failure that dropped one")
	}
}

func TestCollectorErr_NoDropWhenOnlyWarningsWereDropped(t *testing.T) {
	t.Parallel()
	c := diag.NewCollector(1)
	c.Collect(diag.NewIssue(diag.Warning, testCodeWarning, "w1").Build())
	c.Collect(diag.NewIssue(diag.Error, diag.E_SYNTAX, "e1").Build())
	c.Collect(diag.NewIssue(diag.Warning, testCodeWarning, "w2").Build())

	re, ok := errors.AsType[*diag.ResultError](c.Err())
	if !ok {
		t.Fatalf("Err() = %v; want a *diag.ResultError", c.Err())
	}
	if re.Result.LimitReached() || re.Result.DroppedCount() != 0 || re.Result.Len() != 1 {
		t.Errorf("LimitReached() = %t, DroppedCount() = %d, Len() = %d; want false, 0, 1",
			re.Result.LimitReached(), re.Result.DroppedCount(), re.Result.Len())
	}
}

func TestCollectorErr_MergesBackInArrivalOrder(t *testing.T) {
	t.Parallel()
	// e2 sorts before e1 and arrives after it; a collector of limit 1 that
	// merges the failure keeps e1, the first to arrive.
	c := diag.NewCollector(diag.NoLimit)
	c.Collect(issueAt(diag.Error, diag.E_SYNTAX, 9, "e1"))
	c.Collect(issueAt(diag.Error, diag.E_SYNTAX, 1, "e2"))
	re, _ := errors.AsType[*diag.ResultError](c.Err())

	into := diag.NewCollector(1)
	into.Merge(re.Result)
	if got := messages(slices.Collect(into.Result().Issues())); !slices.Equal(got, []string{"e1"}) {
		t.Errorf("merged issues = %q; want [e1]", got)
	}
}

func TestResultErrorIs_ReadsTheSeenCodes(t *testing.T) {
	t.Parallel()
	c := diag.NewCollector(1)
	c.Collect(diag.NewIssue(diag.Error, diag.E_SYNTAX, "stored").Build())
	c.Collect(diag.NewIssue(diag.Error, diag.E_UNKNOWN_TYPE, "dropped").Build())
	c.Collect(diag.NewIssue(diag.Warning, testCodeWarning, "a warning").Build())
	err := c.Err()

	re, _ := errors.AsType[*diag.ResultError](err)
	if re.Result.Len() != 1 || !re.Result.HasCode(diag.E_SYNTAX) {
		t.Fatalf("failure = %v; want E_SYNTAX stored alone", re.Result)
	}
	wrapped := fmt.Errorf("schema_load: %w", err)
	for _, tc := range []struct {
		code diag.Code
		want bool
	}{
		{diag.E_SYNTAX, true},
		{diag.E_UNKNOWN_TYPE, true},
		{testCodeWarning, false},
		{diag.E_INTERNAL, false},
		{diag.Code{}, false},
	} {
		if got := errors.Is(err, tc.code); got != tc.want {
			t.Errorf("errors.Is(err, %v) = %t; want %t", tc.code, got, tc.want)
		}
		if got := errors.Is(wrapped, tc.code); got != tc.want {
			t.Errorf("errors.Is(wrapped, %v) = %t; want %t", tc.code, got, tc.want)
		}
	}
	if errors.Is(err, errors.New("E_SYNTAX")) {
		t.Error("errors.Is matched an error that is not a Code")
	}
}

func TestCodeError_IsTheCodeName(t *testing.T) {
	t.Parallel()
	if got := diag.E_SYNTAX.Error(); got != "E_SYNTAX" {
		t.Errorf("E_SYNTAX.Error() = %q; want %q", got, "E_SYNTAX")
	}
}

func TestResultErrorLogValue_CarriesEveryKeyButContext(t *testing.T) {
	t.Parallel()
	c := diag.NewCollector(2)
	c.Collect(issueAt(diag.Error, diag.E_SYNTAX, 1, "first"))
	c.Collect(issueAt(diag.Fatal, diag.E_INTERNAL, 2, "second"))
	c.Collect(issueAt(diag.Error, diag.E_UNKNOWN_TYPE, 3, "third"))
	re, _ := errors.AsType[*diag.ResultError](c.Err())

	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Error("fail", slog.Any("diagnostic", re))
	var out map[string]any
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal log: %v\nraw: %s", err, buf.String())
	}
	got, ok := out["diagnostic"].(map[string]any)
	if !ok {
		t.Fatalf("diagnostic attribute = %v; want a group", out["diagnostic"])
	}
	gotJSON, _ := json.Marshal(got)
	for _, key := range []string{"code", "counts", "limit_reached", "dropped", "issues"} {
		if _, ok := got[key]; !ok {
			t.Errorf("tree lacks %q: %s", key, gotJSON)
		}
	}
	if _, ok := got["context"]; ok {
		t.Errorf("tree carries context: %s", gotJSON)
	}

	var nilErr *diag.ResultError
	if v := nilErr.LogValue(); v.Kind() != slog.KindGroup || len(v.Group()) != 0 {
		t.Errorf("nil receiver LogValue() = %v; want an empty group", v)
	}
}

// TestCollectorErr_ACollectAfterwardLeavesTheErrorAlone pins that an error
// shares no count with its collector: a failure collected after Err returns
// changes neither the error's codes nor its counts.
func TestCollectorErr_ACollectAfterwardLeavesTheErrorAlone(t *testing.T) {
	t.Parallel()
	c := diag.NewCollector(diag.NoLimit)
	c.Collect(diag.NewIssue(diag.Error, diag.E_SYNTAX, "first").Build())
	err := c.Err()
	c.Collect(diag.NewIssue(diag.Error, diag.E_UNKNOWN_TYPE, "second").Build())
	c.Collect(diag.NewIssue(diag.Error, diag.E_SYNTAX, "third").Build())
	if errors.Is(err, diag.E_UNKNOWN_TYPE) {
		t.Error("the error reads a code collected after it was built")
	}
	re, _ := errors.AsType[*diag.ResultError](err)
	if got := re.Result.CodeCounts(diag.Error); len(got) != 1 || got[diag.E_SYNTAX] != 1 {
		t.Errorf("CodeCounts(Error) = %v; want E_SYNTAX once", got)
	}
}

// TestResultError_AFatalAloneRendersAsAFailure pins that the text of an error
// holding one Fatal counts it, never reading "OK".
func TestResultError_AFatalAloneRendersAsAFailure(t *testing.T) {
	t.Parallel()
	c := diag.NewCollector(diag.NoLimit)
	c.Collect(diag.NewIssue(diag.Fatal, diag.E_INTERNAL, "boom").Build())
	if got, want := c.Err().Error(), "1 error(s)\n  E_INTERNAL: boom"; got != want {
		t.Errorf("Error() = %q; want %q", got, want)
	}
}

// TestResultErrorIs_ReadsAHandBuiltResultByItsFailures pins that a ResultError
// built around a whole Result reads by its failures alone: a warning it holds
// is no failure.
func TestResultErrorIs_ReadsAHandBuiltResultByItsFailures(t *testing.T) {
	t.Parallel()
	c := diag.NewCollector(diag.NoLimit)
	c.Collect(diag.NewIssue(diag.Warning, diag.W_SNAPSHOT_PATH_EXTENSION, "w").Build())
	c.Collect(diag.NewIssue(diag.Error, diag.E_SYNTAX, "e").Build())
	err := error(&diag.ResultError{Result: c.Result()})
	if errors.Is(err, diag.W_SNAPSHOT_PATH_EXTENSION) {
		t.Error("errors.Is reads a warning's code as a failure")
	}
	if !errors.Is(err, diag.E_SYNTAX) {
		t.Error("errors.Is misses the failure's code")
	}
}
