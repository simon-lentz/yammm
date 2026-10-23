package schema_test

import (
	"testing"

	"github.com/simon-lentz/yammm/schema"
)

// TestConstraint_ANilPointerHeldIsNoConstraint pins that each constructor that
// holds a constraint reads a nil pointer of any constraint type as nil: a List
// built over one answers String, Equal, NarrowsTo and IsResolved as a List with
// no element does, and a reference to one answers Equal, NarrowsTo and
// IsResolved as a reference to no constraint does. Called through the nil
// pointer, each of those methods panics.
func TestConstraint_ANilPointerHeldIsNoConstraint(t *testing.T) {
	t.Parallel()
	nilString := (*schema.StringConstraint)(nil)
	nilEnum := (*schema.EnumConstraint)(nil)
	for name, c := range map[string]schema.ListConstraint{
		"NewListConstraint": schema.NewListConstraint(nilString),
		"ListMinLen":        schema.ListMinLen(nilEnum, 1),
		"ListMaxLen":        schema.ListMaxLen(nilString, 2),
		"ListLenBetween":    schema.ListLenBetween(nilEnum, 1, 2),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if c.Element() != nil {
				t.Errorf("Element() = %#v, want nil", c.Element())
			}
			want := schema.ListConstraint{}.String()
			if got := c.String(); got[:len(want)] != want {
				t.Errorf("String() = %q, want it to open %q", got, want)
			}
			twin := c
			if !c.Equal(twin) || c.NarrowsTo(twin) || c.IsResolved() {
				t.Errorf("Equal %v, NarrowsTo %v, IsResolved %v; want true, false, false", c.Equal(twin), c.NarrowsTo(twin), c.IsResolved())
			}
		})
	}
	t.Run("NewAliasConstraint", func(t *testing.T) {
		t.Parallel()
		ref := schema.NewAliasConstraint("A", nilString)
		if ref.Resolved() != nil {
			t.Errorf("Resolved() = %#v, want nil", ref.Resolved())
		}
		if ref.Equal(schema.NewStringConstraint()) || ref.NarrowsTo(ref) || ref.IsResolved() {
			t.Errorf("Equal to a String %v, NarrowsTo itself %v, IsResolved %v; want all false",
				ref.Equal(schema.NewStringConstraint()), ref.NarrowsTo(ref), ref.IsResolved())
		}
		if err := schema.CheckConstraint(ref); err == nil || err.Error() != `schema: datatype "A" resolves to no constraint` {
			t.Errorf("CheckConstraint = %v, want the reference to no constraint", err)
		}
	})
}
