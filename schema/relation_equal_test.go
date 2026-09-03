package schema_test

import (
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema"
)

// Relation.Equal compares identities, then edge properties by name: two
// relations alike in every declared field but one edge property are not equal.
func TestRelation_Equal_ComparesEdgeProperties(t *testing.T) {
	t.Parallel()

	const src = `schema "s"

type X {
    id String primary
}

type A {
    id String primary
    --> R (one) X {
        w Integer
    }
}

type B {
    id String primary
    --> R (one) X {
        w Integer
    }
}

type C {
    id String primary
    --> R (one) X {
        z Integer
    }
}

type D {
    id String primary
    --> R (one) X {
        w String
    }
}
`
	s, res := schema.LoadString(t.Context(), src, "s.yammm")
	if res.Err() != nil {
		t.Fatal(res.Err())
	}
	rel := func(typeName string) *schema.Relation {
		typ, _ := s.Type(typeName)
		r, _ := typ.Relation("R")
		return r
	}
	if !rel("A").Equal(rel("B")) {
		t.Error("relations alike in every field, edge properties included, must be equal")
	}
	if rel("A").Equal(rel("C")) {
		t.Error("relations whose edge properties differ by name must not be equal")
	}
	if rel("A").Equal(rel("D")) {
		t.Error("relations whose edge property differs in constraint must not be equal")
	}
}

// An inherited relation whose target never resolved already carries its own
// diagnostic; a same-named own relation is not also reported as a collision.
func TestRelationCollision_UnresolvedTargetIsNotACollision(t *testing.T) {
	t.Parallel()

	const src = `schema "s"

type Customer {
    id String primary
}

abstract type HasCustomer {
    id String primary
    --> CUSTOMER (one) Missing
}

type Order extends HasCustomer {
    --> CUSTOMER (one) Customer
}
`
	res := loadRules(t, src)
	if countCode(res, diag.E_UNKNOWN_TYPE) != 1 {
		t.Errorf("want the unresolved target reported once; got %v", res.Err())
	}
	if countCode(res, diag.E_RELATION_COLLISION) != 0 {
		t.Errorf("an unresolved side must not be blamed twice; got %v", res.Err())
	}
}
