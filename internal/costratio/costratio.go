// Package costratio tells a run whose cost grows linearly with its size from
// one whose cost grows faster, for a test that guards an algorithm's shape.
//
// A test times each run with [CPUTime], the CPU time the process has spent,
// which other processes' load leaves alone, so a busy host does not move the
// ratio as it moves a wall clock. The process's time includes every other test
// running in it, so a test that measures with [CPUTime] is a top-level test
// that does not call t.Parallel. A parallel test runs with other parallel tests
// only ([testing.T.Parallel]), so such a test runs alone; a subtest of a
// parallel test does not.
//
// The Windows clock behind process times ticks too coarsely for these runs, so
// [CPUTime] is built on unix alone, and a test that calls it carries the unix
// build constraint.
package costratio

import (
	"math"
	"testing"
	"time"
)

// Ratio returns run's cost at eight times small over its cost at small. Each
// size's cost is its fastest of five tries, the sizes taking turns, so a busy
// moment raises one try and not the ratio. A size with no eight-times size, or
// a size whose fastest try measures no time, fails tb, since the ratio says nothing.
func Ratio(tb testing.TB, small int, run func(n int) time.Duration) float64 {
	tb.Helper()
	if small <= 0 || small > math.MaxInt/8 {
		tb.Fatalf("size %d has no eight-times size; give a positive size up to %d", small, math.MaxInt/8)
		return 0
	}
	bestSmall, bestLarge := time.Duration(math.MaxInt64), time.Duration(math.MaxInt64)
	for range 5 {
		bestSmall = min(bestSmall, run(small))
		bestLarge = min(bestLarge, run(8*small))
	}
	if bestSmall <= 0 || bestLarge <= 0 {
		tb.Fatalf("runs of size %d and %d measured %s and %s; raise the size until both measure time", small, 8*small, bestSmall, bestLarge)
		return 0
	}
	return float64(bestLarge) / float64(bestSmall)
}
