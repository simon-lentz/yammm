package eval_test

import (
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/instance/internal/eval"
	"github.com/simon-lentz/yammm/internal/yammmtest"
	"github.com/simon-lentz/yammm/schema/expr"
)

// lit wraps a Go value as a literal expression.
func lit(v any) expr.Expression { return expr.NewLiteral(v) }

// sx builds an S-expression: sx("+", lit(1), lit(2)).
func sx(op string, children ...expr.Expression) expr.SExpr {
	s := expr.SExpr{expr.Op(op)}
	return append(s, children...)
}

// list builds a []-SExpr whose elements are the given values as literals.
func list(vals ...any) expr.SExpr {
	s := expr.SExpr{expr.Op("[]")}
	for _, v := range vals {
		s = append(s, expr.NewLiteral(v))
	}
	return s
}

// evalEq evaluates e in an empty scope and reports any difference from want.
// The evaluator is stateless, so a fresh one per call is equivalent to a
// shared instance.
func evalEq(t *testing.T, e expr.Expression, want any) {
	t.Helper()
	result, err := eval.NewEvaluator().Evaluate(e, eval.EmptyScope())
	if err != nil {
		t.Fatalf("Evaluate(%v): %v", e, err)
	}
	yammmtest.Diff(t, want, result)
}

// evalErr evaluates e in an empty scope, requires an error, and — when substr
// is non-empty — requires the error message to contain substr.
func evalErr(t *testing.T, e expr.Expression, substr string) {
	t.Helper()
	_, err := eval.NewEvaluator().Evaluate(e, eval.EmptyScope())
	if err == nil {
		t.Fatalf("Evaluate(%v): want error containing %q, got nil", e, substr)
	}
	if substr != "" && !strings.Contains(err.Error(), substr) {
		t.Errorf("Evaluate(%v) error = %q; want substring %q", e, err, substr)
	}
}
