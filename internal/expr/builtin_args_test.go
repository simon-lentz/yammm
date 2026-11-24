package expr_test

import (
	"testing"

	"github.com/simon-lentz/yammm/internal/expr"
)

// TestLookupBuiltin_ArgsDoNotAliasTheCatalogue pins that a caller cannot write
// through a returned spec into the package-level table. BuiltinSpec is a value
// type whose every other field is a scalar, so a shared Args backing array
// breaks the reading its shape invites: before the clone, mutating one lookup's
// Args[0] changed every later lookup in the process. Removing the clone from
// LookupBuiltin turns this red.
func TestLookupBuiltin_ArgsDoNotAliasTheCatalogue(t *testing.T) {
	first, ok := expr.LookupBuiltin("Substring")
	if !ok {
		t.Fatal("Substring is not in the catalogue")
	}
	if len(first.Args) == 0 {
		t.Fatal("Substring declares no argument kinds, so this pin asserts nothing")
	}
	want := first.Args[0]

	first.Args[0] = expr.ArgPattern + 1 // a kind no catalogue row holds

	second, ok := expr.LookupBuiltin("Substring")
	if !ok {
		t.Fatal("Substring left the catalogue")
	}
	if second.Args[0] != want {
		t.Errorf("a later lookup saw the first's mutation: Args[0] = %v, want %v", second.Args[0], want)
	}
	if first.Args[0] == want {
		t.Fatal("the mutation did not take, so this test cannot fail for the reason it names")
	}
}

// TestBuiltinSpec_ArgAtRepeatsTheLastKind pins the repeat-last rule ArgAt's
// godoc states. It is vacuous on the catalogue as it stands — Coalesce is the
// only unbounded builtin and its repeating kind is ArgAny — so the rule is
// driven on a spec built here, which is the only way to falsify it.
func TestBuiltinSpec_ArgAtRepeatsTheLastKind(t *testing.T) {
	spec := expr.BuiltinSpec{Args: []expr.ArgKind{expr.ArgString, expr.ArgNumber}}

	for _, tc := range []struct {
		at   int
		want expr.ArgKind
		ok   bool
	}{
		{0, expr.ArgString, true},
		{1, expr.ArgNumber, true},
		{2, expr.ArgNumber, true},  // past the end: the last kind repeats
		{99, expr.ArgNumber, true}, // and keeps repeating
		{-1, expr.ArgAny, false},
	} {
		got, ok := spec.ArgAt(tc.at)
		if ok != tc.ok || got != tc.want {
			t.Errorf("ArgAt(%d) = (%v, %t), want (%v, %t)", tc.at, got, ok, tc.want, tc.ok)
		}
	}

	if _, ok := (expr.BuiltinSpec{}).ArgAt(0); ok {
		t.Error("a builtin taking no argument reported a kind at position 0")
	}
}
