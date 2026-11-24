package snapshot

import (
	"errors"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/instance"
)

// TestRowFindings_ReadsEachErrorItMayBeHanded pins rowFindings over the three
// errors it may be handed: none, a *diag.ResultError, whose failures it
// returns, and any other, which it reads as one Fatal E_INTERNAL.
func TestRowFindings_ReadsEachErrorItMayBeHanded(t *testing.T) {
	t.Parallel()
	if got := rowFindings(instance.ValidInstance{}, nil); got.Len() != 0 {
		t.Errorf("rowFindings(nil) = %s, want empty", got)
	}
	c := diag.NewCollector(diag.NoLimit)
	c.Collect(diag.NewIssue(diag.Error, diag.E_TYPE_MISMATCH, "bad").Build())
	if got := rowFindings(instance.ValidInstance{}, c.Err()); got.Len() != 1 || !got.HasCode(diag.E_TYPE_MISMATCH) {
		t.Errorf("rowFindings(ResultError) = %s, want its one E_TYPE_MISMATCH", got)
	}
	got := rowFindings(instance.ValidInstance{}, errors.New("x"))
	if got.Len() != 1 || got.SeverityCounts().Fatal != 1 || !got.HasCode(diag.E_INTERNAL) {
		t.Fatalf("rowFindings(another error) = %s, want one Fatal E_INTERNAL", got)
	}
	for issue := range got.Issues() {
		if issue.Message() != "x" {
			t.Errorf("message = %q, want %q", issue.Message(), "x")
		}
	}
}
