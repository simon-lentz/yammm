package parse

import (
	"testing"

	"github.com/simon-lentz/yammm/location"
)

// TestParse_AnImportWithNoPathValueKeepsItsAlias pins that an import whose
// path literal has no value stays in the tree with PathRefused set and no
// Path, so the loader can tell a declared alias from an unknown one.
func TestParse_AnImportWithNoPathValueKeepsItsAlias(t *testing.T) {
	t.Parallel()
	for name, path := range map[string]string{
		"an invalid byte":             "\"./a\xff\"",
		"an escape that is not UTF-8": `"./a\xff"`,
		"malformed digits":            `"./a\xZZ"`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f, _ := Parse([]byte("schema \"s\"\nimport "+path+" as a\nimport \"./b\" as b\ntype T {\n\tid String primary\n}\n"), location.NewSourceID("s.yammm"))
			if len(f.Imports) != 2 {
				t.Fatalf("the tree holds %d imports, want 2", len(f.Imports))
			}
			if imp := f.Imports[0]; !imp.PathRefused || imp.Path != "" || imp.Alias != "a" || imp.PathRaw != path {
				t.Errorf("first import = %+v, want PathRefused, no Path, alias a and the raw spelling", imp)
			}
			if imp := f.Imports[1]; imp.PathRefused || imp.Path != "./b" {
				t.Errorf("second import = %+v, want the path ./b", imp)
			}
		})
	}
}
