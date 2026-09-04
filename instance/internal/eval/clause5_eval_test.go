package eval_test

import (
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance/internal/eval"
	"github.com/simon-lentz/yammm/internal/yammmtest"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/schema/expr"
)

type (
	carrierInt   int64
	carrierFloat float64
	carrierF32   float32
	carrierStr   string
)

func stored(t *testing.T, elems ...any) any {
	t.Helper()
	s := immutable.Wrap(elems).Unwrap()
	if _, ok := s.(immutable.Slice); !ok {
		t.Fatalf("fixture: Wrap([]any).Unwrap() is %T, want immutable.Slice", s)
	}
	return s
}

// Every list position reads the stored form: an immutable.Slice — what a
// List or Vector property unwraps to off a ValidInstance — is a list to the
// check, the coercion and Flatten alike, as it already was to value.Canonical.
func TestListPositionsReadTheStoredForm(t *testing.T) {
	t.Parallel()
	lc := schema.NewListConstraint(schema.NewStringConstraint())
	vc := schema.NewVectorConstraint(2)
	t.Run("CheckValue and CoerceValue under List", func(t *testing.T) {
		t.Parallel()
		in := stored(t, "a", "b")
		if err := eval.CheckValue(in, lc); err != nil {
			t.Errorf("CheckValue(immutable.Slice) = %v, want nil", err)
		}
		got, err := eval.CoerceValue(in, lc)
		if err != nil || !reflect.DeepEqual(got, []any{"a", "b"}) {
			t.Errorf("CoerceValue(immutable.Slice) = %#v, %v; want [a b]", got, err)
		}
	})
	t.Run("CheckValue and CoerceValue under Vector", func(t *testing.T) {
		t.Parallel()
		in := stored(t, 1.5, 2.5)
		if err := eval.CheckValue(in, vc); err != nil {
			t.Errorf("CheckValue(immutable.Slice) = %v, want nil", err)
		}
		got, err := eval.CoerceValue(in, vc)
		if err != nil || !reflect.DeepEqual(got, []float64{1.5, 2.5}) {
			t.Errorf("CoerceValue(immutable.Slice) = %#v, %v; want [1.5 2.5]", got, err)
		}
	})
	t.Run("an array is a list", func(t *testing.T) {
		t.Parallel()
		got, err := eval.CoerceValue([2]string{"a", "b"}, lc)
		if err != nil || !reflect.DeepEqual(got, []any{"a", "b"}) {
			t.Errorf("CoerceValue([2]string) = %#v, %v; want [a b]", got, err)
		}
	})
	t.Run("Flatten unwraps a stored nested list", func(t *testing.T) {
		t.Parallel()
		matrix := stored(t, stored(t, int64(1), int64(2)), stored(t, int64(3)))
		got, err := eval.NewEvaluator().Evaluate(makeBuiltinCall(lit(matrix), "Flatten", nil, nil, nil), eval.EmptyScope())
		if err != nil || !reflect.DeepEqual(got, []any{int64(1), int64(2), int64(3)}) {
			t.Errorf("Flatten(stored [[1 2] [3]]) = %#v, %v; want [1 2 3]", got, err)
		}
	})
}

// Sum sums in the kind it returns: a list holding a float is float
// arithmetic, so the discarded integer subtotal cannot overflow it. A list of
// integers still errors past int64, and an integer carried by a json.Number
// stays an integer.
func TestSum_SumsInTheKindItReturns(t *testing.T) {
	t.Parallel()
	maxI := int64(math.MaxInt64)
	for _, tc := range []struct {
		name    string
		in      []any
		want    any
		wantErr string
	}{
		{"float after the overflow", []any{maxI, maxI, 0.5}, float64(maxI) + float64(maxI) + 0.5, ""},
		{"float before the overflow", []any{0.5, maxI, maxI}, 0.5 + float64(maxI) + float64(maxI), ""},
		{"integers overflow", []any{maxI, int64(1)}, nil, "overflow"},
		{"integers in range", []any{int64(1), int64(2)}, int64(3), ""},
		{"json.Number integers stay integer", []any{json.Number("1"), json.Number("2")}, int64(3), ""},
		{"json.Number fraction is float", []any{json.Number("1"), json.Number("2.5")}, 3.5, ""},
		{"non-numeric", []any{int64(1), "a"}, nil, "numeric"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := eval.NewEvaluator().Evaluate(makeBuiltinCall(lit(tc.in), "Sum", nil, nil, nil), eval.EmptyScope())
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("got %v, %v; want an error containing %q", got, err, tc.wantErr)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Errorf("got %v (%T), %v; want %v (%T)", got, got, err, tc.want, tc.want)
			}
		})
	}
}

// Abs, Floor, Ceil and Round keep an integer an integer whatever carries it:
// a json.Number that holds an integer is classified as one, and the
// arithmetic must agree with that classification.
func TestArithmeticBuiltins_KeepAnIntegerCarrierInteger(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"Abs", "Floor", "Ceil", "Round"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := eval.NewEvaluator().Evaluate(makeBuiltinCall(lit(json.Number("5")), name, nil, nil, nil), eval.EmptyScope())
			if err != nil || got != int64(5) {
				t.Errorf("%s(json.Number(\"5\")) = %v (%T), %v; want int64(5)", name, got, got, err)
			}
			got, err = eval.NewEvaluator().Evaluate(makeBuiltinCall(lit(json.Number("2.5")), name, nil, nil, nil), eval.EmptyScope())
			if _, isFloat := got.(float64); err != nil || !isFloat {
				t.Errorf("%s(json.Number(\"2.5\")) = %v (%T), %v; want a float64", name, got, got, err)
			}
		})
	}
}

// One rule for every string-shaped kind: a named carrier over string reads
// at Date and Timestamp as it does at String, Enum, Pattern and UUID.
func TestTemporalChecksReadAStringCarrier(t *testing.T) {
	t.Parallel()
	d, ts := carrierStr("2030-01-01"), carrierStr("2030-01-01T00:00:00Z")
	if ok, msg := eval.IsDate()(d); !ok {
		t.Errorf("IsDate(carrier) = false: %s", msg)
	}
	if ok, msg := eval.IsTimestamp()(ts); !ok {
		t.Errorf("IsTimestamp(carrier) = false: %s", msg)
	}
	if err := eval.CheckValue(d, schema.NewDateConstraint()); err != nil {
		t.Errorf("CheckValue(carrier, Date) = %v", err)
	}
	if err := eval.CheckValue(ts, schema.NewTimestampConstraint()); err != nil {
		t.Errorf("CheckValue(carrier, Timestamp) = %v", err)
	}
	if got, err := eval.CoerceValue(d, schema.NewDateConstraint()); err != nil || got != "2030-01-01" {
		t.Errorf("CoerceValue(carrier, Date) = %#v, %v; want the base string", got, err)
	}
}

// Every CoerceValue arm returns (nil, error) on a wrong-typed value, the
// temporal and UUID arm included: the pass-through is instance.CanonicalValue's
// contract one layer up, not this one's.
func TestCoerceValue_EveryArmReturnsNilOnError(t *testing.T) {
	t.Parallel()
	for name, c := range map[string]schema.Constraint{
		"Timestamp": schema.NewTimestampConstraint(),
		"Date":      schema.NewDateConstraint(),
		"UUID":      schema.NewUUIDConstraint(),
		"String":    schema.NewStringConstraint(),
		"Integer":   schema.NewIntegerConstraint(),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := eval.CoerceValue(struct{ X int }{1}, c)
			if err == nil {
				t.Fatal("no error")
			}
			if got != nil {
				t.Errorf("CoerceValue returned %#v beside the error, want nil", got)
			}
		})
	}
}

// A whole float outside the int64 range at an Integer property is a type
// mismatch, as a fractional float is: the value cannot be the type. The
// message names the fact that holds.
func TestCheckInteger_OutOfRangeWholeFloatIsATypeMismatch(t *testing.T) {
	t.Parallel()
	err := eval.CheckValue(1e19, schema.NewIntegerConstraint())
	var ce *eval.CheckError
	if !errors.As(err, &ce) {
		t.Fatalf("err = %v, want a *CheckError", err)
	}
	if ce.Kind != eval.KindTypeMismatch || !strings.Contains(ce.Msg, "outside the int64 range") {
		t.Errorf("kind=%v msg=%q; want KindTypeMismatch naming the range", ce.Kind, ce.Msg)
	}
}

// The trace op ends with the panic: a crashed evaluation is logged as one,
// and the panic still propagates to the recover that owns it.
func TestEvaluate_EndsTheTraceOpWithThePanic(t *testing.T) {
	t.Parallel()
	h := yammmtest.NewRecordHandler(slog.LevelDebug)
	ev := eval.NewEvaluator(eval.WithLogger(slog.New(h)))
	var recovered any
	func() {
		defer func() { recovered = recover() }()
		_, _ = ev.Evaluate(sx("p", lit("x")), panicScope{})
	}()
	if recovered == nil {
		t.Fatal("the panic did not propagate")
	}
	if !yammmtest.HasAttr(h.Records(), "error", "panic: scope boom") {
		t.Error("the op ended without the panic as its error")
	}
}

type panicScope struct{}

func (panicScope) Lookup(string) (immutable.Value, bool)     { panic("scope boom") }
func (panicScope) LookupFold(string) (immutable.Value, bool) { panic("scope boom") }
func (panicScope) WithVar(string, any) eval.Scope            { panic("scope boom") }

// A nil expression is an evaluation error, not a silent nil: the completer
// refuses an invariant without one, so nothing downstream tolerates it.
func TestEvaluate_NilExpressionIsAnError(t *testing.T) {
	t.Parallel()
	if got, err := eval.NewEvaluator().Evaluate(nil, eval.EmptyScope()); err == nil {
		t.Errorf("Evaluate(nil) = %v, nil; want an error", got)
	}
}

// The short direct-call form — a two- or three-element S-expression with no
// args, params or body slots — is live code reachable through
// schema.Builder, and evaluates as the five-element form does.
func TestEvaluator_DirectBuiltinCallShortForm(t *testing.T) {
	t.Parallel()
	ev := eval.NewEvaluator()
	if got, err := ev.Evaluate(sx("len", lit("hello")), eval.EmptyScope()); err != nil || got != int64(5) {
		t.Errorf("(len \"hello\") = %v, %v; want 5", got, err)
	}
	if got, err := ev.Evaluate(sx("abs", lit(int64(-42))), eval.EmptyScope()); err != nil || got != int64(42) {
		t.Errorf("(abs -42) = %v, %v; want 42", got, err)
	}
	if got, err := ev.Evaluate(sx("compare", lit(int64(5)), lit([]expr.Expression{lit(int64(10))})), eval.EmptyScope()); err != nil || got != int64(-1) {
		t.Errorf("(compare 5 (args 10)) = %v, %v; want -1", got, err)
	}
}

// Min and Max with an argument refuse a list receiver in the stored form
// too, not only a []any literal.
func TestMinMax_WithArgumentRefuseAStoredListReceiver(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"Min", "Max"} {
		got, err := eval.NewEvaluator().Evaluate(makeBuiltinCall(lit(stored(t, "a", "b")), name, []expr.Expression{lit("z")}, nil, nil), eval.EmptyScope())
		if err == nil {
			t.Errorf("%s(stored list, \"z\") = %#v, want an error", name, got)
		}
	}
}

// A named carrier over a number — adapter/gogen's shape for a DataType over
// int64, float64 or float32 — is checked and coerced at Integer, Float and
// Vector as its base value is, and the stored form is the base.
func TestCheckAndCoerce_NamedNumericCarriers(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		in   any
		c    schema.Constraint
		want any
	}{
		{"int64 carrier at Integer", carrierInt(42), schema.NewIntegerConstraint(), int64(42)},
		{"float64 carrier at Float", carrierFloat(2.5), schema.NewFloatConstraint(), 2.5},
		{"float32 carrier at Float", carrierF32(1.5), schema.NewFloatConstraint(), 1.5},
		{"int64 carrier at Float", carrierInt(3), schema.NewFloatConstraint(), 3.0},
		{"carriers at Vector", []any{carrierFloat(1.0), carrierInt(2), carrierF32(3.0)}, schema.NewVectorConstraint(3), []float64{1, 2, 3}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if err := eval.CheckValue(tc.in, tc.c); err != nil {
				t.Errorf("CheckValue(%T) = %v, want nil", tc.in, err)
			}
			got, err := eval.CoerceValue(tc.in, tc.c)
			if err != nil || !reflect.DeepEqual(got, tc.want) {
				t.Errorf("CoerceValue(%T) = %#v (%T), %v; want %#v", tc.in, got, got, err, tc.want)
			}
		})
	}
}

// Substring with a start past the length is the empty string, not a panic:
// the guard is the only thing between the call and a slice-bounds crash.
func TestSubstring_StartPastTheLengthIsEmpty(t *testing.T) {
	t.Parallel()
	got, err := eval.NewEvaluator().Evaluate(makeBuiltinCall(lit("north"), "Substring", []expr.Expression{lit(int64(7)), lit(int64(9))}, nil, nil), eval.EmptyScope())
	if err != nil || got != "" {
		t.Errorf("Substring(7, 9) on a five-rune value = %#v, %v; want \"\"", got, err)
	}
}
