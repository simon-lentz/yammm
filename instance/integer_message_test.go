package instance_test

import (
	"math"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/internal/value"
	"github.com/simon-lentz/yammm/schema"
)

// An Integer refuses every float, whole or not, in range or not, and the check
// and the coercion say so alike: the message names a float and writes it with
// its indicator, so a whole 3.0 does not read as the integer 3.
func TestCheck_IntegerRefusesEveryFloat(t *testing.T) {
	t.Parallel()
	s, loadErr := schema.LoadString(t.Context(), "schema \"p\"\n\ntype T {\n    id String primary\n    n Integer\n}\n", "p.yammm")
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	typ, _ := s.Type("T")
	prop, _ := typ.Property("n")
	cons := prop.Constraint()
	for _, tc := range []struct {
		v    any
		want string
	}{
		{3.0, "float 3.0"},
		{float32(7), "float 7.0"},
		{1e19, "float 10000000000000000000.0"},
		{-1e19, "float -10000000000000000000.0"},
		{3.5, "float 3.5"},
		{math.NaN(), "float NaN"},
	} {
		err := value.Check(tc.v, cons)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("value.Check(%v) = %v; want a refusal naming %q", tc.v, err, tc.want)
		}
	}
	if err := value.Check(int64(3), cons); err != nil {
		t.Errorf("value.Check(int64(3)) refused an integer: %v", err)
	}
}
