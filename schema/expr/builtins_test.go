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
		takesList := s.Receiver == expr.RecvList || s.Receiver == expr.RecvScalarList ||
			s.Receiver == expr.RecvStringList || s.Receiver == expr.RecvNumericList
		if elementOfReceiver && !takesList {
			t.Errorf("%s: binds or yields an element of its receiver but does not take a list", s.Name)
		}
	}
	lest, _ := expr.LookupBuiltin("Lest")
	if !lest.AcceptBody || lest.Params != expr.BindNone || lest.MaxParams != 0 {
		t.Errorf("Lest evaluates its body in the caller's scope: AcceptBody, BindNone, MaxParams 0; got %+v", lest)
	}
}

// The datatype checks are the seven kinds a value can be checked against at
// runtime, matched case-insensitively; a shape or constraint keyword is not
// one, although the parser emits a DatatypeLiteral for it.
func TestIsDatatypeCheck(t *testing.T) {
	for _, name := range []string{"String", "Integer", "Float", "Boolean", "UUID", "Timestamp", "Date", "integer", "uuid"} {
		if !expr.IsDatatypeCheck(name) {
			t.Errorf("IsDatatypeCheck(%q) = false", name)
		}
	}
	for _, name := range []string{"Vector", "List", "Enum", "Pattern", "int", "number", "bool", ""} {
		if expr.IsDatatypeCheck(name) {
			t.Errorf("IsDatatypeCheck(%q) = true", name)
		}
	}
}
