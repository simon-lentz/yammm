package schema_test

import (
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema"
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

// Min with an argument is typed by the order internal/value.Order implements,
// which ranks a number below a string and nil below both: the result is the
// lower of the two, or nil when either is absent. Max yields the lower when the
// higher is absent, so its result is of either kind and no stage refuses it.
func TestStaticInvariant_MinMaxTypedByTheTotalOrder(t *testing.T) {
	t.Parallel()

	// Min of a string and a number is the number, so a string stage refuses it.
	res := loadInvariant(t, `(name -> Min(1)) -> Upper != ""`)
	if !hasCodeMentioning(res, diag.E_INVALID_INVARIANT, "takes a string") {
		t.Errorf("want E_INVALID_INVARIANT mentioning %q; got %v", "takes a string", res.Err())
	}

	for _, inv := range []string{
		`(name -> Min(1)) -> Abs > 0`,
		`(name -> Min("z")) -> Upper != ""`,
		`(name -> Max(1)) -> Upper != ""`,
		`(name -> Max(1)) -> Abs > 0`,
		`(1 -> Max(name)) -> Abs > 0`,
		`!(f1 -> Max(name))`,
		`(MAIN_LINE.qty -> Max(1)) -> Abs > 0`,
	} {
		t.Run(inv, func(t *testing.T) {
			t.Parallel()
			if res := loadInvariant(t, inv); res.Err() != nil {
				t.Errorf("the ranked result was refused at a stage that takes it: %v", res.Err())
			}
		})
	}

	// Max of two numbers is a number, so a string stage refuses it.
	res = loadInvariant(t, `(MAIN_LINE.qty -> Max(1)) -> Upper != ""`)
	if !hasCodeMentioning(res, diag.E_INVALID_INVARIANT, "takes a string") {
		t.Errorf("want E_INVALID_INVARIANT mentioning %q; got %v", "takes a string", res.Err())
	}
}

// A concatenation of two association keys is a list like any other: a member
// read from it names the list, and one read from its element the association.
func TestStaticInvariant_AConcatenationOfKeysIsReadAsAList(t *testing.T) {
	t.Parallel()
	const head = `schema "s"

type Depot {
	region String primary
	code String primary
	name String
}

type Order {
	id String primary
	--> DEPOT (one) Depot
	! "m" `
	for inv, want := range map[string]string{
		`DEPOT.name == "x"`:              "is read through an association",
		`(DEPOT + DEPOT).name == "x"`:    "is read from a list",
		`(DEPOT + DEPOT)[0].name == "x"`: "is read through an association",
	} {
		t.Run(inv, func(t *testing.T) {
			t.Parallel()
			_, res := schema.LoadString(t.Context(), head+inv+"\n}\n", "s.yammm")
			if !hasCodeMentioning(res, diag.E_INVALID_INVARIANT, want) {
				t.Errorf("want E_INVALID_INVARIANT mentioning %q; got %v", want, res.Err())
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
		`name -> Substring(nil) != ""`,    // ArgInteger
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

	// The controls: ArgAny admits nil, and a value the checker cannot type
	// still passes at a typed position.
	for _, inv := range []string{
		`name -> Coalesce(nil) != ""`,
		`name -> Default(nil) != ""`,
		`tags -> Contains(nil) == false`,
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
