package neo4j

import (
	"math"
	mathrand "math/rand"
	randv2 "math/rand/v2"
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

// TestDiff_RowsDifferingInOneFieldKeepThePlan gives two rows that differ in one
// compared field, in either order; neither order may change the plan.
func TestDiff_RowsDifferingInOneFieldKeepThePlan(t *testing.T) {
	t.Parallel()
	owned := ownedSet("app__T", "app__U")
	baseC := RemoteConstraint{Name: "x", Type: "UNIQUENESS", EntityType: "NODE", LabelsOrTypes: []string{"app__T"}, Properties: []string{"q"}}
	for name, mod := range map[string]func(*RemoteConstraint){
		"Type":            func(r *RemoteConstraint) { r.Type = "NODE_PROPERTY_UNIQUENESS" },
		"EntityType":      func(r *RemoteConstraint) { r.EntityType = "node" },
		"LabelsOrTypes":   func(r *RemoteConstraint) { r.LabelsOrTypes = []string{"app__T", "app__U"} },
		"Properties":      func(r *RemoteConstraint) { r.Properties = []string{"r"} },
		"PropertyType":    func(r *RemoteConstraint) { r.PropertyType = "STRING" },
		"CreateStatement": func(r *RemoteConstraint) { r.CreateStatement = "CREATE CONSTRAINT x" },
	} {
		other := baseC
		mod(&other)
		one := diff(cypher.Config{}, nil, nil, []RemoteConstraint{baseC, other}, nil, owned)
		two := diff(cypher.Config{}, nil, nil, []RemoteConstraint{other, baseC}, nil, owned)
		if !reflect.DeepEqual(one, two) {
			t.Errorf("constraint %s: row order changes the plan:\n%+v\n%+v", name, one, two)
		}
	}
	baseI := RemoteIndex{Name: "x", Type: "RANGE", EntityType: "NODE", State: "ONLINE", LabelsOrTypes: []string{"app__T"}, Properties: []string{"q"}}
	declared := []cypher.Index{{Name: "x", Kind: cypher.IndexRange, Label: "app__U", Properties: []string{"id"}}}
	for name, mod := range map[string]func(*RemoteIndex){
		"Type":             func(r *RemoteIndex) { r.Type = "range" },
		"EntityType":       func(r *RemoteIndex) { r.EntityType = "node" },
		"LabelsOrTypes":    func(r *RemoteIndex) { r.LabelsOrTypes = []string{"app__T", "app__U"} },
		"Properties":       func(r *RemoteIndex) { r.Properties = []string{"r"} },
		"State":            func(r *RemoteIndex) { r.State = "FAILED" },
		"OwningConstraint": func(r *RemoteIndex) { r.OwningConstraint = "c" },
		"Options":          func(r *RemoteIndex) { r.Options = map[string]any{"k": "v"} },
		"empty Options":    func(r *RemoteIndex) { r.Options = map[string]any{} },
	} {
		other := baseI
		mod(&other)
		one := diff(cypher.Config{}, nil, declared, nil, []RemoteIndex{baseI, other}, owned)
		two := diff(cypher.Config{}, nil, declared, nil, []RemoteIndex{other, baseI}, owned)
		if !reflect.DeepEqual(one, two) {
			t.Errorf("index %s: row order changes the plan:\n%+v\n%+v", name, one, two)
		}
	}
}

// TestCompareRemoteIndexes_OrdersEveryOptionValue tells apart option values that
// print alike, so two rows tie only when they are equal.
func TestCompareRemoteIndexes_OrdersEveryOptionValue(t *testing.T) {
	t.Parallel()
	type A int
	type B int
	// Two types declared in different scopes print one name and one package.
	intT := func() any { type T int; return T(1) }()
	stringT := func() any { type T string; return T("1") }()
	for _, tc := range []struct {
		name string
		x, y map[string]any
	}{
		{"a nil map and an empty one", nil, map[string]any{}},
		{"an integer and a float of one value", map[string]any{"d": int64(3)}, map[string]any{"d": float64(3)}},
		{"nested maps", map[string]any{"c": map[string]any{"d": int64(3)}}, map[string]any{"c": map[string]any{"d": float64(3)}}},
		{"lists", map[string]any{"l": []any{int64(1)}}, map[string]any{"l": []any{int64(1), int64(2)}}},
		{"another key", map[string]any{"a": "v"}, map[string]any{"b": "v"}},
		{"string slices that print alike", map[string]any{"k": []string{"a b"}}, map[string]any{"k": []string{"a", "b"}}},
		{"a nil list and an empty one", map[string]any{"l": []any(nil)}, map[string]any{"l": []any{}}},
		{"negative and positive zero", map[string]any{"f": math.Copysign(0, -1)}, map[string]any{"f": 0.0}},
		{"a NaN and a number", map[string]any{"f": math.NaN()}, map[string]any{"f": 1.0}},
		{"typed maps", map[string]any{"m": map[string]int{"a": 1}}, map[string]any{"m": map[string]int{"a": 2}}},
		{"pointers", map[string]any{"p": new(int)}, map[string]any{"p": (*int)(nil)}},
		{"structs", map[string]any{"s": struct{ A, B int }{1, 2}}, map[string]any{"s": struct{ A, B int }{1, 3}}},
		{"booleans", map[string]any{"b": false}, map[string]any{"b": true}},
		{"a nil value and a number", map[string]any{"v": nil}, map[string]any{"v": int64(1)}},
		{"unsigned integers", map[string]any{"u": uint8(1)}, map[string]any{"u": uint8(2)}},
		{"complex numbers", map[string]any{"c": complex(1, 0)}, map[string]any{"c": complex(1, 1)}},
		{"arrays", map[string]any{"a": [2]int{1, 2}}, map[string]any{"a": [2]int{1, 3}}},
		{"a list holding two types", map[string]any{"l": []any{"1"}}, map[string]any{"l": []any{int64(1)}}},
		{"typed map values", map[string]any{"m": map[int]string{1: "a"}}, map[string]any{"m": map[int]string{1: "b"}}},
		{"two types of one kind and value", map[string]any{"t": A(1)}, map[string]any{"t": B(1)}},
		{"two types of one name and package", map[string]any{"t": intT}, map[string]any{"t": stringT}},
		{"two types of one name in two packages", map[string]any{"t": mathrand.Zipf{}}, map[string]any{"t": randv2.Zipf{}}},
	} {
		x, y := RemoteIndex{Name: "r", Options: tc.x}, RemoteIndex{Name: "r", Options: tc.y}
		if c := compareRemoteIndexes(x, y); c == 0 || c != -compareRemoteIndexes(y, x) {
			t.Errorf("%s: compare = %d and %d, want opposite non-zero orders", tc.name, c, compareRemoteIndexes(y, x))
		}
	}
	nanKeys := func(a, b int) map[float64]int {
		return map[float64]int{math.NaN(): a, math.NaN(): b}
	}
	x, y := RemoteIndex{Options: map[string]any{"m": nanKeys(1, 2)}}, RemoteIndex{Options: map[string]any{"m": nanKeys(1, 3)}}
	if c := compareRemoteIndexes(x, y); c == 0 || c != -compareRemoteIndexes(y, x) {
		t.Errorf("maps with two NaN keys: compare = %d and %d, want opposite non-zero orders", c, compareRemoteIndexes(y, x))
	}
	same := map[string]any{"c": map[string]any{"d": int64(3)}, "l": []any{"a"}}
	if c := compareRemoteIndexes(RemoteIndex{Options: same}, RemoteIndex{Options: map[string]any{"l": []any{"a"}, "c": map[string]any{"d": int64(3)}}}); c != 0 {
		t.Errorf("equal options compare %d, want 0", c)
	}
}

// TestDiff_TheBackingIndexThatServesIsTheSameInEitherOrder gives one definition
// two backing indexes that differ only in the constraint they back.
func TestDiff_TheBackingIndexThatServesIsTheSameInEitherOrder(t *testing.T) {
	t.Parallel()
	first := RemoteIndex{Name: "x", Type: "RANGE", EntityType: "NODE", State: "ONLINE", LabelsOrTypes: []string{"app__T"}, Properties: []string{"q"}, OwningConstraint: "c1"}
	second := first
	second.OwningConstraint = "c2"
	declared := []cypher.Index{{Name: "i", Kind: cypher.IndexRange, Label: "app__T", Properties: []string{"q"}}}
	for _, rows := range [][]RemoteIndex{{first, second}, {second, first}} {
		plan := diff(cypher.Config{}, nil, declared, nil, rows, ownedSet("app__T")).Indexes
		if len(plan.Match) != 1 || plan.Match[0].Actual.OwningConstraint != "c1" {
			t.Errorf("rows %q, %q: Match = %+v; want the index backing c1", rows[0].OwningConstraint, rows[1].OwningConstraint, plan.Match)
		}
	}
}

// TestDiff_ARowPassedTwiceIsNeverKeptAndDropped reads two identical rows as
// one object: a plan that kept one and dropped the other would drop what it
// kept.
func TestDiff_ARowPassedTwiceIsNeverKeptAndDropped(t *testing.T) {
	t.Parallel()
	owned := ownedSet("app__T")
	ri := RemoteIndex{Name: "x", Type: "RANGE", EntityType: "NODE", State: "ONLINE", LabelsOrTypes: []string{"app__T"}, Properties: []string{"q"}}
	indexes := diff(cypher.Config{}, nil, []cypher.Index{{Name: "x", Kind: cypher.IndexRange, Label: "app__T", Properties: []string{"q"}}}, nil, []RemoteIndex{ri, ri}, owned).Indexes
	for _, m := range indexes.Match {
		for _, d := range indexes.Drop {
			if d.Name == m.Actual.Name {
				t.Errorf("index %q is both a Match's Actual and a Drop", d.Name)
			}
		}
	}
	rc := RemoteConstraint{Name: "u", Type: "UNIQUENESS", EntityType: "NODE", LabelsOrTypes: []string{"app__T"}, Properties: []string{"id"}}
	constraints := diff(cypher.Config{}, []cypher.Constraint{{Name: "u", Kind: cypher.ConstraintUnique, Label: "app__T", Properties: []string{"id"}}}, nil,
		[]RemoteConstraint{rc, rc}, nil, owned).Constraints
	for _, m := range constraints.Match {
		for _, d := range constraints.Drop {
			if d.Name == m.Actual.Name {
				t.Errorf("constraint %q is both a Match's Actual and a Drop", d.Name)
			}
		}
	}
}
