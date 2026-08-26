package instance

import (
	"errors"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
)

// coercionIssue classifies a coercion failure for the two edge slots. Both
// slots previously assigned the raw value and reported nothing, so a failure —
// including a RECOVERED PANIC — reached the validated instance as data while
// the node-property slot reported the same failure.
//
// The path is defensive: CoerceValue is not reachable-in-failure after
// CheckValue passes through any constructor this library exposes — the schema
// Builder cannot attach a constraint to an edge property, and the DSL cannot
// express a malformed one. That is why the classifier is pinned here directly
// rather than through the validator: an end-to-end test would need an input no
// caller can construct.
//
// Mutation: dropping the InternalError arm turns the first subtest red, and the
// panic is reported as an ordinary type mismatch with no stack.
func TestCoercionIssue_ClassifiesAPanicApartFromAConversionFailure(t *testing.T) {
	t.Run("recovered panic is Fatal E_INTERNAL carrying its stack", func(t *testing.T) {
		internal := &InternalError{
			Kind:  KindConstraintPanic,
			Cause: errors.New("boom"),
			Stack: "goroutine 1 [running]:\nmain.main()",
		}

		issue := coercionIssue(internal, `edge property "role"`).Build()

		if issue.Severity() != diag.Fatal {
			t.Errorf("severity = %v, want Fatal", issue.Severity())
		}
		if issue.Code() != diag.E_INTERNAL {
			t.Errorf("code = %s, want E_INTERNAL", issue.Code())
		}
		if !hasDetail(issue, diag.DetailKeyStackTrace, "goroutine") {
			t.Error("a recovered panic carries no stack trace detail")
		}
	})

	t.Run("an ordinary failure is an Error type mismatch naming the slot", func(t *testing.T) {
		issue := coercionIssue(errors.New("cannot convert"), `FK field "_target_id"`).Build()

		if issue.Severity() != diag.Error {
			t.Errorf("severity = %v, want Error", issue.Severity())
		}
		if issue.Code() != ErrTypeMismatch {
			t.Errorf("code = %s, want %s", issue.Code(), ErrTypeMismatch)
		}
		if !strings.Contains(issue.Message(), `FK field "_target_id"`) {
			t.Errorf("message does not name the slot: %s", issue.Message())
		}
		if !strings.Contains(issue.Message(), "coercion error") {
			t.Errorf("message does not say what failed: %s", issue.Message())
		}
		if hasDetail(issue, diag.DetailKeyStackTrace, "") {
			t.Error("an ordinary conversion failure carries a stack trace")
		}
	})
}

// hasDetail reports whether issue carries key, and — when substr is non-empty —
// whether its value contains substr.
func hasDetail(issue diag.Issue, key, substr string) bool {
	for _, d := range issue.Details() {
		if d.Key == key {
			return substr == "" || strings.Contains(d.Value, substr)
		}
	}
	return false
}
