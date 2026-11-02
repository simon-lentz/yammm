package neo4j

import (
	"testing"

	"github.com/simon-lentz/yammm/instance"
)

// A value of a scalar kind holds itself, so asking whether it holds none
// allocates nothing.
func TestIsAbsent_AScalarAllocatesNothing(t *testing.T) {
	for _, v := range []any{1000, int64(1000), uint16(1000), 2.5, "x", true} {
		if n := testing.AllocsPerRun(100, func() { _ = instance.IsAbsent(v) }); n != 0 {
			t.Errorf("instance.IsAbsent(%T) allocates %v times; want none", v, n)
		}
	}
}
