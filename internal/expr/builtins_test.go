package expr_test

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/internal/expr"
)

func TestLookupBuiltin_ReadsTheExactName(t *testing.T) {
	if s, ok := expr.LookupBuiltin("Len"); !ok || s.Name != "Len" {
		t.Errorf(`LookupBuiltin("Len") = %+v, %v; want the Len spec`, s, ok)
	}
	for _, name := range []string{"len", "LEN", "lEN", "Bogus", ""} {
		if s, ok := expr.LookupBuiltin(name); ok {
			t.Errorf("LookupBuiltin(%q) = %+v; want no builtin", name, s)
		}
	}
}

// testdata/builtin_names.txt lists the catalogue for the tests of other packages
// that walk it, and holds exactly its names.
func TestBuiltins_NamesFileListsTheCatalogue(t *testing.T) {
	data, err := os.ReadFile("testdata/builtin_names.txt")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, s := range expr.Builtins() {
		names = append(names, s.Name)
	}
	if got, want := strings.Fields(string(data)), names; !slices.Equal(got, want) {
		t.Errorf("builtin_names.txt holds %v, the catalogue %v", got, want)
	}
}

// The catalogue's fields describe one fact each way: a builtin that binds a
// parameter accepts a body and allows at least one parameter; one that binds
// none allows no parameter, and accepts a body only when it evaluates that
// body in the caller's scope, as Lest does; a builtin that binds an element
// of its receiver, or yields one, takes a list receiver.
func TestBuiltins_FieldsAgree(t *testing.T) {
	specs := expr.Builtins()
	if len(specs) < 40 {
		t.Fatalf("catalogue holds %d builtins; the language defines more than 40", len(specs))
	}
	for _, s := range specs {
		binds := s.Params != expr.BindNone
		if binds && (!s.AcceptBody || s.MaxParams == 0) {
			t.Errorf("%s: binds a parameter but AcceptBody=%v, MaxParams=%d", s.Name, s.AcceptBody, s.MaxParams)
		}
		if !binds && s.MaxParams != 0 {
			t.Errorf("%s: binds no parameter but allows %d", s.Name, s.MaxParams)
		}
		if s.Params == expr.BindAccumulatorElement && s.MaxParams != 2 {
			t.Errorf("%s: binds an accumulator and an element but allows %d parameters", s.Name, s.MaxParams)
		}
		elementOfReceiver := s.Params == expr.BindElement || s.Params == expr.BindAccumulatorElement ||
			s.Result == expr.ResultElement || s.Result == expr.ResultFlattened
		takesList := s.Receiver == expr.RecvList || s.Receiver == expr.RecvSeededList || s.Receiver == expr.RecvOrderedList ||
			s.Receiver == expr.RecvStringList || s.Receiver == expr.RecvNumericList
		if elementOfReceiver && !takesList {
			t.Errorf("%s: binds or yields an element of its receiver but does not take a list", s.Name)
		}
		// Args states one kind per position: none for a builtin that takes no
		// argument, exactly MaxArgs for a bounded one, at least one — the
		// repeating entry — for an unbounded one.
		switch {
		case s.MaxArgs == 0 && len(s.Args) != 0:
			t.Errorf("%s: takes no argument but states %d argument kinds", s.Name, len(s.Args))
		case s.MaxArgs > 0 && len(s.Args) != s.MaxArgs:
			t.Errorf("%s: takes up to %d arguments but states %d argument kinds", s.Name, s.MaxArgs, len(s.Args))
		case s.MaxArgs < 0 && len(s.Args) == 0:
			t.Errorf("%s: takes unbounded arguments but states no repeating argument kind", s.Name)
		}
	}
	sub, _ := expr.LookupBuiltin("Substring")
	if k, ok := sub.ArgAt(1); !ok || k != expr.ArgInteger {
		t.Errorf("Substring's second argument: %v, %v; want an integer", k, ok)
	}
	coalesce, _ := expr.LookupBuiltin("Coalesce")
	if k, ok := coalesce.ArgAt(7); !ok || k != expr.ArgAny {
		t.Errorf("Coalesce's eighth argument: %v, %v; want the repeating entry", k, ok)
	}
	if _, ok := sub.ArgAt(-1); ok {
		t.Error("ArgAt(-1) reported a kind")
	}
	lest, _ := expr.LookupBuiltin("Lest")
	if !lest.AcceptBody || lest.Params != expr.BindNone || lest.MaxParams != 0 {
		t.Errorf("Lest evaluates its body in the caller's scope: AcceptBody, BindNone, MaxParams 0; got %+v", lest)
	}
}
