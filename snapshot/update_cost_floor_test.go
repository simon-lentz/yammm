//go:build unix

package snapshot_test

import (
	"context"
	"math"
	"runtime"
	"testing"
	"time"

	"github.com/simon-lentz/yammm/internal/costratio"
	"github.com/simon-lentz/yammm/internal/raceskip"
	"github.com/simon-lentz/yammm/snapshot"
)

// TestUpdateMetadataRatioFloor holds UpdateMetadata to at most a third of the
// CPU time Unmarshal + Marshal takes on a 14.7 MB document. Each side's cost is its
// fastest of five runs, the sides taking turns, timed in process CPU time,
// which other processes' load leaves alone.
//
// It does not run under -short, and [raceskip.Skip] skips it under the race
// detector, which slows the two sides by different factors; scripts/test.sh
// runs it again without -race. It is a top-level test that does not call
// t.Parallel, as a [costratio.CPUTime] measure requires.
func TestUpdateMetadataRatioFloor(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping the cost floor under -short")
	}
	raceskip.Skip(t)
	in := loadBenchInputs(t)
	ctx := context.Background()

	cpu := func(run func()) time.Duration {
		runtime.GC()
		start := costratio.CPUTime(t)
		run()
		return costratio.CPUTime(t) - start
	}
	update := func() {
		if _, res := snapshot.UpdateMetadata(ctx, in.data, in.newMeta); res.HasErrors() {
			t.Fatalf("UpdateMetadata: %v", res.Err())
		}
	}
	reMarshal := func() {
		loaded, res := snapshot.Unmarshal(ctx, in.data, in.schema)
		if res.HasErrors() {
			t.Fatalf("Unmarshal: %v", res.Err())
		}
		if _, res := snapshot.Marshal(ctx, loaded, snapshot.WithMetadata(in.newMeta)); res.HasErrors() {
			t.Fatalf("Marshal: %v", res.Err())
		}
	}

	fast, slow := time.Duration(math.MaxInt64), time.Duration(math.MaxInt64)
	for range 5 {
		fast = min(fast, cpu(update))
		slow = min(slow, cpu(reMarshal))
	}
	if fast <= 0 {
		t.Fatalf("UpdateMetadata measured %s of CPU time; the document is too small to time", fast)
	}
	ratio := float64(slow) / float64(fast)
	t.Logf("UpdateMetadata %s, Unmarshal + Marshal %s of CPU time: %.2fx", fast, slow, ratio)
	const floor = 3.0
	if ratio < floor {
		t.Errorf("Unmarshal + Marshal costs %.2fx UpdateMetadata's CPU time, below the %.2fx floor", ratio, floor)
	}
}
