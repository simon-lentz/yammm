package schema_test

import (
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema"
)

// A regex literal runs to the end of its line. A backslash before a raw
// newline must not extend it onto the next line; the schema is malformed and
// says so with E_SYNTAX rather than loading a two-line pattern.
func TestLoad_RegexLiteralCannotCrossALine(t *testing.T) {
	t.Parallel()
	_, res := schema.LoadString(t.Context(), "schema \"s\"\n\ntype T {\n\tid String primary\n\tcode String\n\t! \"code shape\" code =~ /a\\\nb/\n}\n", "re.yammm")
	if !res.HasCode(diag.E_SYNTAX) {
		t.Fatalf("a regex literal spanning two lines must be a syntax error; got %v", res)
	}
}
