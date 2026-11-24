package diagtest_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/simon-lentz/yammm/internal/diagtest"
)

// TestFailures_ReportsAnErrorWithNoDiagnostics pins that a failure holding no
// *diag.ResultError fails the test, naming the error, and adds no issue.
func TestFailures_ReportsAnErrorWithNoDiagnostics(t *testing.T) {
	t.Parallel()
	tb := &recordingTB{TB: t}
	res := diagtest.Failures(tb, errors.New("refused option"))
	if len(tb.errors) != 1 || tb.errors[0] != "diagtest: the call failed with no diagnostics: refused option" {
		t.Errorf("Errorf calls = %q; want one naming the error", tb.errors)
	}
	if res.Len() != 0 {
		t.Errorf("result holds %d issues; want none", res.Len())
	}
}

// recordingTB records Errorf instead of failing the test.
type recordingTB struct {
	testing.TB
	errors []string
}

func (r *recordingTB) Helper() {}

func (r *recordingTB) Errorf(format string, args ...any) {
	r.errors = append(r.errors, fmt.Sprintf(format, args...))
}
