package cli

import (
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/simon-lentz/yammm/diag"
)

// TestFailureIssues pins the three readings of a call's error: nil is the
// empty result, a [diag.ResultError] is the result it holds, and any other
// error is one Fatal E_INTERNAL carrying the error's text.
func TestFailureIssues(t *testing.T) {
	t.Parallel()

	t.Run("a nil error is the empty result", func(t *testing.T) {
		t.Parallel()
		got := FailureIssues(nil)
		if got.Len() != 0 || got.LimitReached() || got.DroppedCount() != 0 {
			t.Errorf("FailureIssues(nil) = %v, want the empty result", got)
		}
	})

	t.Run("a ResultError is the result it holds", func(t *testing.T) {
		t.Parallel()
		c := diag.NewCollector(diag.NoLimit)
		c.Collect(diag.NewIssue(diag.Error, diag.E_CONSTRAINT_FAIL, "first").Build())
		c.Collect(diag.NewIssue(diag.Fatal, diag.E_SNAPSHOT_MALFORMED, "second").Build())
		err := c.Err()
		re, ok := errors.AsType[*diag.ResultError](err)
		if !ok {
			t.Fatalf("Collector.Err() = %T, want *diag.ResultError", err)
		}
		want := slices.Collect(re.Result.Issues())
		if len(want) != 2 {
			t.Fatalf("the collector's failure holds %d issues, want 2", len(want))
		}
		for name, err := range map[string]error{"bare": err, "wrapped": fmt.Errorf("call: %w", err)} {
			got := slices.Collect(FailureIssues(err).Issues())
			if !slices.EqualFunc(got, want, sameIssue) {
				t.Errorf("%s: FailureIssues = %v, want %v", name, got, want)
			}
		}
	})

	t.Run("any other error is one Fatal E_INTERNAL", func(t *testing.T) {
		t.Parallel()
		got := slices.Collect(FailureIssues(errors.New("x")).Issues())
		if len(got) != 1 {
			t.Fatalf("FailureIssues drew %d issues, want 1: %v", len(got), got)
		}
		iss := got[0]
		if iss.Severity() != diag.Fatal || iss.Code() != diag.E_INTERNAL || iss.Message() != "x" {
			t.Errorf("FailureIssues issue = %s %s %q, want %s %s %q",
				iss.Severity(), iss.Code(), iss.Message(), diag.Fatal, diag.E_INTERNAL, "x")
		}
	})
}

// sameIssue reports whether two issues agree in severity, code and message.
func sameIssue(a, b diag.Issue) bool {
	return a.Severity() == b.Severity() && a.Code() == b.Code() && a.Message() == b.Message()
}
