// The Windows clock behind process times ticks too coarsely for these runs.

//go:build unix

package schema_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/simon-lentz/yammm/internal/costratio"
	"github.com/simon-lentz/yammm/internal/raceskip"
	"github.com/simon-lentz/yammm/schema"
)

// maxQuadraticRatio separates eight times the depth at quadratic cost, measured
// at 58 to 82, from cubic cost, measured at 219 from a depth of 150.
const maxQuadraticRatio = 160

// maxLinearMixinRatio separates eight times the mixins at linear cost,
// measured at 6.4 to 7.1, from quadratic cost, measured at 16.5 to 16.8.
const maxLinearMixinRatio = 12

// A type that extends n mixins, each defining one property, costs about n
// comparisons for it in every order, also when no definition narrows the rest:
// here each mixin narrows the one before it and the last narrows none.
func TestInheritance_ManyMixinsOfOnePropertyCostTheirNumber(t *testing.T) {
	raceskip.Skip(t)
	ratio := costratio.Ratio(t, 2000, func(n int) time.Duration {
		var src strings.Builder
		src.WriteString("schema \"m\"\n")
		for i := range n {
			fmt.Fprintf(&src, "abstract type M%d { x Integer[%d, %d] }\n", i, i, 2*n-i)
		}
		fmt.Fprintf(&src, "abstract type M%d { x Integer[-5, -1] }\ntype T extends M0", n)
		for i := 1; i <= n; i++ {
			fmt.Fprintf(&src, ", M%d", i)
		}
		src.WriteString(" { id String primary }\n")
		start := costratio.CPUTime(t)
		_, res := schema.LoadString(t.Context(), src.String(), "m.yammm")
		spent := costratio.CPUTime(t) - start
		if res.OK() {
			t.Fatal("the load succeeded, want one E_PROPERTY_CONFLICT")
		}
		return spent
	})
	t.Logf("eight times the mixins cost %.1f times the load", ratio)
	if ratio >= maxLinearMixinRatio {
		t.Errorf("eight times the mixins cost %.0f times the load, want under %d", ratio, maxLinearMixinRatio)
	}
}

// A chain whose every type declares a property holds about n²/2 merged
// properties, and a load costs about that: each type reads each ancestor's
// own declarations once, never each ancestor's merged view.
func TestInheritance_AChainThatDeclaresAPropertyAtEachLevelCostsItsSquare(t *testing.T) {
	raceskip.Skip(t)
	ratio := costratio.Ratio(t, 150, func(n int) time.Duration {
		var src strings.Builder
		src.WriteString("schema \"m\"\nabstract type T0 { p0 String }\n")
		for i := 1; i <= n; i++ {
			fmt.Fprintf(&src, "abstract type T%d extends T%d { p%d String }\n", i, i-1, i)
		}
		fmt.Fprintf(&src, "type Leaf extends T%d { id String primary }\n", n)
		start := costratio.CPUTime(t)
		_, res := schema.LoadString(t.Context(), src.String(), "m.yammm")
		spent := costratio.CPUTime(t) - start
		if !res.OK() {
			t.Fatal(res)
		}
		return spent
	})
	t.Logf("eight times the depth cost %.1f times the load", ratio)
	if ratio >= maxQuadraticRatio {
		t.Errorf("eight times the depth cost %.0f times the load, want under %d", ratio, maxQuadraticRatio)
	}
}
