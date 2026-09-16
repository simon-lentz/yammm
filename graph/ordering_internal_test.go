package graph

import (
	"fmt"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/location/path"
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

// TestCompareProps_OrdersCompositeValuesByContent drives property values that
// hold a map or a slice. Their order must follow their content: two equal
// values built apart tie, and never compare by where they were allocated.
func TestCompareProps_OrdersCompositeValuesByContent(t *testing.T) {
	t.Parallel()
	props := func(v any) immutable.Properties { return immutable.WrapProperties(map[string]any{"p": v}) }

	// A slice holding a map is the case a slice's own %v form cannot settle.
	equal := []struct {
		name string
		v    func() any
	}{
		{"a map holding a slice", func() any { return map[string]any{"x": int64(1), "y": []any{"a"}} }},
		{"a slice holding a map", func() any { return []any{map[string]any{"k": int64(1)}} }},
		// A container reached through a map the package stores as given. Only a
		// Map[string] carries a pointer, so a nested one decides the order by the
		// address of its memoised view unless the clone reads through it.
		{"a stored-as-is map holding a Map[string]", func() any {
			return map[int]any{1: immutable.WrapMap(map[string]any{"a": int64(1)})}
		}},
		{"a stored-as-is map holding a Properties of a map", func() any {
			return map[int]any{1: immutable.WrapProperties(map[string]any{"a": map[string]any{"b": int64(1)}})}
		}},
	}
	for _, e := range equal {
		for range 10 {
			if c := compareProps(props(e.v()), props(e.v())); c != 0 {
				t.Fatalf("%s: two equal values built apart compare %d, want 0", e.name, c)
			}
		}
	}
	// Each pair is ordered by its quoted content alone. A rendering that drops the
	// quoting, merges nil with empty, or leads with a length ties or reverses one.
	ordered := []struct {
		name   string
		lo, hi any
	}{
		{"maps", map[string]any{"b": int64(1)}, map[string]any{"b": int64(2)}},
		{"slices", []any{"a"}, []any{"b"}},
		{"slices of maps", []any{map[string]any{"b": int64(1)}}, []any{map[string]any{"b": int64(2)}}},
		{"a longer slice whose first element sorts first", []any{"a", "z"}, []any{"b"}},
		{"a larger map whose first key sorts first", map[string]any{"a": int64(1), "z": int64(1)}, map[string]any{"b": int64(1)}},
		{"one string holding a space against two strings", []any{"a b"}, []any{"a", "b"}},
		{"a nil map against an empty map", map[string]any(nil), map[string]any{}},
		{"a nil slice against an empty slice", []any(nil), []any{}},
	}
	for _, o := range ordered {
		lo, hi := props(o.lo), props(o.hi)
		if compareProps(lo, hi) >= 0 || compareProps(hi, lo) <= 0 {
			t.Errorf("%s: %#v and %#v do not order by content", o.name, o.lo, o.hi)
		}
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

// TestCompareProps_LengthArm pins the arm that separates two property sets
// where one is a prefix of the other. Without it they tie and slices.SortFunc
// leaves their order to the input, which for edges and duplicates is map order.
func TestCompareProps_LengthArm(t *testing.T) {
	t.Parallel()

	short := immutable.WrapProperties(map[string]any{"a": "1"})
	long := immutable.WrapProperties(map[string]any{"a": "1", "b": "2"})

	if c := compareProps(short, long); c >= 0 {
		t.Errorf("compareProps(short, long) = %d, want < 0", c)
	}
	if c := compareProps(long, short); c <= 0 {
		t.Errorf("compareProps(long, short) = %d, want > 0", c)
	}
	if c := compareProps(short, short); c != 0 {
		t.Errorf("compareProps(x, x) = %d, want 0", c)
	}
}

// TestCompareDuplicates_ProvenanceDiscriminates pins the final arm: two rows of
// one file colliding with one instance, identical in every other field and in
// their properties, separated only by where they came from.
func TestCompareDuplicates_ProvenanceDiscriminates(t *testing.T) {
	t.Parallel()

	at := func(line int) *Instance {
		return newInstance("Person", orderingTypeID("Person"),
			immutable.WrapKey([]any{"p1"}),
			immutable.WrapProperties(map[string]any{"id": "p1"}),
			location.NewProvenance("people.json", path.Root().Index(line),
				location.Span{Start: location.Position{Line: line, Column: 1}}),
			false)
	}
	conflict := at(1)

	a := newDuplicate(at(7), conflict, nil, "", diag.Issue{})
	b := newDuplicate(at(9), conflict, nil, "", diag.Issue{})

	if compareDuplicates(a, b) == 0 {
		t.Error("two rejections differing only in source position compare equal, so their order on the wire is the input order")
	}
	if compareDuplicates(a, b) != -compareDuplicates(b, a) {
		t.Error("compareDuplicates is not antisymmetric across the provenance arm")
	}
	if c := compareDuplicates(a, a); c != 0 {
		t.Errorf("compareDuplicates(a, a) = %d, want 0", c)
	}
}

// TestCompareProps_DiscriminatesByStoredTypeBeforeContent drives the half of
// renderValue's key that its content rows cannot reach. A Properties and a
// Map[string] of one content clone to the same map, so only the stored type
// separates them; and where the type order and the content order disagree, the
// type decides, which is what compareProps' godoc promises.
func TestCompareProps_DiscriminatesByStoredTypeBeforeContent(t *testing.T) {
	t.Parallel()
	props := func(v any) immutable.Properties { return immutable.WrapProperties(map[string]any{"p": v}) }

	sameContent := props(immutable.WrapProperties(map[string]any{"a": int64(1)}))
	asMap := props(immutable.WrapMap(map[string]any{"a": int64(1)}))
	if compareProps(sameContent, asMap) == 0 {
		t.Error("a Properties and a Map[string] of one content compare equal; the stored type does not reach the key")
	}
	if compareProps(sameContent, asMap) != -compareProps(asMap, sameContent) {
		t.Error("compareProps is not antisymmetric over two container types of one content")
	}

	// Map[string] sorts below Properties by type name, and above it by content,
	// so a key that leads with the content reverses this pair.
	lo := props(immutable.WrapMap(map[string]any{"z": int64(1)}))
	hi := props(immutable.WrapProperties(map[string]any{"a": int64(1)}))
	if compareProps(lo, hi) >= 0 {
		t.Error("a Map[string] does not sort below a Properties; the key leads with the content, not the type")
	}
}

// TestRenderValue_SeparatorAppearsOnceForEveryStoredType holds the claim
// renderValue's godoc rests on: the type and the content are split on a byte
// no Go type name can hold. A type argument carries its whole import path, so
// a separator drawn from that alphabet splits in the wrong place.
func TestRenderValue_SeparatorAppearsOnceForEveryStoredType(t *testing.T) {
	t.Parallel()
	values := []any{
		"x",
		int64(1),
		map[string]any{"a": int64(1)},
		[]any{"a"},
		map[int]any{1: "a"},
		immutable.WrapProperties(map[string]any{"a": int64(1)}),
		immutable.WrapKey([]any{int64(1)}),
		immutable.WrapMap(map[schema.TypeID]any{orderingTypeID("T"): int64(1)}),
	}
	for _, in := range values {
		v, ok := immutable.WrapProperties(map[string]any{"p": in}).Get("p")
		if !ok {
			t.Fatalf("the property under test is absent for %T", in)
		}
		got := renderValue(v)
		if n := strings.Count(got, "|"); n != 1 {
			t.Errorf("renderValue writes %d separators for %T, want 1: %s", n, in, got)
		}
		wantType := fmt.Sprintf("%T", v.Unwrap())
		if prefix, _, _ := strings.Cut(got, "|"); prefix != wantType {
			t.Errorf("renderValue's type half reads %q for %T, want %q", prefix, in, wantType)
		}
	}
}
