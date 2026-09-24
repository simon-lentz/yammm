package snapshot_test

import (
	"maps"
	"math"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/instancetest"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

const unwritableSchema = `schema "unwritable"

type Company {
	company_id String primary
}

type Employee {
	employee_id String primary
	rating Float
	scores List<Float>
	note String
	--> WORKS_AT (_:one) Company {
		weight Float
		alpha Float
	}
	*-> BADGES (many) Badge
}

part type Badge {
	label String
}
`

func loadUnwritable(t *testing.T) *schema.Schema {
	t.Helper()
	s, res := schema.LoadString(t.Context(), unwritableSchema, "unwritable.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res)
	}
	return s
}

// unwritableEmployee is Employee e1 with props beside its key, and one
// WORKS_AT target carrying edgeProps.
func unwritableEmployee(t *testing.T, s *schema.Schema, props map[string]any, target string, edgeProps map[string]any) *instance.ValidInstance {
	t.Helper()
	empT, _ := s.Type("Employee")
	p := map[string]any{"employee_id": "e1"}
	maps.Copy(p, props)
	return instancetest.VI("Employee", instancetest.TypeID(empT.ID()), instancetest.PK("e1"),
		instancetest.Props(p),
		instancetest.Edges(map[string]*instance.ValidEdgeData{
			"WORKS_AT": instance.NewValidEdgeData([]instance.ValidEdgeTarget{
				instance.NewValidEdgeTarget(immutable.WrapKey([]any{target}), immutable.WrapProperties(edgeProps)),
			}),
		}))
}

// requireUnwritable asserts res is the one refusal of a value the wire cannot
// carry, with exactly the details want names and a message holding every
// phrase in says.
func requireUnwritable(t *testing.T, res diag.Result, want map[string]string, says ...string) {
	t.Helper()
	n := 0
	for issue := range res.Issues() {
		if issue.Code() != diag.E_SNAPSHOT_MALFORMED || issue.Severity() != diag.Error {
			t.Errorf("unexpected issue %s %s: %s", issue.Severity(), issue.Code(), issue.Message())
			continue
		}
		n++
		details := map[string]string{}
		for _, d := range issue.Details() {
			details[d.Key] = d.Value
		}
		if !maps.Equal(details, want) {
			t.Errorf("details = %v, want %v", details, want)
		}
		for _, phrase := range says {
			if !strings.Contains(issue.Message(), phrase) {
				t.Errorf("message does not say %q: %s", phrase, issue.Message())
			}
		}
	}
	if n != 1 {
		t.Errorf("Marshal refused %d times, want once with Error %s: %s", n, diag.E_SNAPSHOT_MALFORMED, res)
	}
}

// A value the wire cannot carry is data an unvalidated instance holds, so
// Marshal names where it sits: the type, the key, the property and, for an edge
// property, the relation. Each case holds the one bad value at one position.
func TestMarshal_RefusesAnUnwritableValueByName(t *testing.T) {
	t.Parallel()
	s := loadUnwritable(t)
	compT, _ := s.Type("Company")
	company := instancetest.VI("Company", instancetest.TypeID(compT.ID()), instancetest.PK("c1"),
		instancetest.Props(map[string]any{"company_id": "c1"}))
	employee := snapshot.TypeRef{Schema: "unwritable", Name: "Employee"}.String()
	fine := map[string]any{"weight": 1.5}

	for _, c := range []struct {
		name     string
		emp      *instance.ValidInstance
		property string
		relation string
	}{
		{"non-finite float at a Float property", unwritableEmployee(t, s, map[string]any{"rating": math.NaN()}, "c1", fine), "rating", ""},
		{"non-finite float32 at a Float property", unwritableEmployee(t, s, map[string]any{"rating": float32(math.NaN())}, "c1", fine), "rating", ""},
		{"non-finite float inside a list", unwritableEmployee(t, s, map[string]any{"scores": []any{1.5, math.Inf(1)}}, "c1", fine), "scores", ""},
		{"non-finite float inside a map", unwritableEmployee(t, s, map[string]any{"note": map[string]any{"x": math.Inf(-1)}}, "c1", fine), "note", ""},
		{"a Go value encoding/json refuses", unwritableEmployee(t, s, map[string]any{"note": make(chan int)}, "c1", fine), "note", ""},
		{"non-finite float at an edge property", unwritableEmployee(t, s, nil, "c1", map[string]any{"weight": math.Inf(1)}), "weight", "WORKS_AT"},
		{"non-finite float at an unresolved record's property", unwritableEmployee(t, s, nil, "c9", map[string]any{"weight": math.Inf(-1)}), "weight", "WORKS_AT"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			g := graph.New(s)
			for _, inst := range []*instance.ValidInstance{company, c.emp} {
				if r := g.Add(t.Context(), inst); r.HasErrors() {
					t.Fatalf("Add: %s", r)
				}
			}
			data, res := snapshot.Marshal(t.Context(), g.Snapshot())
			if data != nil {
				t.Errorf("Marshal wrote %d bytes for a value the wire cannot carry", len(data))
			}
			want := map[string]string{
				diag.DetailKeyTypeName:     employee,
				diag.DetailKeyPrimaryKey:   `["e1"]`,
				diag.DetailKeyPropertyName: c.property,
			}
			says := []string{`property "` + c.property + `"`}
			if c.relation != "" {
				want[diag.DetailKeyRelationName] = c.relation
				says = append(says, `under "`+c.relation+`"`)
			}
			requireUnwritable(t, res, want, says...)
		})
	}
}

// Among several unwritable values of one property map the refusal names the
// least property name, so one snapshot always draws one message: an instance's
// own properties and an edge's alike.
func TestMarshal_NamesTheLeastUnwritableProperty(t *testing.T) {
	t.Parallel()
	s := loadUnwritable(t)
	for _, c := range []struct {
		name     string
		emp      *instance.ValidInstance
		property string
	}{
		{"instance properties", unwritableEmployee(t, s, map[string]any{"rating": math.NaN(), "note": make(chan int)}, "c9", nil), "note"},
		{"edge properties", unwritableEmployee(t, s, nil, "c9", map[string]any{"weight": math.NaN(), "alpha": math.Inf(1)}), "alpha"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			g := graph.New(s)
			if r := g.Add(t.Context(), c.emp); r.HasErrors() {
				t.Fatalf("Add: %s", r)
			}
			for range 8 {
				_, res := snapshot.Marshal(t.Context(), g.Snapshot())
				if !strings.Contains(res.String(), `property "`+c.property+`"`) {
					t.Fatalf("refusal does not name the least property: %s", res)
				}
			}
		})
	}
}

// A composed child and a duplicate record's instance have no address of their
// own a reader can find, so the refusal names the root or the duplicate record
// that holds the value, and the composition path down to it.
func TestMarshal_NamesWhereAnUnwritableValueSits(t *testing.T) {
	t.Parallel()
	s := loadUnwritable(t)
	empT, _ := s.Type("Employee")
	badgeT, _ := s.Type("Badge")
	badge := func(label any) *instance.ValidInstance {
		return instancetest.VI("Badge", instancetest.TypeID(badgeT.ID()), instancetest.NoKey(),
			instancetest.Props(map[string]any{"label": label}))
	}
	employee := snapshot.TypeRef{Schema: "unwritable", Name: "Employee"}.String()
	root := func(rating any) *instance.ValidInstance {
		return instancetest.VI("Employee", instancetest.TypeID(empT.ID()), instancetest.PK("e1"),
			instancetest.Props(map[string]any{"employee_id": "e1", "rating": rating}))
	}

	t.Run("a composed child", func(t *testing.T) {
		t.Parallel()
		g := graph.New(s)
		if r := g.Add(t.Context(), root(1.5)); r.HasErrors() {
			t.Fatalf("Add: %s", r)
		}
		for _, label := range []any{"fine", math.NaN()} {
			if r := g.AddComposed(t.Context(), empT.ID(), `["e1"]`, "BADGES", badge(label)); r.HasErrors() {
				t.Fatalf("AddComposed: %s", r)
			}
		}
		_, res := snapshot.Marshal(t.Context(), g.Snapshot())
		requireUnwritable(t, res, map[string]string{
			diag.DetailKeyTypeName: employee, diag.DetailKeyPrimaryKey: `["e1"]`, diag.DetailKeyPropertyName: "label",
		}, employee+`[["e1"]].BADGES[1]: property "label"`)
	})

	t.Run("a duplicate record's instance", func(t *testing.T) {
		t.Parallel()
		g := graph.New(s)
		if r := g.Add(t.Context(), root(1.5)); r.HasErrors() {
			t.Fatalf("Add: %s", r)
		}
		if r := g.Add(t.Context(), root(math.NaN())); !r.HasCode(diag.E_DUPLICATE_PK) {
			t.Fatalf("the second e1 was not recorded as a duplicate: %s", r)
		}
		_, res := snapshot.Marshal(t.Context(), g.Snapshot())
		requireUnwritable(t, res, map[string]string{
			diag.DetailKeyTypeName: employee, diag.DetailKeyPrimaryKey: `["e1"]`, diag.DetailKeyPropertyName: "rating",
		}, `duplicate record `+employee+`[["e1"]]: property "rating"`)
	})
}
