package eval_test

import (
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance/internal/eval"
	"github.com/simon-lentz/yammm/internal/expr"
)

func stored(t *testing.T, elems ...any) any {
	t.Helper()
	s := immutable.Wrap(elems).Unwrap()
	if _, ok := s.(immutable.Slice); !ok {
		t.Fatalf("fixture: Wrap([]any).Unwrap() is %T, want immutable.Slice", s)
	}
	return s
}

// Flatten reads a nested list in the stored form, an immutable.Slice at each
// depth, as it reads a []any.
func TestFlatten_UnwrapsAStoredNestedList(t *testing.T) {
	t.Parallel()
	matrix := stored(t, stored(t, int64(1), int64(2)), stored(t, int64(3)))
	got, err := eval.NewEvaluator().Evaluate(t.Context(), makeBuiltinCall(lit(matrix), "Flatten", nil, nil, nil), eval.EmptyScope())
	if err != nil || !reflect.DeepEqual(got, []any{int64(1), int64(2), int64(3)}) {
		t.Errorf("Flatten(stored [[1 2] [3]]) = %#v, %v; want [1 2 3]", got, err)
	}
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
			got, err := eval.NewEvaluator().Evaluate(t.Context(), makeBuiltinCall(lit(tc.in), "Sum", nil, nil, nil), eval.EmptyScope())
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
			got, err := eval.NewEvaluator().Evaluate(t.Context(), makeBuiltinCall(lit(json.Number("5")), name, nil, nil, nil), eval.EmptyScope())
			if err != nil || got != int64(5) {
				t.Errorf("%s(json.Number(\"5\")) = %v (%T), %v; want int64(5)", name, got, got, err)
			}
			got, err = eval.NewEvaluator().Evaluate(t.Context(), makeBuiltinCall(lit(json.Number("2.5")), name, nil, nil, nil), eval.EmptyScope())
			if _, isFloat := got.(float64); err != nil || !isFloat {
				t.Errorf("%s(json.Number(\"2.5\")) = %v (%T), %v; want a float64", name, got, got, err)
			}
		})
	}
}

// The Date and Timestamp type checkers read a named string as the String
// checker does.
func TestTemporalTypeCheckers_ReadANamedString(t *testing.T) {
	t.Parallel()
	type named string
	if ok, msg := eval.IsDate()(named("2030-01-01")); !ok {
		t.Errorf("IsDate(a named string) = false: %s", msg)
	}
	if ok, msg := eval.IsTimestamp()(named("2030-01-01T00:00:00Z")); !ok {
		t.Errorf("IsTimestamp(a named string) = false: %s", msg)
	}
}

// A nil expression is an evaluation error, not a silent nil: the completer
// refuses an invariant without one, so nothing downstream tolerates it.
func TestEvaluate_NilExpressionIsAnError(t *testing.T) {
	t.Parallel()
	if got, err := eval.NewEvaluator().Evaluate(t.Context(), nil, eval.EmptyScope()); err == nil {
		t.Errorf("Evaluate(nil) = %v, nil; want an error", got)
	}
}

// The short direct-call form — a two- or three-element S-expression with no
// args, params or body slots — evaluates as the five-element form does.
func TestEvaluator_DirectBuiltinCallShortForm(t *testing.T) {
	t.Parallel()
	ev := eval.NewEvaluator()
	if got, err := ev.Evaluate(t.Context(), sx("len", lit("hello")), eval.EmptyScope()); err != nil || got != int64(5) {
		t.Errorf("(len \"hello\") = %v, %v; want 5", got, err)
	}
	if got, err := ev.Evaluate(t.Context(), sx("abs", lit(int64(-42))), eval.EmptyScope()); err != nil || got != int64(42) {
		t.Errorf("(abs -42) = %v, %v; want 42", got, err)
	}
	if got, err := ev.Evaluate(t.Context(), sx("compare", lit(int64(5)), lit([]expr.Expression{lit(int64(10))})), eval.EmptyScope()); err != nil || got != int64(-1) {
		t.Errorf("(compare 5 (args 10)) = %v, %v; want -1", got, err)
	}
}

// Min and Max with an argument refuse a list receiver in the stored form
// too, not only a []any literal.
func TestMinMax_WithArgumentRefuseAStoredListReceiver(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"Min", "Max"} {
		got, err := eval.NewEvaluator().Evaluate(t.Context(), makeBuiltinCall(lit(stored(t, "a", "b")), name, []expr.Expression{lit("z")}, nil, nil), eval.EmptyScope())
		if err == nil {
			t.Errorf("%s(stored list, \"z\") = %#v, want an error", name, got)
		}
	}
}

// Substring with a start past the length is the empty string, not a panic:
// the guard is the only thing between the call and a slice-bounds crash.
func TestSubstring_StartPastTheLengthIsEmpty(t *testing.T) {
	t.Parallel()
	got, err := eval.NewEvaluator().Evaluate(t.Context(), makeBuiltinCall(lit("north"), "Substring", []expr.Expression{lit(int64(7)), lit(int64(9))}, nil, nil), eval.EmptyScope())
	if err != nil || got != "" {
		t.Errorf("Substring(7, 9) on a five-rune value = %#v, %v; want \"\"", got, err)
	}
}
