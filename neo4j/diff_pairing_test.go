package neo4j

import (
	"slices"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/schema/cypher"
)

// Pairing behavior shared by the constraint and index diffs: which remote object
// realises a declaration, which is redundant, and which belongs to somebody
// else.

// ownedSet builds the label set the diff entry points scope themselves to.
// Tests state it explicitly so each case's ownership assumption is visible at
// the call site rather than inferred from a schema name.
func ownedSet(labels ...string) labelSet {
	o := make(labelSet, len(labels))
	for _, l := range labels {
		o[l] = struct{}{}
	}
	return o
}

// labelSet is an [owner] over a fixed set of labels.
type labelSet map[string]struct{}

func (o labelSet) LabelOf(labels []string) (string, bool) {
	for _, l := range labels {
		if _, ok := o[l]; ok {
			return l, true
		}
	}
	return "", false
}

// testOwned and appOwned are the label sets the fixtures in this package emit.
// Naming them keeps each test's ownership assumption to one token, so a test
// that cares about a specific label states that instead of restating four.
func testOwned() labelSet {
	return ownedSet("test__Entity", "test__Company", "test__Employee", "test__Record")
}

func appOwned() labelSet {
	return ownedSet("app__T", "app__Foo", "app__Core__Widget")
}

func rc(name, label string, props ...string) RemoteConstraint {
	return RemoteConstraint{
		Name: name, Type: "UNIQUENESS", EntityType: "NODE",
		LabelsOrTypes: []string{label}, Properties: props,
	}
}

func dc(name, label string, props ...string) cypher.Constraint {
	return cypher.Constraint{Name: name, Kind: cypher.ConstraintUnique, Label: label, Properties: props}
}

func constraintCounts(t *testing.T, r ConstraintDiff, match, drift, create, drop, unverified int) {
	t.Helper()
	assertCounts(t, "constraint",
		len(r.Match), len(r.Drift), len(r.Create), len(r.Drop), len(r.Unverified),
		match, drift, create, drop, unverified)
}

// A second remote constraint sharing one identity is the redundant, undeclared
// definition the diff exists to surface: one of the two realises the
// declaration and the other is a drop. Both must be reported, so pairing has to
// be one-to-one rather than a lookup by identity.
func TestConstraintDiff_DuplicateOfMatchedStillDrops(t *testing.T) {
	t.Parallel()
	got := diff(cypher.Config{}, []cypher.Constraint{dc("app__T_id_unique", "app__T", "id")}, nil, []RemoteConstraint{
		rc("app__T_id_unique", "app__T", "id"),
		rc("hand_made_id_unique", "app__T", "id"),
	}, nil, appOwned()).Constraints

	constraintCounts(t, got, 1, 0, 0, 1, 0)
	if len(got.Drop) == 1 && got.Drop[0].Name != "hand_made_id_unique" {
		t.Errorf("Drop[0] = %q; the undeclared duplicate is the one to drop", got.Drop[0].Name)
	}
}

// Which of two same-identity remotes is Match and which is Drop must not depend
// on the order SHOW CONSTRAINTS returned them in: the one the generator itself
// named is the realisation, whichever row arrives first. SHOW CONSTRAINTS
// orders by name, so a hand-made name sorting ahead of the emitted one is the
// ordering a real server produces.
func TestConstraintDiff_PairingIsOrderIndependent(t *testing.T) {
	t.Parallel()
	a := cypher.Config{}
	desired := []cypher.Constraint{dc("app__T_id_unique", "app__T", "id")}
	own := rc("app__T_id_unique", "app__T", "id")
	adhoc := rc("aaa_hand_made", "app__T", "id")

	for _, tc := range []struct {
		name   string
		actual []RemoteConstraint
	}{
		{"own first", []RemoteConstraint{own, adhoc}},
		{"adhoc first", []RemoteConstraint{adhoc, own}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := diff(a, desired, nil, tc.actual, nil, ownedSet("app__T", "app__Foo")).Constraints
			constraintCounts(t, got, 1, 0, 0, 1, 0)
			if len(got.Match) == 1 && got.Match[0].Actual.Name != "app__T_id_unique" {
				t.Errorf("Match = %q; the schema's own constraint must be the match", got.Match[0].Actual.Name)
			}
			if len(got.Drop) == 1 && got.Drop[0].Name != "aaa_hand_made" {
				t.Errorf("Drop = %q; the undeclared constraint must be the drop", got.Drop[0].Name)
			}
		})
	}
}

// Two desired constraints sharing one identity must consume two distinct remote
// constraints rather than both claiming the first.
func TestConstraintDiff_TwoDesiredShareIdentity(t *testing.T) {
	t.Parallel()
	got := diff(cypher.Config{}, []cypher.Constraint{dc("", "app__T", "id"), dc("", "app__T", "id")}, nil, []RemoteConstraint{rc("a", "app__T", "id"), rc("b", "app__T", "id")}, nil, appOwned()).Constraints
	constraintCounts(t, got, 2, 0, 0, 0, 0)
}

// Ownership is anchored: a schema named "app" must not claim the objects of a
// schema named "app__legacy", whose labels also start with "app__". Reporting
// them as drops surfaced drift no edit to "app" could ever resolve.
func TestConstraintDiff_SiblingSchemaNotClaimed(t *testing.T) {
	t.Parallel()
	got := diff(cypher.Config{}, nil, nil, []RemoteConstraint{rc("other", "app__legacy__Foo", "x")}, nil, ownedSet("app__T", "app__Foo")).Constraints
	constraintCounts(t, got, 0, 0, 0, 0, 0)
}

// ...but the schema's own undeclared constraint still drops, so the anchoring
// above did not simply stop claiming everything.
func TestConstraintDiff_OwnSchemaStillDrops(t *testing.T) {
	t.Parallel()
	got := diff(cypher.Config{}, nil, nil, []RemoteConstraint{rc("mine", "app__Foo", "x")}, nil, ownedSet("app__T", "app__Foo")).Constraints
	constraintCounts(t, got, 0, 0, 0, 1, 0)
}

// A type with no schema name gets a bare label: Config.Label returns the
// sanitized type name with no prefix and no separator. Those labels
// carry no namespace, so nothing about them distinguishes this schema's objects
// from any other application's — only the label SET can.
func TestConstraintDiff_UnscopedLabels(t *testing.T) {
	t.Parallel()
	got := diff(cypher.Config{}, []cypher.Constraint{dc("Person_id_unique", "Person", "id")}, nil, []RemoteConstraint{rc("Person_id_unique", "Person", "id")}, nil, ownedSet("Person")).Constraints
	constraintCounts(t, got, 1, 0, 0, 0, 0)
}

// An unscoped schema owns the labels it emits and NOTHING ELSE. Sharing a
// database with another yammm schema, or with an unrelated application, must not
// turn their objects into drops — following such a plan deletes indexes and
// constraints this schema never owned.
func TestDiff_UnscopedDoesNotClaimTheDatabase(t *testing.T) {
	t.Parallel()
	a := cypher.Config{}
	owned := ownedSet("Person")

	cons := diff(a, nil, nil, []RemoteConstraint{
		rc("theirs", "other__Foo", "x"),
		rc("unrelated", "LegacyThing", "y"),
	}, nil, owned).Constraints
	constraintCounts(t, cons, 0, 0, 0, 0, 0)

	idx := diff(a, nil, nil, nil, []RemoteIndex{
		{Name: "theirs", Type: "RANGE", EntityType: "NODE", LabelsOrTypes: []string{"other__Foo"}, Properties: []string{"x"}},
		{Name: "unrelated", Type: "RANGE", EntityType: "NODE", LabelsOrTypes: []string{"LegacyThing"}, Properties: []string{"y"}},
	}, owned).Indexes
	wantCounts(t, idx, 0, 0, 0, 0, 0)
}

// A sibling schema whose name extends this one's is not owned, whatever the case
// of the segment that follows. No rule reading a label can settle this — "app"
// and "app__Core" both produce labels beginning "app__" — so ownership is
// membership in the emitting schema's own label set.
func TestConstraintDiff_SiblingSchemaNotClaimed_AnyCase(t *testing.T) {
	t.Parallel()
	owned := ownedSet("app__T", "app__Foo")
	for _, label := range []string{
		"app__legacy__Foo",  // lowercase-leading sibling segment
		"app__Core__Widget", // uppercase-leading sibling segment
		"app__T__Nested",    // extends a label this schema DOES own
	} {
		t.Run(label, func(t *testing.T) {
			t.Parallel()
			got := diff(cypher.Config{}, nil, nil, []RemoteConstraint{rc("sib", label, "id")}, nil, owned).Constraints
			constraintCounts(t, got, 0, 0, 0, 0, 0)
		})
	}
}

// Ownership does not consult the label separator or prefix at all, so no
// configuration of them can widen or narrow what the schema claims.
func TestConstraintDiff_OwnershipIgnoresSeparatorConfig(t *testing.T) {
	t.Parallel()
	owned := ownedSet("app__T")
	for _, sep := range []string{"__", "_", "", "-", "X"} {
		t.Run("sep="+sep, func(t *testing.T) {
			t.Parallel()
			a := cypher.Config{LabelSeparator: sep}
			// A label outside the set is never owned...
			outside := diff(a, nil, nil, []RemoteConstraint{rc("sib", "appLegacyFoo", "id")}, nil, owned).Constraints
			constraintCounts(t, outside, 0, 0, 0, 0, 0)
			// ...and one inside it always is.
			inside := diff(a, nil, nil, []RemoteConstraint{rc("mine", "app__T", "id")}, nil, owned).Constraints
			constraintCounts(t, inside, 0, 0, 0, 1, 0)
		})
	}
}

// Constraint members are a set, so member order does not distinguish two
// constraints — the deliberate divergence from the index diff, where composite
// order IS significant.
func TestConstraintDiff_MemberOrderInsensitive(t *testing.T) {
	t.Parallel()
	got := diff(cypher.Config{}, []cypher.Constraint{dc("", "app__T", "a", "b")}, nil, []RemoteConstraint{rc("r", "app__T", "b", "a")}, nil, appOwned()).Constraints
	constraintCounts(t, got, 1, 0, 0, 0, 0)
}

// The identity encoding must be injective. Remote property names are
// database-supplied and unvalidated — Neo4j accepts any key when it is
// backticked — so a single member literally named "a,b" must not encode the same
// as the two-member set (a, b): under a separator join it would, and a
// constraint the schema declared would read as in sync and never be created.
func TestConstraintDiff_IdentityEncodingIsInjective(t *testing.T) {
	t.Parallel()
	got := diff(cypher.Config{}, []cypher.Constraint{dc("", "app__T", "a", "b")}, nil, []RemoteConstraint{rc("r", "app__T", "a,b")}, nil, appOwned()).Constraints
	constraintCounts(t, got, 0, 0, 1, 1, 0)
}

// A remote constraint owned via a non-first label must be keyed by the label
// that made it owned. Ownership and identity read the same label or an object
// admitted under one is keyed by another, giving it an identity no desired
// constraint can produce: the diff then advises creating one that already exists
// and dropping the one that is correct.
func TestConstraintDiff_MultiLabelUsesOwnedLabel(t *testing.T) {
	t.Parallel()
	got := diff(cypher.Config{}, []cypher.Constraint{dc("", "app__T", "x")}, nil, []RemoteConstraint{{
		Name: "ml", Type: "UNIQUENESS", EntityType: "NODE",
		LabelsOrTypes: []string{"other__Z", "app__T"}, Properties: []string{"x"},
	}}, nil, appOwned()).Constraints
	constraintCounts(t, got, 1, 0, 0, 0, 0)
}

// A TYPE constraint whose enforced type differs is drift, not a match.
func TestConstraintDiff_TypeExprDrift(t *testing.T) {
	t.Parallel()
	got := diff(cypher.Config{}, []cypher.Constraint{{
		Name: "c", Kind: cypher.ConstraintType, Label: "app__T",
		Properties: []string{"amount"}, TypeExpr: "INTEGER",
	}}, nil, []RemoteConstraint{{
		Name: "c", Type: "NODE_PROPERTY_TYPE", EntityType: "NODE",
		LabelsOrTypes: []string{"app__T"}, Properties: []string{"amount"},
		PropertyType: "STRING",
	}}, nil, appOwned()).Constraints
	constraintCounts(t, got, 0, 1, 0, 0, 0)
}

// A TYPE constraint the server would not describe (PropertyType is empty on
// Neo4j < 5.9, and on any driver path that omits the column) is unverified, not
// matched: folding it into Match reported an unchecked constraint as verified,
// and `yammm neo4j diff` then exited 0 on a genuinely drifted database.
func TestConstraintDiff_UnreportedTypeExprIsUnverified(t *testing.T) {
	t.Parallel()
	got := diff(cypher.Config{}, []cypher.Constraint{{
		Name: "c", Kind: cypher.ConstraintType, Label: "app__T",
		Properties: []string{"amount"}, TypeExpr: "INTEGER",
	}}, nil, []RemoteConstraint{{
		Name: "c", Type: "NODE_PROPERTY_TYPE", EntityType: "NODE",
		LabelsOrTypes: []string{"app__T"}, Properties: []string{"amount"},
	}}, nil, appOwned()).Constraints
	constraintCounts(t, got, 0, 0, 0, 0, 1)
	if got.Unverified[0].Reason == "" {
		t.Error("an unverified entry should carry a reason")
	}
}

// A name match whose definitions disagree is drift, not Create + Drop: emitted
// statements carry IF NOT EXISTS, so re-creating under a name the database
// already holds silently does nothing and the plan never converges.
func TestConstraintDiff_SameNameDifferentDefinitionIsDrift(t *testing.T) {
	t.Parallel()
	got := diff(cypher.Config{}, []cypher.Constraint{dc("app__T_id_unique", "app__T", "id")}, nil, []RemoteConstraint{rc("app__T_id_unique", "app__T", "other_id")}, nil, appOwned()).Constraints
	constraintCounts(t, got, 0, 1, 0, 0, 0)
}

// Create must keep desired order, and Drop the sorted order whatever order the
// rows arrive in, so the printed plan is stable run to run.
func TestConstraintDiff_ResultOrderIsStable(t *testing.T) {
	t.Parallel()
	got := diff(cypher.Config{}, []cypher.Constraint{dc("", "app__T", "c3"), dc("", "app__T", "c1"), dc("", "app__T", "c2")}, nil, []RemoteConstraint{rc("r2", "app__T", "x2"), rc("r1", "app__T", "x1")}, nil, appOwned()).Constraints

	var created, dropped []string
	for _, c := range got.Create {
		created = append(created, c.Properties[0])
	}
	for _, d := range got.Drop {
		dropped = append(dropped, d.Name)
	}
	if !slices.Equal(created, []string{"c3", "c1", "c2"}) {
		t.Errorf("Create order = %v; want desired order", created)
	}
	if !slices.Equal(dropped, []string{"r1", "r2"}) {
		t.Errorf("Drop order = %v; want the sorted order", dropped)
	}
}

// The index diff's name pass must be exercised with a desired index that
// actually carries its emitted name. Every other index test leaves Name empty,
// so only the identity phase runs and the name wiring is unverified.
func TestIndexDiff_NamePassIsWired(t *testing.T) {
	t.Parallel()
	a := cypher.Config{}
	owned := ownedSet("app__T")
	desired := []cypher.Index{{Name: "app__T_code_idx", Kind: cypher.IndexRange, Label: "app__T", Properties: []string{"code"}}}
	own := RemoteIndex{
		Name: "app__T_code_idx", Type: "RANGE", EntityType: "NODE",
		LabelsOrTypes: []string{"app__T"}, Properties: []string{"code"},
	}
	adhoc := RemoteIndex{
		Name: "aaa_hand_made", Type: "RANGE", EntityType: "NODE",
		LabelsOrTypes: []string{"app__T"}, Properties: []string{"code"},
	}

	for _, tc := range []struct {
		name   string
		actual []RemoteIndex
	}{
		{"own first", []RemoteIndex{own, adhoc}},
		{"adhoc first", []RemoteIndex{adhoc, own}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := diff(a, nil, desired, nil, tc.actual, owned).Indexes
			wantCounts(t, got, 1, 0, 0, 1, 0)
			if len(got.Match) == 1 && got.Match[0].Actual.Name != "app__T_code_idx" {
				t.Errorf("Match = %q; the schema's own index must be the match", got.Match[0].Actual.Name)
			}
			if len(got.Drop) == 1 && got.Drop[0].Name != "aaa_hand_made" {
				t.Errorf("Drop = %q; the undeclared index must be the drop", got.Drop[0].Name)
			}
		})
	}
}

// An exact realisation outranks a name-holder whose definition differs. Claiming
// by name first lets the misnamed index consume the declaration and reports the
// index that actually satisfies it as the drop.
func TestIndexDiff_ExactRealisationOutranksNameHolder(t *testing.T) {
	t.Parallel()
	got := diff(cypher.Config{}, nil, []cypher.Index{{Name: "app__T_x_idx", Kind: cypher.IndexRange, Label: "app__T", Properties: []string{"x"}}}, nil, []RemoteIndex{
		{Name: "app__T_x_idx", Type: "RANGE", EntityType: "NODE", LabelsOrTypes: []string{"app__T"}, Properties: []string{"y"}},
		{Name: "adhoc_on_x", Type: "RANGE", EntityType: "NODE", LabelsOrTypes: []string{"app__T"}, Properties: []string{"x"}},
	}, ownedSet("app__T")).Indexes

	wantCounts(t, got, 1, 0, 0, 1, 0)
	if len(got.Match) == 1 && got.Match[0].Actual.Name != "adhoc_on_x" {
		t.Errorf("Match = %q; the index realising the declaration must match", got.Match[0].Actual.Name)
	}
	if len(got.Drop) == 1 && got.Drop[0].Name != "app__T_x_idx" {
		t.Errorf("Drop = %q; the misnamed index must be the drop", got.Drop[0].Name)
	}
}

// A desired index whose emitted name is held by a kind the DSL cannot express is
// not creatable: CREATE ... IF NOT EXISTS matches on name, so the statement is a
// silent no-op and the same Create would be reported forever.
func TestIndexDiff_NameHeldByUndeclarableKind(t *testing.T) {
	t.Parallel()
	got := diff(cypher.Config{}, nil, []cypher.Index{{Name: "app__T_body_idx", Kind: cypher.IndexRange, Label: "app__T", Properties: []string{"body"}}}, nil, []RemoteIndex{{
		Name: "app__T_body_idx", Type: "FULLTEXT", EntityType: "NODE",
		LabelsOrTypes: []string{"app__T"}, Properties: []string{"body"},
	}}, ownedSet("app__T")).Indexes

	wantCounts(t, got, 0, 1, 0, 0, 0)
	if len(got.Drift) == 1 && !strings.Contains(got.Drift[0].Reason, "FULLTEXT") {
		t.Errorf("Drift reason = %q; it should name the kind holding the name", got.Drift[0].Reason)
	}
}

// A relationship constraint is not declarable: LabelsOrTypes holds a
// relationship TYPE rather than a label, so ownership would be decided against
// the wrong namespace entirely, and no schema edit could resolve the drop.
func TestConstraintDiff_IgnoresUndeclarableKinds(t *testing.T) {
	t.Parallel()
	got := diff(cypher.Config{}, nil, nil, []RemoteConstraint{
		{
			Name: "relc", Type: "RELATIONSHIP_PROPERTY_EXISTENCE", EntityType: "RELATIONSHIP",
			LabelsOrTypes: []string{"app__T"}, Properties: []string{"since"},
		},
		{
			Name: "labelex", Type: "NODE_LABEL_EXISTENCE", EntityType: "NODE",
			LabelsOrTypes: []string{"app__T"}, Properties: []string{"x"},
		},
	}, nil, ownedSet("app__T")).Constraints
	constraintCounts(t, got, 0, 0, 0, 0, 0)
}

// ...but a declarable node constraint on an owned label still drops, so the
// filter did not simply stop reporting everything.
func TestConstraintDiff_DeclarableKindStillDrops(t *testing.T) {
	t.Parallel()
	got := diff(cypher.Config{}, nil, nil, []RemoteConstraint{rc("mine", "app__T", "id")}, nil, ownedSet("app__T")).Constraints
	constraintCounts(t, got, 0, 0, 0, 1, 0)
}

// A candidate one desired object rejects must stay available to a later one
// that is its exact name-and-identity realisation. Rejection is the common case
// in the name-and-identity phase, so each desired object scans its bucket from
// the front: a candidate consumed by nothing stays offered, and only `claimed`
// takes one out of circulation.
func TestPairObjects_RejectedCandidateStaysAvailable(t *testing.T) {
	t.Parallel()
	type d struct{ name, key string }
	type a struct{ name, key string }

	pairs, unmatched, _ := pairObjects(
		[]d{{"N", "A"}, {"Q", "B"}, {"N", "B"}},
		[]a{{"N", "B"}, {"N", "A"}},
		identityFuncs[d, a]{
			desiredName: func(x d) string { return x.name },
			actualName:  func(x a) string { return x.name },
			desiredKey:  func(x d) string { return x.key },
			actualKey:   func(x a) string { return x.key },
		},
	)

	got := map[string]string{}
	for _, p := range pairs {
		got[p.Desired.name+"/"+p.Desired.key] = p.Actual.name + "/" + p.Actual.key
		if p.byName {
			t.Errorf("pair %v was matched by name alone; both are exact identity matches", p.Desired)
		}
	}
	for _, want := range [][2]string{{"N/A", "N/A"}, {"N/B", "N/B"}} {
		if got[want[0]] != want[1] {
			t.Errorf("desired %s paired with %q; want %q", want[0], got[want[0]], want[1])
		}
	}
	if len(unmatched) != 1 || unmatched[0].name != "Q" {
		t.Errorf("unmatched = %v; want exactly Q, whose only candidate was legitimately claimed", unmatched)
	}
}

// The Type column is canonically upper-case, but a consumer assembling its own
// records may normalise it. A case difference must not make a declarable object
// invisible to matching and dropping.
func TestDeclarability_TypeComparisonIsCaseInsensitive(t *testing.T) {
	t.Parallel()
	owned := ownedSet("app__T")

	idx := diff(cypher.Config{}, nil, nil, nil, []RemoteIndex{
		{Name: "i", Type: "range", EntityType: "node", LabelsOrTypes: []string{"app__T"}, Properties: []string{"x"}},
	}, owned).Indexes
	wantCounts(t, idx, 0, 0, 0, 1, 0)

	cons := diff(cypher.Config{}, nil, nil, []RemoteConstraint{
		{Name: "c", Type: "uniqueness", EntityType: "node", LabelsOrTypes: []string{"app__T"}, Properties: []string{"id"}},
	}, nil, owned).Constraints
	constraintCounts(t, cons, 0, 0, 0, 1, 0)
}

// Neo4j keeps index and constraint names in ONE namespace, so an index holding
// the name a constraint wants makes the server refuse CREATE CONSTRAINT ... IF NOT
// EXISTS: the diff must report drift naming the holder rather than a create that
// fails and reprints unchanged on every run.
func TestConstraintDiff_IndexNameBlocksCreate(t *testing.T) {
	t.Parallel()
	desired := []cypher.Constraint{
		{Name: "test__Entity_id_unique", Kind: cypher.ConstraintUnique, Label: "test__Entity", Properties: []string{"id"}},
	}
	handMade := func(name string) []RemoteIndex {
		return []RemoteIndex{{
			Name: name, Type: "RANGE", EntityType: "NODE",
			LabelsOrTypes: []string{"test__Entity"}, Properties: []string{"id"},
		}}
	}

	got := diff(cypher.Config{}, desired, nil, nil, handMade("test__Entity_id_unique"), testOwned()).Constraints
	constraintCounts(t, got, 0, 1, 0, 0, 0)
	if len(got.Drift) == 1 {
		// The holder is an index, so it is listed with the indexes and a
		// consumer generating remediation DDL issues DROP INDEX.
		if !slices.Equal(blockerNames(got.Drift[0].Blockers), []string{"i:test__Entity_id_unique"}) {
			t.Fatalf("Blockers = %q; want the blocking index alone", blockerNames(got.Drift[0].Blockers))
		}
		if !strings.Contains(got.Drift[0].Reason, "index") {
			t.Errorf("Reason = %q; want it to say the holder is an index", got.Drift[0].Reason)
		}
	}

	// Passing no indexes is the degraded contract, not a different classification
	// rule: the same declaration is a Create because nothing told the diff the
	// name was taken.
	constraintCounts(t, diff(cypher.Config{}, desired, nil, nil, nil, testOwned()).Constraints, 0, 0, 1, 0, 0)

	// An index that collides on NEITHER name NOR definition leaves the create
	// alone, so the branch turns on a blocker and not on any index being
	// present. Built fresh: assigning through a shared slice header would
	// rewrite the fixture the assertions above used.
	elsewhere := []RemoteIndex{{
		Name: "unrelated_idx", Type: "RANGE", EntityType: "NODE",
		LabelsOrTypes: []string{"test__Entity"}, Properties: []string{"other_prop"},
	}}
	constraintCounts(t, diff(cypher.Config{}, desired, nil, nil, elsewhere, testOwned()).Constraints, 0, 0, 1, 0, 0)

	// A plain index over the SAME label and properties blocks under any name:
	// the server refuses a constraint whose backing index an equivalent index
	// already provides, so reporting a Create would be a plan that repeats
	// unchanged for ever.
	//
	// Mutation: dropping the rangeByDef loop in constraintBlockerFinder.blockers
	// turns this red.
	equivalent := []RemoteIndex{{
		Name: "some_other_name", Type: "RANGE", EntityType: "NODE",
		LabelsOrTypes: []string{"test__Entity"}, Properties: []string{"id"},
	}}
	byDef := diff(cypher.Config{}, desired, nil, nil, equivalent, testOwned()).Constraints
	constraintCounts(t, byDef, 0, 1, 0, 0, 0)
	if len(byDef.Drift) == 1 && !strings.Contains(byDef.Drift[0].Reason, "already serves") {
		t.Errorf("Reason = %q; want it to say an equivalent index already serves the definition", byDef.Drift[0].Reason)
	}

	// A backing index is not an independent blocker: it exists because its own
	// constraint does, and that constraint is matched or dropped on its own.
	backing := []RemoteIndex{{
		Name: "some_other_name", Type: "RANGE", EntityType: "NODE",
		LabelsOrTypes: []string{"test__Entity"}, Properties: []string{"id"},
		OwningConstraint: "test__Entity_id_unique",
	}}
	constraintCounts(t, diff(cypher.Config{}, desired, nil, nil, backing, testOwned()).Constraints, 0, 0, 1, 0, 0)
}

// A remote CONSTRAINT whose type the caller lower-cased must still pair with its
// declaration: the declarability gate folds case, so the identity key must too,
// or the diff drops the constraint enforcing the schema and re-creates it.
func TestConstraintDiff_LowercaseRemoteTypeStillPairs(t *testing.T) {
	t.Parallel()
	desired := []cypher.Constraint{
		{Name: "schema_name", Kind: cypher.ConstraintUnique, Label: "test__Entity", Properties: []string{"id"}},
	}
	actual := []RemoteConstraint{{
		Name: "database_name", Type: "uniqueness", EntityType: "NODE",
		LabelsOrTypes: []string{"test__Entity"}, Properties: []string{"id"},
	}}

	constraintCounts(t, diff(cypher.Config{}, desired, nil, actual, nil, testOwned()).Constraints, 1, 0, 0, 0, 0)
}

// Under the Community edition the generator produces UNIQUE alone, so a NOT NULL
// or TYPE constraint in the database is a kind THIS configuration cannot
// declare. Admitting it makes every one an unmatched orphan, and the plan tells
// the operator to drop every required-property guarantee the database has.
func TestConstraintDiff_CommunityDoesNotDropEnterpriseKinds(t *testing.T) {
	t.Parallel()
	// A database provisioned at the Enterprise default.
	actual := []RemoteConstraint{
		{Name: "u", Type: "UNIQUENESS", EntityType: "NODE", LabelsOrTypes: []string{"test__Entity"}, Properties: []string{"id"}},
		{Name: "nn", Type: "NODE_PROPERTY_EXISTENCE", EntityType: "NODE", LabelsOrTypes: []string{"test__Entity"}, Properties: []string{"name"}},
		{Name: "ty", Type: "NODE_PROPERTY_TYPE", EntityType: "NODE", LabelsOrTypes: []string{"test__Entity"}, Properties: []string{"name"}, PropertyType: "STRING"},
	}
	desired := []cypher.Constraint{
		{Name: "u", Kind: cypher.ConstraintUnique, Label: "test__Entity", Properties: []string{"id"}},
	}

	got := diff(cypher.Config{Edition: cypher.Community}, desired, nil, actual, nil, testOwned()).Constraints
	constraintCounts(t, got, 1, 0, 0, 0, 0)
	if got.Excluded != 2 {
		t.Errorf("Excluded = %d; want the two Enterprise-only constraints counted as not compared", got.Excluded)
	}

	// Control: the same database under the Enterprise default DOES own them, so
	// the assertion above turns on the edition and not on the fixture.
	enterprise := diff(cypher.Config{}, desired, nil, actual, nil, testOwned()).Constraints
	constraintCounts(t, enterprise, 1, 0, 0, 2, 0)
	if enterprise.Excluded != 0 {
		t.Errorf("Excluded = %d under Enterprise; want 0", enterprise.Excluded)
	}
}

// The five buckets say what the diff COMPARED. An object it could not compare —
// on a label no current type declares, which is where a deleted or renamed
// type's constraints end up — must be counted, or "0 to drop" reads as "the
// database is accounted for".
func TestConstraintDiff_ExcludedCountsWhatWasNotCompared(t *testing.T) {
	t.Parallel()
	actual := []RemoteConstraint{
		rc("mine", "app__T", "x"),
		// Left behind by a type since deleted from the schema.
		rc("stale", "app__Publisher", "id"),
		// A relationship constraint: LabelsOrTypes holds a TYPE, not a label.
		{
			Name: "rel", Type: "RELATIONSHIP_PROPERTY_EXISTENCE", EntityType: "RELATIONSHIP",
			LabelsOrTypes: []string{"app__T"}, Properties: []string{"since"},
		},
	}
	got := diff(cypher.Config{}, []cypher.Constraint{dc("mine", "app__T", "x")}, nil, actual, nil, ownedSet("app__T")).Constraints
	constraintCounts(t, got, 1, 0, 0, 0, 0)
	if got.Excluded != 2 {
		t.Errorf("Excluded = %d; want the stale-label and relationship constraints counted", got.Excluded)
	}
}

// A remote CONSTRAINT holding a desired constraint's name is Drift, not Create.
// It is the third producer of ConstraintDiff.Drift and had no test, so
// deleting the branch left the suite green.
//
// Mutation: removing the byName loop in constraintBlockerFinder.blockers turns
// this red.
func TestConstraintDiff_ConstraintNameBlocksCreate(t *testing.T) {
	t.Parallel()
	desired := []cypher.Constraint{{
		Name: "shared_name", Kind: cypher.ConstraintUnique,
		Label: "app__T", Properties: []string{"id"},
	}}
	// The holder is on a label this schema does not own, so it pairs with
	// nothing and only its NAME collides.
	remote := []RemoteConstraint{{
		Name: "shared_name", Type: "UNIQUENESS", EntityType: "NODE",
		LabelsOrTypes: []string{"other__Foo"}, Properties: []string{"id"},
	}}

	got := diff(cypher.Config{}, desired, nil, remote, nil, appOwned()).Constraints
	constraintCounts(t, got, 0, 1, 0, 0, 0)
	if len(got.Drift) == 1 {
		if !strings.Contains(got.Drift[0].Reason, "constraint name") {
			t.Errorf("Reason = %q; want it to name the constraint-name collision", got.Drift[0].Reason)
		}
		if !slices.Equal(blockerNames(got.Drift[0].Blockers), []string{"c:shared_name"}) || got.Drift[0].Blockers.Constraints[0].Type != "UNIQUENESS" {
			t.Errorf("Blockers = %+v; want the holder alone, carrying its own type", got.Drift[0].Blockers)
		}
	}
}

// The blocker message names an owning constraint when the holder is a backing
// index, because DROP INDEX on one is refused by the server — the operator has
// to drop the constraint.
//
// Mutation: removing the OwningConstraint branch in describeIndexBlocker turns
// this red.
func TestDescribeIndexBlocker_NamesTheOwningConstraint(t *testing.T) {
	t.Parallel()
	desired := cypher.Index{
		Name: "app__T_id_unique", Kind: cypher.IndexRange,
		Label: "app__T", Properties: []string{"code"},
	}
	blocker := RemoteIndex{
		Name: "app__T_id_unique", Type: "RANGE", EntityType: "NODE",
		LabelsOrTypes: []string{"app__T"}, Properties: []string{"id"},
		OwningConstraint: "app__T_id_unique",
	}

	msg := describeIndexBlocker(desired, blocker, map[string]struct{}{})
	for _, want := range []string{"backing constraint", "app__T_id_unique"} {
		if !strings.Contains(msg, want) {
			t.Errorf("blocker message %q does not mention %q", msg, want)
		}
	}
}
