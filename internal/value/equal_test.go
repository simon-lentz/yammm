package value_test

import (
	"math"
	"testing"

	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/internal/value"
)

// Equal is structural and never errors: two values that Order can rank are
// equal when it ranks them together; two maps are equal when they hold the
// same keys and each value is equal by this rule; a list when its elements
// are pairwise equal; and a mismatch of kinds is not equal, never an error.
func TestEqual(t *testing.T) {
	t.Parallel()
	m := func(kv ...any) map[string]any {
		out := map[string]any{}
		for i := 0; i+1 < len(kv); i += 2 {
			out[kv[i].(string)] = kv[i+1]
		}
		return out
	}
	cases := []struct {
		name string
		a, b any
		want bool
	}{
		{"nil nil", nil, nil, true},
		{"nil int", nil, int64(0), false},
		{"int int", int64(1), int64(1), true},
		{"int float exact", int64(1), 1.0, true},
		{"int float inexact", int64(1), 1.5, false},
		{"string string", "a", "a", true},
		{"string int", "1", int64(1), false},
		{"nan nan", math.NaN(), math.NaN(), true},
		{"list list", []any{int64(1), "a"}, []any{int64(1), "a"}, true},
		{"list shorter", []any{int64(1)}, []any{int64(1), int64(2)}, false},
		{"list immutable", []any{int64(1), int64(2)}, immutable.Wrap([]any{int64(1), int64(2)}).Unwrap(), true},
		{"map map", m("a", int64(1), "b", "x"), m("a", int64(1), "b", "x"), true},
		{"map value differs", m("a", int64(1)), m("a", int64(2)), false},
		{"map key differs", m("a", int64(1)), m("b", int64(1)), false},
		{"map subset", m("a", int64(1)), m("a", int64(1), "b", int64(2)), false},
		{"map nested", m("a", m("x", []any{int64(1)})), m("a", m("x", []any{int64(1)})), true},
		{"map immutable both", immutable.Wrap(m("a", int64(1))).Unwrap(), immutable.Wrap(m("a", int64(1))).Unwrap(), true},
		{"map immutable mixed", immutable.Wrap(m("a", int64(1))).Unwrap(), m("a", int64(1)), true},
		{"map vs list", m("a", int64(1)), []any{int64(1)}, false},
		{"map vs scalar", m("a", int64(1)), int64(1), false},
		{"unrankable struct", struct{ X int }{1}, struct{ X int }{1}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := value.Equal(tc.a, tc.b); got != tc.want {
				t.Errorf("Equal(%#v, %#v) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
			if got := value.Equal(tc.b, tc.a); got != tc.want {
				t.Errorf("Equal is not symmetric on (%#v, %#v)", tc.b, tc.a)
			}
		})
	}
}
