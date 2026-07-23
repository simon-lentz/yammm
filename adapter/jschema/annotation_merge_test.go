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
// were merged across ancestors still resolves its $defs key. Keying the table by
// the raw merged pointer instead of the declared one made Marshal fail with "no
// registered $defs key for datatype property" on a schema that loads clean.
func TestMarshal_MergedAnnotationProperty(t *testing.T) {
	s := loadFixture(t, mergedAnnotationSchema, "probe.yammm")

	got, err := Marshal(s)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !strings.Contains(string(got), `"amount"`) {
		t.Errorf("generated schema should carry the amount property; got:\n%s", got)
	}
}
