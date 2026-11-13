package instance_test

import (
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/yammmtest"
)

// Each invariant issue names its invariant in a detail, beside the type. A
// failure's message is the invariant's name, and an evaluation error's message
// names the invariant before its cause.
func TestValidateOne_InvariantIssuesNameTheirInvariant(t *testing.T) {
	t.Parallel()
	s := loadT(t, `schema "s"

type T {
	id String primary
	n Integer
	! "n is positive" n > 0
	! "ten over n is whole" 10 % n == 0
}
`)
	_, res := instance.NewValidator(s).ValidateOne(t.Context(), "T", instance.RawInstance{Properties: map[string]any{"id": "x", "n": int64(0)}})

	failed := mustIssue(t, res, diag.E_INVARIANT_FAIL)
	yammmtest.Diff(t, "n is positive", failed.Message())
	yammmtest.Diff(t, []string{"n is positive"}, detailValues(failed, diag.DetailKeyInvariantName))
	yammmtest.Diff(t, []string{"T"}, detailValues(failed, diag.DetailKeyTypeName))

	errored := mustIssue(t, res, diag.E_EVAL_ERROR)
	yammmtest.Diff(t, `invariant "ten over n is whole": modulo by zero`, errored.Message())
	yammmtest.Diff(t, []string{"ten over n is whole"}, detailValues(errored, diag.DetailKeyInvariantName))
	yammmtest.Diff(t, []string{"T"}, detailValues(errored, diag.DetailKeyTypeName))

	// The key's text and the details' order are what a JSON reader sees.
	yammmtest.Diff(t, []diag.Detail{{Key: "type", Value: "T"}, {Key: "invariant", Value: "n is positive"}}, failed.Details())
	yammmtest.Diff(t, []diag.Detail{{Key: "type", Value: "T"}, {Key: "invariant", Value: "ten over n is whole"}}, errored.Details())

	if got := len(codes(res)); got != 2 {
		t.Errorf("ValidateOne reported %d issues, want the two invariants': %v", got, res.Err())
	}
}

// An evaluation error's message writes the invariant's name as a quoted
// string, so a quote mark in the name is escaped there. The failure's message
// and the detail hold the name as the schema wrote it.
func TestValidateOne_AnEvaluationErrorQuotesTheInvariantName(t *testing.T) {
	t.Parallel()
	s := loadT(t, `schema "s"

type T {
	id String primary
	n Integer
	! "\"n\" is positive" n > 0
	! "ten over \"n\" is whole" 10 % n == 0
}
`)
	_, res := instance.NewValidator(s).ValidateOne(t.Context(), "T", instance.RawInstance{Properties: map[string]any{"id": "x", "n": int64(0)}})

	failed := mustIssue(t, res, diag.E_INVARIANT_FAIL)
	yammmtest.Diff(t, `"n" is positive`, failed.Message())
	yammmtest.Diff(t, []string{`"n" is positive`}, detailValues(failed, diag.DetailKeyInvariantName))

	errored := mustIssue(t, res, diag.E_EVAL_ERROR)
	yammmtest.Diff(t, `invariant "ten over \"n\" is whole": modulo by zero`, errored.Message())
	yammmtest.Diff(t, []string{`ten over "n" is whole`}, detailValues(errored, diag.DetailKeyInvariantName))
}

// Max with an argument yields the lower-ranked side when the higher is absent,
// so the check judges no later stage by the higher side alone: the invariant
// loads, holds without the string and errors with it.
func TestValidateOne_MaxYieldsTheLowerSideWhenTheHigherIsAbsent(t *testing.T) {
	t.Parallel()
	s := loadT(t, `schema "s"

type T {
	id String primary
	name String
	flag Boolean
	! "m" (name -> Max(1)) -> Abs > 0 && !(name -> Max(flag))
}
`)
	v := instance.NewValidator(s)
	_, res := v.ValidateOne(t.Context(), "T", instance.RawInstance{Properties: map[string]any{"id": "x", "flag": false}})
	if res.Err() != nil {
		t.Errorf("without the string: %v", res.Err())
	}
	_, res = v.ValidateOne(t.Context(), "T", instance.RawInstance{Properties: map[string]any{"id": "x", "flag": false, "name": "n"}})
	mustIssue(t, res, diag.E_EVAL_ERROR)
}

// A lambda over the empty list literal never binds its parameter, so the check
// judges no operand in its body by the parameter.
func TestValidateOne_ALambdaOverTheEmptyListIsNotJudgedByItsParameter(t *testing.T) {
	t.Parallel()
	s := loadT(t, `schema "s"

type T {
	id String primary
	tags List<String>
	! "m" [] -> All |$x| { !$x && $x % 2 == 0 } && [] -> Map { $0 -> Upper } -> Len == 0 && ([] + tags)[0] -> Upper != "A"
}
`)
	_, res := instance.NewValidator(s).ValidateOne(t.Context(), "T", instance.RawInstance{Properties: map[string]any{"id": "x", "tags": []any{"b"}}})
	if res.Err() != nil {
		t.Errorf("got %v, want the invariant to hold", res.Err())
	}
}
