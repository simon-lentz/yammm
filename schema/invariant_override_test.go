package schema_test

import (
	"testing"

	"github.com/simon-lentz/yammm/schema"
)

// overrideChain declares "bound" on A and re-declares it on B, with C inheriting
// silently. Depth two is the shallowest depth at which the defect appears: at
// depth one the child's own declaration wins by keep-first.
const overrideChain = `schema "x"

abstract type A {
    id String primary
    n Integer
    ! "bound" n > 0
}

type B extends A {
    ! "bound" n > 100
}

type C extends B {
}
`

func loadOverrideChain(t *testing.T) *schema.Schema {
	t.Helper()
	s, res := schema.LoadString(t.Context(), overrideChain, "x.yammm")
	if res.HasErrors() {
		t.Fatalf("load: %s", res)
	}
	return s
}

func invariantNamed(t *testing.T, typ *schema.Type, name string) *schema.Invariant {
	t.Helper()
	for _, inv := range typ.AllInvariantsSlice() {
		if inv.Name() == name {
			return inv
		}
	}
	t.Fatalf("type %q carries no invariant %q", typ.Name(), name)
	return nil
}

// A subtype inherits its DIRECT parent's override, not the grandparent's
// original. The merge walked the linearized ancestor list, which is
// ancestors-first, so the grandparent's declaration won the keep-first race and
// every type below the overriding one silently evaluated the wrong rule.
//
// The projection in complete_test.go records invariant NAMES, and both
// declarations are named "bound", so no golden can see this. Identity is what
// distinguishes them.
//
// Mutation: reverting mergeInvariants to walk the linearized `supers` turns this
// red — C carries A's declaration.
func TestInvariantOverride_SubtypeInheritsTheNearestDeclaration(t *testing.T) {
	s := loadOverrideChain(t)

	typeB, ok := s.Type("B")
	if !ok {
		t.Fatal("type B missing")
	}
	typeC, ok := s.Type("C")
	if !ok {
		t.Fatal("type C missing")
	}

	ownB := typeB.InvariantsSlice()
	if len(ownB) != 1 {
		t.Fatalf("B declares %d invariants, want 1", len(ownB))
	}

	if got := invariantNamed(t, typeC, "bound"); got != ownB[0] {
		t.Errorf("C inherited a different %q declaration than B's own — the grandparent's original reappeared past the parent that replaced it", "bound")
	}
}

// The override does not multiply: C carries one "bound", not both.
func TestInvariantOverride_TheShadowedDeclarationIsNotAlsoInherited(t *testing.T) {
	s := loadOverrideChain(t)

	typeC, ok := s.Type("C")
	if !ok {
		t.Fatal("type C missing")
	}

	count := 0
	for _, inv := range typeC.AllInvariantsSlice() {
		if inv.Name() == "bound" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("C carries %d invariants named %q, want 1", count, "bound")
	}
}

// Properties never had this defect, and the reason is worth pinning: their
// merge has a narrowing-replacement arm that reinstates the nearer declaration.
// Invariant expressions admit no narrowing relation, so nothing there could
// rescue a wrong pick — which is why the fix had to change the walk itself.
func TestInvariantOverride_PropertiesWereAlreadyCorrect(t *testing.T) {
	s, res := schema.LoadString(t.Context(), `schema "x"

abstract type A {
    id String primary
    code String
}

type B extends A {
    code String[1, 3]
}

type C extends B {
}
`, "x.yammm")
	if res.HasErrors() {
		t.Fatalf("load: %s", res)
	}

	typeC, ok := s.Type("C")
	if !ok {
		t.Fatal("type C missing")
	}
	prop, ok := typeC.Property("code")
	if !ok {
		t.Fatal("C carries no property \"code\"")
	}
	typeB, ok := s.Type("B")
	if !ok {
		t.Fatal("type B missing")
	}
	ownB, ok := typeB.Property("code")
	if !ok {
		t.Fatal("B carries no property \"code\"")
	}
	if prop.Origin() != ownB.Origin() {
		t.Error("C.code does not originate at B's narrowed declaration")
	}
}
