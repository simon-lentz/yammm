package instance

import (
	"slices"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema"
)

// TestInvariant_TheCheckerAndTheEvaluatorReadARelationByOneRule holds the
// relation-spelling rule's two implementations to one another: for each
// spelling of the ITEM relation, the checker's verdict on a read it types (MAIN_LINE.S) agrees with
// what the evaluator reads through one it cannot type ([MAIN_LINE, name][0].S).
// A spelling the checker refuses reads nothing, and one it admits reads the
// child, so the invariant holds on the good order exactly when the checker
// admits the spelling. A variable names the relation by its field name.
func TestInvariant_TheCheckerAndTheEvaluatorReadARelationByOneRule(t *testing.T) {
	t.Parallel()
	for _, spelling := range []string{"ITEM", "item", "Item", "iTEM", "ItEm"} {
		t.Run(spelling, func(t *testing.T) {
			t.Parallel()
			_, typed := schema.LoadString(t.Context(), contractSource("MAIN_LINE."+spelling+" != nil"), "s.yammm")
			admitted := !typed.HasErrors()
			if !admitted {
				if !slices.ContainsFunc(slices.Collect(typed.Issues()), func(i diag.Issue) bool { return i.Code() == diag.E_INVALID_NAME }) {
					t.Fatalf("the typed read is refused, but not with E_INVALID_NAME: %v", typed.Err())
				}
			}
			s, res := schema.LoadString(t.Context(), contractSource("[MAIN_LINE, name][0]."+spelling+" != nil"), "s.yammm")
			if res.HasErrors() {
				t.Fatalf("the untyped read does not load: %v", res.Err())
			}
			vres := validateOrder(t, s, goodOrder())
			if holds := !vres.HasErrors(); holds != admitted {
				t.Errorf("the checker admits %s: %v; the evaluator reads the child through it: %v (%v)", spelling, admitted, holds, vres.Err())
			}
		})
	}
	// An association is reached untyped only through the instance itself, since
	// a part type, the one a list literal of children holds, declares none.
	for _, spelling := range []string{"PLACED_BY", "placed_by", "Placed_By", "pLACED_BY"} {
		t.Run("association "+spelling, func(t *testing.T) {
			t.Parallel()
			_, typed := schema.LoadString(t.Context(), contractSource("self."+spelling+" != nil"), "s.yammm")
			admitted := !typed.HasErrors()
			s, res := schema.LoadString(t.Context(), contractSource("[$self, name][0]."+spelling+" != nil"), "s.yammm")
			if res.HasErrors() {
				t.Fatalf("the untyped read does not load: %v", res.Err())
			}
			if holds := !validateOrder(t, s, goodOrder()).HasErrors(); holds != admitted {
				t.Errorf("the checker admits %s: %v; the evaluator reads the association through it: %v", spelling, admitted, holds)
			}
		})
	}
	t.Run("a variable by the field name", func(t *testing.T) {
		t.Parallel()
		s, res := schema.LoadString(t.Context(), contractSource("$main_line != nil && $placed_by != nil"), "s.yammm")
		if res.HasErrors() {
			t.Fatalf("load: %v", res.Err())
		}
		if vres := validateOrder(t, s, goodOrder()); vres.HasErrors() {
			t.Errorf("a variable naming a relation by its field name read nothing: %v", vres.Err())
		}
	})
}
