package eval_test

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/internal/expr"
)

// Member access folds a property's name by immutable.Properties' rule — ASCII
// only, the alphabetically first key on a collision — and reads a relation,
// keyed by its UPPER_SNAKE name, by that name or its field name alone. It reads
// only the wrapped map every production value is; a raw Go map is not a
// receiver.
func TestAccessMember_FoldsByThePropertiesRule(t *testing.T) {
	t.Parallel()
	m := immutable.WrapMap(map[string]any{"Name": "mixed", "name": "lower", "É": "acute", "WORKS_AT": "k", "ZONE_AT": "z"})
	t.Run("exact match first", func(t *testing.T) {
		t.Parallel()
		evalEq(t, sx(".", lit(m), lit("name")), "lower")
	})
	t.Run("collision takes the alphabetically first key", func(t *testing.T) {
		t.Parallel()
		evalEq(t, sx(".", lit(m), lit("nAmE")), "mixed")
	})
	t.Run("a relation by its name or its field name", func(t *testing.T) {
		t.Parallel()
		evalEq(t, sx(".", lit(m), lit("WORKS_AT")), "k")
		evalEq(t, sx(".", lit(m), lit("works_at")), "k")
		evalEq(t, sx(".", lit(m), lit("zone_at")), "z")
	})
	t.Run("a relation in another casing reads nothing", func(t *testing.T) {
		t.Parallel()
		evalEq(t, sx(".", lit(m), lit("Works_At")), nil)
		evalEq(t, sx(".", lit(m), lit("wORKS_AT")), nil)
		evalEq(t, sx(".", lit(m), lit("zONE_AT")), nil)
	})
	t.Run("the fold is ASCII only", func(t *testing.T) {
		t.Parallel()
		evalEq(t, sx(".", lit(m), lit("é")), nil)
	})
	t.Run("a raw Go map is not a receiver", func(t *testing.T) {
		t.Parallel()
		evalErr(t, sx(".", lit(map[string]any{"name": "x"}), lit("name")), "cannot access member")
	})
}

// Substring clamps its indices before narrowing to int, so an index no int
// can hold is clamped rather than wrapped. These rows pin the clamping at
// the int64 edges on a 64-bit build, where int and int64 coincide; the
// narrowing itself is observable only on a 32-bit target, which CI does not
// run.
func TestSubstring_ClampsInInt64(t *testing.T) {
	t.Parallel()
	sub := func(args ...any) expr.Expression {
		exprs := make([]expr.Expression, len(args))
		for i, a := range args {
			exprs[i] = lit(a)
		}
		return makeBuiltinCall(lit("hello"), "Substring", exprs, nil, nil)
	}
	evalEq(t, sub(int64(math.MaxInt64)), "")
	evalEq(t, sub(int64(0), int64(math.MaxInt64)), "hello")
	evalEq(t, sub(int64(math.MinInt64)), "hello")
	evalEq(t, sub(int64(0), int64(math.MinInt64)), "")
	evalEq(t, sub(int64(-3)), "llo")
	evalEq(t, sub(int64(1), int64(-1)), "ell")
}

// Division reaches the integer domain through numericOp like every other
// operator; its zero and overflow guards live in checkedDiv alone.
func TestDiv_IntegerDomainThroughNumericOp(t *testing.T) {
	t.Parallel()
	evalErr(t, sx("/", lit(int64(1)), lit(int64(0))), "division by zero")
	evalErr(t, sx("/", lit(int64(math.MinInt64)), lit(int64(-1))), "integer overflow in /")
	evalEq(t, sx("/", lit(int64(7)), lit(int64(2))), int64(3))
	evalEq(t, sx("/", lit(int64(1)), lit(0.0)), math.Inf(1))
	evalEq(t, sx("/", lit(uint8(6)), lit(int64(3))), int64(2))
}

// TypeOf reads a carrier the way the value layer classifies it: a json.Number
// is the number it spells, and a named scalar is its base kind.
func TestTypeOf_ReadsCarriers(t *testing.T) {
	t.Parallel()
	type carrier string
	type count int32
	type flag bool
	for _, r := range []struct {
		val  any
		want string
	}{
		{json.Number("1"), "integer"},
		{json.Number("1.5"), "float"},
		{carrier("x"), "string"},
		{count(3), "integer"},
		{flag(true), "boolean"},
	} {
		evalEq(t, makeBuiltinCall(lit(r.val), "TypeOf", nil, nil, nil), r.want)
	}
}

// A bare Op is not an expression: the parser reads it through SExpr.Op and
// never hands one to evaluate.
func TestEvaluate_OpIsNotAnExpression(t *testing.T) {
	t.Parallel()
	evalErr(t, expr.Op("test"), "unknown expression type")
}

// Min and Max with an argument rank a scalar receiver against it, as the
// catalogue's ResultElementOrArg promises a scalar; a list receiver is
// refused rather than ranked by strata above every scalar.
func TestMinMax_WithArgumentRefuseAListReceiver(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"Min", "Max"} {
		evalErr(t, makeBuiltinCall(list("a", "b"), name, []expr.Expression{lit("z")}, nil, nil), "list")
		evalErr(t, makeBuiltinCall(lit([]any{int64(1)}), name, []expr.Expression{lit(int64(2))}, nil, nil), "list")
	}
	evalEq(t, makeBuiltinCall(lit("a"), "Max", []expr.Expression{lit("z")}, nil, nil), "z")
	evalEq(t, makeBuiltinCall(lit(nil), "Max", []expr.Expression{lit("z")}, nil, nil), "z")
}
