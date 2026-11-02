package eval

import (
	"os"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/internal/expr"
)

// Every builtin the language defines has an implementation, and every
// implementation is a builtin the language defines.
func TestBuiltinRegistry_MatchesTheCatalogue(t *testing.T) {
	data, err := os.ReadFile("../../../internal/expr/testdata/builtin_names.txt")
	if err != nil {
		t.Fatal(err)
	}
	for name := range strings.FieldsSeq(string(data)) {
		if _, ok := lookupBuiltin(strings.ToLower(name)); !ok {
			t.Errorf("catalogue entry %s has no implementation", name)
		}
	}
	for name := range builtinRegistry {
		if _, ok := expr.LookupBuiltin(name); !ok {
			t.Errorf("implementation %s is not in the catalogue", name)
		}
	}
}
