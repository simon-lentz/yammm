package schema_test

import (
	"slices"
	"testing"

	"github.com/simon-lentz/yammm/schema"
)

func TestExtendsClause_HoldsEachSpelling(t *testing.T) {
	t.Parallel()

	s, res := schema.LoadString(t.Context(), "schema \"m\"\ntype B { id String primary }\ntype C { id String primary }\ntype A extends B, C, B { }\n", "m.yammm")
	if !res.OK() {
		t.Fatal(res)
	}
	a, ok := s.Type("A")
	if !ok {
		t.Fatal("no type A")
	}

	clause := slices.Collect(a.ExtendsClause())
	var names []string
	for _, ref := range clause {
		names = append(names, ref.Name())
	}
	if !slices.Equal(names, []string{"B", "C", "B"}) {
		t.Errorf("ExtendsClause names %v, want [B C B]", names)
	}

	inherits := slices.Collect(a.Inherits())
	if len(inherits) != 2 || inherits[0] != clause[0] || inherits[1] != clause[1] {
		t.Errorf("Inherits = %v, want the first spelling of B, then C", inherits)
	}
}

// The clause names one type twice only when qualifier and name both repeat: a bare R
// and an imported a.R are two supertypes.
func TestExtendsClause_ABareAndAQualifiedNameAreTwoSupertypes(t *testing.T) {
	t.Parallel()

	sources := map[string][]byte{
		"/virtual/f.yammm": []byte("schema \"f\"\ntype R { rid String primary }\n"),
		"/virtual/e.yammm": []byte("schema \"e\"\nimport \"./f\" as a\ntype R { id String primary }\ntype X extends R, a.R, R { }\n"),
	}
	s, res := schema.LoadSourcesWithEntry(t.Context(), sources, "/virtual/e.yammm", "/virtual")
	if !res.OK() {
		t.Fatal(res)
	}
	x, ok := s.Type("X")
	if !ok {
		t.Fatal("no type X")
	}

	var inherits []string
	for ref := range x.Inherits() {
		inherits = append(inherits, ref.String())
	}
	if !slices.Equal(inherits, []string{"R", "a.R"}) {
		t.Errorf("Inherits = %v, want [R a.R]", inherits)
	}
	if got := len(slices.Collect(x.SuperTypes())); got != 2 {
		t.Errorf("X holds %d supertypes, want 2", got)
	}
}

// The clause's spellings reach no digest: a repeated supertype is one edge, and a
// stored .ys must not go stale because a schema repeats a name.
func TestExtendsClause_ARepeatedNameLeavesTheStructuralHash(t *testing.T) {
	t.Parallel()

	hash := func(clause string) string {
		s, res := schema.LoadString(t.Context(), "schema \"m\"\ntype B { id String primary }\ntype A extends "+clause+" { }\n", "m.yammm")
		if !res.OK() {
			t.Fatal(res)
		}
		return schema.StructuralHash(s)
	}
	if once, twice := hash("B"), hash("B, B"); once != twice {
		t.Errorf("B hashes %s and B, B hashes %s, want one hash", once, twice)
	}
}
