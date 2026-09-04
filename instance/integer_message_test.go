package instance_test

import (
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
)

// A whole float outside int64 and a float with a fractional part are two
// different facts, and the message names the one that holds.
func TestCheckValue_IntegerMessageNamesTheRightFact(t *testing.T) {
	t.Parallel()
	s, res := schema.LoadString(t.Context(), "schema \"p\"\n\ntype T {\n    id String primary\n    n Integer\n}\n", "p.yammm")
	if res.Err() != nil {
		t.Fatal(res.Err())
	}
	typ, _ := s.Type("T")
	prop, _ := typ.Property("n")
	cons := prop.Constraint()
	for _, tc := range []struct {
		f    float64
		want string
		not  string
	}{
		{1e19, "int64 range", "fractional"},
		{-1e19, "int64 range", "fractional"},
		{3.5, "fractional", "range"},
	} {
		err := instance.CheckValue(tc.f, cons)
		if err == nil {
			t.Fatalf("CheckValue(%g) accepted", tc.f)
		}
		if !strings.Contains(err.Error(), tc.want) || strings.Contains(err.Error(), tc.not) {
			t.Errorf("CheckValue(%g) = %q; want it to name %q and not %q", tc.f, err, tc.want, tc.not)
		}
		if _, cerr := instance.CanonicalValue(tc.f, cons); cerr == nil || !strings.Contains(cerr.Error(), tc.want) || strings.Contains(cerr.Error(), tc.not) {
			t.Errorf("CanonicalValue(%g) = %v; want it to name %q and not %q", tc.f, cerr, tc.want, tc.not)
		}
	}
	if err := instance.CheckValue(3.0, cons); err != nil {
		t.Errorf("CheckValue(3.0) refused a whole float in range: %v", err)
	}
}
