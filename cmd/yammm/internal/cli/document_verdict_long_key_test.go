package cli

import (
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema"
)

// The verdict's duplicate and the graph's repeat a long key alike: cut to its
// head, by the rule every message that repeats a value follows.
func TestBuildGraph_ReportsALongDuplicateKeyAsTheGraphDoes(t *testing.T) {
	t.Parallel()
	s, res := schema.LoadString(t.Context(), teamSchema, "t.yammm")
	if res.HasErrors() {
		t.Fatal(res.String())
	}
	long := strings.Repeat("t", 100)
	validated, _ := ValidateInstances(t.Context(), s, teamRaws([]map[string]any{{"id": long}, {"id": long, "name": "A"}, {"id": long, "name": "B"}}, nil))
	_, result := BuildGraph(t.Context(), s, nil, validated)
	want := `duplicate primary key ["` + long[:30] + `… (104 bytes) for type "Team"`
	n := 0
	for issue := range result.Issues() {
		if issue.Code() != diag.E_DUPLICATE_PK {
			continue
		}
		n++
		if issue.Message() != want {
			t.Errorf("message = %q, want %q", issue.Message(), want)
		}
	}
	if n != 2 {
		t.Fatalf("%d E_DUPLICATE_PK, want 2: the verdict's and the graph's\n%s", n, result)
	}
}
