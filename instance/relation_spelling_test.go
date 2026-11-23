package instance

import (
	"slices"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/internal/diagtest"
	"github.com/simon-lentz/yammm/schema"
)

// TestInvariant_TheCheckerAndTheEvaluatorReadARelationByOneRule holds the
// relation-spelling rule's two implementations to one another: for each
// spelling of the ITEM relation, the checker's verdict on a read it types (MAIN_LINE.S) agrees with
// what the evaluator reads through one it cannot type ([MAIN_LINE, name][0].S).
// A spelling the checker refuses reads nothing, and one it admits reads the
// child, so the invariant holds on the good order exactly when the checker
// admits the spelling. A variable names the relation by its declared name
// alone.
func TestInvariant_TheCheckerAndTheEvaluatorReadARelationByOneRule(t *testing.T) {
	t.Parallel()
	for _, spelling := range []string{"ITEM", "item", "Item", "iTEM", "ItEm"} {
		t.Run(spelling, func(t *testing.T) {
			t.Parallel()
			_, typed := diagtest.Capture(t, func(warn func(diag.Issue)) (schema.Schema, error) {
				return schema.LoadString(t.Context(), contractSource("MAIN_LINE."+spelling+" != nil"), "s.yammm", schema.WithWarnings(warn))
			})
			admitted := diagtest.Err(typed) == nil
			if !admitted {
				if !slices.ContainsFunc(slices.Collect(typed.Issues()), func(i diag.Issue) bool { return i.Code() == diag.E_INVALID_NAME }) {
					t.Fatalf("the typed read is refused, but not with E_INVALID_NAME: %v", typed)
				}
			}
			s, err := schema.LoadString(t.Context(), contractSource("[MAIN_LINE, name][0]."+spelling+" != nil"), "s.yammm")
			if err != nil {
				t.Fatalf("the untyped read does not load: %v", err)
			}
			vres := validateOrder(t, s, goodOrder())
			if holds := diagtest.Err(vres) == nil; holds != admitted {
				t.Errorf("the checker admits %s: %v; the evaluator reads the child through it: %v (%v)", spelling, admitted, holds, vres)
			}
		})
	}
	// An association is reached untyped only through the instance itself, since
	// a part type, the one a list literal of children holds, declares none.
	for _, spelling := range []string{"PLACED_BY", "placed_by", "Placed_By", "pLACED_BY"} {
		t.Run("association "+spelling, func(t *testing.T) {
			t.Parallel()
			_, err := schema.LoadString(t.Context(), contractSource("self."+spelling+" != nil"), "s.yammm")
			admitted := err == nil
			s, err := schema.LoadString(t.Context(), contractSource("[$self, name][0]."+spelling+" != nil"), "s.yammm")
			if err != nil {
				t.Fatalf("the untyped read does not load: %v", err)
			}
			if holds := diagtest.Err(validateOrder(t, s, goodOrder())) == nil; holds != admitted {
				t.Errorf("the checker admits %s: %v; the evaluator reads the association through it: %v", spelling, admitted, holds)
			}
		})
	}
	t.Run("a variable by the declared name", func(t *testing.T) {
		t.Parallel()
		s, err := schema.LoadString(t.Context(), contractSource("$MAIN_LINE != nil && $PLACED_BY != nil"), "s.yammm")
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		if vres := validateOrder(t, s, goodOrder()); diagtest.Err(vres) != nil {
			t.Errorf("a variable naming a relation by its declared name read nothing: %v", vres)
		}
	})
	for _, spelling := range []string{"$placed_by", "$Placed_By", "$main_line"} {
		t.Run("a variable in another casing "+spelling, func(t *testing.T) {
			t.Parallel()
			_, res := diagtest.Capture(t, func(warn func(diag.Issue)) (schema.Schema, error) {
				return schema.LoadString(t.Context(), contractSource(spelling+" != nil"), "s.yammm", schema.WithWarnings(warn))
			})
			codes := slices.Collect(func(yield func(diag.Code) bool) {
				for i := range res.Issues() {
					if !yield(i.Code()) {
						return
					}
				}
			})
			if !slices.Equal(codes, []diag.Code{diag.E_INVALID_NAME}) {
				t.Errorf("%s drew %v, want one E_INVALID_NAME as the bare and member reads draw", spelling, codes)
			}
		})
	}
}
