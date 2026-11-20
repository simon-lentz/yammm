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
		"NewListConstraint": schema.TestNewListConstraint(nilString),
		"ListMinLen":        schema.TestListMinLen(nilEnum, 1),
		"ListMaxLen":        schema.TestListMaxLen(nilString, 2),
		"ListLenBetween":    schema.TestListLenBetween(nilEnum, 1, 2),
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
			if !schema.TestConstraintEqual(c, twin) || schema.TestConstraintNarrowsTo(c, twin) || schema.TestConstraintIsResolved(c) {
				t.Errorf("Equal %v, NarrowsTo %v, IsResolved %v; want true, false, false", schema.TestConstraintEqual(c, twin), schema.TestConstraintNarrowsTo(c, twin), schema.TestConstraintIsResolved(c))
			}
		})
	}
	t.Run("NewAliasConstraint", func(t *testing.T) {
		t.Parallel()
		ref := schema.TestNewAliasConstraint("A", nilString)
		if ref.Resolved() != nil {
			t.Errorf("Resolved() = %#v, want nil", ref.Resolved())
		}
		if schema.TestConstraintEqual(ref, schema.TestNewStringConstraint()) || schema.TestConstraintNarrowsTo(ref, ref) || schema.TestConstraintIsResolved(ref) {
			t.Errorf("Equal to a String %v, NarrowsTo itself %v, IsResolved %v; want all false",
				schema.TestConstraintEqual(ref, schema.TestNewStringConstraint()), schema.TestConstraintNarrowsTo(ref, ref), schema.TestConstraintIsResolved(ref))
		}
		if err := schema.CheckConstraint(ref); err == nil || err.Error() != `schema: datatype "A" resolves to no constraint` {
			t.Errorf("CheckConstraint = %v, want the reference to no constraint", err)
		}
	})
}
