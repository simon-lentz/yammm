package schema_test

import (
	"testing"

	"github.com/simon-lentz/yammm/diag"
)

// invariantSchema writes one invariant on a type with an age and a list of tags.
func invariantSchema(expression string) string {
	return "schema \"m\"\ntype T {\n\tid String primary\n\tage Integer\n\ttags List<String>\n\t! \"inv\" " + expression + "\n}\n"
}

// A call named p is a call whatever its receiver: it draws what any unknown name
// draws, though a property access writes the same op with one child.
func TestInvariant_ACallNamedPIsAnUnknownFunction(t *testing.T) {
	t.Parallel()
	for _, expression := range []string{
		"id -> p",
		"$self -> p",
		"self -> p",
		"1 -> p",
		`"nope" -> p`,
		`"id" -> p`,
		`"age" -> p > 1`,
		"id -> p()",
		"tags -> p |$x| { $x }",
	} {
		t.Run(expression, func(t *testing.T) {
			t.Parallel()
			res := loadStringErr(t, invariantSchema(expression))
			wantCounts(t, res, map[diag.Code]int{diag.E_INVALID_INVARIANT: 1})
			for issue := range res.Issues() {
				if want := `unknown function "p" in invariant "inv" on type "T"`; issue.Message() != want {
					t.Errorf("message = %q, want %q", issue.Message(), want)
				}
			}
		})
	}
}

// A conditional's condition is judged as its branches are.
func TestInvariant_AConditionIsJudged(t *testing.T) {
	t.Parallel()
	res := loadStringErr(t, invariantSchema("nope ? { true : false }"))
	wantCounts(t, res, map[diag.Code]int{diag.E_UNKNOWN_PROPERTY: 1})
}
