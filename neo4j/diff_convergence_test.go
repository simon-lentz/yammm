package neo4j

import (
	"slices"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/schema/cypher"
)

// blockerNames lists a drift's blockers as "c:" or "i:" and the name, the
// constraints first, each list in order.
func blockerNames(b Blockers) []string {
	names := make([]string, 0, len(b.Constraints)+len(b.Indexes))
	for _, rc := range b.Constraints {
		names = append(names, "c:"+rc.Name)
	}
	for _, ri := range b.Indexes {
		names = append(names, "i:"+ri.Name)
	}
	return names
}

// TestConstraintDiff_AnIndexHoldingOnlyTheNameIsABlocker names an index that
// holds a NOT NULL declaration's name over another property: no definition rule
// reaches it, and the server still refuses the create.
func TestConstraintDiff_AnIndexHoldingOnlyTheNameIsABlocker(t *testing.T) {
	t.Parallel()
	desired := []cypher.Constraint{{Name: "n", Kind: cypher.ConstraintNotNull, Label: "probe__Doc", Properties: []string{"id"}}}
	indexes := []RemoteIndex{{Name: "n", Type: "TEXT", EntityType: "NODE", LabelsOrTypes: []string{"probe__Doc"}, Properties: []string{"z"}}}
	plan := diff(cypher.Config{}, desired, nil, nil, indexes, ownedSet("probe__Doc")).Constraints
	if len(plan.Drift) != 1 || len(plan.Create) != 0 {
		t.Fatalf("Drift = %+v, Create = %+v; want one drift", plan.Drift, plan.Create)
	}
	d := plan.Drift[0]
	if !slices.Equal(blockerNames(d.Blockers), []string{"i:n"}) || d.Blockers.Indexes[0].Type != "TEXT" {
		t.Errorf("Blockers = %+v; want the TEXT index n alone, as an index", d.Blockers)
	}
	if !strings.Contains(d.Reason, "share one namespace") {
		t.Errorf("Reason = %q; want it to say index and constraint names share one namespace", d.Reason)
	}
}

// TestConstraintDiff_EveryBlockerIsNamed names the holder of the declaration's
// name, a RANGE index serving its definition and a key holder of the other kind
// in one drift, since the server refuses the create while any of them stands.
func TestConstraintDiff_EveryBlockerIsNamed(t *testing.T) {
	t.Parallel()
	desired := []cypher.Constraint{{Name: "k", Kind: cypher.ConstraintUnique, Label: "probe__Doc", Properties: []string{"id"}}}
	actual := []RemoteConstraint{
		{Name: "nk", Type: "NODE_KEY", EntityType: "NODE", LabelsOrTypes: []string{"probe__Doc"}, Properties: []string{"id"}},
		{Name: "k", Type: "NODE_PROPERTY_EXISTENCE", EntityType: "NODE", LabelsOrTypes: []string{"other__T"}, Properties: []string{"z"}},
	}
	indexes := []RemoteIndex{{Name: "r", Type: "RANGE", EntityType: "NODE", LabelsOrTypes: []string{"probe__Doc"}, Properties: []string{"id"}}}
	plan := diff(cypher.Config{}, desired, nil, actual, indexes, ownedSet("probe__Doc")).Constraints
	if len(plan.Drift) != 1 || len(plan.Create) != 0 || len(plan.Drop) != 0 {
		t.Fatalf("Drift = %+v, Create = %+v, Drop = %+v; want one drift and nothing else", plan.Drift, plan.Create, plan.Drop)
	}
	d := plan.Drift[0]
	if !slices.Equal(blockerNames(d.Blockers), []string{"c:k", "c:nk", "i:r"}) {
		t.Errorf("Blockers = %q, want the name holder, the NODE KEY and the RANGE index", blockerNames(d.Blockers))
	}
	if d.Blockers.Indexes[0].Type != "RANGE" {
		t.Errorf("Blockers.Indexes[0].Type = %q; the RANGE index must keep its Type", d.Blockers.Indexes[0].Type)
	}
	if n := strings.Count(d.Reason, "; "); n < 2 {
		t.Errorf("Reason = %q; want a clause per blocker", d.Reason)
	}
}

// TestConstraintDiff_APairedDriftNamesItsNameHolder gives a TYPE drift the
// object holding its name as a blocker, beside the constraint it pairs with:
// the create is a silent no-op while the name is held.
func TestConstraintDiff_APairedDriftNamesItsNameHolder(t *testing.T) {
	t.Parallel()
	desired := []cypher.Constraint{{Name: "app__T_z_type", Kind: cypher.ConstraintType, Label: "app__T", Properties: []string{"z"}, TypeExpr: "INTEGER"}}
	actual := []RemoteConstraint{
		{Name: "a", Type: "NODE_PROPERTY_TYPE", EntityType: "NODE", LabelsOrTypes: []string{"app__T"}, Properties: []string{"z"}, PropertyType: "STRING"},
		{Name: "app__T_z_type", Type: "NODE_PROPERTY_EXISTENCE", EntityType: "NODE", LabelsOrTypes: []string{"app__T"}, Properties: []string{"q"}},
	}
	plan := diff(cypher.Config{}, desired, nil, actual, nil, ownedSet("app__T")).Constraints
	if len(plan.Drift) != 1 || !slices.Equal(blockerNames(plan.Drift[0].Blockers), []string{"c:a", "c:app__T_z_type"}) {
		t.Fatalf("Drift = %+v, want the TYPE drift blocked by a and the name holder", plan.Drift)
	}
	if !strings.HasPrefix(plan.Drift[0].Reason, "property type mismatch") {
		t.Errorf("Reason = %q; want the pair's own clause first", plan.Drift[0].Reason)
	}
	if len(plan.Drop) != 0 {
		t.Errorf("Drop = %+v; a blocker is not also a Drop", plan.Drop)
	}
}

// TestConstraintDiff_ARealisationAnotherCreateNeedsDroppedIsDrift moves a
// matched declaration to Drift when its constraint holds another declaration's
// name: the drop that clears that name removes its realisation too.
func TestConstraintDiff_ARealisationAnotherCreateNeedsDroppedIsDrift(t *testing.T) {
	t.Parallel()
	desired := []cypher.Constraint{
		{Name: "app__T_id_unique", Kind: cypher.ConstraintUnique, Label: "app__T", Properties: []string{"id"}},
		{Name: "app__T_z_type", Kind: cypher.ConstraintType, Label: "app__T", Properties: []string{"z"}, TypeExpr: "INTEGER"},
	}
	actual := []RemoteConstraint{
		{Name: "a", Type: "NODE_PROPERTY_TYPE", EntityType: "NODE", LabelsOrTypes: []string{"app__T"}, Properties: []string{"z"}, PropertyType: "STRING"},
		{Name: "app__T_z_type", Type: "UNIQUENESS", EntityType: "NODE", LabelsOrTypes: []string{"app__T"}, Properties: []string{"id"}},
	}
	plan := diff(cypher.Config{}, desired, nil, actual, nil, ownedSet("app__T")).Constraints
	if len(plan.Match) != 0 || len(plan.Drift) != 2 {
		t.Fatalf("Match = %+v, Drift = %+v; want both declarations drifted", plan.Match, plan.Drift)
	}
	const reason = `a UNIQUENESS constraint "app__T_z_type" realises this declaration and the plan drops it; this constraint is created again under its own name`
	if !slices.Equal(blockerNames(plan.Drift[0].Blockers), []string{"c:app__T_z_type"}) || plan.Drift[0].Reason != reason {
		t.Errorf("Drift[0] = %+v; want the UNIQUE's own realisation named, and why", plan.Drift[0])
	}
	if !slices.Equal(blockerNames(plan.Drift[1].Blockers), []string{"c:a", "c:app__T_z_type"}) || !strings.Contains(plan.Drift[1].Reason, "realises another declaration") {
		t.Errorf("Drift[1] = %+v; want the TYPE drift blocked by a and the name holder, and why", plan.Drift[1])
	}
}

// TestIndexDiff_APairedDriftNamesItsNameHolder gives a vector drift and a
// FAILED index's drift the index holding the declaration's name as a blocker,
// beside the index each pairs with.
func TestIndexDiff_APairedDriftNamesItsNameHolder(t *testing.T) {
	t.Parallel()
	holder := RemoteIndex{Name: "app__T_v_idx", Type: "RANGE", EntityType: "NODE", State: "ONLINE", LabelsOrTypes: []string{"app__T"}, Properties: []string{"q"}}
	for _, tc := range []struct {
		name    string
		desired cypher.Index
		paired  RemoteIndex
		clause  string
	}{
		{
			name:    "a vector index of another dimension",
			desired: cypher.Index{Name: "app__T_v_idx", Kind: cypher.IndexVector, Label: "app__T", Properties: []string{"v"}, VectorDimensions: 4, VectorSimilarity: "cosine"},
			paired: RemoteIndex{
				Name: "op_vec", Type: "VECTOR", EntityType: "NODE", State: "ONLINE", LabelsOrTypes: []string{"app__T"}, Properties: []string{"v"},
				Options: map[string]any{"indexConfig": map[string]any{"vector.dimensions": int64(3), "vector.similarity_function": "COSINE"}},
			},
			clause: "vector dimension mismatch",
		},
		{
			name:    "a FAILED index",
			desired: cypher.Index{Name: "app__T_v_idx", Kind: cypher.IndexRange, Label: "app__T", Properties: []string{"v"}},
			paired:  RemoteIndex{Name: "op_range", Type: "RANGE", EntityType: "NODE", State: "FAILED", LabelsOrTypes: []string{"app__T"}, Properties: []string{"v"}},
			clause:  "does not serve queries",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			plan := diff(cypher.Config{}, nil, []cypher.Index{tc.desired}, nil, []RemoteIndex{holder, tc.paired}, ownedSet("app__T")).Indexes
			if len(plan.Drift) != 1 || !slices.Equal(blockerNames(plan.Drift[0].Blockers), []string{"i:" + tc.paired.Name, "i:app__T_v_idx"}) {
				t.Fatalf("Drift = %+v, want one drift blocked by %s and the name holder", plan.Drift, tc.paired.Name)
			}
			if !strings.Contains(plan.Drift[0].Reason, tc.clause) {
				t.Errorf("Reason = %q, want %q", plan.Drift[0].Reason, tc.clause)
			}
			if len(plan.Drop) != 0 {
				t.Errorf("Drop = %+v; a blocker is not also a Drop", plan.Drop)
			}
		})
	}
}

// TestIndexDiff_EveryBlockerIsNamed names the index holding the declaration's
// name and the index serving its definition in one drift.
func TestIndexDiff_EveryBlockerIsNamed(t *testing.T) {
	t.Parallel()
	desired := []cypher.Index{{Name: "idx", Kind: cypher.IndexRange, Label: "app__T", Properties: []string{"id"}}}
	actual := []RemoteIndex{
		{Name: "idx", Type: "TEXT", EntityType: "NODE", LabelsOrTypes: []string{"other__T"}, Properties: []string{"z"}},
		{Name: "held", Type: "RANGE", EntityType: "NODE", LabelsOrTypes: []string{"app__T"}, Properties: []string{"id"}},
	}
	plan := diff(cypher.Config{}, nil, desired, nil, actual, ownedSet("app__Other")).Indexes
	if len(plan.Drift) != 1 || !slices.Equal(blockerNames(plan.Drift[0].Blockers), []string{"i:idx", "i:held"}) || len(plan.Create) != 0 {
		t.Errorf("Drift = %+v, Create = %+v; want one drift blocked by idx and held", plan.Drift, plan.Create)
	}
	if plan.Excluded != 0 {
		t.Errorf("Excluded = %d; a row a drift names is not also excluded", plan.Excluded)
	}
}

// TestConstraintDiff_TheExistenceClauseFollowsItsBlocker states the NODE KEY's
// existence rule for a UNIQUE over its key named first, beside a RANGE index
// named after it, each clause joined by "; ".
func TestConstraintDiff_TheExistenceClauseFollowsItsBlocker(t *testing.T) {
	t.Parallel()
	desired := []cypher.Constraint{{Name: "k", Kind: cypher.ConstraintNodeKey, Label: "app__T", Properties: []string{"id"}}}
	actual := []RemoteConstraint{{Name: "k", Type: "UNIQUENESS", EntityType: "NODE", LabelsOrTypes: []string{"app__T"}, Properties: []string{"id"}}}
	indexes := []RemoteIndex{{Name: "r", Type: "RANGE", EntityType: "NODE", LabelsOrTypes: []string{"app__T"}, Properties: []string{"id"}}}
	plan := diff(cypher.Config{}, desired, nil, actual, indexes, ownedSet("app__T")).Constraints
	if len(plan.Drift) != 1 || !slices.Equal(blockerNames(plan.Drift[0].Blockers), []string{"c:k", "i:r"}) {
		t.Fatalf("Drift = %+v; want k then r", plan.Drift)
	}
	if r := plan.Drift[0].Reason; !strings.Contains(r, "database UNIQUENESS(id); a RANGE index") || !strings.Contains(r, "requires every node to hold each key property") {
		t.Errorf("Reason = %q; want the clauses joined by \"; \" and the existence rule", r)
	}
}

// TestConstraintDiff_TheDeclaredOrderWinsInEitherSpelling pairs the twin in the
// declared order when the server spells UNIQUE as CYPHER 25 does.
func TestConstraintDiff_TheDeclaredOrderWinsInEitherSpelling(t *testing.T) {
	t.Parallel()
	desired := []cypher.Constraint{{Name: "app__T_a_b_unique", Kind: cypher.ConstraintUnique, Label: "app__T", Properties: []string{"a", "b"}}}
	reordered := RemoteConstraint{Name: "aaa", Type: "UNIQUENESS", EntityType: "NODE", LabelsOrTypes: []string{"app__T"}, Properties: []string{"b", "a"}}
	declared := RemoteConstraint{Name: "zzz", Type: "NODE_PROPERTY_UNIQUENESS", EntityType: "NODE", LabelsOrTypes: []string{"app__T"}, Properties: []string{"a", "b"}}
	plan := diff(cypher.Config{}, desired, nil, []RemoteConstraint{reordered, declared}, nil, ownedSet("app__T")).Constraints
	if len(plan.Match) != 1 || plan.Match[0].Actual.Name != "zzz" {
		t.Errorf("Match = %+v; want zzz", plan.Match)
	}
}

// TestIndexDiff_ABackingIndexNeverBlocksADefinition names a name-only pair's
// realisation alone beside a backing index over the declared definition.
func TestIndexDiff_ABackingIndexNeverBlocksADefinition(t *testing.T) {
	t.Parallel()
	desired := []cypher.Index{{Name: "i", Kind: cypher.IndexRange, Label: "app__T", Properties: []string{"id"}}}
	actual := []RemoteIndex{
		{Name: "i", Type: "RANGE", EntityType: "NODE", State: "ONLINE", LabelsOrTypes: []string{"app__T"}, Properties: []string{"q"}},
		{Name: "c", Type: "RANGE", EntityType: "NODE", State: "ONLINE", LabelsOrTypes: []string{"app__T"}, Properties: []string{"id"}, OwningConstraint: "c"},
	}
	plan := diff(cypher.Config{}, nil, desired, nil, actual, ownedSet("app__T")).Indexes
	if len(plan.Drift) != 1 || !slices.Equal(blockerNames(plan.Drift[0].Blockers), []string{"i:i"}) {
		t.Errorf("Drift = %+v; want i alone", plan.Drift)
	}
}

// TestDiff_ABlockerOfTheOtherKindCarriesItsEntityType keeps a cross-namespace
// blocker's EntityType, which says whether to drop a node or relationship object.
func TestDiff_ABlockerOfTheOtherKindCarriesItsEntityType(t *testing.T) {
	t.Parallel()
	c := diff(cypher.Config{}, []cypher.Constraint{{Name: "k", Kind: cypher.ConstraintUnique, Label: "app__T", Properties: []string{"id"}}}, nil, nil,
		[]RemoteIndex{{Name: "k", Type: "TEXT", EntityType: "RELATIONSHIP", LabelsOrTypes: []string{"REL"}, Properties: []string{"z"}}}, ownedSet("app__T")).Constraints
	if len(c.Drift) != 1 || len(c.Drift[0].Blockers.Indexes) != 1 || c.Drift[0].Blockers.Indexes[0].EntityType != "RELATIONSHIP" {
		t.Errorf("constraint drift = %+v; want the index blocker's EntityType", c.Drift)
	}
	i := diff(cypher.Config{}, nil, []cypher.Index{{Name: "k", Kind: cypher.IndexRange, Label: "app__T", Properties: []string{"id"}}},
		[]RemoteConstraint{{Name: "k", Type: "RELATIONSHIP_PROPERTY_EXISTENCE", EntityType: "RELATIONSHIP", LabelsOrTypes: []string{"REL"}, Properties: []string{"z"}}}, nil, ownedSet("app__T")).Indexes
	if len(i.Drift) != 1 || len(i.Drift[0].Blockers.Constraints) != 1 || i.Drift[0].Blockers.Constraints[0].EntityType != "RELATIONSHIP" {
		t.Errorf("index drift = %+v; want the constraint blocker's EntityType", i.Drift)
	}
}

// TestIndexDiff_ClausesJoinInBlockerOrder joins an index drift's clauses with
// "; " in the order Blockers names them.
func TestIndexDiff_ClausesJoinInBlockerOrder(t *testing.T) {
	t.Parallel()
	desired := []cypher.Index{{Name: "idx", Kind: cypher.IndexRange, Label: "app__T", Properties: []string{"id"}}}
	actual := []RemoteIndex{
		{Name: "idx", Type: "TEXT", EntityType: "NODE", LabelsOrTypes: []string{"other__T"}, Properties: []string{"z"}},
		{Name: "held", Type: "RANGE", EntityType: "NODE", LabelsOrTypes: []string{"app__T"}, Properties: []string{"id"}},
	}
	plan := diff(cypher.Config{}, nil, desired, nil, actual, ownedSet("app__Other")).Indexes
	if len(plan.Drift) != 1 || !strings.Contains(plan.Drift[0].Reason, "can be created; this definition is already served") {
		t.Errorf("Drift = %+v; want the two clauses joined by \"; \"", plan.Drift)
	}
}

// TestIndexDiff_TwoHoldersOfADefinitionAreBothNamed names two indexes that
// serve one declaration's definition under other names, so dropping one leaves
// none standing.
func TestIndexDiff_TwoHoldersOfADefinitionAreBothNamed(t *testing.T) {
	t.Parallel()
	declaredI := []cypher.Index{{Name: "k", Kind: cypher.IndexRange, Label: "app__T", Properties: []string{"id"}}}
	served := RemoteIndex{Name: "a", Type: "RANGE", EntityType: "NODE", State: "ONLINE", LabelsOrTypes: []string{"app__T"}, Properties: []string{"id"}}
	renamed := served
	renamed.Name = "b"
	plan := diff(cypher.Config{}, nil, declaredI, nil, []RemoteIndex{served, renamed}, ownedSet("o__T")).Indexes
	if len(plan.Drift) != 1 || len(plan.Drift[0].Blockers.Indexes) != 2 {
		t.Errorf("Drift = %+v; want both definition holders", plan.Drift)
	}
}

// TestDiff_ABlockerWithNoTypeKeepsItsKind lists a name holder whose record
// carries no type under its own kind, so the caller drops it with the right
// verb.
func TestDiff_ABlockerWithNoTypeKeepsItsKind(t *testing.T) {
	t.Parallel()
	c := diff(cypher.Config{}, []cypher.Constraint{{Name: "k", Kind: cypher.ConstraintUnique, Label: "app__T", Properties: []string{"id"}}}, nil,
		[]RemoteConstraint{{Name: "k", EntityType: "NODE", LabelsOrTypes: []string{"o__T"}, Properties: []string{"z"}}}, nil, ownedSet("app__T")).Constraints
	if len(c.Drift) != 1 || !slices.Equal(blockerNames(c.Drift[0].Blockers), []string{"c:k"}) {
		t.Errorf("constraint drift = %+v; want the untyped constraint k listed as a constraint", c.Drift)
	}
	i := diff(cypher.Config{}, nil, []cypher.Index{{Name: "k", Kind: cypher.IndexRange, Label: "app__T", Properties: []string{"id"}}},
		nil, []RemoteIndex{{Name: "k", EntityType: "NODE", LabelsOrTypes: []string{"o__T"}, Properties: []string{"z"}}}, ownedSet("app__T")).Indexes
	if len(i.Drift) != 1 || !slices.Equal(blockerNames(i.Drift[0].Blockers), []string{"i:k"}) {
		t.Errorf("index drift = %+v; want the untyped index k listed as an index", i.Drift)
	}
}

// TestDiff_ALookupIndexHoldingADeclaredNameIsABlocker names a LOOKUP index
// that holds a declared name in that declaration's drift, on either side.
func TestDiff_ALookupIndexHoldingADeclaredNameIsABlocker(t *testing.T) {
	t.Parallel()
	lookup := []RemoteIndex{{Name: "k", Type: "LOOKUP", EntityType: "NODE", State: "ONLINE"}}
	c := diff(cypher.Config{}, []cypher.Constraint{{Name: "k", Kind: cypher.ConstraintUnique, Label: "app__T", Properties: []string{"id"}}}, nil, nil, lookup, ownedSet("app__T")).Constraints
	if len(c.Drift) != 1 || !slices.Equal(blockerNames(c.Drift[0].Blockers), []string{"i:k"}) || len(c.Create) != 0 {
		t.Fatalf("constraint plan = %+v; want k drifted on the LOOKUP index", c)
	}
	if !strings.Contains(c.Drift[0].Reason, "LOOKUP index on no label") {
		t.Errorf("Reason = %q; want it to say the LOOKUP index is on no label", c.Drift[0].Reason)
	}
	i := diff(cypher.Config{}, nil, []cypher.Index{{Name: "k", Kind: cypher.IndexRange, Label: "app__T", Properties: []string{"id"}}}, nil, lookup, ownedSet("app__T")).Indexes
	if len(i.Drift) != 1 || !slices.Equal(blockerNames(i.Drift[0].Blockers), []string{"i:k"}) || len(i.Create) != 0 {
		t.Errorf("index plan = %+v; want k drifted on the LOOKUP index", i)
	}
}
