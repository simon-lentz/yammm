package neo4j

import (
	"slices"
	"testing"
)

// TestOrderDriftsForApply_ACreateFollowsEveryDropOfItsName keeps a later drop by
// name from removing what an earlier create made, and a create from running while
// its name is still held; drifts that wait on nothing keep their order.
func TestOrderDriftsForApply_ACreateFollowsEveryDropOfItsName(t *testing.T) {
	t.Parallel()
	drift := func(desired, actual string) ConstraintDrift {
		return ConstraintDrift{Desired: Constraint{Name: desired}, Actual: RemoteConstraint{Name: actual, Type: "UNIQUENESS"}}
	}
	order := func(ds []ConstraintDrift) []string {
		var out []string
		for _, d := range ds {
			out = append(out, d.Desired.Name+"<"+d.Actual.Name)
		}
		return out
	}
	for _, tc := range []struct {
		name string
		in   []ConstraintDrift
		want []string
	}{
		{"its own Actual's name", []ConstraintDrift{drift("x", "x"), drift("a", "z"), drift("k1", "x"), drift("k2", "x")}, []string{"a<z", "k1<x", "k2<x", "x<x"}},
		{"another drift's Actual's name", []ConstraintDrift{drift("t", "a"), drift("k", "t")}, []string{"k<t", "t<a"}},
		{"a chain of names", []ConstraintDrift{drift("p", "a"), drift("q", "p"), drift("r", "q")}, []string{"r<q", "q<p", "p<a"}},
		{"a cycle of names breaks at its first drift", []ConstraintDrift{drift("p", "q"), drift("q", "p")}, []string{"p<q", "q<p"}},
		{"an unnamed Desired waits on nothing", []ConstraintDrift{drift("", "y"), drift("y", "y")}, []string{"<y", "y<y"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := order(orderDriftsForApply(tc.in)); !slices.Equal(got, tc.want) {
				t.Errorf("order = %q, want %q", got, tc.want)
			}
		})
	}
	if orderDriftsForApply(nil) != nil {
		t.Error("orderDriftsForApply(nil) is not nil")
	}
	in := []ConstraintDrift{drift("a", "b"), drift("c", "d")}
	if got := orderDriftsForApply(in); &got[0] != &in[0] {
		t.Error("a plan no drift moves in is copied, not returned unchanged")
	}
}

// TestIndexHeap_PopsInAscendingOrder checks the heap orderDriftsForApply picks
// the earliest free drift from.
func TestIndexHeap_PopsInAscendingOrder(t *testing.T) {
	t.Parallel()
	in := []int{5, 3, 8, 1, 9, 2, 7, 3, 0, 6, 4}
	var h indexHeap
	for _, i := range in {
		h.push(i)
	}
	var got []int
	for len(h) > 0 {
		got = append(got, h.pop())
	}
	if want := slices.Sorted(slices.Values(in)); !slices.Equal(got, want) {
		t.Errorf("pops = %v, want %v", got, want)
	}
}
