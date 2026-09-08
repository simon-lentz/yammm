package schema_test

import (
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
)

// TestStaticInvariant_GuardVerdictDoesNotDependOnOrder pins that a nil guard
// reaches the same verdict however its alternatives are written. The fold that
// compared each alternative against a widening accumulator let a subkind-less
// alternative mask a disjoint one behind it, so the same two alternatives were
// refused in one order and accepted in the other. Restoring the fold turns the
// first row of each pair red.
func TestStaticInvariant_GuardVerdictDoesNotDependOnOrder(t *testing.T) {
	t.Parallel()

	// The masking alternative — a conditional whose branches disagree, so it
	// types as a scalar of unknown subkind — stands first in one row, last in the other.
	pairs := [][2]string{
		{
			`(note -> Coalesce((f1 ? { note : MAIN_LINE.qty }), 1)) -> Upper == "A"`,
			`(note -> Coalesce(1, (f1 ? { note : MAIN_LINE.qty }))) -> Upper == "A"`,
		},
		{
			`(note -> Coalesce((f1 ? { note : MAIN_LINE.qty }), MAIN_LINE)) -> Upper == "A"`,
			`(note -> Coalesce(MAIN_LINE, (f1 ? { note : MAIN_LINE.qty }))) -> Upper == "A"`,
		},
	}
	for _, pair := range pairs {
		for _, inv := range pair {
			t.Run(inv, func(t *testing.T) {
				t.Parallel()
				res := loadInvariant(t, inv)
				if res.Err() == nil {
					t.Fatalf("a guard over disjoint alternatives loaded clean")
				}
				if !hasCodeMentioning(res, diag.E_INVALID_INVARIANT, "Coalesce") {
					t.Errorf("want E_INVALID_INVARIANT mentioning Coalesce; got %v", res.Err())
				}
			})
		}
	}
}

// TestStaticInvariant_GuardStillAdmitsAgreeingAlternatives is the control for
// the test above: widening alternatives are admitted, and only a genuinely
// disjoint pair is refused. Without it a guard that refused everything would
// pass the order-independence pin.
func TestStaticInvariant_GuardStillAdmitsAgreeingAlternatives(t *testing.T) {
	t.Parallel()

	for _, inv := range []string{
		`(note -> Coalesce((f1 ? { note : MAIN_LINE.qty }), "x")) -> Upper == "A"`,
		`(note -> Coalesce("x", (f1 ? { note : MAIN_LINE.qty }))) -> Upper == "A"`,
		`(note -> Coalesce(nil, "x")) -> Upper == "A"`,
		`(ALT -> Coalesce(nil, OTHER)).label != ""`,
	} {
		t.Run(inv, func(t *testing.T) {
			t.Parallel()
			if res := loadInvariant(t, inv); res.Err() != nil {
				t.Errorf("a guard over agreeing alternatives was refused: %v", res.Err())
			}
		})
	}
}

// TestStaticInvariant_MinMaxTypedByTheTotalOrder pins that Min and Max with an
// argument are typed by the order internal/value.Order implements — a number
// ranks below a string — rather than merged to a scalar of unknown subkind. The
// merged typing admitted both rows below, each of which fails at evaluation on
// every instance. Restoring mergeType turns them red.
func TestStaticInvariant_MinMaxTypedByTheTotalOrder(t *testing.T) {
	t.Parallel()

	rows := []struct {
		inv  string
		want string
	}{
		// Min of a string and a number is the number, so a string stage refuses it.
		{`(name -> Min(1)) -> Upper != ""`, "takes a string"},
		// Max of the same pair is the string, so a numeric stage refuses it.
		{`(name -> Max(1)) -> Abs > 0`, "takes a number"},
	}
	for _, row := range rows {
		t.Run(row.inv, func(t *testing.T) {
			t.Parallel()
			res := loadInvariant(t, row.inv)
			if res.Err() == nil {
				t.Fatalf("a mixed Min/Max pair reached a stage that refuses it, and loaded clean")
			}
			if !hasCodeMentioning(res, diag.E_INVALID_INVARIANT, row.want) {
				t.Errorf("want E_INVALID_INVARIANT mentioning %q; got %v", row.want, res.Err())
			}
		})
	}

	// The controls: the ranked result is the exact type, so the stage that
	// takes THAT type loads clean, and a same-subkind pair is unaffected.
	for _, inv := range []string{
		`(name -> Min(1)) -> Abs > 0`,
		`(name -> Max(1)) -> Upper != ""`,
		`(name -> Min("z")) -> Upper != ""`,
		`(MAIN_LINE.qty -> Max(1)) -> Abs > 0`,
	} {
		t.Run(inv, func(t *testing.T) {
			t.Parallel()
			if res := loadInvariant(t, inv); res.Err() != nil {
				t.Errorf("the ranked result was refused at the stage that takes it: %v", res.Err())
			}
		})
	}
}

// TestStaticInvariant_UnionMemberOfDisjointKindsIsRefused pins that a member
// declared at disjoint kinds by the types a value may be is refused at load.
// The member read merged the alternatives' types and threw away the one bit the
// join computes to answer this, so the member typed as a scalar of unknown
// subkind and every downstream builtin admitted it. Restoring mergeType turns
// this red.
func TestStaticInvariant_UnionMemberOfDisjointKindsIsRefused(t *testing.T) {
	t.Parallel()

	for _, inv := range []string{
		`(STAG -> Default(NTAG)).tag -> Upper != ""`,
		`(NTAG -> Default(STAG)).tag -> Upper != ""`,
		// The refusal is at the member read, so it does not need a stage after it.
		`(STAG -> Default(NTAG)).tag != nil`,
	} {
		t.Run(inv, func(t *testing.T) {
			t.Parallel()
			res := loadInvariant(t, inv)
			if res.Err() == nil {
				t.Fatalf("a member declared at disjoint kinds by the union's alternatives loaded clean")
			}
			if !hasCodeMentioning(res, diag.E_INVALID_INVARIANT, "disjoint kinds") {
				t.Errorf("want E_INVALID_INVARIANT mentioning disjoint kinds; got %v", res.Err())
			}
		})
	}

	// The controls: a member both alternatives declare at ONE kind still reads,
	// and a single-type receiver has no pair to disagree.
	for _, inv := range []string{
		`(STAG -> Default(NTAG)).label != ""`,
		`STAG.tag -> Upper != ""`,
		`NTAG.tag -> Abs > 0`,
	} {
		t.Run(inv, func(t *testing.T) {
			t.Parallel()
			if res := loadInvariant(t, inv); res.Err() != nil {
				t.Errorf("a member of one kind was refused: %v", res.Err())
			}
		})
	}
}

// TestStaticInvariant_NilLiteralRefusedAtATypedArgument pins that the nil
// literal is refused where the catalogue states a kind, and admitted where it
// states none. A schema taking the nil literal at a typed position loaded clean
// and failed on every instance forever. Returning kindNil to checkArgs' open
// disjunction turns the refuse rows red.
func TestStaticInvariant_NilLiteralRefusedAtATypedArgument(t *testing.T) {
	t.Parallel()

	for _, inv := range []string{
		`name -> Substring(nil) != ""`,    // ArgNumber
		`name -> TrimPrefix(nil) != ""`,   // ArgString
		`name -> Match(nil) -> Len > 0`,   // ArgPattern
		`name -> Substring(1, nil) != ""`, // the repeat position is typed too
	} {
		t.Run(inv, func(t *testing.T) {
			t.Parallel()
			res := loadInvariant(t, inv)
			if res.Err() == nil {
				t.Fatalf("the nil literal at a typed argument position loaded clean")
			}
			if !hasCodeMentioning(res, diag.E_INVALID_INVARIANT, "as its argument") {
				t.Errorf("want E_INVALID_INVARIANT mentioning the argument rule; got %v", res.Err())
			}
		})
	}

	// The controls: ArgAny admits nil, the receiver rule is unchanged, and a
	// value the checker cannot type still passes at a typed position.
	for _, inv := range []string{
		`name -> Coalesce(nil) != ""`,
		`name -> Default(nil) != ""`,
		`tags -> Contains(nil) == false`,
		`nil -> Substring(1) == nil`,
		`name -> Substring(tags -> Reduce(0) |$a, $b| { $a }) != ""`,
	} {
		t.Run(inv, func(t *testing.T) {
			t.Parallel()
			if res := loadInvariant(t, inv); res.Err() != nil {
				t.Errorf("an admitted argument was refused: %v", res.Err())
			}
		})
	}
}

func hasCodeMentioning(res diag.Result, code diag.Code, fragment string) bool {
	for is := range res.Issues() {
		if is.Code() == code && strings.Contains(is.Message(), fragment) {
			return true
		}
	}
	return false
}
