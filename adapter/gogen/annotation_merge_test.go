package gogen_test

import (
	"context"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/adapter/gogen"
	"github.com/simon-lentz/yammm/schema"
)

// mergedAnnotationSchema declares the same datatype-typed property on two
// abstract ancestors, one of them annotated, so linearization gives the child a
// merged view whose property carries the union of both annotation sets. That
// merged property is a synthesized copy that appears in no type's own property
// slice, which the datatype field table is keyed by.
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
// were merged across ancestors still resolves its named DataType. Keying the
// table by the raw merged pointer instead of the declared one made Marshal fail
// with "no registered Go name for datatype property" on a schema that loads
// clean.
func TestMarshal_MergedAnnotationProperty(t *testing.T) {
	s, res := schema.LoadString(context.Background(), mergedAnnotationSchema, "probe.yammm")
	if res.HasErrors() {
		t.Fatalf("load: %v", res.Err())
	}

	got, err := gogen.Marshal(s)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !strings.Contains(string(got), "Amount *Money") {
		t.Errorf("generated C should carry the named Money field; got:\n%s", got)
	}
}
