package schema_test

import (
	"slices"
	"testing"

	"github.com/simon-lentz/yammm/schema"
)

func mustLoad(t *testing.T, src string) schema.Schema {
	t.Helper()
	s, res := schema.LoadString(t.Context(), src, "s.yammm")
	if s.IsZero() {
		t.Fatalf("load: %v", res)
	}
	return s
}

// TestStructuralHash_AKeysOrderIsHashed pins that two schemas whose composite
// keys hold one set of properties in two orders hash apart: an instance reads
// a composite key as a list, so the order decides which data is valid.
func TestStructuralHash_AKeysOrderIsHashed(t *testing.T) {
	t.Parallel()
	pairs := []struct{ name, first, second string }{
		{
			"declared",
			"schema \"h\"\ntype Pair {\n\ta String primary\n\tb String primary\n}\n",
			"schema \"h\"\ntype Pair {\n\tb String primary\n\ta String primary\n}\n",
		},
		{
			"inherited",
			"schema \"h\"\nabstract type A {\n\ta String primary\n}\nabstract type B {\n\tb String primary\n}\ntype K extends A, B {\n\tz String primary\n}\n",
			"schema \"h\"\nabstract type A {\n\ta String primary\n}\nabstract type B {\n\tb String primary\n}\ntype K extends B, A {\n\tz String primary\n}\n",
		},
	}
	for _, p := range pairs {
		t.Run(p.name, func(t *testing.T) {
			t.Parallel()
			if schema.StructuralHash(mustLoad(t, p.first)) == schema.StructuralHash(mustLoad(t, p.second)) {
				t.Error("two key orders hash alike")
			}
		})
	}
}

// TestType_PrimaryKeysAreInKeyOrder pins the key order: the type's own key
// properties in declaration order, then inherited ones in linearization order.
func TestType_PrimaryKeysAreInKeyOrder(t *testing.T) {
	t.Parallel()
	s := mustLoad(t, `schema "h"
abstract type A {
	a String primary
}
abstract type B {
	b String primary
}
type K extends B, A {
	z String primary
	y String primary
}
`)
	k, _ := s.Type("K")
	var got []string
	for p := range k.PrimaryKeys() {
		got = append(got, p.Name())
	}
	if want := []string{"z", "y", "b", "a"}; !slices.Equal(got, want) {
		t.Errorf("key order %v, want %v", got, want)
	}
}
