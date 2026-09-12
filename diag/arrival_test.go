package diag

import (
	"slices"
	"testing"
)

// TestMerge_StoresSurvivorsInArrivalOrder holds a merged result's issues to the
// order they were collected in, not the order they sort in. The collector
// evicts the LATEST-ARRIVED of the least severe, so storing in sort order makes
// the victim depend on how the merged messages happen to compare.
//
// The merged warnings arrive A, B, C and sort C, B, A. Two evictions must drop
// C then B, the two latest-arrived; storing in sort order drops A then B, so
// the survivor names which order was stored.
func TestMerge_StoresSurvivorsInArrivalOrder(t *testing.T) {
	t.Parallel()

	src := NewCollectorUnlimited()
	src.Collect(NewIssue(Warning, E_SYNTAX, "c arrives first, sorts last").Build())
	src.Collect(NewIssue(Warning, E_SYNTAX, "b arrives second, sorts second").Build())
	src.Collect(NewIssue(Warning, E_SYNTAX, "a arrives third, sorts first").Build())

	c := NewCollector(3)
	c.Merge(src.Result())
	// Two issues past the limit, each more severe than every warning, so two
	// warnings yield their slots and the one that arrived first survives.
	c.Collect(NewIssue(Error, E_INTERNAL, "first error").Build())
	c.Collect(NewIssue(Error, E_INTERNAL, "second error").Build())

	got := retainedMessages(c)
	want := []string{"c arrives first, sorts last", "first error", "second error"}
	if !slices.Equal(got, want) {
		t.Errorf("retained = %v\nwant       %v\nthe evicted warnings are not the latest-arrived", got, want)
	}
}

// TestResult_KeepsArrivalOrderAcrossMerge holds one merged result's issues to
// arrival order twice over: a second merge of the same result must evict the
// same issues, whatever the messages sort like.
func TestResult_KeepsArrivalOrderAcrossMerge(t *testing.T) {
	t.Parallel()

	src := NewCollectorUnlimited()
	src.Collect(NewIssue(Warning, E_SYNTAX, "z first").Build())
	src.Collect(NewIssue(Warning, E_SYNTAX, "y second").Build())
	src.Collect(NewIssue(Warning, E_SYNTAX, "x third").Build())
	res := src.Result()

	// A merge of a merge keeps the order the first one stored.
	relay := NewCollectorUnlimited()
	relay.Merge(res)

	c := NewCollector(2)
	c.Merge(relay.Result())
	c.Collect(NewIssue(Error, E_INTERNAL, "the error").Build())

	got := retainedMessages(c)
	want := []string{"the error", "z first"}
	if !slices.Equal(got, want) {
		t.Errorf("retained = %v\nwant       %v\narrival order did not survive two merges", got, want)
	}
}
