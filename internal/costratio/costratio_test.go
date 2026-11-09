package costratio_test

import (
	"math"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/simon-lentz/yammm/internal/costratio"
)

// The ratio is the large size's fastest try over the small size's fastest, and
// the sizes take turns, so neither size's tries fall in one busy stretch.
func TestRatio_DividesEachSizesFastestTry(t *testing.T) {
	t.Parallel()
	tries := map[int][]time.Duration{
		10: {5, 3, 4, 9, 6},
		80: {40, 30, 24, 50, 60},
	}
	var sizes []int
	ratio := costratio.Ratio(t, 10, func(n int) time.Duration {
		sizes = append(sizes, n)
		cost := tries[n][0]
		tries[n] = tries[n][1:]
		return cost
	})
	if ratio != 8 {
		t.Errorf("ratio %v, want 8: 24 over 3", ratio)
	}
	if want := []int{10, 80, 10, 80, 10, 80, 10, 80, 10, 80}; !slices.Equal(sizes, want) {
		t.Errorf("sizes run %v, want %v", sizes, want)
	}
}

// recorder is a testing.TB whose Fatalf records its message and returns.
type recorder struct {
	testing.TB
	fatal string
}

func (r *recorder) Helper() {}

func (r *recorder) Fatalf(format string, args ...any) {
	r.fatal = format
}

// A size that measures no time gives no ratio: dividing by the small size would
// fail a linear run as infinitely superlinear, and a large size at zero would
// pass a quadratic one.
func TestRatio_FailsARunThatMeasuresNoTime(t *testing.T) {
	t.Parallel()
	for name, c := range map[string]struct {
		size int
		cost time.Duration
	}{
		"small zero":     {4, 0},
		"small negative": {4, -1},
		"large zero":     {32, 0},
		"large negative": {32, -1},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r := &recorder{TB: t}
			ratio := costratio.Ratio(r, 4, func(n int) time.Duration {
				if n == c.size {
					return c.cost
				}
				return time.Millisecond
			})
			if !strings.Contains(r.fatal, "raise the size") {
				t.Errorf("Fatalf message %q, want one that says to raise the size", r.fatal)
			}
			if ratio != 0 {
				t.Errorf("ratio %v after the failure, want 0", ratio)
			}
		})
	}
}

// A size whose eight-times size is not a positive int gives no ratio, and no run
// is timed.
func TestRatio_RefusesASizeWithNoEightTimesSize(t *testing.T) {
	t.Parallel()
	for _, small := range []int{0, -1, math.MaxInt/8 + 1} {
		t.Run(strconv.Itoa(small), func(t *testing.T) {
			t.Parallel()
			r := &recorder{TB: t}
			runs := 0
			ratio := costratio.Ratio(r, small, func(int) time.Duration {
				runs++
				return time.Millisecond
			})
			if !strings.Contains(r.fatal, "positive size") {
				t.Errorf("Fatalf message %q, want one that asks for a positive size", r.fatal)
			}
			if ratio != 0 || runs != 0 {
				t.Errorf("ratio %v after %d runs, want 0 after none", ratio, runs)
			}
		})
	}
}
