package schema_test

import (
	"testing"
)

// TestInheritance_ARedeclarationStandsInPlaceOfWhatItRedeclares pins the
// record a subtype holds for a property its supertype re-declares equal to an
// ancestor's: the supertype's, with its documentation, scope and datatype.
func TestInheritance_ARedeclarationStandsInPlaceOfWhatItRedeclares(t *testing.T) {
	t.Parallel()
	s := mustLoad(t, `schema "s"
type D = Integer[0, 10]
abstract type G {
	/* the grandparent's */
	x Integer[0, 10]
}
abstract type P extends G {
	/* the parent's */
	x D
}
type C extends P {
	id String primary
}
`)
	c, _ := s.Type("C")
	x, _ := c.Property("x")
	if got := x.Documentation(); got != "the parent's" {
		t.Errorf("C.x's documentation is %q, want the parent's", got)
	}
	if got := x.DeclaringScope().String(); got != "P" {
		t.Errorf("C.x is declared on %s, want P", got)
	}
	if got := x.DataTypeRef().Name(); got != "D" {
		t.Errorf("C.x reads datatype %q, want D", got)
	}
}
