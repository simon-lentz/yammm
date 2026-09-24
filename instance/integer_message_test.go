package instance_test

import (
	"math"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
)

// An Integer refuses every float, whole or not, in range or not, and the check
// and the coercion say so alike: the message names a float and writes it with
// its indicator, so a whole 3.0 does not read as the integer 3.
func TestCheckValue_IntegerRefusesEveryFloat(t *testing.T) {
	t.Parallel()
	s, res := schema.LoadString(t.Context(), "schema \"p\"\n\ntype T {\n    id String primary\n    n Integer\n}\n", "p.yammm")
	if res.Err() != nil {
		t.Fatal(res.Err())
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
		err := instance.CheckValue(tc.v, cons)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("CheckValue(%v) = %v; want a refusal naming %q", tc.v, err, tc.want)
		}
		if _, cerr := instance.CanonicalValue(tc.v, cons); cerr == nil || !strings.Contains(cerr.Error(), tc.want) {
			t.Errorf("CanonicalValue(%v) = %v; want a refusal naming %q", tc.v, cerr, tc.want)
		}
	}
	if err := instance.CheckValue(int64(3), cons); err != nil {
		t.Errorf("CheckValue(int64(3)) refused an integer: %v", err)
	}
}
