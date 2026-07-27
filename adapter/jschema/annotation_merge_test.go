package jschema

import (
	"strings"
	"testing"
)

// mergedAnnotationSchema declares the same datatype-typed property on two
// abstract ancestors, one of them annotated, so linearization gives the child a
// merged view whose property carries the union of both annotation sets. That
// merged property is a synthesized copy that appears in no type's own property
// slice, which the $defs datatype table is keyed by.
const mergedAnnotationSchema = `schema "probe"
type Money = Float[0.0, _]
abstract type A {
	id String primary
	amount Money
}
abstract type B {
	id String primary
	amount Money @writeOnce
}
type C extends A, B {}
`

// TestMarshal_MergedAnnotationProperty pins that a property whose annotations
// were merged across ancestors resolves its named DataType, because the $defs
// table is keyed by the declared property and read through [schema.Property.Origin].
//
// The assertion is the emitted $ref, not the property name: the name appears
// whether the property rendered as a reference to Money or as an inlined
// {"type":"number","minimum":0}, so checking for it alone would still pass if a
// future change silently degraded every merged DataType property to an inline
// constraint — losing the $defs indirection the generator exists to emit.
func TestMarshal_MergedAnnotationProperty(t *testing.T) {
	s := loadFixture(t, mergedAnnotationSchema, "probe.yammm")

	got, err := Marshal(s)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	out := string(got)
	if !strings.Contains(out, `"amount"`) {
		t.Errorf("generated schema should carry the amount property; got:\n%s", out)
	}
	if !strings.Contains(out, `"#/$defs/Money"`) {
		t.Errorf("amount should render as a $ref to the Money datatype; got:\n%s", out)
	}
}
