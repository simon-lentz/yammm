package schema_test

import (
	"slices"
	"testing"

	"github.com/simon-lentz/yammm/diag"
)

// A property is read by its declared name alone: a bare name, a member and a
// $ variable spelled in another case read no member, so load refuses each.
func TestStaticInvariant_APropertyInAnotherCaseIsNoMember(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		inv  string
		code diag.Code
	}{
		{`Name != ""`, diag.E_UNKNOWN_PROPERTY},
		{`MAIN_LINE.QTY > 0`, diag.E_UNKNOWN_PROPERTY},
		{`$Name != ""`, diag.E_INVALID_INVARIANT},
	} {
		t.Run(c.inv, func(t *testing.T) {
			t.Parallel()
			issues := slices.Collect(loadInvariant(t, c.inv).Issues())
			if len(issues) != 1 || issues[0].Code() != c.code {
				t.Errorf("issues = %v, want one %s", issues, c.code)
			}
		})
	}
}
