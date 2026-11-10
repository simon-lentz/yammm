package eval_test

import (
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance/internal/eval"
	"github.com/simon-lentz/yammm/internal/expr"
)

// A datatype check (`=~ Kind`) is the property checker's rule for that kind:
// what a property of the kind refuses, the check refuses.
func TestDatatypeCheck_IsTheCheckerRule(t *testing.T) {
	t.Parallel()
	type carrier string
	type flag bool
	rows := []struct {
		name string
		val  any
		kind string
		want bool
	}{
		{"NaN is not a Float", math.NaN(), "Float", false},
		{"+Inf is not a Float", math.Inf(1), "Float", false},
		{"an integer is a Float", int64(3), "Float", true},
		{"uint64 above int64 is not an Integer", uint64(1 << 63), "Integer", false},
		{"a whole float is not an Integer", 3.0, "Integer", false},
		{"a whole-valued exponent literal is not an Integer", json.Number("3e0"), "Integer", false},
		{"an integer literal is an Integer", json.Number("3"), "Integer", true},
		{"a fractional float is not an Integer", 3.5, "Integer", false},
		{"a named string carrier is a String", carrier("x"), "String", true},
		{"a json.Number is not a String", json.Number("1"), "String", false},
		{"a named bool carrier is a Boolean", flag(true), "Boolean", true},
		{"fractional seconds are a Timestamp", "2024-01-15T10:30:00.123456789Z", "Timestamp", true},
		{"a time.Time is a Timestamp", time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC), "Timestamp", true},
		{"an int is not a Timestamp", 12345, "Timestamp", false},
		{"a date in a declared layout is not a Timestamp", "2030-01-02", "Timestamp", false},
		{"nil is no kind", nil, "String", false},
		{"a pointer to a string is not a String", new("x"), "String", false},
		{"a nil pointer is not a String", (*string)(nil), "String", false},
		{"a pointer to an int64 is not an Integer", new(int64(3)), "Integer", false},
		{"a pointer to a time is not a Timestamp", new(time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)), "Timestamp", false},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			got, err := eval.NewEvaluator().Evaluate(t.Context(), sx("=~", lit(r.val), expr.DatatypeLiteral(r.kind)), eval.EmptyScope())
			if err != nil {
				t.Fatal(err)
			}
			if got != r.want {
				t.Errorf("%#v =~ %s = %v, want %v", r.val, r.kind, got, r.want)
			}
		})
	}
}

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

// A datatype the language cannot check at runtime — a shape or a constraint
// keyword the parser still emits as a DatatypeLiteral — is an error, by the
// one set internal/expr defines for both layers.
func TestDatatypeCheck_UnknownNameErrors(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"Vector", "List", "Enum", "Pattern", "int", "number"} {
		evalErr(t, sx("=~", lit("x"), expr.DatatypeLiteral(name)), "unknown datatype")
	}
	evalEq(t, sx("=~", lit("x"), expr.DatatypeLiteral("STRING")), true)
}
