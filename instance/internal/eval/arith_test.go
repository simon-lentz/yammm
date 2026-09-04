package eval_test

import (
	"math"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/instance/internal/eval"
	"github.com/simon-lentz/yammm/schema/expr"
)

// Integer arithmetic is exact within int64: a result outside it is an
// evaluation error, as division and modulo by zero are — never a wrap.
func TestArithmetic_Int64OverflowIsAnError(t *testing.T) {
	t.Parallel()
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()
	minI, maxI := int64(math.MinInt64), int64(math.MaxInt64)
	cases := []struct {
		name string
		e    expr.Expression
	}{
		{"max + 1", expr.SExpr{expr.Op("+"), lit(maxI), lit(int64(1))}},
		{"min - 1", expr.SExpr{expr.Op("-"), lit(minI), lit(int64(1))}},
		{"max * 2", expr.SExpr{expr.Op("*"), lit(maxI), lit(int64(2))}},
		{"min / -1", expr.SExpr{expr.Op("/"), lit(minI), lit(int64(-1))}},
		{"-min", expr.SExpr{expr.Op("-x"), lit(minI)}},
		{"Sum wraps", makeBuiltinCall(expr.SExpr{expr.Op("[]"), lit(maxI), lit(int64(1))}, "Sum", nil, nil, nil)},
		{"Abs min", makeBuiltinCall(lit(minI), "Abs", nil, nil, nil)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := ev.Evaluate(tc.e, scope)
			if err == nil {
				t.Fatalf("evaluated to %v (%T); want an overflow error", got, got)
			}
			if !strings.Contains(err.Error(), "overflow") {
				t.Errorf("error %q does not name the overflow", err)
			}
		})
	}
	// The same operations inside the domain still answer.
	for _, tc := range []struct {
		name string
		e    expr.Expression
		want any
	}{
		{"max - 1 + 1", expr.SExpr{expr.Op("+"), lit(maxI - 1), lit(int64(1))}, maxI},
		{"min + 1 - 1", expr.SExpr{expr.Op("-"), lit(minI + 1), lit(int64(1))}, minI},
		{"Abs min+1", makeBuiltinCall(lit(minI+1), "Abs", nil, nil, nil), maxI},
		{"Sum in range", makeBuiltinCall(expr.SExpr{expr.Op("[]"), lit(int64(1)), lit(int64(2))}, "Sum", nil, nil, nil), int64(3)},
		{"float promotes, no guard", expr.SExpr{expr.Op("+"), lit(maxI), lit(1.0)}, float64(maxI) + 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := ev.Evaluate(tc.e, scope)
			if err != nil || got != tc.want {
				t.Errorf("got %v, %v; want %v", got, err, tc.want)
			}
		})
	}
}

// `+` on a nil operand is an evaluation error, as `-`, `*` and `/` are; the
// three arms SPEC defines still work.
func TestAdd_NilOperandIsAnError(t *testing.T) {
	t.Parallel()
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()
	list := func(vs ...any) expr.Expression {
		e := expr.SExpr{expr.Op("[]")}
		for _, v := range vs {
			e = append(e, lit(v))
		}
		return e
	}
	for _, tc := range []struct {
		name string
		e    expr.Expression
	}{
		{"nil + nil", expr.SExpr{expr.Op("+"), lit(nil), lit(nil)}},
		{"nil + list", expr.SExpr{expr.Op("+"), lit(nil), list(int64(1))}},
		{"list + nil", expr.SExpr{expr.Op("+"), list(int64(1)), lit(nil)}},
		{"nil + 1", expr.SExpr{expr.Op("+"), lit(nil), lit(int64(1))}},
		{"nil + string", expr.SExpr{expr.Op("+"), lit(nil), lit("a")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got, err := ev.Evaluate(tc.e, scope); err == nil {
				t.Errorf("evaluated to %#v; want an error", got)
			}
		})
	}
	for _, tc := range []struct {
		name string
		e    expr.Expression
		want any
	}{
		{"numbers", expr.SExpr{expr.Op("+"), lit(int64(1)), lit(int64(2))}, int64(3)},
		{"strings", expr.SExpr{expr.Op("+"), lit("a"), lit("b")}, "ab"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got, err := ev.Evaluate(tc.e, scope); err != nil || got != tc.want {
				t.Errorf("got %v, %v; want %v", got, err, tc.want)
			}
		})
	}
	t.Run("lists", func(t *testing.T) {
		t.Parallel()
		got, err := ev.Evaluate(expr.SExpr{expr.Op("+"), list(int64(1)), list(int64(2))}, scope)
		s, ok := got.([]any)
		if err != nil || !ok || len(s) != 2 || s[0] != int64(1) || s[1] != int64(2) {
			t.Errorf("got %#v, %v; want [1 2]", got, err)
		}
	})
}
