package diag

import (
	"slices"
	"testing"
)

func TestCollect_BuildsResultFromIssues(t *testing.T) {
	t.Parallel()
	res := Collect(
		NewIssue(Error, E_INTERNAL, "boom").Build(),
		NewIssue(Warning, E_INTERNAL, "careful").Build(),
	)
	if !res.HasErrors() {
		t.Fatal("Collect with an Error issue: HasErrors() = false, want true")
	}
	if got := res.Len(); got != 2 {
		t.Fatalf("Len() = %d, want 2", got)
	}
	if c := res.SeverityCounts(); c.Errors != 1 || c.Warnings != 1 {
		t.Fatalf("counts = %+v, want Errors:1 Warnings:1", c)
	}
}

func TestCollect_EmptyIsOK(t *testing.T) {
	t.Parallel()
	res := Collect()
	if !res.OK() {
		t.Fatal("Collect() with no issues: OK() = false, want true")
	}
	if res.Len() != 0 {
		t.Fatalf("Len() = %d, want 0", res.Len())
	}
}

func TestCollect_MatchesCollectorSort(t *testing.T) {
	t.Parallel()
	a := NewIssue(Error, E_TYPE_COLLISION, "alpha").Build()
	b := NewIssue(Error, E_INTERNAL, "bravo").Build()
	c := NewIssue(Warning, E_SYNTAX, "charlie").Build()

	// Collect funnels through the same Collector machinery, so its order is the
	// deterministic total order regardless of input order.
	got := slices.Collect(Collect(c, a, b).Issues())

	col := NewCollectorUnlimited()
	col.CollectAll([]Issue{a, b, c})
	want := slices.Collect(col.Result().Issues())

	if len(got) != len(want) {
		t.Fatalf("len mismatch: Collect=%d Collector=%d", len(got), len(want))
	}
	for i := range got {
		if got[i].Message() != want[i].Message() {
			t.Errorf("order[%d]: Collect=%q Collector=%q", i, got[i].Message(), want[i].Message())
		}
	}
}

func TestCollect_PanicsOnZeroIssue(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Fatal("Collect(zero Issue) did not panic; want panic matching Collector.Collect")
		}
	}()
	Collect(Issue{})
}
