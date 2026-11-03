package neo4j

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/schema"
)

// TestDiffConstraints_TheOtherKeyKindIsDrift pins the server rule that a UNIQUE
// and a NODE KEY constraint cannot stand over one label and property order: a
// declaration of one kind beside a remote constraint of the other is drift naming
// that constraint, never a Create the server refuses beside a Drop of it.
func TestDiffConstraints_TheOtherKeyKindIsDrift(t *testing.T) {
	t.Parallel()
	load := func(src string) schema.Schema {
		s, res := schema.LoadString(t.Context(), src, "flip.yammm")
		if res.HasErrors() {
			t.Fatal(res)
		}
		return s
	}
	one := load("schema \"flip\"\n\ntype Doc {\n\tid String primary\n}\n")
	two := load("schema \"flip\"\n\ntype Doc {\n\ta String primary\n\tb String primary\n}\n")
	unsorted := load("schema \"flip\"\n\ntype Doc {\n\tb String primary\n\ta String primary\n}\n")
	remote := func(name, typ string, props ...string) RemoteConstraint {
		return RemoteConstraint{Name: name, Type: typ, EntityType: "NODE", LabelsOrTypes: []string{"flip__Doc"}, Properties: props}
	}
	nodeKey := func(named bool) *Adapter { return New(WithNodeKeyConstraints(true), WithNamedConstraints(named)) }
	unique := func(named bool) *Adapter { return New(WithNamedConstraints(named)) }

	for _, tc := range []struct {
		name         string
		adapter      *Adapter
		schema       schema.Schema
		remote       []RemoteConstraint
		wantBlocker  string // the drift's Actual name, or "-" for no drift
		wantInReason string
		wantDrop     []string
		wantExcluded int
	}{
		{
			name: "UNIQUE and NOT NULL to NODE KEY", adapter: nodeKey(true), schema: one,
			remote:      []RemoteConstraint{remote("flip__Doc_id_not_null", "NODE_PROPERTY_EXISTENCE", "id"), remote("flip__Doc_id_unique", "UNIQUENESS", "id")},
			wantBlocker: "flip__Doc_id_unique", wantInReason: `a UNIQUENESS constraint "flip__Doc_id_unique" already holds flip__Doc(id)`,
			wantDrop: []string{"flip__Doc_id_not_null"},
		},
		{
			name: "a NOT NULL alone blocks nothing", adapter: nodeKey(true), schema: one,
			remote:      []RemoteConstraint{remote("flip__Doc_id_not_null", "NODE_PROPERTY_EXISTENCE", "id")},
			wantBlocker: "-", wantDrop: []string{"flip__Doc_id_not_null"},
		},
		{
			name: "a 2026 server's UNIQUE spelling to NODE KEY", adapter: nodeKey(true), schema: one,
			remote:      []RemoteConstraint{remote("flip__Doc_id_unique", "NODE_PROPERTY_UNIQUENESS", "id")},
			wantBlocker: "flip__Doc_id_unique", wantInReason: "NODE_PROPERTY_UNIQUENESS",
		},
		{
			name: "NODE KEY to UNIQUE and NOT NULL", adapter: unique(true), schema: one,
			remote:      []RemoteConstraint{remote("flip__Doc_id_node_key", "NODE_KEY", "id")},
			wantBlocker: "flip__Doc_id_node_key", wantInReason: `a NODE_KEY constraint "flip__Doc_id_node_key"`,
		},
		{
			name: "UNIQUE to NODE KEY, unnamed", adapter: nodeKey(false), schema: one,
			remote:      []RemoteConstraint{remote("constraint_a1", "UNIQUENESS", "id")},
			wantBlocker: "constraint_a1",
		},
		{
			name: "NODE KEY to UNIQUE, unnamed", adapter: unique(false), schema: one,
			remote:      []RemoteConstraint{remote("constraint_a1", "NODE_KEY", "id")},
			wantBlocker: "constraint_a1",
		},
		{
			name: "a remote constraint with no name", adapter: nodeKey(false), schema: one,
			remote:      []RemoteConstraint{remote("", "UNIQUENESS", "id")},
			wantBlocker: "", wantInReason: "an unnamed UNIQUENESS constraint already holds flip__Doc(id)",
		},
		{
			name: "a composite in the same order", adapter: nodeKey(true), schema: two,
			remote:      []RemoteConstraint{remote("c1", "UNIQUENESS", "a", "b")},
			wantBlocker: "c1", wantInReason: "flip__Doc(a, b)",
		},
		{
			name: "a composite in the reversed order, which the server creates beside it", adapter: nodeKey(true), schema: two,
			remote:      []RemoteConstraint{remote("c1", "UNIQUENESS", "b", "a")},
			wantBlocker: "-", wantDrop: []string{"c1"},
		},
		{
			name: "a composite declared out of name order, beside the same order", adapter: nodeKey(true), schema: unsorted,
			remote:      []RemoteConstraint{remote("c1", "UNIQUENESS", "b", "a")},
			wantBlocker: "c1", wantInReason: "flip__Doc(b, a)",
		},
		{
			name: "a composite declared out of name order, beside the sorted order", adapter: nodeKey(true), schema: unsorted,
			remote:      []RemoteConstraint{remote("c1", "UNIQUENESS", "a", "b")},
			wantBlocker: "-", wantDrop: []string{"c1"},
		},
		{
			name: "a lower-case entity type and kind", adapter: nodeKey(true), schema: one,
			remote: []RemoteConstraint{{
				Name: "u", Type: "uniqueness", EntityType: "node",
				LabelsOrTypes: []string{"flip__Doc"}, Properties: []string{"id"},
			}},
			wantBlocker: "u",
		},
		{
			name: "a relationship row with a node constraint's kind", adapter: nodeKey(true), schema: one,
			remote: []RemoteConstraint{{
				Name: "rel_u", Type: "UNIQUENESS", EntityType: "RELATIONSHIP",
				LabelsOrTypes: []string{"flip__Doc"}, Properties: []string{"id"},
			}},
			wantBlocker: "-", wantExcluded: 1,
		},
		{
			name: "a row over two labels, which no server holds", adapter: nodeKey(true), schema: one,
			remote: []RemoteConstraint{{
				Name: "multi", Type: "UNIQUENESS", EntityType: "NODE",
				LabelsOrTypes: []string{"flip__Doc", "X"}, Properties: []string{"id"},
			}},
			wantBlocker: "-", wantDrop: []string{"multi"},
		},
		{
			name: "a row with no label", adapter: nodeKey(true), schema: one,
			remote:      []RemoteConstraint{{Name: "bare", Type: "UNIQUENESS", EntityType: "NODE", Properties: []string{"id"}}},
			wantBlocker: "-", wantExcluded: 1,
		},
		{
			name: "a Community configuration over a remote NODE KEY it cannot declare", adapter: New(WithEdition(Community)), schema: one,
			remote:      []RemoteConstraint{remote("flip__Doc_id_node_key", "NODE_KEY", "id")},
			wantBlocker: "flip__Doc_id_node_key", wantExcluded: 1,
		},
		{
			name: "the same kind is a match", adapter: nodeKey(true), schema: one,
			remote:      []RemoteConstraint{remote("flip__Doc_id_node_key", "NODE_KEY", "id")},
			wantBlocker: "-",
		},
		{
			name: "a relationship constraint spelled like the label", adapter: nodeKey(true), schema: one,
			remote: []RemoteConstraint{{
				Name: "rel_unique", Type: "RELATIONSHIP_UNIQUENESS", EntityType: "RELATIONSHIP",
				LabelsOrTypes: []string{"flip__Doc"}, Properties: []string{"id"},
			}},
			wantBlocker: "-", wantExcluded: 1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			desired, res := tc.adapter.ConstraintsStructured(t.Context(), tc.schema)
			if res.HasErrors() {
				t.Fatal(res)
			}
			key := slices.IndexFunc(desired, func(c Constraint) bool { return constraintHasBackingIndex(c.Kind) })
			if key < 0 {
				t.Fatalf("the emitter declared no UNIQUE or NODE KEY: %+v", desired)
			}
			plan := tc.adapter.DiffConstraints(desired, tc.remote, tc.adapter.OwnedLabels(t.Context(), tc.schema))

			keyCreated := slices.ContainsFunc(plan.Create, func(c Constraint) bool { return c.Name == desired[key].Name && c.Kind == desired[key].Kind })
			if tc.wantBlocker == "-" {
				if len(plan.Drift) != 0 {
					t.Errorf("Drift = %+v, want none", plan.Drift)
				}
			} else {
				if len(plan.Drift) != 1 {
					t.Fatalf("Drift = %+v, want the one key-kind drift", plan.Drift)
				}
				d := plan.Drift[0]
				if d.Actual.Name != tc.wantBlocker || d.Desired.Kind != desired[key].Kind {
					t.Errorf("Drift = desired %v, actual %q; want desired %v, actual %q", d.Desired.Kind, d.Actual.Name, desired[key].Kind, tc.wantBlocker)
				}
				if i := slices.IndexFunc(tc.remote, func(rc RemoteConstraint) bool { return rc.Name == tc.wantBlocker }); !reflect.DeepEqual(d.Actual, tc.remote[i]) {
					t.Errorf("Actual = %+v, want the remote row %+v whole", d.Actual, tc.remote[i])
				}
				if !strings.Contains(d.Reason, tc.wantInReason) || !strings.Contains(d.Reason, "drop it before this constraint can be created") {
					t.Errorf("Reason = %q, want it to hold %q and the action", d.Reason, tc.wantInReason)
				}
				if keyCreated {
					t.Errorf("Create holds %s, which the server refuses beside the blocker", desired[key].Statement)
				}
				const existence = "requires every node to hold each key property"
				if got, want := strings.Contains(d.Reason, existence), desired[key].Kind == ConstraintNodeKey && canonicalRemoteConstraintType(d.Actual.Type) == "UNIQUENESS"; got != want {
					t.Errorf("Reason = %q; states the NODE KEY's existence rule: %v, want %v", d.Reason, got, want)
				}
			}
			var drops []string
			for _, d := range plan.Drop {
				drops = append(drops, d.Name)
			}
			if !slices.Equal(drops, tc.wantDrop) {
				t.Errorf("Drop = %q, want %q", drops, tc.wantDrop)
			}
			if plan.Excluded != tc.wantExcluded {
				t.Errorf("Excluded = %d, want %d", plan.Excluded, tc.wantExcluded)
			}
		})
	}
}

// TestDiffConstraints_AKeyKindHolderBlocksEveryDeclarationOfTheOtherKind reports
// each declaration the server refuses beside one holder as drift naming it, and
// the holder as no Drop.
func TestDiffConstraints_AKeyKindHolderBlocksEveryDeclarationOfTheOtherKind(t *testing.T) {
	t.Parallel()
	desired := []Constraint{
		{Name: "a", Kind: ConstraintNodeKey, Label: "probe__Doc", Properties: []string{"id"}},
		{Name: "b", Kind: ConstraintNodeKey, Label: "probe__Doc", Properties: []string{"id"}},
	}
	actual := []RemoteConstraint{{Name: "u", Type: "UNIQUENESS", EntityType: "NODE", LabelsOrTypes: []string{"probe__Doc"}, Properties: []string{"id"}}}
	plan := New().DiffConstraints(desired, actual, ownedSet("probe__Doc"))
	if len(plan.Drift) != 2 || plan.Drift[0].Actual.Name != "u" || plan.Drift[1].Actual.Name != "u" {
		t.Errorf("Drift = %+v, want a and b each blocked by u", plan.Drift)
	}
	if len(plan.Create) != 0 || len(plan.Drop) != 0 {
		t.Errorf("Create = %+v, Drop = %+v; want neither", plan.Create, plan.Drop)
	}
}

// TestDiffConstraints_AnUnownedKeyConstraintCostsWhatAnyUnownedRowCosts holds a
// shared database's other schemas out of the key-kind search: no declaration names
// their labels, so a UNIQUE there costs no more than a NOT NULL.
func TestDiffConstraints_AnUnownedKeyConstraintCostsWhatAnyUnownedRowCosts(t *testing.T) {
	s, res := schema.LoadString(t.Context(), "schema \"app\"\n\ntype Doc {\n\tid String primary\n}\n", "app.yammm")
	if res.HasErrors() {
		t.Fatal(res)
	}
	a := New(WithNodeKeyConstraints(true))
	desired, _ := a.ConstraintsStructured(t.Context(), s)
	owned := a.OwnedLabels(t.Context(), s)
	allocs := func(typ string) float64 {
		remote := make([]RemoteConstraint, 500)
		for i := range remote {
			remote[i] = RemoteConstraint{Name: fmt.Sprintf("c%d", i), Type: typ, EntityType: "NODE", LabelsOrTypes: []string{fmt.Sprintf("other__T%d", i)}, Properties: []string{"id"}}
		}
		return testing.AllocsPerRun(10, func() { a.DiffConstraints(desired, remote, owned) })
	}
	if unique, notNull := allocs("UNIQUENESS"), allocs("NODE_PROPERTY_EXISTENCE"); unique != notNull {
		t.Errorf("500 unowned rows: UNIQUE %v allocations, NOT NULL %v; want equal", unique, notNull)
	}
}

// TestDiffConstraints_AKeyKindHolderPairedByNameStillBlocks names a UNIQUE that
// name pairing gave to another declaration as the NODE KEY's blocker too: the
// server refuses the NODE KEY beside it whichever declaration it is paired with.
func TestDiffConstraints_AKeyKindHolderPairedByNameStillBlocks(t *testing.T) {
	t.Parallel()
	s, res := schema.LoadString(t.Context(), "schema \"flip\"\n\ntype Doc {\n\tid String primary\n\tname String required\n}\n", "flip.yammm")
	if res.HasErrors() {
		t.Fatal(res)
	}
	a := New(WithNodeKeyConstraints(true))
	desired, _ := a.ConstraintsStructured(t.Context(), s)
	u := RemoteConstraint{Name: "flip__Doc_name_not_null", Type: "UNIQUENESS", EntityType: "NODE", LabelsOrTypes: []string{"flip__Doc"}, Properties: []string{"id"}}
	plan := a.DiffConstraints(desired, []RemoteConstraint{u}, a.OwnedLabels(t.Context(), s))
	var kinds []ConstraintKind
	for _, d := range plan.Drift {
		if !reflect.DeepEqual(d.Actual, u) {
			t.Errorf("Drift %s names %+v, want %+v", d.Desired.Statement, d.Actual, u)
		}
		kinds = append(kinds, d.Desired.Kind)
	}
	if !slices.Equal(kinds, []ConstraintKind{ConstraintNodeKey, ConstraintNotNull}) {
		t.Errorf("Drift kinds = %v, want the NODE KEY, then the NOT NULL that takes the blocker's name", kinds)
	}
	if slices.ContainsFunc(plan.Create, func(c Constraint) bool { return c.Kind == ConstraintNodeKey }) || len(plan.Drop) != 0 {
		t.Errorf("Create = %+v, Drop = %+v; want no NODE KEY Create and no Drop", plan.Create, plan.Drop)
	}
}

// TestDiffConstraints_ACleanPlanHasNilDrift keeps Drift nil when nothing
// drifts, as the index side's is.
func TestDiffConstraints_ACleanPlanHasNilDrift(t *testing.T) {
	t.Parallel()
	plan := New().DiffConstraints(nil, nil, ownedSet("probe__Doc"))
	if plan.Drift != nil {
		t.Errorf("Drift = %#v, want nil", plan.Drift)
	}
}

// TestDiffConstraints_TheKeyKindRuleReadsTheLabelNotOwnership names a UNIQUE on a
// declaration's own label as the NODE KEY's blocker under an owned set that does
// not hold the label, as the name and RANGE-index blockers do.
func TestDiffConstraints_TheKeyKindRuleReadsTheLabelNotOwnership(t *testing.T) {
	t.Parallel()
	desired := []Constraint{{Name: "k", Kind: ConstraintNodeKey, Label: "probe__Doc", Properties: []string{"id"}}}
	u := RemoteConstraint{Name: "u", Type: "UNIQUENESS", EntityType: "NODE", LabelsOrTypes: []string{"probe__Doc"}, Properties: []string{"id"}}
	plan := New().DiffConstraints(desired, []RemoteConstraint{u}, ownedSet("probe__Other"))
	if len(plan.Drift) != 1 || plan.Drift[0].Actual.Name != "u" || len(plan.Create) != 0 || plan.Excluded != 1 {
		t.Errorf("Drift = %+v, Create = %+v, Excluded = %d; want u's drift, no Create, u excluded", plan.Drift, plan.Create, plan.Excluded)
	}
	same := u
	same.Type = "NODE_KEY"
	if plan := New().DiffConstraints(desired, []RemoteConstraint{same}, ownedSet("probe__Other")); len(plan.Drift) != 0 {
		t.Errorf("Drift = %+v beside a NODE KEY of the declaration's own kind, want none", plan.Drift)
	}
}

// TestDiffConstraints_OfTwoHoldersTheSecondIsStillADrop accounts for every remote
// row: the first holder over a key is the drift's Actual and the second, which no
// server holds beside it, stays a Drop rather than leaving the plan.
func TestDiffConstraints_OfTwoHoldersTheSecondIsStillADrop(t *testing.T) {
	t.Parallel()
	desired := []Constraint{{Name: "k", Kind: ConstraintNodeKey, Label: "probe__Doc", Properties: []string{"id"}}}
	row := func(name string) RemoteConstraint {
		return RemoteConstraint{Name: name, Type: "UNIQUENESS", EntityType: "NODE", LabelsOrTypes: []string{"probe__Doc"}, Properties: []string{"id"}}
	}
	plan := New().DiffConstraints(desired, []RemoteConstraint{row("u"), row("v")}, ownedSet("probe__Doc"))
	if len(plan.Drift) != 1 || plan.Drift[0].Actual.Name != "u" || len(plan.Drop) != 1 || plan.Drop[0].Name != "v" {
		t.Errorf("Drift = %+v, Drop = %+v; want u's drift and v dropped", plan.Drift, plan.Drop)
	}
}

// TestDiffConstraints_ANameOnlyPairStatesTheNodeKeyExistenceRule gives a drift
// the existence clause when its drop removes a UNIQUE on the NODE KEY's label over
// members within its key, in any order, and no other drift.
func TestDiffConstraints_ANameOnlyPairStatesTheNodeKeyExistenceRule(t *testing.T) {
	t.Parallel()
	const existence = "requires every node to hold each key property"
	for _, tc := range []struct {
		name   string
		remote RemoteConstraint
		want   bool
	}{
		{"a UNIQUE over the key", RemoteConstraint{Name: "k", Type: "UNIQUENESS", EntityType: "NODE", LabelsOrTypes: []string{"probe__Doc"}, Properties: []string{"id"}}, true},
		{"a UNIQUE over another key", RemoteConstraint{Name: "k", Type: "UNIQUENESS", EntityType: "NODE", LabelsOrTypes: []string{"probe__Doc"}, Properties: []string{"z"}}, false},
		{"a NOT NULL over the key", RemoteConstraint{Name: "k", Type: "NODE_PROPERTY_EXISTENCE", EntityType: "NODE", LabelsOrTypes: []string{"probe__Doc"}, Properties: []string{"id"}}, false},
		{"a UNIQUE over the key reversed", RemoteConstraint{Name: "k", Type: "UNIQUENESS", EntityType: "NODE", LabelsOrTypes: []string{"probe__Doc"}, Properties: []string{"b", "id"}}, true},
		{"a UNIQUE over part of the key", RemoteConstraint{Name: "k", Type: "UNIQUENESS", EntityType: "NODE", LabelsOrTypes: []string{"probe__Doc"}, Properties: []string{"b"}}, true},
		{"a UNIQUE over more than the key", RemoteConstraint{Name: "k", Type: "UNIQUENESS", EntityType: "NODE", LabelsOrTypes: []string{"probe__Doc"}, Properties: []string{"id", "b", "z"}}, false},
		{"a UNIQUE on another label", RemoteConstraint{Name: "k", Type: "UNIQUENESS", EntityType: "NODE", LabelsOrTypes: []string{"probe__Other"}, Properties: []string{"id", "b"}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			desired := []Constraint{{Name: "k", Kind: ConstraintNodeKey, Label: "probe__Doc", Properties: []string{"id", "b"}}}
			plan := New().DiffConstraints(desired, []RemoteConstraint{tc.remote}, ownedSet("probe__Doc", "probe__Other"))
			if len(plan.Drift) != 1 {
				t.Fatalf("Drift = %+v, want the name pair's drift", plan.Drift)
			}
			if got := strings.Contains(plan.Drift[0].Reason, existence); got != tc.want {
				t.Errorf("Reason = %q; states the existence rule: %v, want %v", plan.Drift[0].Reason, got, tc.want)
			}
		})
	}
}

// TestDiffConstraints_ANameHolderStatesTheNodeKeyExistenceRule gives the name
// blocker's drift the clause too: under an owned set that lacks the label, a UNIQUE
// holding the NODE KEY's name is that drift's Actual.
func TestDiffConstraints_ANameHolderStatesTheNodeKeyExistenceRule(t *testing.T) {
	t.Parallel()
	desired := []Constraint{{Name: "k", Kind: ConstraintNodeKey, Label: "probe__Doc", Properties: []string{"id"}}}
	u := RemoteConstraint{Name: "k", Type: "UNIQUENESS", EntityType: "NODE", LabelsOrTypes: []string{"probe__Doc"}, Properties: []string{"id"}}
	plan := New().DiffConstraints(desired, []RemoteConstraint{u}, ownedSet("probe__Other"))
	if len(plan.Drift) != 1 || !strings.Contains(plan.Drift[0].Reason, "requires every node to hold each key property") {
		t.Errorf("Drift = %+v, want the name holder's drift with the existence clause", plan.Drift)
	}
}

// TestDiffConstraints_TwoBlockersNameTheFirstInOrder names the holder of the
// declaration's name before a key holder of the other kind, as the blocker order
// states, and leaves the key holder a Drop the next diff then names.
func TestDiffConstraints_TwoBlockersNameTheFirstInOrder(t *testing.T) {
	t.Parallel()
	desired := []Constraint{{Name: "k", Kind: ConstraintNodeKey, Label: "probe__Doc", Properties: []string{"id"}}}
	nameHolder := RemoteConstraint{Name: "k", Type: "UNIQUENESS", EntityType: "NODE", LabelsOrTypes: []string{"other__T"}, Properties: []string{"z"}}
	keyHolder := RemoteConstraint{Name: "u", Type: "UNIQUENESS", EntityType: "NODE", LabelsOrTypes: []string{"probe__Doc"}, Properties: []string{"id"}}
	plan := New().DiffConstraints(desired, []RemoteConstraint{nameHolder, keyHolder}, ownedSet("probe__Doc"))
	if len(plan.Drift) != 1 || plan.Drift[0].Actual.Name != "k" || len(plan.Drop) != 1 || plan.Drop[0].Name != "u" {
		t.Errorf("Drift = %+v, Drop = %+v; want the name holder's drift and u dropped", plan.Drift, plan.Drop)
	}
}
