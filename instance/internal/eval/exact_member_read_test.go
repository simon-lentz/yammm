package eval_test

import (
	"testing"

	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance/internal/eval"
)

// exactMembers holds a property and a relation, each under its declared name.
func exactMembers() map[string]any {
	return map[string]any{"name": "Alice", "WORKS_AT": "k"}
}

// A bare name reads a member by its declared name alone: another casing of a
// property or of a relation reads nothing.
func TestEvaluate_ABareNameInAnotherCaseReadsNothing(t *testing.T) {
	t.Parallel()
	scope := eval.PropertyScopeFromMap(exactMembers())
	for name, want := range map[string]any{"name": "Alice", "WORKS_AT": "k", "Name": nil, "NAME": nil, "works_at": nil, "Works_At": nil} {
		got, err := eval.NewEvaluator().Evaluate(t.Context(), sx("p", lit(name)), scope)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got != want {
			t.Errorf("%s = %v, want %v", name, got, want)
		}
	}
}

// A member after a dot is read by its declared name alone, and only from the
// wrapped map every production value is.
func TestEvaluate_AMemberInAnotherCaseReadsNothing(t *testing.T) {
	t.Parallel()
	m := immutable.WrapProperties(exactMembers())
	for name, want := range map[string]any{"name": "Alice", "WORKS_AT": "k", "Name": nil, "nAmE": nil, "works_at": nil, "wORKS_AT": nil} {
		evalEq(t, sx(".", lit(m), lit(name)), want)
	}
	evalErr(t, sx(".", lit(map[string]any{"name": "x"}), lit("name")), "cannot access member")
}
