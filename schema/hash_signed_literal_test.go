package schema_test

import (
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema"
)

func hashOfInvariant(t *testing.T, invariant string) string {
	t.Helper()
	src := "schema \"s\"\n\ntype T {\n\tid String primary\n\tn Integer\n\tf Float\n\t! \"m\" " + invariant + "\n}\n"
	s, res := schema.LoadString(t.Context(), src, "s.yammm")
	if res.Err() != nil {
		t.Fatalf("load %q: %v", invariant, res.Err())
	}
	return schema.StructuralHash(s)
}

// A signed number is one literal in the hash, however the sign is spaced, and
// a negated operand is another expression.
func TestStructuralHash_SignedLiteralIsOneLiteral(t *testing.T) {
	t.Parallel()
	signed := hashOfInvariant(t, "n > -5")
	if spaced := hashOfInvariant(t, "n > - 5"); spaced != signed {
		t.Errorf("-5 and - 5 hash apart:\n %s\n %s", signed, spaced)
	}
	for _, other := range []string{"n > -(5)", "n > 5", "n > -5.0", "n > 0 - 5"} {
		if h := hashOfInvariant(t, other); h == signed {
			t.Errorf("%q hashes as n > -5 does: %s", other, h)
		}
	}
	if zero, negated := hashOfInvariant(t, "n > 0"), hashOfInvariant(t, "n > -0"); zero != negated {
		t.Errorf("the integer -0 is 0, and the two hash apart:\n %s\n %s", zero, negated)
	}
}

// A float literal's zero keeps its sign in the hash: a quotient by each zero
// is a different value.
func TestStructuralHash_FloatLiteralZeroKeepsItsSign(t *testing.T) {
	t.Parallel()
	if pos, neg := hashOfInvariant(t, "f / 0.0 > 0.0"), hashOfInvariant(t, "f / -0.0 > 0.0"); pos == neg {
		t.Errorf("f / 0.0 and f / -0.0 hash alike: %s", pos)
	}
}

// Two inherited definitions that differ in a zero's sign are two definitions.
func TestInvariantConflict_FloatLiteralZerosOfTwoSignsDiffer(t *testing.T) {
	t.Parallel()
	const src = `schema "s"

abstract type P {
	f Float
	! "m" f / 0.0 > 0.0
}

abstract type Q {
	f Float
	! "m" f / -0.0 > 0.0
}

type T extends P, Q {
	id String primary
}
`
	_, res := schema.LoadString(t.Context(), src, "s.yammm")
	if !res.HasCode(diag.E_INVARIANT_CONFLICT) {
		t.Errorf("want E_INVARIANT_CONFLICT; got %v", res.Err())
	}
}

// A signed literal and a negated literal are two definitions at a merge, and
// two spellings of one signed literal are one.
func TestInvariantConflict_ASignedLiteralAndANegationDiffer(t *testing.T) {
	t.Parallel()
	load := func(p, q string) diag.Result {
		src := "schema \"s\"\n\nabstract type P {\n\tn Integer\n\t! \"m\" " + p + "\n}\n\n" +
			"abstract type Q {\n\tn Integer\n\t! \"m\" " + q + "\n}\n\ntype T extends P, Q {\n\tid String primary\n}\n"
		_, res := schema.LoadString(t.Context(), src, "s.yammm")
		return res
	}
	if res := load("n > -1", "n > -(1)"); !res.HasCode(diag.E_INVARIANT_CONFLICT) {
		t.Errorf("-1 against -(1): want E_INVARIANT_CONFLICT; got %v", res.Err())
	}
	if res := load("n > -1", "n > - 1"); res.Err() != nil {
		t.Errorf("-1 against - 1: want one definition; got %v", res.Err())
	}
}

// The digest of a schema whose invariant holds a signed literal. It moves when
// the tree a signed literal is read as moves, which no test that compares two
// hashes of one build can see.
func TestStructuralHash_SignedLiteralDigest(t *testing.T) {
	t.Parallel()
	const want = "sha256:fa5b9b66e335935ed54f2726b05e95a1b0e2c443a146be08a9490ac1f7024c3a"
	if got := hashOfInvariant(t, "n > -5 && f > -0.5"); got != want {
		t.Errorf("StructuralHash = %s, want %s", got, want)
	}
}
