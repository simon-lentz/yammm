package value_test

import (
	"testing"

	"github.com/simon-lentz/yammm/internal/value"
)

// A value of a scalar kind holds itself, so asking whether it holds none
// allocates nothing.
func TestAbsent_AScalarAllocatesNothing(t *testing.T) {
	for _, v := range []any{1000, int64(1000), uint16(1000), 2.5, "x", true} {
		if n := testing.AllocsPerRun(100, func() { _ = value.Absent(v) }); n != 0 {
			t.Errorf("value.Absent(%T) allocates %v times; want none", v, n)
		}
	}
}
