package neo4j

import (
	"maps"
	"reflect"
	"slices"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/internal/diagtest"
	"github.com/simon-lentz/yammm/schema/cypher"
)

// Diff refuses each remote row it cannot plan from, one issue per break, and
// returns no plan.
func TestDiff_RefusesARemoteRowItCannotPlanFrom(t *testing.T) {
	t.Parallel()

	s := loadInline(t, "schema \"p\"\n\ntype T {\n\tid String primary\n\tx String @index\n}\n")
	unique := func(name string) RemoteConstraint {
		return RemoteConstraint{Name: name, Type: "UNIQUENESS", EntityType: "NODE", LabelsOrTypes: []string{"p__T"}, Properties: []string{"id"}}
	}
	typed := func(name, propertyType string) RemoteConstraint {
		return RemoteConstraint{Name: name, Type: "NODE_PROPERTY_TYPE", EntityType: "NODE", LabelsOrTypes: []string{"p__T"}, Properties: []string{"x"}, PropertyType: propertyType}
	}
	rangeIndex := func(name string) RemoteIndex {
		return RemoteIndex{Name: name, Type: "RANGE", EntityType: "NODE", LabelsOrTypes: []string{"p__T"}, Properties: []string{"x"}, State: "ONLINE"}
	}

	for _, tc := range []struct {
		name        string
		constraints []RemoteConstraint
		indexes     []RemoteIndex
		want        []string
	}{
		{
			name:        "a constraint with no name",
			constraints: []RemoteConstraint{unique("a"), unique("")},
			want:        []string{"remote constraint 1 has no name"},
		},
		{
			name:    "an index with no name",
			indexes: []RemoteIndex{rangeIndex("")},
			want:    []string{"remote index 0 has no name"},
		},
		{
			name:        "two constraints under one name",
			constraints: []RemoteConstraint{unique("k"), unique("a"), typed("k", "STRING")},
			want:        []string{`remote constraints 0 and 2 share the name "k"`},
		},
		{
			name:    "three indexes under one name, and two under another",
			indexes: []RemoteIndex{rangeIndex("i"), rangeIndex("i"), rangeIndex("j"), rangeIndex("i"), rangeIndex("j")},
			want:    []string{`remote indexes 0, 1 and 3 share the name "i"`, `remote indexes 2 and 4 share the name "j"`},
		},
		{
			name:        "a property type constraint with no property type",
			constraints: []RemoteConstraint{typed("t", "")},
			want:        []string{"remote constraint 0 is a property type constraint that reports no property type"},
		},
		{
			name:        "a nameless property type constraint with no property type",
			constraints: []RemoteConstraint{typed("", "")},
			want:        []string{"remote constraint 0 has no name", "remote constraint 0 is a property type constraint that reports no property type"},
		},
		{
			name:        "a break on each side",
			constraints: []RemoteConstraint{unique("")},
			indexes:     []RemoteIndex{rangeIndex("i"), rangeIndex("i")},
			want:        []string{"remote constraint 0 has no name", `remote indexes 0 and 1 share the name "i"`},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			plan, result := diagtest.Capture(t, func(warn func(diag.Issue)) (Plan, error) {
				return Diff(cypher.Config{}, s, tc.constraints, tc.indexes, WithWarnings(warn))
			})
			if !reflect.DeepEqual(plan, Plan{}) {
				t.Errorf("plan = %+v, want the zero Plan", plan)
			}
			var got []string
			for issue := range result.Issues() {
				if issue.Code() != E_NEO4J_INVALID_REMOTE_OBJECT || issue.Severity() != diag.Error {
					t.Errorf("issue %s %s %q, want an Error %s", issue.Severity(), issue.Code(), issue.Message(), E_NEO4J_INVALID_REMOTE_OBJECT)
				}
				got = append(got, issue.Message())
			}
			slices.Sort(got)
			if !slices.Equal(got, tc.want) {
				t.Errorf("messages = %q, want %q", got, tc.want)
			}
		})
	}
}

// A constraint and the index that backs it share one name, which Diff takes:
// the two are of different kinds.
func TestDiff_AConstraintAndItsBackingIndexShareTheirName(t *testing.T) {
	t.Parallel()

	s := loadInline(t, "schema \"p\"\n\ntype T {\n\tid String primary\n}\n")
	c := cypher.Config{}
	desired, err := c.Constraints(s)
	if err != nil {
		t.Fatal(err)
	}
	constraints := asRemote(desired)
	var indexes []RemoteIndex
	for _, rc := range constraints {
		if rc.Type == "UNIQUENESS" {
			indexes = append(indexes, RemoteIndex{Name: rc.Name, Type: "RANGE", EntityType: "NODE", LabelsOrTypes: rc.LabelsOrTypes, Properties: rc.Properties, State: "ONLINE", OwningConstraint: rc.Name})
		}
	}
	if len(indexes) != 1 {
		t.Fatalf("the generator emitted %d UNIQUE constraints, want 1: %+v", len(indexes), desired)
	}
	plan, err := Diff(c, s, constraints, indexes)
	if err != nil {
		t.Fatalf("result = %v, want no error", err)
	}
	if got := len(plan.Constraints.Match); got != len(desired) {
		t.Errorf("constraint Match = %d, want %d: %+v", got, len(desired), plan.Constraints)
	}
	if got := plan.Indexes.Excluded; got != 1 {
		t.Errorf("index Excluded = %d, want the backing index alone: %+v", got, plan.Indexes)
	}
}

// A remote row's break and a schema's break are independent, so Diff reports
// both.
func TestDiff_ReportsARemoteRowBesideASchemaError(t *testing.T) {
	t.Parallel()

	s := loadInline(t, "schema \"p\"\n\ntype T {\n\tid String primary\n}\n")
	plan, result := diagtest.Capture(t, func(warn func(diag.Issue)) (Plan, error) {
		return Diff(cypher.Config{LabelPrefix: "9"}, s, []RemoteConstraint{{Type: "UNIQUENESS"}}, nil, WithWarnings(warn))
	})
	if !reflect.DeepEqual(plan, Plan{}) {
		t.Errorf("plan = %+v, want the zero Plan", plan)
	}
	if got, want := codeCounts(result), map[string]int{"E_NEO4J_INVALID_IDENTIFIER": 1, "E_NEO4J_INVALID_REMOTE_OBJECT": 1}; !maps.Equal(got, want) {
		t.Errorf("codes = %v, want %v", got, want)
	}
}

// The remote rows' issues arrive in one order on every run, so a collector
// that keeps the first issue keeps the same one each time.
func TestDiff_RemoteRowIssuesArriveInOneOrder(t *testing.T) {
	t.Parallel()

	s := loadInline(t, "schema \"p\"\n\ntype T {\n\tid String primary\n}\n")
	row := func(name string) RemoteIndex {
		return RemoteIndex{Name: name, Type: "RANGE", EntityType: "NODE", LabelsOrTypes: []string{"o__U"}, Properties: []string{"q"}, State: "ONLINE"}
	}
	indexes := []RemoteIndex{row("b"), row("a"), row("b"), row("a"), row("c"), row("c")}
	for range 100 {
		_, result := diagtest.Capture(t, func(warn func(diag.Issue)) (Plan, error) {
			return Diff(cypher.Config{}, s, nil, indexes, WithWarnings(warn))
		})
		kept := diag.NewCollector(1)
		kept.Merge(result)
		var got []string
		for issue := range kept.Result().Issues() {
			got = append(got, issue.Message())
		}
		if want := []string{`remote indexes 1 and 3 share the name "a"`}; !slices.Equal(got, want) {
			t.Fatalf("a one-issue collector kept %q, want %q", got, want)
		}
	}
}
