package diag_test

import (
	"testing"

	"github.com/simon-lentz/yammm/diag"
)

// MergeFunc is Merge with a transform on each stored issue: the receiver
// carries res's severity counts and truncation facts exactly as Merge does,
// and stores fn(issue) in place of issue.
func TestCollector_MergeFunc_TransformsEachIssueAndCarriesTruncation(t *testing.T) {
	capped := diag.NewCollector(2)
	for _, msg := range []string{"a", "b", "c"} {
		capped.Collect(diag.NewIssue(diag.Error, diag.E_UNKNOWN_FIELD, msg).Build())
	}
	res := capped.Result()
	if !res.LimitReached() || res.DroppedCount() != 1 {
		t.Fatalf("the source result is not truncated as expected: %s", res)
	}

	batch := diag.NewCollectorUnlimited()
	batch.MergeFunc(res, func(is diag.Issue) diag.Issue {
		return diag.FromIssue(is).WithDetail(diag.DetailKeyInstanceIndex, "7").Build()
	})
	out := batch.Result()
	if !out.LimitReached() || out.DroppedCount() != 1 || out.SeverityCounts().Errors != 3 || out.Len() != 2 {
		t.Errorf("limitReached=%v dropped=%d errors=%d len=%d", out.LimitReached(), out.DroppedCount(), out.SeverityCounts().Errors, out.Len())
	}
	for is := range out.Issues() {
		var stamped bool
		for _, d := range is.Details() {
			stamped = stamped || (d.Key == diag.DetailKeyInstanceIndex && d.Value == "7")
		}
		if !stamped {
			t.Errorf("not stamped: %v", is)
		}
	}
}
