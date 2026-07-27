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
// were merged across ancestors resolves its named DataType, because the datatype
// table is keyed by the declared property and read through
// [schema.Property.Origin]. A merged property is a synthesized copy that appears
// in no type's own property slice, so a lookup by the raw pointer finds nothing
// and Marshal fails on a schema that loads clean.
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

// mergedListAnnotationSchema is the List<DataType> counterpart: the same
// element-named list property on two abstract ancestors, one annotated, so the
// child's view is a synthesized copy.
const mergedListAnnotationSchema = `schema "probe"
type Code = String
abstract type A {
	id String primary
	tags List<Code>
}
abstract type B {
	id String primary
	tags List<Code> @writeOnce
}
type C extends A, B {}
`

// A List<DataType> property keeps its named element type through an annotation
// merge, which requires goListType to read the datatype table through Origin()
// exactly as goFieldType does. Read by the raw merged pointer instead, the same
// schema property emits []Code on its ancestors and []string on the child — a
// generated file where assigning one to the other does not compile.
func TestMarshal_MergedAnnotationListProperty(t *testing.T) {
	s, res := schema.LoadString(context.Background(), mergedListAnnotationSchema, "probe.yammm")
	if res.HasErrors() {
		t.Fatalf("load: %v", res.Err())
	}

	got, err := gogen.Marshal(s)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if n := strings.Count(string(got), "Tags []Code"); n != 3 {
		t.Errorf("every type declaring tags should emit []Code; got %d of 3 in:\n%s", n, got)
	}
	if strings.Contains(string(got), "Tags []string") {
		t.Errorf("the merged property must not degrade to the primitive element type; got:\n%s", got)
	}
}
