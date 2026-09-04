package instance

import (
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/instance/internal/eval"
)

// One classifier for a check error, at every check site: a recovered panic
// is Fatal E_INTERNAL with its stack, a constraint failure E_CONSTRAINT_FAIL,
// anything else E_TYPE_MISMATCH.
func TestCheckIssue_ClassifiesEveryKind(t *testing.T) {
	t.Parallel()
	internal := &InternalError{Kind: KindConstraintPanic, Stack: "goroutine 1"}
	is := checkIssue(internal, "FK field \"x\"").Build()
	if is.Code() != diag.E_INTERNAL || is.Severity() != diag.Fatal {
		t.Errorf("a recovered panic is %s %s, want Fatal E_INTERNAL", is.Severity(), is.Code())
	}
	cf := checkIssue(&eval.CheckError{Kind: eval.KindConstraintFail, Msg: "too big"}, "FK field \"x\"").Build()
	if cf.Code() != diag.E_CONSTRAINT_FAIL {
		t.Errorf("a constraint failure is %s, want E_CONSTRAINT_FAIL", cf.Code())
	}
	tm := checkIssue(&eval.CheckError{Kind: eval.KindTypeMismatch, Msg: "wrong"}, "FK field \"x\"").Build()
	if tm.Code() != diag.E_TYPE_MISMATCH {
		t.Errorf("a type mismatch is %s, want E_TYPE_MISMATCH", tm.Code())
	}
}
