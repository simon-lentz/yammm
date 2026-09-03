package schema_test

import (
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema"
)

func loadRules(t *testing.T, src string) diag.Result {
	t.Helper()
	_, res := schema.LoadString(t.Context(), src, "s.yammm")
	return res
}

func countCode(res diag.Result, code diag.Code) int {
	n := 0
	for is := range res.Issues() {
		if is.Code() == code {
			n++
		}
	}
	return n
}

func issueWith(res diag.Result, code diag.Code, fragment string) (diag.Issue, bool) {
	for is := range res.Issues() {
		if is.Code() == code && strings.Contains(is.Message(), fragment) {
			return is, true
		}
	}
	return diag.Issue{}, false
}

// Relation targets resolve before inheritance merges, so an own relation that
// shadows an inherited one under the same name is compared by target identity:
// two same-named targets in different schemas are two definitions, not one.
func TestRelationCollision_ShadowingRelationWithADifferentTarget(t *testing.T) {
	t.Parallel()

	const src = `schema "s"

type Customer {
    id String primary
}

type Company {
    id String primary
}

abstract type HasCustomer {
    id String primary
    --> CUSTOMER (one) Customer
}

type Order extends HasCustomer {
    --> CUSTOMER (one) Company
}
`
	res := loadRules(t, src)
	if _, ok := issueWith(res, diag.E_RELATION_COLLISION, `"CUSTOMER"`); !ok {
		t.Errorf("an own relation replacing an inherited one bound to a different type must collide; got %v", res.Err())
	}
}

// The same definition inherited twice is one relation, not a collision.
func TestRelationCollision_IdenticalInheritedDefinitionsMerge(t *testing.T) {
	t.Parallel()

	const src = `schema "s"

type Customer {
    id String primary
}

abstract type A {
    id String primary
    --> CUSTOMER (one) Customer
}

abstract type B {
    id String primary
    --> CUSTOMER (one) Customer
}

type Order extends A, B {
    n Integer
}
`
	if res := loadRules(t, src); res.Err() != nil {
		t.Errorf("identical inherited relations must merge silently: %v", res.Err())
	}
}

// An invariant's message is its identity: declaring it twice on one type is a
// duplicate, and inheriting two different expressions under one message from
// two ancestors is a conflict. A subtype overriding its ancestor is neither.
func TestInvariantIdentity(t *testing.T) {
	t.Parallel()

	t.Run("own duplicate", func(t *testing.T) {
		t.Parallel()
		res := loadRules(t, `schema "s"

type T {
    id String primary
    qty Integer
    ! "dup" qty > 0
    ! "dup" qty > 0
}
`)
		if countCode(res, diag.E_DUPLICATE_INVARIANT) != 1 {
			t.Errorf("want one E_DUPLICATE_INVARIANT; got %v", res.Err())
		}
	})

	t.Run("inherited conflict", func(t *testing.T) {
		t.Parallel()
		res := loadRules(t, `schema "s"

abstract type A1 {
    id String primary
    x Integer
    ! "rule" x > 0
}

abstract type A2 {
    id String primary
    x Integer
    ! "rule" x < 100
}

type T extends A1, A2 {
    y Integer
}
`)
		if countCode(res, diag.E_INVARIANT_CONFLICT) != 1 {
			t.Errorf("want one E_INVARIANT_CONFLICT; got %v", res.Err())
		}
	})

	t.Run("inherited agreement", func(t *testing.T) {
		t.Parallel()
		res := loadRules(t, `schema "s"

abstract type A1 {
    id String primary
    x Integer
    ! "rule" x > 0
}

abstract type A2 {
    id String primary
    x Integer
    ! "rule" x > 0
}

type T extends A1, A2 {
    y Integer
}
`)
		if res.Err() != nil {
			t.Errorf("two ancestors stating one rule must merge: %v", res.Err())
		}
	})

	t.Run("subtype override", func(t *testing.T) {
		t.Parallel()
		res := loadRules(t, `schema "s"

abstract type Base {
    id String primary
    x Integer
    ! "rule" x > 0
}

type T extends Base {
    ! "rule" x > 10
}
`)
		if res.Err() != nil {
			t.Errorf("a subtype overriding an inherited invariant is documented: %v", res.Err())
		}
	})
}

// A part type cannot carry an association, whether it declares or inherits it.
func TestPartType_CannotInheritAnAssociation(t *testing.T) {
	t.Parallel()

	const src = `schema "s"

type Customer {
    id String primary
}

abstract type HasOwner {
    id String primary
    --> OWNER (one) Customer
}

part type Line extends HasOwner {
    qty Integer
}

type Order {
    id String primary
    *-> LINES (one:many) Line
}
`
	res := loadRules(t, src)
	is, ok := issueWith(res, diag.E_INVALID_ASSOCIATION_TARGET, `"Line"`)
	if !ok {
		t.Fatalf("a part type inheriting an association must be refused; got %v", res.Err())
	}
	if !strings.Contains(is.Message(), "inherits") {
		t.Errorf("the diagnostic should say the association is inherited: %q", is.Message())
	}
}

// One declaration-site mistake is reported once, at the declaration that
// introduced it, and never again in a descendant.
func TestCollisions_ReportedOnceAtTheDeclaration(t *testing.T) {
	t.Parallel()

	const src = `schema "s"

abstract type Base {
    id String primary
    firstName String
}

type Child extends Base {
    firstname String
}

type Grand extends Child {
    extra Integer
}
`
	res := loadRules(t, src)
	if n := countCode(res, diag.E_CASE_COLLISION); n != 1 {
		t.Fatalf("want one E_CASE_COLLISION; got %d: %v", n, res.Err())
	}
	is, _ := issueWith(res, diag.E_CASE_COLLISION, "firstname")
	line := strings.Split(src, "\n")[is.Span().Start.Line-1]
	if !strings.Contains(line, "firstname String") {
		t.Errorf("the diagnostic anchors at %q, want the child's declaration", strings.TrimSpace(line))
	}
}

// Two ancestors whose members collide are reported once, on the type that
// combined them.
func TestCollisions_TwoInheritedMembersReportedOnTheCombiningType(t *testing.T) {
	t.Parallel()

	const src = `schema "s"

abstract type A {
    id String primary
    firstName String
}

abstract type B {
    id String primary
    firstname String
}

type T extends A, B {
    extra Integer
}
`
	res := loadRules(t, src)
	if n := countCode(res, diag.E_CASE_COLLISION); n != 1 {
		t.Fatalf("want one E_CASE_COLLISION; got %d: %v", n, res.Err())
	}
	is, _ := issueWith(res, diag.E_CASE_COLLISION, "firstName")
	line := strings.Split(src, "\n")[is.Span().Start.Line-1]
	if !strings.Contains(line, "type T extends") {
		t.Errorf("the diagnostic anchors at %q, want the combining type", strings.TrimSpace(line))
	}
}
