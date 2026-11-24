//go:build unix

package costratio_test

import (
	"testing"
	"time"

	"github.com/simon-lentz/yammm/internal/costratio"
)

var sink int

// A spin that makes no system call is user time alone, so CPUTime must count user
// time: over the spin the process's CPU time grows by a share of the wall time it
// took. A loaded host can schedule the spin out, so the bound is low and it
// judges the best share of five tries.
func TestCPUTime_CountsUserTime(t *testing.T) {
	best := 0.0
	for range 5 {
		cpuStart, wallStart := costratio.CPUTime(t), time.Now()
		for time.Since(wallStart) < 50*time.Millisecond {
			for i := range 1 << 12 {
				sink += i
			}
		}
		cpu, wall := costratio.CPUTime(t)-cpuStart, time.Since(wallStart)
		if cpu < 0 {
			t.Fatalf("CPU time ran back by %s", -cpu)
		}
		best = max(best, float64(cpu)/float64(wall))
	}
	if best < 0.1 {
		t.Errorf("a spin's CPU time was at best %.3f of its wall time, want at least 0.1", best)
	}
}
