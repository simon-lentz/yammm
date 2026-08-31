package graph

import (
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// Ordering.
//
// Graph.Snapshot sorts edges, duplicates and unresolved records with an
// unstable sort over input that arrives in map-iteration order for the
// unresolved half, so any pair the comparator cannot separate has its position
// decided by that order and the same graph writes different bytes. These tests
// drive the pairs that two earlier attempts at "total" left equal. In-package
// because the comparators are unexported and are the thing under test.

func orderingTypeID(name string) schema.TypeID {
	return schema.NewTypeID(location.MustNewSourceID("test://ordering.yammm"), name)
}

func orderingInstance(t *testing.T, name, key string) *Instance {
	t.Helper()
	return rebuildInstance(InstanceParts{
		TypeName:   name,
		TypeID:     orderingTypeID(name),
		PrimaryKey: immutable.WrapKey([]any{key}),
		Properties: immutable.WrapProperties(map[string]any{"id": key}),
	})
}

// TestCompareProps_SeparatorsDoNotCollide drives the values a rendered
// "name=value;" key could not tell apart. An edge property is an ordinary
// String, so one holding ';' or '=' is not exotic, and while the rendering
// collided those two edges tied and the document's byte order became
// map-iteration order.
func TestCompareProps_SeparatorsDoNotCollide(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		a, b map[string]any
	}{
		{
			"a value carrying the pair separator",
			map[string]any{"note": "x;y=z"},
			map[string]any{"note": "x", "y": "z"},
		},
		{
			"a value carrying the field separator",
			map[string]any{"a": "b=c"},
			map[string]any{"a=b": "c"},
		},
		{
			"one value split across two fields",
			map[string]any{"k": "v;w"},
			map[string]any{"k": "v", "w": ""},
		},
		{
			"the same text at two types",
			map[string]any{"n": "1"},
			map[string]any{"n": int64(1)},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			a := immutable.WrapProperties(tc.a)
			b := immutable.WrapProperties(tc.b)
			if compareProps(a, b) == 0 {
				t.Errorf("two distinct property sets compare equal, so any ordering built on them is not total:\n  a = %v\n  b = %v", tc.a, tc.b)
			}
			if compareProps(a, b) != -compareProps(b, a) {
				t.Errorf("compareProps is not antisymmetric for %v vs %v", tc.a, tc.b)
			}
		})
	}
}

// TestCompareProps_EqualSetsCompareEqual is the positive half. Without it the
// test above passes on a comparator that calls everything distinct.
func TestCompareProps_EqualSetsCompareEqual(t *testing.T) {
	t.Parallel()
	one := immutable.WrapProperties(map[string]any{"b": "2", "a": int64(1)})
	two := immutable.WrapProperties(map[string]any{"a": int64(1), "b": "2"})
	if c := compareProps(one, two); c != 0 {
		t.Errorf("two equal property sets compare %d, want 0 — the ordering is sensitive to map order", c)
	}
}

// TestCompareDuplicates_ParentDiscriminates drives two composed-child
// duplicates rejected from different parent slots. The wire carries the parent
// coordinates, so they are different records; a comparator that ignores the
// parent leaves their order to the input.
func TestCompareDuplicates_ParentDiscriminates(t *testing.T) {
	t.Parallel()

	child := orderingInstance(t, "Child", "c1")
	conflict := orderingInstance(t, "Child", "c1")
	parentA := orderingInstance(t, "Parent", "p1")
	parentB := orderingInstance(t, "Parent", "p2")

	a := newDuplicate(child, conflict, parentA, "CHILDREN", diag.Issue{})
	b := newDuplicate(child, conflict, parentB, "CHILDREN", diag.Issue{})

	if compareDuplicates(a, b) == 0 {
		t.Error("two duplicates differing only in the parent slot compare equal, so their order on the wire is whatever the input order was")
	}
	if compareDuplicates(a, b) != -compareDuplicates(b, a) {
		t.Error("compareDuplicates is not antisymmetric across the parent arm")
	}
	if c := compareDuplicates(a, a); c != 0 {
		t.Errorf("compareDuplicates(a, a) = %d, want 0", c)
	}
}

// TestCompareDuplicates_RootAndComposedDoNotCollide pins the boundary between a
// root duplicate, which has no parent, and a composed one that does.
func TestCompareDuplicates_RootAndComposedDoNotCollide(t *testing.T) {
	t.Parallel()

	inst := orderingInstance(t, "Child", "c1")
	conflict := orderingInstance(t, "Child", "c1")
	parent := orderingInstance(t, "Parent", "p1")

	root := newDuplicate(inst, conflict, nil, "", diag.Issue{})
	composed := newDuplicate(inst, conflict, parent, "CHILDREN", diag.Issue{})

	if compareDuplicates(root, composed) == 0 {
		t.Error("a root duplicate and a composed one compare equal")
	}
}

// TestCompareDuplicates_PropertiesDiscriminate drives the final arm: two
// rejections of one key against one conflict, from one slot, differing only
// in the rejected instance's payload. Every earlier arm ties, so nothing but
// the properties can separate them and the wire order would otherwise be the
// input order.
func TestCompareDuplicates_PropertiesDiscriminate(t *testing.T) {
	t.Parallel()

	withName := func(name string) *Instance {
		return rebuildInstance(InstanceParts{
			TypeName:   "Person",
			TypeID:     orderingTypeID("Person"),
			PrimaryKey: immutable.WrapKey([]any{"p1"}),
			Properties: immutable.WrapProperties(map[string]any{"id": "p1", "name": name}),
		})
	}
	conflict := withName("first")

	a := newDuplicate(withName("alice"), conflict, nil, "", diag.Issue{})
	b := newDuplicate(withName("bob"), conflict, nil, "", diag.Issue{})

	if compareDuplicates(a, b) == 0 {
		t.Error("two rejections differing only in the instance payload compare equal, so their order on the wire is the input order")
	}
	if compareDuplicates(a, b) != -compareDuplicates(b, a) {
		t.Error("compareDuplicates is not antisymmetric across the properties arm")
	}
	if c := compareDuplicates(a, a); c != 0 {
		t.Errorf("compareDuplicates(a, a) = %d, want 0", c)
	}
}
