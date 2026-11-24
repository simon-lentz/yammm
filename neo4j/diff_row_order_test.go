package neo4j

import (
	"reflect"
	"slices"
	"testing"

	"github.com/simon-lentz/yammm/schema/cypher"
)

// permutations returns every ordering of rows.
func permutations[T any](rows []T) [][]T {
	if len(rows) <= 1 {
		return [][]T{slices.Clone(rows)}
	}
	var out [][]T
	for i := range rows {
		rest := slices.Concat(rows[:i:i], rows[i+1:])
		for _, p := range permutations(rest) {
			out = append(out, append([]T{rows[i]}, p...))
		}
	}
	return out
}

// TestConstraintDiff_RowOrderDoesNotChangeThePlan diffs every ordering of the
// remote rows: two UNIQUE constraints over the declared key under names the
// declaration does not carry, in both server spellings, beside rows that block
// another declaration.
func TestConstraintDiff_RowOrderDoesNotChangeThePlan(t *testing.T) {
	t.Parallel()
	desired := []cypher.Constraint{
		{Name: "app__T_id_unique", Kind: cypher.ConstraintUnique, Label: "app__T", Properties: []string{"id"}},
		{Name: "app__T_k_key", Kind: cypher.ConstraintNodeKey, Label: "app__T", Properties: []string{"k"}},
	}
	row := func(name, typ string, props ...string) RemoteConstraint {
		return RemoteConstraint{Name: name, Type: typ, EntityType: "NODE", LabelsOrTypes: []string{"app__T"}, Properties: props}
	}
	actual := []RemoteConstraint{
		row("aaa", "UNIQUENESS", "id"),
		row("zzz", "NODE_PROPERTY_UNIQUENESS", "id"),
		row("u", "UNIQUENESS", "k"),
		row("app__T_k_key", "NODE_PROPERTY_EXISTENCE", "q"),
	}
	indexes := []RemoteIndex{
		{Name: "r", Type: "RANGE", EntityType: "NODE", LabelsOrTypes: []string{"app__T"}, Properties: []string{"k"}},
		{Name: "s", Type: "RANGE", EntityType: "NODE", LabelsOrTypes: []string{"app__T"}, Properties: []string{"id"}},
	}
	want := diff(cypher.Config{}, desired, nil, actual, indexes, ownedSet("app__T")).Constraints
	for _, rows := range permutations(actual) {
		for _, idx := range permutations(indexes) {
			if got := diff(cypher.Config{}, desired, nil, rows, idx, ownedSet("app__T")).Constraints; !reflect.DeepEqual(got, want) {
				t.Fatalf("rows %v, indexes %v:\ngot  %+v\nwant %+v", rows, idx, got, want)
			}
		}
	}
}

// TestConstraintDiff_TheDeclaredOrderWinsAReorderedTwin pairs the remote
// constraint whose properties are in the declared order, whichever row comes
// first, and leaves the reordered one a Drop.
func TestConstraintDiff_TheDeclaredOrderWinsAReorderedTwin(t *testing.T) {
	t.Parallel()
	desired := []cypher.Constraint{{Name: "app__T_a_b_unique", Kind: cypher.ConstraintUnique, Label: "app__T", Properties: []string{"a", "b"}}}
	reordered := RemoteConstraint{Name: "aaa", Type: "UNIQUENESS", EntityType: "NODE", LabelsOrTypes: []string{"app__T"}, Properties: []string{"b", "a"}}
	declared := RemoteConstraint{Name: "zzz", Type: "UNIQUENESS", EntityType: "NODE", LabelsOrTypes: []string{"app__T"}, Properties: []string{"a", "b"}}
	for _, rows := range [][]RemoteConstraint{{reordered, declared}, {declared, reordered}} {
		got := diff(cypher.Config{}, desired, nil, rows, nil, ownedSet("app__T")).Constraints
		if len(got.Match) != 1 || got.Match[0].Actual.Name != "zzz" || len(got.Drop) != 1 || got.Drop[0].Name != "aaa" {
			t.Errorf("rows %v: Match = %+v, Drop = %+v; want zzz matched and aaa dropped", rows, got.Match, got.Drop)
		}
	}
}

// TestIndexDiff_RowOrderDoesNotChangeThePlan diffs every ordering of the
// remote rows: two indexes of one definition under names the declaration does
// not carry, beside rows that block another declaration.
func TestIndexDiff_RowOrderDoesNotChangeThePlan(t *testing.T) {
	t.Parallel()
	desired := []cypher.Index{
		{Name: "app__T_x_idx", Kind: cypher.IndexRange, Label: "app__T", Properties: []string{"x"}},
		{Name: "app__T_y_idx", Kind: cypher.IndexRange, Label: "app__T", Properties: []string{"y"}},
	}
	row := func(name string, props ...string) RemoteIndex {
		return RemoteIndex{Name: name, Type: "RANGE", EntityType: "NODE", State: "ONLINE", LabelsOrTypes: []string{"app__T"}, Properties: props}
	}
	actual := []RemoteIndex{row("aaa", "x"), row("zzz", "x"), row("app__T_y_idx", "q"), row("other", "y")}
	constraints := []RemoteConstraint{
		{Name: "c1", Type: "NODE_PROPERTY_EXISTENCE", EntityType: "NODE", LabelsOrTypes: []string{"app__T"}, Properties: []string{"y"}},
		{Name: "c2", Type: "NODE_PROPERTY_EXISTENCE", EntityType: "NODE", LabelsOrTypes: []string{"app__T"}, Properties: []string{"z"}},
	}
	want := diff(cypher.Config{}, nil, desired, constraints, actual, ownedSet("app__T")).Indexes
	for _, rows := range permutations(actual) {
		for _, cs := range permutations(constraints) {
			if got := diff(cypher.Config{}, nil, desired, cs, rows, ownedSet("app__T")).Indexes; !reflect.DeepEqual(got, want) {
				t.Fatalf("rows %v, constraints %v:\ngot  %+v\nwant %+v", rows, cs, got, want)
			}
		}
	}
}

// TestDiff_RowOrderDoesNotChangeTheJointPlan diffs every ordering of both
// sides' rows where each side's drops reach the other side's declarations.
func TestDiff_RowOrderDoesNotChangeTheJointPlan(t *testing.T) {
	t.Parallel()
	desiredConstraints := []cypher.Constraint{
		{Name: "app__T_title_not_null", Kind: cypher.ConstraintNotNull, Label: "app__T", Properties: []string{"title"}},
		{Name: "app__T_id_unique", Kind: cypher.ConstraintUnique, Label: "app__T", Properties: []string{"id"}},
	}
	desiredIndexes := []cypher.Index{
		{Name: "app__T_title_idx", Kind: cypher.IndexRange, Label: "app__T", Properties: []string{"title"}},
		{Name: "app__T_body_idx", Kind: cypher.IndexRange, Label: "app__T", Properties: []string{"body"}},
	}
	constraints := []RemoteConstraint{
		{Name: "app__T_body_idx", Type: "UNIQUENESS", EntityType: "NODE", LabelsOrTypes: []string{"app__T"}, Properties: []string{"id"}},
		{Name: "u_body", Type: "UNIQUENESS", EntityType: "NODE", LabelsOrTypes: []string{"app__T"}, Properties: []string{"body"}},
	}
	indexes := []RemoteIndex{
		{Name: "app__T_title_not_null", Type: "RANGE", EntityType: "NODE", State: "ONLINE", LabelsOrTypes: []string{"app__T"}, Properties: []string{"title"}},
		{Name: "app__T_body_idx", Type: "RANGE", EntityType: "NODE", State: "ONLINE", LabelsOrTypes: []string{"app__T"}, Properties: []string{"id"}, OwningConstraint: "app__T_body_idx"},
		{Name: "u_body", Type: "RANGE", EntityType: "NODE", State: "ONLINE", LabelsOrTypes: []string{"app__T"}, Properties: []string{"body"}, OwningConstraint: "u_body"},
	}
	want := diff(cypher.Config{}, desiredConstraints, desiredIndexes, constraints, indexes, ownedSet("app__T"))
	if len(want.Constraints.Drift) != 2 || len(want.Indexes.Drift) != 2 || len(want.Indexes.Create) != 0 {
		t.Fatalf("plan = %+v; want both constraints and both indexes drifted", want)
	}
	for _, cs := range permutations(constraints) {
		for _, idx := range permutations(indexes) {
			if got := diff(cypher.Config{}, desiredConstraints, desiredIndexes, cs, idx, ownedSet("app__T")); !reflect.DeepEqual(got, want) {
				t.Fatalf("constraints %v, indexes %v:\ngot  %+v\nwant %+v", cs, idx, got, want)
			}
		}
	}
}

// TestDiff_LeavesTheCallersRowsInTheirOrder sorts its own copies of the rows.
func TestDiff_LeavesTheCallersRowsInTheirOrder(t *testing.T) {
	t.Parallel()
	constraints := []RemoteConstraint{
		{Name: "z", Type: "UNIQUENESS", EntityType: "NODE", LabelsOrTypes: []string{"app__T"}, Properties: []string{"q"}},
		{Name: "a", Type: "UNIQUENESS", EntityType: "NODE", LabelsOrTypes: []string{"app__T"}, Properties: []string{"p"}},
	}
	indexes := []RemoteIndex{
		{Name: "z", Type: "RANGE", EntityType: "NODE", LabelsOrTypes: []string{"app__T"}, Properties: []string{"q"}},
		{Name: "a", Type: "RANGE", EntityType: "NODE", LabelsOrTypes: []string{"app__T"}, Properties: []string{"p"}},
	}
	diff(cypher.Config{}, nil, nil, constraints, indexes, ownedSet("app__T"))
	if constraints[0].Name != "z" || indexes[0].Name != "z" {
		t.Errorf("Diff reordered the caller's rows: constraints start %q, indexes start %q", constraints[0].Name, indexes[0].Name)
	}
}

// TestDiff_TheBackingIndexThatServesIsTheSameInEitherOrder gives one definition
// two backing indexes, each named for the constraint it backs.
func TestDiff_TheBackingIndexThatServesIsTheSameInEitherOrder(t *testing.T) {
	t.Parallel()
	first := RemoteIndex{Name: "c1", Type: "RANGE", EntityType: "NODE", State: "ONLINE", LabelsOrTypes: []string{"app__T"}, Properties: []string{"q"}, OwningConstraint: "c1"}
	second := first
	second.Name, second.OwningConstraint = "c2", "c2"
	declared := []cypher.Index{{Name: "i", Kind: cypher.IndexRange, Label: "app__T", Properties: []string{"q"}}}
	for _, rows := range [][]RemoteIndex{{first, second}, {second, first}} {
		plan := diff(cypher.Config{}, nil, declared, nil, rows, ownedSet("app__T")).Indexes
		if len(plan.Match) != 1 || plan.Match[0].Actual.OwningConstraint != "c1" {
			t.Errorf("rows %q, %q: Match = %+v; want the index backing c1", rows[0].OwningConstraint, rows[1].OwningConstraint, plan.Match)
		}
	}
}
