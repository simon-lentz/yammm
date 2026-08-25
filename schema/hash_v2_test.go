package schema_test

import (
	"testing"

	"github.com/simon-lentz/yammm/schema"
)

func loadHashSchema(t *testing.T, src string) *schema.Schema {
	t.Helper()
	s, res := schema.LoadString(t.Context(), src, "hashv2.yammm")
	if res.HasErrors() {
		t.Fatalf("load: %s", res.String())
	}
	return s
}

func TestStructuralHash_InvariantsMoveTheHash(t *testing.T) {
	t.Parallel()
	base := loadHashSchema(t, `schema "h"

type Thing {
	id String primary
	age Integer

	! "age rule" age >= 0
}
`)
	changed := loadHashSchema(t, `schema "h"

type Thing {
	id String primary
	age Integer

	! "age rule" age >= 1
}
`)
	if schema.StructuralHash(base) == schema.StructuralHash(changed) {
		t.Fatal("schemas differing only in an invariant share a hash; the hash must cover every rule that decides validity")
	}
}

func TestStructuralHash_InvariantOrderIsIrrelevant(t *testing.T) {
	t.Parallel()
	// Two schemas differing only in invariant order accept the same data,
	// so they must share a hash.
	ab := loadHashSchema(t, `schema "h"

type Thing {
	id String primary
	age Integer

	! "a" age >= 0
	! "b" age <= 100
}
`)
	ba := loadHashSchema(t, `schema "h"

type Thing {
	id String primary
	age Integer

	! "b" age <= 100
	! "a" age >= 0
}
`)
	if schema.StructuralHash(ab) != schema.StructuralHash(ba) {
		t.Fatal("invariant declaration order moved the hash")
	}
}

func TestStructuralHash_AbstractFlagMovesTheHash(t *testing.T) {
	t.Parallel()
	concrete := loadHashSchema(t, `schema "h"

type Thing {
	id String primary
}
`)
	abstract := loadHashSchema(t, `schema "h"

abstract type Thing {
	id String primary
}
`)
	if schema.StructuralHash(concrete) == schema.StructuralHash(abstract) {
		t.Fatal("the abstract flag did not move the hash; it decides which instances are valid")
	}
}

func TestStructuralHash_PartFlagMovesTheHash(t *testing.T) {
	t.Parallel()
	concrete := loadHashSchema(t, `schema "h"

type Thing {
	id String primary
}
`)
	part := loadHashSchema(t, `schema "h"

part type Thing {
	id String primary
}
`)
	if schema.StructuralHash(concrete) == schema.StructuralHash(part) {
		t.Fatal("the part flag did not move the hash; it decides which instances are valid")
	}
}
