package neo4j

import (
	"fmt"
	"testing"

	"github.com/simon-lentz/yammm/internal/raceskip"
)

// nameChain builds n+1 declarations where declaration k is realised by a row
// holding declaration k+1's name, and the last declaration has no row: every
// declaration drifts, each found only after the one after it.
func nameChain(n int) ([]Index, []RemoteIndex) {
	var desired []Index
	var actual []RemoteIndex
	for k := 0; k <= n; k++ {
		prop := fmt.Sprintf("p%04d", k)
		desired = append(desired, Index{Name: fmt.Sprintf("n%04d", k), Kind: IndexRange, Label: "app__T", Properties: []string{prop}})
		if k < n {
			actual = append(actual, RemoteIndex{
				Name: fmt.Sprintf("n%04d", k+1), Type: "RANGE", EntityType: "NODE", State: "ONLINE",
				LabelsOrTypes: []string{"app__T"}, Properties: []string{prop},
			})
		}
	}
	return desired, actual
}

// TestIndexDiff_ANameChainCostsLinearWork doubles a name chain and requires
// the allocations to grow by less than 2.5 times: a fixed point that rescans the
// waiting plans per settled one grows by about 3.5. Skipped under -race, whose
// sync.Pool drops a share of what it holds and moves the count.
func TestIndexDiff_ANameChainCostsLinearWork(t *testing.T) {
	raceskip.Skip(t)
	owned := ownedSet("app__T")
	allocs := func(n int) float64 {
		desired, actual := nameChain(n)
		if plan := &New().Diff(nil, desired, nil, actual, owned).Indexes; len(plan.Drift) != n+1 {
			t.Fatalf("Drift = %d declarations; want all %d drifted", len(plan.Drift), n+1)
		}
		return testing.AllocsPerRun(3, func() { New().Diff(nil, desired, nil, actual, owned) })
	}
	small, large := allocs(200), allocs(400)
	if ratio := large / small; ratio >= 2.5 {
		t.Errorf("allocations grew %.2f times (%.0f to %.0f) when the chain doubled; want linear growth", ratio, small, large)
	}
}
