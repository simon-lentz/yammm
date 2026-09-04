package eval_test

import (
	"encoding/json"
	"log/slog"
	"math"
	"testing"
	"time"

	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance/internal/eval"
	"github.com/simon-lentz/yammm/internal/yammmtest"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/schema/expr"
)

// checkFloat accepts every unsigned value coerceFloat converts: a uint64
// above math.MaxInt64 is a finite float, not a type mismatch, and the largest
// uintptr is accepted whatever its width.
func TestCheckFloat_AcceptsUnsignedAboveInt64(t *testing.T) {
	t.Parallel()
	c := schema.NewFloatConstraint()
	for name, tc := range map[string]struct {
		val  any
		want float64
	}{
		"uint64":  {uint64(1<<63 + 5), float64(1<<63 + 5)},
		"uintptr": {^uintptr(0), float64(^uintptr(0))},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if err := eval.CheckValue(tc.val, c); err != nil {
				t.Fatalf("CheckValue(%T) = %v, want nil", tc.val, err)
			}
			got, err := eval.CoerceValue(tc.val, c)
			if err != nil || got != tc.want {
				t.Errorf("CoerceValue(%T) = %v, %v; want %v", tc.val, got, err, tc.want)
			}
		})
	}
}

// The String, Enum, Pattern and Boolean arms of CoerceValue refuse a value of
// the wrong kind, as every other arm does: a wrong-typed value or a typed nil
// pointer is an error, never reported as its own stored form.
func TestCoerceValue_RefusesTheWrongKind(t *testing.T) {
	t.Parallel()
	type carrier string
	var nilStr *string
	refuse := []struct {
		name string
		val  any
		c    schema.Constraint
	}{
		{"int at String", 42, schema.NewStringConstraint()},
		{"typed nil at String", nilStr, schema.NewStringConstraint()},
		{"string at Boolean", "true", schema.NewBooleanConstraint()},
		{"int at Enum", 7, schema.NewEnumConstraint([]string{"a"})},
		{"int at Pattern", 7, schema.NewPatternConstraint(nil)},
		{"json.Number at String", json.Number("42"), schema.NewStringConstraint()},
	}
	for _, tc := range refuse {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got, err := eval.CoerceValue(tc.val, tc.c); err == nil {
				t.Errorf("CoerceValue(%#v) = %#v, nil; want an error", tc.val, got)
			}
		})
	}
	if got, err := eval.CoerceValue(carrier("x"), schema.NewStringConstraint()); err != nil || got != "x" {
		t.Errorf("a named string carrier must coerce to its base: got %#v, %v", got, err)
	}
	if got, err := eval.CoerceValue(true, schema.NewBooleanConstraint()); err != nil || got != true {
		t.Errorf("a bool must coerce to itself: got %#v, %v", got, err)
	}
}

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
		{"a whole float is an Integer", 3.0, "Integer", true},
		{"a fractional float is not an Integer", 3.5, "Integer", false},
		{"a named string carrier is a String", carrier("x"), "String", true},
		{"a json.Number is not a String", json.Number("1"), "String", false},
		{"a named bool carrier is a Boolean", flag(true), "Boolean", true},
		{"fractional seconds are a Timestamp", "2024-01-15T10:30:00.123456789Z", "Timestamp", true},
		{"a time.Time is a Timestamp", time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC), "Timestamp", true},
		{"an int is not a Timestamp", 12345, "Timestamp", false},
		{"nil is no kind", nil, "String", false},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			got, err := eval.NewEvaluator().Evaluate(sx("=~", lit(r.val), expr.DatatypeLiteral(r.kind)), eval.EmptyScope())
			if err != nil {
				t.Fatal(err)
			}
			if got != r.want {
				t.Errorf("%#v =~ %s = %v, want %v", r.val, r.kind, got, r.want)
			}
		})
	}
}

// The trace op that brackets Evaluate ends with the evaluation's error, so a
// failed evaluation is logged as one; a successful one carries no error.
func TestEvaluate_EndsTheTraceOpWithItsError(t *testing.T) {
	t.Parallel()
	h := yammmtest.NewRecordHandler(slog.LevelDebug)
	ev := eval.NewEvaluator(eval.WithLogger(slog.New(h)))
	if _, err := ev.Evaluate(lit(int64(42)), eval.EmptyScope()); err != nil {
		t.Fatal(err)
	}
	if !yammmtest.HasAttr(h.Records(), "op", "yammm.eval.expr") {
		t.Fatal("the trace op was never logged")
	}
	if hasAttrKey(h.Records(), "error") {
		t.Error("a successful evaluation logged an error attribute")
	}
	if _, err := ev.Evaluate(sx("/", lit(int64(1)), lit(int64(0))), eval.EmptyScope()); err == nil {
		t.Fatal("control: 1/0 did not error")
	}
	if !yammmtest.HasAttr(h.Records(), "error", "division by zero") {
		t.Error("the failed evaluation's op ended without its error")
	}
}

func hasAttrKey(records []slog.Record, key string) bool {
	for _, r := range records {
		found := false
		r.Attrs(func(a slog.Attr) bool {
			if a.Key == key {
				found = true
				return false
			}
			return true
		})
		if found {
			return true
		}
	}
	return false
}

// Member access folds by immutable.Properties' rule — ASCII only, the
// alphabetically first key on a collision — and reads only the wrapped map
// every production value is; a raw Go map is not a receiver.
func TestAccessMember_FoldsByThePropertiesRule(t *testing.T) {
	t.Parallel()
	m := immutable.WrapMap(map[string]any{"NAME": "upper", "Name": "mixed", "name": "lower", "É": "acute"})
	t.Run("exact match first", func(t *testing.T) {
		t.Parallel()
		evalEq(t, sx(".", lit(m), lit("name")), "lower")
	})
	t.Run("collision takes the alphabetically first key", func(t *testing.T) {
		t.Parallel()
		evalEq(t, sx(".", lit(m), lit("nAmE")), "upper")
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
// one set schema/expr defines for both layers.
func TestDatatypeCheck_UnknownNameErrors(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"Vector", "List", "Enum", "Pattern", "int", "number"} {
		evalErr(t, sx("=~", lit("x"), expr.DatatypeLiteral(name)), "unknown datatype")
	}
	evalEq(t, sx("=~", lit("x"), expr.DatatypeLiteral("STRING")), true)
}
