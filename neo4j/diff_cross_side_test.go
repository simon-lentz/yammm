package neo4j

import (
	"slices"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/schema/cypher"
)

const crossLabel = "px__Doc"

func crossConstraint(name, typ string, props ...string) RemoteConstraint {
	return RemoteConstraint{Name: name, Type: typ, EntityType: "NODE", LabelsOrTypes: []string{crossLabel}, Properties: props}
}

func crossIndex(name, owner, state string, props ...string) RemoteIndex {
	return RemoteIndex{Name: name, Type: "RANGE", EntityType: "NODE", State: state, LabelsOrTypes: []string{crossLabel}, Properties: props, OwningConstraint: owner}
}

func crossDeclaredIndex(name string, props ...string) cypher.Index {
	return cypher.Index{Name: name, Kind: cypher.IndexRange, Label: crossLabel, Properties: props}
}

func crossDeclaredConstraint(name string, kind cypher.ConstraintKind, props ...string) cypher.Constraint {
	return cypher.Constraint{Name: name, Kind: kind, Label: crossLabel, Properties: props}
}

// TestDiff_AConstraintDropServingAnIndexLeavesItACreate creates a declared index
// whose only server is the backing index of a constraint the plan drops: the
// create is a no-op while the constraint stands and realises the index once a
// Drop removes it.
func TestDiff_AConstraintDropServingAnIndexLeavesItACreate(t *testing.T) {
	t.Parallel()
	plan := diff(cypher.Config{}, nil, []cypher.Index{crossDeclaredIndex("px__Doc_body_idx", "body")},
		[]RemoteConstraint{crossConstraint("u_body", "UNIQUENESS", "body")},
		[]RemoteIndex{crossIndex("u_body", "u_body", "ONLINE", "body")}, ownedSet(crossLabel))
	if names := constraintNames(plan.Constraints.Drop); !slices.Equal(names, []string{"u_body"}) {
		t.Errorf("Constraints.Drop = %q, want u_body", names)
	}
	if len(plan.Indexes.Match) != 0 || len(plan.Indexes.Create) != 1 || plan.Indexes.Create[0].Name != "px__Doc_body_idx" {
		t.Errorf("Indexes.Match = %+v, Create = %+v; want body_idx created", plan.Indexes.Match, plan.Indexes.Create)
	}
}

// TestDiff_AConstraintDriftsBlockerServingAnIndexLeavesItACreate creates a
// declared index whose server is the backing index of a constraint another
// declaration's drift drops.
func TestDiff_AConstraintDriftsBlockerServingAnIndexLeavesItACreate(t *testing.T) {
	t.Parallel()
	z := crossDeclaredConstraint("px__Doc_z_type", cypher.ConstraintType, "z")
	z.TypeExpr = "INTEGER"
	plan := diff(cypher.Config{}, []cypher.Constraint{z}, []cypher.Index{crossDeclaredIndex("px__Doc_body_idx", "body")},
		[]RemoteConstraint{crossConstraint("px__Doc_z_type", "UNIQUENESS", "body")},
		[]RemoteIndex{crossIndex("px__Doc_z_type", "px__Doc_z_type", "ONLINE", "body")}, ownedSet(crossLabel))
	if len(plan.Constraints.Drift) != 1 || !slices.Equal(blockerNames(plan.Constraints.Drift[0].Blockers), []string{"c:px__Doc_z_type"}) {
		t.Fatalf("Constraints.Drift = %+v, want z_type blocked by its name holder", plan.Constraints.Drift)
	}
	if len(plan.Indexes.Match) != 0 || len(plan.Indexes.Create) != 1 {
		t.Errorf("Indexes.Match = %+v, Create = %+v; want body_idx created", plan.Indexes.Match, plan.Indexes.Create)
	}
}

// TestDiff_AnIndexRealisationAConstraintCreateNeedsDroppedIsDrift moves a
// matched index to Drift when its row holds a declared constraint's name.
func TestDiff_AnIndexRealisationAConstraintCreateNeedsDroppedIsDrift(t *testing.T) {
	t.Parallel()
	plan := diff(cypher.Config{},
		[]cypher.Constraint{crossDeclaredConstraint("px__Doc_title_not_null", cypher.ConstraintNotNull, "title")},
		[]cypher.Index{crossDeclaredIndex("px__Doc_title_idx", "title")},
		nil, []RemoteIndex{crossIndex("px__Doc_title_not_null", "", "ONLINE", "title")}, ownedSet(crossLabel))
	if len(plan.Constraints.Drift) != 1 || !slices.Equal(blockerNames(plan.Constraints.Drift[0].Blockers), []string{"i:px__Doc_title_not_null"}) {
		t.Fatalf("Constraints.Drift = %+v, want title_not_null blocked by the index", plan.Constraints.Drift)
	}
	if len(plan.Indexes.Match) != 0 || len(plan.Indexes.Drift) != 1 {
		t.Fatalf("Indexes.Match = %+v, Drift = %+v; want title_idx drifted", plan.Indexes.Match, plan.Indexes.Drift)
	}
	d := plan.Indexes.Drift[0]
	const reason = `index "px__Doc_title_not_null" realises this declaration and the plan drops it; this index is created again under its own name`
	if !slices.Equal(blockerNames(d.Blockers), []string{"i:px__Doc_title_not_null"}) || d.Reason != reason {
		t.Errorf("Indexes.Drift[0] = %+v; want its realisation named, and why", d)
	}
}

// TestDiff_AConstraintRealisationAnIndexCreateNeedsDroppedIsDrift moves a
// matched constraint to Drift when it holds a declared index's name, whether it
// has a backing index or not, and names a constraint and its backing index as
// one blocker.
func TestDiff_AConstraintRealisationAnIndexCreateNeedsDroppedIsDrift(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		desired cypher.Constraint
		holder  RemoteConstraint
		backing []RemoteIndex
		owner   string
	}{
		{
			name:    "a NOT NULL",
			desired: crossDeclaredConstraint("px__Doc_title_not_null", cypher.ConstraintNotNull, "title"),
			holder:  crossConstraint("px__Doc_body_idx", "NODE_PROPERTY_EXISTENCE", "title"),
		},
		{
			name:    "a UNIQUE and its backing index",
			desired: crossDeclaredConstraint("px__Doc_id_unique", cypher.ConstraintUnique, "id"),
			holder:  crossConstraint("px__Doc_body_idx", "UNIQUENESS", "id"),
			backing: []RemoteIndex{crossIndex("px__Doc_body_idx", "px__Doc_body_idx", "ONLINE", "id")},
			owner:   "px__Doc_body_idx",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			plan := diff(cypher.Config{}, []cypher.Constraint{tc.desired}, []cypher.Index{crossDeclaredIndex("px__Doc_body_idx", "body")},
				[]RemoteConstraint{tc.holder}, tc.backing, ownedSet(crossLabel))
			if len(plan.Indexes.Drift) != 1 {
				t.Fatalf("Indexes.Drift = %+v, want body_idx blocked", plan.Indexes.Drift)
			}
			want := []string{"c:px__Doc_body_idx"}
			if tc.owner != "" {
				want = []string{"i:px__Doc_body_idx"}
			}
			b := plan.Indexes.Drift[0].Blockers
			if !slices.Equal(blockerNames(b), want) || (tc.owner != "" && b.Indexes[0].OwningConstraint != tc.owner) {
				t.Errorf("Indexes.Drift[0].Blockers = %+v, want the holder once, as %q with OwningConstraint %q", b, want, tc.owner)
			}
			if len(plan.Constraints.Match) != 0 || len(plan.Constraints.Drift) != 1 || !slices.Equal(blockerNames(plan.Constraints.Drift[0].Blockers), []string{"c:px__Doc_body_idx"}) {
				t.Errorf("Constraints.Match = %+v, Drift = %+v; want the declaration drifted on its realisation", plan.Constraints.Match, plan.Constraints.Drift)
			}
		})
	}
}

// TestDiff_AFailedBackingIndexRecreatesItsConstraint drifts both a declared
// index served by a FAILED backing index and the constraint that index backs:
// dropping the index means dropping the constraint, which must be created again.
func TestDiff_AFailedBackingIndexRecreatesItsConstraint(t *testing.T) {
	t.Parallel()
	plan := diff(cypher.Config{},
		[]cypher.Constraint{crossDeclaredConstraint("px__Doc_id_unique", cypher.ConstraintUnique, "id")},
		[]cypher.Index{crossDeclaredIndex("px__Doc_id_idx", "id")},
		[]RemoteConstraint{crossConstraint("px__Doc_id_unique", "UNIQUENESS", "id")},
		[]RemoteIndex{crossIndex("px__Doc_id_unique", "px__Doc_id_unique", "FAILED", "id")}, ownedSet(crossLabel))
	if len(plan.Indexes.Drift) != 1 || len(plan.Indexes.Drift[0].Blockers.Indexes) != 1 || plan.Indexes.Drift[0].Blockers.Indexes[0].OwningConstraint != "px__Doc_id_unique" {
		t.Fatalf("Indexes.Drift = %+v, want the index drifted on the FAILED backing index", plan.Indexes.Drift)
	}
	if len(plan.Constraints.Match) != 0 || len(plan.Constraints.Drift) != 1 {
		t.Fatalf("Constraints.Match = %+v, Drift = %+v; want the UNIQUE drifted", plan.Constraints.Match, plan.Constraints.Drift)
	}
	if d := plan.Constraints.Drift[0]; !slices.Equal(blockerNames(d.Blockers), []string{"c:px__Doc_id_unique"}) || !strings.Contains(d.Reason, "created again under its own name") {
		t.Errorf("Constraints.Drift[0] = %+v; want its own realisation named, and that it is created again", d)
	}
}

// TestDiff_ARowEitherSideNamesIsNeverADrop holds an owned index out of the
// index side's Drop when a constraint drift names it: dropped twice, the late
// drop would remove what the plan created under that name.
func TestDiff_ARowEitherSideNamesIsNeverADrop(t *testing.T) {
	t.Parallel()
	plan := diff(cypher.Config{},
		[]cypher.Constraint{crossDeclaredConstraint("px__Doc_id_unique", cypher.ConstraintUnique, "id")}, nil,
		[]RemoteConstraint{crossConstraint("nk", "NODE_KEY", "id")},
		[]RemoteIndex{crossIndex("px__Doc_id_unique", "", "ONLINE", "z"), crossIndex("nk", "nk", "ONLINE", "id")}, ownedSet(crossLabel))
	if len(plan.Constraints.Drift) != 1 || !slices.Equal(blockerNames(plan.Constraints.Drift[0].Blockers), []string{"c:nk", "i:px__Doc_id_unique"}) {
		t.Fatalf("Constraints.Drift = %+v, want id_unique blocked by the index and the NODE KEY", plan.Constraints.Drift)
	}
	if len(plan.Indexes.Drop) != 0 || len(plan.Constraints.Drop) != 0 {
		t.Errorf("Indexes.Drop = %+v, Constraints.Drop = %+v; a blocker is never a Drop", plan.Indexes.Drop, plan.Constraints.Drop)
	}
	if plan.Indexes.Excluded != 0 {
		t.Errorf("Indexes.Excluded = %d; the blocker and the blocked constraint's backing index are both named", plan.Indexes.Excluded)
	}
}

// TestDiff_AConstraintAndItsBackingIndexAreOneBlocker names a UNIQUE holding a
// declaration's name once, on either side, beside the backing index row that
// carries the same name.
func TestDiff_AConstraintAndItsBackingIndexAreOneBlocker(t *testing.T) {
	t.Parallel()
	holder := RemoteConstraint{Name: "k", Type: "UNIQUENESS", EntityType: "NODE", LabelsOrTypes: []string{"other__T"}, Properties: []string{"z"}}
	backing := RemoteIndex{Name: "k", Type: "RANGE", EntityType: "NODE", State: "ONLINE", LabelsOrTypes: []string{"other__T"}, Properties: []string{"z"}, OwningConstraint: "k"}

	c := diff(cypher.Config{}, []cypher.Constraint{{Name: "k", Kind: cypher.ConstraintUnique, Label: crossLabel, Properties: []string{"id"}}}, nil,
		[]RemoteConstraint{holder}, []RemoteIndex{backing}, ownedSet(crossLabel)).Constraints
	if len(c.Drift) != 1 || !slices.Equal(blockerNames(c.Drift[0].Blockers), []string{"c:k"}) || c.Drift[0].Blockers.Constraints[0].Type != "UNIQUENESS" {
		t.Errorf("Constraints.Drift = %+v; want the constraint k alone", c.Drift)
	}
	x := diff(cypher.Config{}, nil, []cypher.Index{crossDeclaredIndex("k", "id")},
		[]RemoteConstraint{holder}, []RemoteIndex{backing}, ownedSet(crossLabel)).Indexes
	if len(x.Drift) != 1 || !slices.Equal(blockerNames(x.Drift[0].Blockers), []string{"i:k"}) || x.Drift[0].Blockers.Indexes[0].OwningConstraint != "k" {
		t.Errorf("Indexes.Drift = %+v; want the backing index alone, naming its constraint", x.Drift)
	}
}

func constraintNames(rcs []RemoteConstraint) []string {
	names := make([]string, 0, len(rcs))
	for _, rc := range rcs {
		names = append(names, rc.Name)
	}
	return names
}

// TestDiff_ADroppedConstraintsBackingIndexIsNotExcluded accounts for the
// backing index of a constraint the plan drops through that Drop, and leaves an
// unowned constraint's backing index in Excluded.
func TestDiff_ADroppedConstraintsBackingIndexIsNotExcluded(t *testing.T) {
	t.Parallel()
	plan := diff(cypher.Config{}, nil, nil,
		[]RemoteConstraint{crossConstraint("u_old", "UNIQUENESS", "x")},
		[]RemoteIndex{crossIndex("u_old", "u_old", "ONLINE", "x")}, ownedSet(crossLabel))
	if len(plan.Constraints.Drop) != 1 || plan.Constraints.Excluded != 0 || plan.Indexes.Excluded != 0 {
		t.Errorf("Constraints.Drop = %+v, Excluded = %d, Indexes.Excluded = %d; want u_old dropped and nothing excluded",
			plan.Constraints.Drop, plan.Constraints.Excluded, plan.Indexes.Excluded)
	}
	unowned := diff(cypher.Config{}, nil, nil,
		[]RemoteConstraint{crossConstraint("u_old", "UNIQUENESS", "x")},
		[]RemoteIndex{crossIndex("u_old", "u_old", "ONLINE", "x")}, ownedSet("other__T"))
	if len(unowned.Constraints.Drop) != 0 || unowned.Constraints.Excluded != 1 || unowned.Indexes.Excluded != 1 {
		t.Errorf("unowned: Constraints.Drop = %+v, Excluded = %d, Indexes.Excluded = %d; want no Drop and both rows excluded",
			unowned.Constraints.Drop, unowned.Constraints.Excluded, unowned.Indexes.Excluded)
	}
}
