package eval

import (
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/schema/expr"
)

// Every builtin the language defines has an implementation, and every
// implementation is a builtin the language defines.
func TestBuiltinRegistry_MatchesTheCatalogue(t *testing.T) {
	for _, s := range expr.Builtins() {
		if _, ok := lookupBuiltin(strings.ToLower(s.Name)); !ok {
			t.Errorf("catalogue entry %s has no implementation", s.Name)
		}
	}
	for name := range builtinRegistry {
		if _, ok := expr.LookupBuiltin(name); !ok {
			t.Errorf("implementation %s is not in the catalogue", name)
		}
	}
}
