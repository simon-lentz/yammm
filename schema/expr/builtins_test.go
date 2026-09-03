package expr_test

import (
	"testing"

	"github.com/simon-lentz/yammm/schema/expr"
)

func TestLookupBuiltin_IsCaseInsensitive(t *testing.T) {
	for _, name := range []string{"Len", "len", "LEN"} {
		s, ok := expr.LookupBuiltin(name)
		if !ok || s.Name != "Len" {
			t.Errorf("LookupBuiltin(%q) = %+v, %v; want the Len spec", name, s, ok)
		}
	}
	if _, ok := expr.LookupBuiltin("Bogus"); ok {
		t.Error("an unknown name must not resolve")
	}
}

// A builtin that binds a lambda parameter must accept a body, and one that
// binds none must not: the two fields describe one fact.
func TestBuiltins_BodyAndBindingAgree(t *testing.T) {
	specs := expr.Builtins()
	if len(specs) < 40 {
		t.Fatalf("catalogue holds %d builtins; the language defines more than 40", len(specs))
	}
	for _, s := range specs {
		if s.AcceptBody != (s.Params != expr.BindNone) {
			t.Errorf("%s: AcceptBody=%v but Params=%v", s.Name, s.AcceptBody, s.Params)
		}
		if s.MaxParams == 0 && s.AcceptBody {
			t.Errorf("%s: accepts a body but allows no parameters", s.Name)
		}
		if s.Params == expr.BindAccumulatorElement && s.MaxParams != 2 {
			t.Errorf("%s: binds an accumulator and an element but allows %d parameters", s.Name, s.MaxParams)
		}
	}
}
