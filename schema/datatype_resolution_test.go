package schema_test

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema"
)

// A datatype may list one declared after it. Resolution follows the references,
// not the declaration order, so both orders load the same schema: every
// constraint resolved to its terminal, and one structural hash.
func TestLoad_ResolvesDataTypesInAnyDeclarationOrder(t *testing.T) {
	const body = "type T {\n\tid String primary\n\tv A\n\tw List<B>\n}\n"
	orders := map[string]string{
		"each datatype listing one declared after it":  "type A = List<B>\ntype B = List<C>\ntype C = Integer[0, 9]\n",
		"each datatype listing one declared before it": "type C = Integer[0, 9]\ntype B = List<C>\ntype A = List<B>\n",
	}
	hashes := make(map[string]string, len(orders))
	for name, datatypes := range orders {
		s, res := schema.LoadString(t.Context(), "schema \"p\"\n\n"+datatypes+body, "p.yammm")
		if res.HasErrors() {
			t.Fatalf("%s: load: %v", name, res.Err())
		}
		for _, dt := range s.DataTypesSlice() {
			if !dt.Constraint().IsResolved() {
				t.Errorf("%s: datatype %s is not resolved: %v", name, dt.Name(), dt.Constraint())
			}
		}
		tt, _ := s.Type("T")
		for _, field := range []string{"v", "w"} {
			p, _ := tt.Property(field)
			if !p.Constraint().IsResolved() {
				t.Errorf("%s: property %s is not resolved: %v", name, field, p.Constraint())
			}
			if got := innermost(p.Constraint()); !got.Equal(schema.IntegerBetween(0, 9)) {
				t.Errorf("%s: property %s's innermost element is %v; want Integer[0, 9]", name, field, got)
			}
		}
		hashes[name] = schema.StructuralHash(s)
	}
	if got := slices.Compact(slices.Sorted(maps.Values(hashes))); len(got) != 1 {
		t.Errorf("the two orders hash differently: %v", hashes)
	}
}

// innermost unwraps every List layer and DataType reference of c.
func innermost(c schema.Constraint) schema.Constraint {
	for {
		switch x := schema.ResolveAlias(c).(type) {
		case schema.ListConstraint:
			c = x.Element()
		default:
			return x
		}
	}
}

// A datatype that reaches itself through its references, a List element
// included, has no constraint to resolve to. Each datatype on the cycle is
// refused at its own declaration, and one outside it that lists a member draws
// nothing more.
func TestLoad_RefusesADataTypeCycleAtEachMember(t *testing.T) {
	cases := map[string]struct {
		datatypes string
		members   []string
	}{
		"a datatype listing itself":            {"type A = List<A>\n", []string{"A"}},
		"two datatypes listing each other":     {"type A = List<B>\ntype B = List<A>\n", []string{"A", "B"}},
		"a cycle through a nested List":        {"type A = List<List<B>[1, 2]>\ntype B = List<A>\n", []string{"A", "B"}},
		"a datatype outside the cycle":         {"type X = List<A>\ntype A = List<B>\ntype B = List<A>\n", []string{"A", "B"}},
		"a cycle entered from its last member": {"type B = List<A>\ntype A = List<B>\ntype X = List<A>\n", []string{"A", "B"}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			src := "schema \"p\"\n\n" + tc.datatypes + "type T {\n\tid String primary\n}\n"
			res := loadStringErr(t, src)
			got := map[string]int{}
			for issue := range res.Issues() {
				if issue.Code() != diag.E_INVALID_CONSTRAINT {
					t.Errorf("unexpected issue %v: %s", issue.Code(), issue.Message())
					continue
				}
				var member string
				if _, err := fmt.Sscanf(issue.Message(), "datatype %q forms a cycle", &member); err != nil {
					t.Errorf("message %q does not name a cycle member", issue.Message())
					continue
				}
				if want := declarationLine(src, member); issue.Span().Start.Line != want {
					t.Errorf("cycle at %s reported on line %d; its declaration is line %d", member, issue.Span().Start.Line, want)
				}
				got[member]++
			}
			want := map[string]int{}
			for _, m := range tc.members {
				want[m] = 1
			}
			if !maps.Equal(got, want) {
				t.Errorf("cycle reports = %v; want one at each of %v", got, tc.members)
			}
		})
	}
}

// declarationLine returns the 1-based line of "type <name> =" in src.
func declarationLine(src, name string) int {
	for i, line := range strings.Split(src, "\n") {
		if strings.HasPrefix(line, "type "+name+" =") {
			return i + 1
		}
	}
	return 0
}

// A reference to a datatype that failed adds no report of its own, and still
// carries the constraint the datatype declares, so a later phase judges its
// shape: a List datatype on an edge is refused whether or not its element
// resolved, and the cycle or unknown name is reported once, at the datatype.
func TestLoad_AFailedDataTypeKeepsItsShapeForLaterPhases(t *testing.T) {
	const edge = "type T {\n\tid String primary\n\t--> R (one) T {\n\t\tp X\n\t}\n}\n"
	for name, tc := range map[string]struct {
		datatypes string
		want      map[diag.Code]int
	}{
		"an unknown element": {"type X = List<Mystery>\n", map[diag.Code]int{diag.E_UNKNOWN_TYPE: 1, diag.E_LIST_ON_EDGE: 1}},
		"a cycle":            {"type X = List<Y>\ntype Y = List<X>\n", map[diag.Code]int{diag.E_INVALID_CONSTRAINT: 2, diag.E_LIST_ON_EDGE: 1}},
	} {
		t.Run(name, func(t *testing.T) {
			src := "schema \"p\"\n\n" + tc.datatypes + edge
			res := loadStringErr(t, src)
			wantCounts(t, res, tc.want)
			for issue := range res.Issues() {
				if issue.Code() == diag.E_UNKNOWN_TYPE && issue.Span().Start.Line != declarationLine(src, "X") {
					t.Errorf("the unknown element is reported on line %d; X is declared on line %d", issue.Span().Start.Line, declarationLine(src, "X"))
				}
			}
		})
	}
}

// A datatype listing an element through an import that failed defers to the
// import's own report and stays a List, so an edge property typed by it is still
// refused for its shape.
func TestLoad_ADataTypeOverAFailedImportStaysAList(t *testing.T) {
	s, res := schema.LoadSourcesWithEntry(t.Context(), map[string][]byte{
		"main.yammm": []byte("schema \"main\"\nimport \"./missing\" as bad\n\ntype X = List<bad.Y>\n\ntype T {\n\tid String primary\n\t--> R (one) T {\n\t\tp X\n\t}\n}\n"),
	}, "main.yammm", t.TempDir())
	if s != nil {
		t.Fatal("a schema importing a missing file loaded")
	}
	wantCounts(t, res, map[diag.Code]int{diag.E_IMPORT_RESOLVE: 1, diag.E_LIST_ON_EDGE: 1, diag.E_UNKNOWN_TYPE: 0})
}

// The primary-key type check names the kind a datatype resolves to, not the
// reference's own kind: a List datatype is refused as a List, whether its
// element resolved or it sits on a cycle.
func TestLoad_APrimaryKeyTypedByAListDataTypeNamesTheList(t *testing.T) {
	for name, datatypes := range map[string]string{
		"a resolved List datatype":   "type Tags = List<String>\n",
		"a List datatype on a cycle": "type Tags = List<Tags>\n",
	} {
		t.Run(name, func(t *testing.T) {
			res := loadStringErr(t, "schema \"p\"\n\n"+datatypes+"type T {\n\tid Tags primary\n}\n")
			found := false
			for issue := range res.Issues() {
				if issue.Code() != diag.E_INVALID_PRIMARY_KEY_TYPE {
					continue
				}
				found = true
				if !strings.Contains(issue.Message(), "List cannot be used as a primary key") {
					t.Errorf("message %q does not name the List", issue.Message())
				}
			}
			if !found {
				t.Error("no E_INVALID_PRIMARY_KEY_TYPE")
			}
		})
	}
}
