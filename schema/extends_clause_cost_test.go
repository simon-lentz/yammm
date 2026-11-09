// The Windows clock behind process times ticks too coarsely for these runs.

//go:build unix

package schema_test

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/simon-lentz/yammm/internal/costratio"
	"github.com/simon-lentz/yammm/internal/raceskip"
	"github.com/simon-lentz/yammm/schema"
)

// maxLinearRatio separates eight times the names at linear cost, measured at 6 to 9
// in CPU time on a loaded host, from quadratic cost, measured at 24 to 46.
const maxLinearRatio = 16

// The CPU time of a load carries the shape in both tests here: a repeated comparison
// allocates nothing, so no count shows it. Unknown names are refused, after the clause's
// spellings are deduped and before the supertypes are reduced.
func TestExtendsClause_ALongClauseOfUnknownNamesLoadsInLinearTime(t *testing.T) {
	raceskip.Skip(t)
	ratio := costratio.Ratio(t, 8000, func(n int) time.Duration {
		names := make([]string, n)
		for i := range names {
			names[i] = "T" + strconv.Itoa(i)
		}
		src := "schema \"m\"\ntype A extends " + strings.Join(names, ", ") + " { id String primary }\n"
		start := costratio.CPUTime(t)
		_, _ = schema.LoadString(t.Context(), src, "m.yammm")
		return costratio.CPUTime(t) - start
	})
	if ratio >= maxLinearRatio {
		t.Errorf("eight times the names cost %.0f times the load, want under %d", ratio, maxLinearRatio)
	}
}

// A clause of declared names reaches the reduction over direct supertypes too, which a
// clause of unknown names does not.
func TestExtendsClause_ALongClauseOfDeclaredTypesLoadsInLinearTime(t *testing.T) {
	raceskip.Skip(t)
	ratio := costratio.Ratio(t, 2000, func(n int) time.Duration {
		var src strings.Builder
		src.WriteString("schema \"m\"\n")
		names := make([]string, n)
		for i := range names {
			names[i] = "T" + strconv.Itoa(i)
			src.WriteString("abstract type " + names[i] + " { }\n")
		}
		src.WriteString("type A extends " + strings.Join(names, ", ") + " { id String primary }\n")
		start := costratio.CPUTime(t)
		_, res := schema.LoadString(t.Context(), src.String(), "m.yammm")
		if !res.OK() {
			t.Fatal(res)
		}
		return costratio.CPUTime(t) - start
	})
	if ratio >= maxLinearRatio {
		t.Errorf("eight times the names cost %.0f times the load, want under %d", ratio, maxLinearRatio)
	}
}
