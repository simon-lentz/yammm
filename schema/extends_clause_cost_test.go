package schema_test

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/simon-lentz/yammm/internal/raceskip"
	"github.com/simon-lentz/yammm/schema"
)

// maxLinearRatio separates eight times the names at linear cost, which reads under 11
// on a loaded host, from quadratic cost, which reads over 30.
const maxLinearRatio = 24

// costRatio returns the cost of eight times small names over the cost of small names.
// A busy host only raises a load's time, so each size's cost is its fastest of three
// tries, and one low reading cannot pass a quadratic load.
func costRatio(small int, load func(n int) time.Duration) float64 {
	fastest := func(n int) time.Duration {
		best := load(n)
		for range 2 {
			best = min(best, load(n))
		}
		return best
	}
	return float64(fastest(8*small)) / float64(fastest(small))
}

// The time of a load carries the shape in both tests here: a repeated comparison
// allocates nothing, so no count shows it. Unknown names are refused, after the clause's
// spellings are deduped and before the supertypes are reduced.
func TestExtendsClause_ALongClauseOfUnknownNamesLoadsInLinearTime(t *testing.T) {
	raceskip.Skip(t)
	ratio := costRatio(8000, func(n int) time.Duration {
		names := make([]string, n)
		for i := range names {
			names[i] = "T" + strconv.Itoa(i)
		}
		src := "schema \"m\"\ntype A extends " + strings.Join(names, ", ") + " { id String primary }\n"
		start := time.Now()
		_, _ = schema.LoadString(t.Context(), src, "m.yammm")
		return time.Since(start)
	})
	if ratio >= maxLinearRatio {
		t.Errorf("eight times the names cost %.0f times the load, want under %d", ratio, maxLinearRatio)
	}
}

// A clause of declared names reaches the reduction over direct supertypes too, which a
// clause of unknown names does not.
func TestExtendsClause_ALongClauseOfDeclaredTypesLoadsInLinearTime(t *testing.T) {
	raceskip.Skip(t)
	ratio := costRatio(2000, func(n int) time.Duration {
		var src strings.Builder
		src.WriteString("schema \"m\"\n")
		names := make([]string, n)
		for i := range names {
			names[i] = "T" + strconv.Itoa(i)
			src.WriteString("abstract type " + names[i] + " { }\n")
		}
		src.WriteString("type A extends " + strings.Join(names, ", ") + " { id String primary }\n")
		start := time.Now()
		_, res := schema.LoadString(t.Context(), src.String(), "m.yammm")
		if !res.OK() {
			t.Fatal(res)
		}
		return time.Since(start)
	})
	if ratio >= maxLinearRatio {
		t.Errorf("eight times the names cost %.0f times the load, want under %d", ratio, maxLinearRatio)
	}
}
