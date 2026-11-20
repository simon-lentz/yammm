// Package schematest builds schema values from their DSL form, for tests
// outside the schema package. The schema package constructs every constraint
// itself, through its parser, so a test states a constraint as a .yammm source
// states it and reads the value a load builds.
//
// It is test support: it may import "testing" and accept testing.TB, and it
// must never be imported by non-test code.
package schematest

import (
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/schema"
)

// Constraint returns the constraint the DSL text c states, as a loaded schema's
// property holds it: "String[1, 10]", "List<Timestamp>" or "Vector[3]". decls
// are declarations c may name, such as "type Name = String". It fails tb when
// the schema does not load.
func Constraint(tb testing.TB, c string, decls ...string) schema.Constraint {
	tb.Helper()
	var src strings.Builder
	src.WriteString("schema \"schematest\"\n\n")
	for _, d := range decls {
		src.WriteString(d)
		src.WriteString("\n\n")
	}
	src.WriteString("type T {\n\tid String primary\n\tv " + c + "\n}\n")
	s, res := schema.LoadString(tb.Context(), src.String(), "schematest.yammm")
	if res.HasErrors() {
		tb.Fatalf("schematest.Constraint(%q): %s\n%s", c, res, src.String())
	}
	t, _ := s.Type("T")
	p, _ := t.Property("v")
	return p.Constraint()
}
