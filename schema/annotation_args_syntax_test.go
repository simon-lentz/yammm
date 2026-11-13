package schema_test

import (
	"slices"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema"
)

// An annotation whose argument list does not parse draws the list's E_SYNTAX
// alone: no second issue claims the annotation was written with no argument.
func TestLoad_AnAnnotationWhoseArgumentsDoNotParseDrawsOneIssue(t *testing.T) {
	t.Parallel()
	for _, member := range []string{
		"\t@@index(-1)", "\t@@index()", "\t@@index(,a)", "\t@@index(a b)",
		"\t@@index(Integer)", "\t@@index(as)", `	@@index("\q")`,
		"\tv Vector[3] @vector()", "\tv Vector[3] @vector(-1)",
		"\tv Vector[3] @vector /* c */ (cosine)", "\tv Vector[3] @vector // c\n\t\t()",
		"\t@@index ()", "\t@@index // c\n\t()", "\t@@index /* c */ (a)", "\tb String @index ()",
	} {
		t.Run(member, func(t *testing.T) {
			t.Parallel()
			src := "schema \"s\"\ntype T {\n\tid String primary\n\ta String\n" + member + "\n}\n"
			_, res := schema.LoadString(t.Context(), src, "s.yammm")
			issues := slices.Collect(res.Issues())
			if len(issues) != 1 || issues[0].Code() != diag.E_SYNTAX {
				t.Errorf("want one E_SYNTAX; got %v", res)
			}
		})
	}
}
