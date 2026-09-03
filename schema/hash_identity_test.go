package schema_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/simon-lentz/yammm/schema"
)

// hashTwoVariants loads two closures that differ only in the entry file and
// returns their structural hashes. Each variant gets its own directory so the
// two entries can share a file name.
func hashTwoVariants(t *testing.T, shared map[string]string, entryA, entryB string) (string, string) {
	t.Helper()

	hashOne := func(entry string) string {
		t.Helper()
		dir := t.TempDir()
		for name, src := range shared {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o600); err != nil {
				t.Fatalf("write %s: %v", name, err)
			}
		}
		path := filepath.Join(dir, "main.yammm")
		if err := os.WriteFile(path, []byte(entry), 0o600); err != nil {
			t.Fatalf("write main.yammm: %v", err)
		}
		s, res := schema.Load(t.Context(), path, schema.WithModuleRoot(dir))
		if res.Err() != nil {
			t.Fatalf("load %s: %v", entry, res.Err())
		}
		if s == nil {
			t.Fatalf("load %s returned a nil schema", entry)
		}
		return schema.StructuralHash(s)
	}

	return hashOne(entryA), hashOne(entryB)
}

// Two members of one closure may declare types of the same name. A target
// written into the digest by name alone cannot tell them apart, so two schemas
// whose relations bind to different types hash identically — and the hash is
// the only gate snapshot loading gets.
const (
	hashAlpha = `schema "alpha"

type Person {
    id String primary
    n Integer[0,5]
}

abstract type Base {
    id String primary
    fromAlpha String
}
`
	hashBeta = `schema "beta"

type Person {
    id String primary
    n Integer[0,9999]
}

abstract type Base {
    id String primary
    fromBeta String
}
`
)

func hashImports() map[string]string {
	return map[string]string{"alpha.yammm": hashAlpha, "beta.yammm": hashBeta}
}

func entryWithTarget(target string) string {
	return `schema "app"

import "./alpha.yammm" as a
import "./beta.yammm" as b

type T {
    id String primary
    --> OWNER (one) ` + target + `
}
`
}

func entryWithSuper(super string) string {
	return `schema "app"

import "./alpha.yammm" as a
import "./beta.yammm" as b

type T extends ` + super + ` {
    tag String
}
`
}

// TestStructuralHash_RelationTargetDistinguishesOwningSchema pins that an
// association target carries which closure member owns it.
func TestStructuralHash_RelationTargetDistinguishesOwningSchema(t *testing.T) {
	t.Parallel()

	a, b := hashTwoVariants(t, hashImports(), entryWithTarget("a.Person"), entryWithTarget("b.Person"))
	if a == b {
		t.Errorf("relations binding to different same-named types share a structural hash: %s", a)
	}
}

// TestStructuralHash_AncestorMembersReachTheDigest pins that an ancestor's
// rules reach the digest through the members merged into the subtype: two
// same-named ancestors in different schemas that differ in a member hash the
// entry differently, and one ancestor hashes one way. The extends edge itself
// hashes by name; inheritance copies the rules, and the copies are what the
// digest reads.
func TestStructuralHash_AncestorMembersReachTheDigest(t *testing.T) {
	t.Parallel()

	a, b := hashTwoVariants(t, hashImports(), entryWithSuper("a.Base"), entryWithSuper("b.Base"))
	if a == b {
		t.Errorf("ancestors that differ in a member share a structural hash: %s", a)
	}
	same, again := hashTwoVariants(t, hashImports(), entryWithSuper("a.Base"), entryWithSuper("a.Base"))
	if same != again {
		t.Errorf("one ancestor hashes two ways: %s / %s", same, again)
	}
}

// TestStructuralHash_NegativeZeroBoundHashesAsZero pins the digest to the type
// system's own equality: FloatConstraint.Equal compares with ==, which calls
// 0.0 and -0.0 equal, so two schemas it cannot tell apart must hash alike.
func TestStructuralHash_NegativeZeroBoundHashesAsZero(t *testing.T) {
	t.Parallel()

	bound := func(lo string) string {
		return `schema "zero"

type T {
    id String primary
    v Float[` + lo + `,10.0]
}
`
	}

	pos, res := schema.LoadString(t.Context(), bound("0.0"), "pos.yammm")
	if res.Err() != nil {
		t.Fatalf("load 0.0 bound: %v", res.Err())
	}
	neg, res2 := schema.LoadString(t.Context(), bound("-0.0"), "neg.yammm")
	if res2.Err() != nil {
		t.Fatalf("load -0.0 bound: %v", res2.Err())
	}

	if got, want := schema.StructuralHash(neg), schema.StructuralHash(pos); got != want {
		t.Errorf("float bounds the type system compares equal hash differently:\n -0.0 %s\n  0.0 %s", got, want)
	}
}
