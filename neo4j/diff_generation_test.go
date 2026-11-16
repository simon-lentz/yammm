package neo4j

import (
	"maps"
	"reflect"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema/cypher"
)

// codeCounts counts r's issues by code.
func codeCounts(r diag.Result) map[string]int {
	m := map[string]int{}
	for issue := range r.Issues() {
		m[issue.Code().String()]++
	}
	return m
}

// asRemote spells desired as the rows SHOW CONSTRAINTS returns for it.
func asRemote(desired []cypher.Constraint) []RemoteConstraint {
	out := make([]RemoteConstraint, 0, len(desired))
	for _, d := range desired {
		out = append(out, RemoteConstraint{
			Name: d.Name, Type: d.Kind.String(), EntityType: "NODE",
			LabelsOrTypes: []string{d.Label}, Properties: d.Properties, PropertyType: d.TypeExpr,
		})
	}
	return out
}

// A constraint error returns before the index generation runs, so an invalid
// label both passes would refuse is reported once.
func TestDiff_AConstraintErrorReturnsBeforeTheIndexes(t *testing.T) {
	t.Parallel()

	s := loadInline(t, "schema \"p\"\n\ntype T {\n\tid String primary\n\tx String @index\n}\n")
	plan, result := Diff(cypher.Config{LabelPrefix: "9"}, s, nil, nil)
	if !reflect.DeepEqual(plan, Plan{}) {
		t.Errorf("plan = %+v, want the zero Plan", plan)
	}
	if got, want := codeCounts(result), map[string]int{"E_NEO4J_INVALID_IDENTIFIER": 1}; !maps.Equal(got, want) {
		t.Errorf("codes = %v, want %v", got, want)
	}
}

// An index error after a constraint pass that holds only warnings returns no
// plan, and the result holds both passes' issues.
func TestDiff_AnIndexErrorKeepsTheConstraintWarnings(t *testing.T) {
	t.Parallel()

	s := loadInline(t, "schema \"p\"\n\ntype T {\n\tid String primary\n\tmatch String @index\n}\n")
	c := cypher.Config{Edition: cypher.Community, NodeKeys: true, TypeConstraints: cypher.NoProperties}
	plan, result := Diff(c, s, nil, nil)
	if !reflect.DeepEqual(plan, Plan{}) {
		t.Errorf("plan = %+v, want the zero Plan", plan)
	}
	want := map[string]int{
		"E_NEO4J_INVALID_IDENTIFIER":         1,
		"W_NEO4J_EDITION_CONSTRAINT_OMITTED": 1,
		"W_NEO4J_NODE_KEY_UNSUPPORTED":       1,
	}
	if got := codeCounts(result); !maps.Equal(got, want) {
		t.Errorf("codes = %v, want %v", got, want)
	}
}

// A plan comes back with the generation's warnings.
func TestDiff_APlanKeepsTheGenerationsWarnings(t *testing.T) {
	t.Parallel()

	s := loadInline(t, "schema \"p\"\n\ntype T {\n\tid String primary\n\tx String @index\n}\n")
	plan, result := Diff(cypher.Config{Edition: cypher.Community, NodeKeys: true}, s, nil, nil)
	if result.HasErrors() || len(plan.Constraints.Create) == 0 || len(plan.Indexes.Create) != 1 {
		t.Fatalf("plan = %+v; result:\n%s", plan, result)
	}
	if !result.HasCode(cypher.W_NEO4J_NODE_KEY_UNSUPPORTED) {
		t.Errorf("the result lost the generation's warning:\n%s", result)
	}
}

// Diff owns the labels the Config composes: under a prefix every declared row
// matches and an owned stale row is a Drop.
func TestDiff_OwnsTheLabelsTheConfigComposes(t *testing.T) {
	t.Parallel()

	s := loadInline(t, "schema \"p\"\n\ntype T {\n\tid String primary\n}\n")
	c := cypher.Config{LabelPrefix: "app_", TypeConstraints: cypher.NoProperties}
	desired, result := c.Constraints(s)
	if result.HasErrors() {
		t.Fatal(result)
	}
	stale := RemoteConstraint{Name: "stale", Type: "UNIQUENESS", EntityType: "NODE", LabelsOrTypes: []string{"app_p__T"}, Properties: []string{"zz"}}
	plan, result := Diff(c, s, append(asRemote(desired), stale), nil)
	if result.HasErrors() {
		t.Fatal(result)
	}
	if len(plan.Constraints.Match) != len(desired) || len(plan.Constraints.Drop) != 1 {
		t.Errorf("matched %d of %d and dropped %d; want every declared row matched and the stale row dropped",
			len(plan.Constraints.Match), len(desired), len(plan.Constraints.Drop))
	}
}

// Diff judges a remote constraint's kind under the Config's edition: on
// Community an owned NOT NULL is Excluded, never a Drop.
func TestDiff_JudgesAKindUnderTheConfigsEdition(t *testing.T) {
	t.Parallel()

	s := loadInline(t, "schema \"p\"\n\ntype T {\n\tid String primary\n}\n")
	c := cypher.Config{Edition: cypher.Community}
	desired, result := c.Constraints(s)
	if result.HasErrors() {
		t.Fatal(result)
	}
	notNull := RemoteConstraint{Name: "nn", Type: "NODE_PROPERTY_EXISTENCE", EntityType: "NODE", LabelsOrTypes: []string{"p__T"}, Properties: []string{"zz"}}
	plan, result := Diff(c, s, append(asRemote(desired), notNull), nil)
	if result.HasErrors() {
		t.Fatal(result)
	}
	if len(plan.Constraints.Drop) != 0 || plan.Constraints.Excluded != 1 {
		t.Errorf("dropped %v and excluded %d; a Community plan must exclude the NOT NULL", plan.Constraints.Drop, plan.Constraints.Excluded)
	}
}
