package schema_test

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema"
)

// TestWithWarnings_DeliversEachWarningInOrderAndNeverInTheError holds the
// callback to the issues below failure severity, each delivered once in the
// order the load finds them, and the load's error to its failures alone.
func TestWithWarnings_DeliversEachWarningInOrderAndNeverInTheError(t *testing.T) {
	t.Parallel()
	var src strings.Builder
	src.WriteString("schema \"main\"\ntype Orphan {\n")
	for i := range 5 {
		fmt.Fprintf(&src, "\tn%d Integer[-_, 5]\n", i)
	}
	src.WriteString("}")

	var lines []int
	s, err := schema.LoadString(t.Context(), src.String(), "main.yammm",
		schema.WithWarnings(func(issue diag.Issue) {
			if issue.Severity() != diag.Warning || issue.Code() != diag.E_INVALID_CONSTRAINT {
				t.Errorf("the callback received %v %v", issue.Severity(), issue.Code())
			}
			lines = append(lines, issue.Span().Start.Line)
		}))
	if !s.IsZero() {
		t.Fatal("a schema with an error loaded")
	}
	if want := []int{3, 4, 5, 6, 7}; !slices.Equal(lines, want) {
		t.Errorf("warning lines = %v; want %v", lines, want)
	}
	re, ok := errors.AsType[*diag.ResultError](err)
	if !ok {
		t.Fatalf("err = %v; want a *diag.ResultError", err)
	}
	if counts := re.Result.SeverityCounts(); counts.Warnings+counts.Info+counts.Hints != 0 || re.Result.Len() != 1 {
		t.Errorf("the error = %v; want the one failure and no issue below it", re.Result)
	}
	if errors.Is(err, diag.E_INVALID_CONSTRAINT) || !errors.Is(err, diag.E_NO_PRIMARY_KEY) {
		t.Errorf("the error = %v; want E_NO_PRIMARY_KEY and not the warnings' code", err)
	}
}

// TestWithWarnings_NilDiscards pins that a nil callback loads as the option's
// absence does: the warnings are dropped and the load succeeds.
func TestWithWarnings_NilDiscards(t *testing.T) {
	t.Parallel()
	const src = "schema \"main\"\ntype T {\n\tid String primary\n\tn Integer[-_, 5]\n}"
	for name, opts := range map[string][]schema.Option{
		"nil":    {schema.WithWarnings(nil)},
		"absent": nil,
	} {
		s, err := schema.LoadString(t.Context(), src, "main.yammm", opts...)
		if err != nil || s.IsZero() {
			t.Errorf("%s: load = (zero %t, %v); want a schema", name, s.IsZero(), err)
		}
	}
}
