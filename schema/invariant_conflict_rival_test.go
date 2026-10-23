package schema_test

import (
	"maps"
	"testing"

	"github.com/simon-lentz/yammm/diag"
)

// TestInvariant_ARivalReachedTwiceIsNamedOnce pins that an inherited
// definition that conflicts, reached through two supertypes, is named once
// among the conflict's related spans.
func TestInvariant_ARivalReachedTwiceIsNamedOnce(t *testing.T) {
	t.Parallel()
	res := loadStringErr(t, `schema "main"
abstract type P {
	x Integer
	! "m" x == 1
}
abstract type Q {
	x Integer
	! "m" x == 2
}
abstract type M1 extends Q { y Integer }
abstract type M2 extends Q { z Integer }
type T extends P, M1, M2 { id String primary }`)
	wantCounts(t, res, map[diag.Code]int{diag.E_INVARIANT_CONFLICT: 1})
	got := make(map[int]string)
	count := make(map[int]int)
	for i := range res.Issues() {
		if i.Code() == diag.E_INVARIANT_CONFLICT {
			for _, r := range i.Related() {
				got[r.Span.Start.Line] = r.Message
				count[r.Span.Start.Line]++
			}
		}
	}
	want := map[int]string{4: "kept definition declared here", 8: "conflicting definition declared here"}
	if !maps.Equal(got, want) || !maps.Equal(count, map[int]int{4: 1, 8: 1}) {
		t.Errorf("related spans by line %v, counted %v; want %v, once each", got, count, want)
	}
}
