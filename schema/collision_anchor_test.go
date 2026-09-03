package schema_test

import (
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
)

// anchorLine returns the source line the first issue with code and fragment
// points at, so a test can say where a collision is reported.
func anchorLine(t *testing.T, src string, res diag.Result, code diag.Code, fragment string) string {
	t.Helper()
	is, ok := issueWith(res, code, fragment)
	if !ok {
		t.Fatalf("want %s mentioning %q; got %v", code, fragment, res.Err())
	}
	return strings.TrimSpace(strings.Split(src, "\n")[is.Span().Start.Line-1])
}

// A relation collision anchors at the declaration that introduced it: the own
// composition when it collides with an inherited association, the own
// association when it collides with an inherited composition, and the
// combining type when both halves are inherited.
func TestRelationCollision_AnchorsAtTheDeclarationThatIntroducedIt(t *testing.T) {
	t.Parallel()

	const ownComposition = `schema "s"

type Widget {
    id String primary
}

part type Part {
    id String primary
}

abstract type HasAssoc {
    id String primary
    --> ITEMS (one:many) Widget
}

type Box extends HasAssoc {
    *-> ITEMS (one:many) Part
}
`
	res := loadRules(t, ownComposition)
	if got := anchorLine(t, ownComposition, res, diag.E_RELATION_COLLISION, `"ITEMS"`); !strings.HasPrefix(got, "*->") {
		t.Errorf("own composition colliding with an inherited association anchors at %q, want the composition", got)
	}

	const ownAssociation = `schema "s"

type Widget {
    id String primary
}

part type Part {
    id String primary
}

abstract type HasComp {
    id String primary
    *-> ITEMS (one:many) Part
}

type Box extends HasComp {
    --> ITEMS (one:many) Widget
}
`
	res = loadRules(t, ownAssociation)
	if got := anchorLine(t, ownAssociation, res, diag.E_RELATION_COLLISION, `"ITEMS"`); !strings.HasPrefix(got, "-->") {
		t.Errorf("own association colliding with an inherited composition anchors at %q, want the association", got)
	}

	const bothInherited = `schema "s"

type Widget {
    id String primary
}

part type Part {
    id String primary
}

abstract type HasAssoc {
    id String primary
    --> ITEMS (one:many) Widget
}

abstract type HasComp {
    id String primary
    *-> ITEMS (one:many) Part
}

type Box extends HasAssoc, HasComp {
    label String
}
`
	res = loadRules(t, bothInherited)
	if got := anchorLine(t, bothInherited, res, diag.E_RELATION_COLLISION, `"ITEMS"`); !strings.HasPrefix(got, "type Box") {
		t.Errorf("two inherited halves anchor at %q, want the combining type", got)
	}
}

// A property colliding with an inherited relation's field name is reported at
// the own property when the property is the type's own.
func TestPropertyRelationCollision_AnchorsAtTheOwnProperty(t *testing.T) {
	t.Parallel()

	const src = `schema "s"

type Company {
    id String primary
}

abstract type Employed {
    id String primary
    --> EMPLOYER (one) Company
}

type Person extends Employed {
    employer String
}
`
	res := loadRules(t, src)
	if got := anchorLine(t, src, res, diag.E_PROPERTY_RELATION_COLLISION, `"EMPLOYER"`); got != "employer String" {
		t.Errorf("an own property colliding with an inherited relation anchors at %q, want the property", got)
	}
}

// Two own properties differing only in case anchor at the second declaration,
// the one that introduced the collision.
func TestCaseCollision_TwoOwnPropertiesAnchorAtTheSecond(t *testing.T) {
	t.Parallel()

	const src = `schema "s"

type T {
    id String primary
    firstName String
    firstname String
}
`
	res := loadRules(t, src)
	if n := countCode(res, diag.E_CASE_COLLISION); n != 1 {
		t.Fatalf("want one E_CASE_COLLISION; got %d: %v", n, res.Err())
	}
	if got := anchorLine(t, src, res, diag.E_CASE_COLLISION, "firstname"); got != "firstname String" {
		t.Errorf("anchors at %q, want the second declaration", got)
	}
}
