//go:build neo4j_integration

package integration

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v6/neo4j"
	n4j "github.com/simon-lentz/yammm/adapter/neo4j"
	"github.com/simon-lentz/yammm/schema"
)

// isEnterprise reports whether the running server supports the Enterprise-only
// constraint kinds, so a Community override skips rather than fails.
func isEnterprise(t *testing.T, ctx context.Context) bool {
	t.Helper()
	for _, rec := range query(t, ctx, "CALL dbms.components() YIELD edition") {
		if edition, _ := rec["edition"].(string); strings.EqualFold(edition, "enterprise") {
			return true
		}
	}
	return false
}

func requireEnterprise(t *testing.T, ctx context.Context) {
	t.Helper()
	if !isEnterprise(t, ctx) {
		t.Skip("Enterprise-only capability; set YAMMM_NEO4J_TEST_IMAGE back to an Enterprise image")
	}
}

// Every constraint kind the adapter can emit must execute. Three of the four —
// NOT NULL, TYPE, and NODE KEY — are Enterprise-only, so on Community this
// assertion covers a quarter of the surface.
func TestAllConstraintKindsExecute(t *testing.T) {
	ctx := context.Background()
	driver(t)
	requireEnterprise(t, ctx)
	dropAll(t, ctx)
	t.Cleanup(func() { dropAll(t, ctx) })

	a := newAdapter(t, ctx, n4j.WithNodeKeyConstraints(true))
	s := loadRoundTripSchema(t)

	constraints, res := a.ConstraintsStructured(ctx, s)
	if res.HasErrors() {
		t.Fatalf("emitting constraints: %v", res.Err())
	}
	for _, c := range constraints {
		run(t, ctx, c.Statement)
	}
	awaitIndexes(t, ctx)

	// Every kind the emitter produced must be present on the server.
	emitted := map[n4j.ConstraintKind]bool{}
	for _, c := range constraints {
		emitted[c.Kind] = true
	}
	live := map[string]bool{}
	for _, rec := range query(t, ctx, "SHOW CONSTRAINTS YIELD type") {
		typ, _ := rec["type"].(string)
		live[typ] = true
	}
	// Each kind is asserted emitted BEFORE it is asserted live. Guarding the
	// server check on emitted[kind] alone would let this test pass vacuously for
	// any kind the adapter stopped emitting — the exact regression it exists to
	// catch would silently remove its own coverage.
	for kind, want := range map[n4j.ConstraintKind]string{
		n4j.ConstraintNotNull: "NODE_PROPERTY_EXISTENCE",
		n4j.ConstraintType:    "NODE_PROPERTY_TYPE",
		n4j.ConstraintNodeKey: "NODE_KEY",
	} {
		if !emitted[kind] {
			t.Errorf("the adapter emitted no %v constraint for this schema; that kind is now unproven against the server", kind)
			continue
		}
		if !live[want] {
			t.Errorf("the adapter emitted %v but no %s constraint exists on the server", kind, want)
		}
	}
	if !emitted[n4j.ConstraintNodeKey] {
		t.Error("WithNodeKeyConstraints(true) emitted no NODE KEY constraint")
	}
	t.Logf("constraint kinds live on the server: %v", slices.Sorted(maps.Keys(live)))
}

// A TYPE constraint's propertyType must be reported, and must match what the
// adapter emitted — otherwise every TYPE constraint lands in Unverified and
// `yammm neo4j diff` exits 3 forever. This is the column that does not exist
// before Neo4j 5.9 and cannot be exercised on Community at all.
func TestTypeConstraint_PropertyTypeRoundTrips(t *testing.T) {
	ctx := context.Background()
	driver(t)
	requireEnterprise(t, ctx)
	dropAll(t, ctx)
	t.Cleanup(func() { dropAll(t, ctx) })

	a := newAdapter(t, ctx)
	s := loadRoundTripSchema(t)
	applySchemaDDL(t, ctx, a, s)

	desired, _ := a.ConstraintsStructured(ctx, s)
	var wantType int
	for _, c := range desired {
		if c.Kind == n4j.ConstraintType {
			wantType++
		}
	}
	if wantType == 0 {
		t.Fatal("the fixture emitted no TYPE constraints; this test would prove nothing")
	}

	records := query(t, ctx, n4j.IntrospectConstraintsQuery())
	parsed, err := n4j.ParseRemoteConstraints(records)
	if err != nil {
		t.Fatalf("ParseRemoteConstraints: %v", err)
	}
	var withPropertyType int
	for _, rc := range parsed {
		if rc.Type == "NODE_PROPERTY_TYPE" {
			if rc.PropertyType == "" {
				t.Errorf("constraint %q is NODE_PROPERTY_TYPE but parsed PropertyType empty; it would be Unverified", rc.Name)
				continue
			}
			withPropertyType++
		}
	}
	if withPropertyType != wantType {
		t.Errorf("%d of %d TYPE constraints reported a propertyType", withPropertyType, wantType)
	}

	// And they must compare equal, not drift.
	diff := a.DiffConstraints(desired, parsed, a.OwnedLabels(ctx, s))
	if len(diff.Unverified) != 0 {
		for _, u := range diff.Unverified {
			t.Errorf("unverified: %s (%s)", u.Desired.Name, u.Reason)
		}
	}
	for _, d := range diff.Drift {
		t.Errorf("drift on a constraint the adapter itself applied: %s (%s)", d.Desired.Name, d.Reason)
	}
}

// A TYPE constraint whose enforced type differs from the schema's must be
// reported as drift, with the server's own type expression in the reason. This
// is the only drift the constraint diff can produce from real data.
func TestTypeConstraint_DriftIsDetected(t *testing.T) {
	ctx := context.Background()
	driver(t)
	requireEnterprise(t, ctx)
	dropAll(t, ctx)
	t.Cleanup(func() { dropAll(t, ctx) })

	a := newAdapter(t, ctx)
	s := loadRoundTripSchema(t)
	desired, _ := a.ConstraintsStructured(ctx, s)

	// Find a TYPE constraint and apply a deliberately different one under the
	// same label and property.
	var target n4j.Constraint
	for _, c := range desired {
		if c.Kind == n4j.ConstraintType && c.TypeExpr != "STRING" && len(c.Properties) == 1 {
			target = c
			break
		}
	}
	if target.Name == "" {
		t.Skip("no single-property non-STRING TYPE constraint in the fixture")
	}
	run(t, ctx, fmt.Sprintf(
		"CREATE CONSTRAINT %s IF NOT EXISTS FOR (n:%s) REQUIRE n.%s IS :: STRING",
		target.Name, target.Label, target.Properties[0],
	))

	parsed, err := n4j.ParseRemoteConstraints(query(t, ctx, n4j.IntrospectConstraintsQuery()))
	if err != nil {
		t.Fatalf("ParseRemoteConstraints: %v", err)
	}
	diff := a.DiffConstraints([]n4j.Constraint{target}, parsed, a.OwnedLabels(ctx, s))
	if len(diff.Drift) != 1 {
		t.Fatalf("drift = %d, want 1 (schema declares %s, database enforces STRING)", len(diff.Drift), target.TypeExpr)
	}
	t.Logf("drift reason: %s", diff.Drift[0].Reason)
	if !strings.Contains(diff.Drift[0].Reason, target.TypeExpr) {
		t.Errorf("drift reason %q does not name the schema's type %q", diff.Drift[0].Reason, target.TypeExpr)
	}
}

// A caller whose own introspection omits propertyType gets Unverified, not a
// false Match. This is the reachable trigger for the constraint Unverified
// bucket: a server old enough to lack the column is also too old to hold the
// constraint, so the bucket only ever fills from a caller's query.
func TestTypeConstraint_MissingPropertyTypeIsUnverified(t *testing.T) {
	ctx := context.Background()
	driver(t)
	requireEnterprise(t, ctx)
	dropAll(t, ctx)
	t.Cleanup(func() { dropAll(t, ctx) })

	a := newAdapter(t, ctx)
	s := loadRoundTripSchema(t)
	applySchemaDDL(t, ctx, a, s)

	desired, _ := a.ConstraintsStructured(ctx, s)
	var typeConstraints []n4j.Constraint
	for _, c := range desired {
		if c.Kind == n4j.ConstraintType {
			typeConstraints = append(typeConstraints, c)
		}
	}
	if len(typeConstraints) == 0 {
		t.Skip("no TYPE constraints in the fixture")
	}

	// A caller's own query that does not yield propertyType.
	records := query(t, ctx,
		"SHOW CONSTRAINTS YIELD name, type, entityType, labelsOrTypes, properties, createStatement")
	parsed, err := n4j.ParseRemoteConstraints(records)
	if err != nil {
		t.Fatalf("ParseRemoteConstraints: %v", err)
	}

	diff := a.DiffConstraints(typeConstraints, parsed, a.OwnedLabels(ctx, s))
	if len(diff.Unverified) != len(typeConstraints) {
		t.Errorf("unverified = %d, want %d — an uncompared constraint must not read as matched",
			len(diff.Unverified), len(typeConstraints))
	}
	if len(diff.Match) != 0 {
		t.Errorf("matched %d constraints whose type was never compared", len(diff.Match))
	}
}

// Index kinds the DSL cannot express must not be reported as drops: no schema
// edit could resolve one, so the diff would never converge.
func TestUndeclarableIndexKinds_AreNotDropped(t *testing.T) {
	ctx := context.Background()
	driver(t)
	dropAll(t, ctx)
	t.Cleanup(func() { dropAll(t, ctx) })

	a := newAdapter(t, ctx)
	s := loadRoundTripSchema(t)
	applySchemaDDL(t, ctx, a, s)

	// Hand-create kinds the schema has no vocabulary for, on a label it owns.
	run(t, ctx, "CREATE FULLTEXT INDEX rt_ft IF NOT EXISTS FOR (n:rt__Document) ON EACH [n.title]")
	run(t, ctx, "CREATE TEXT INDEX rt_text IF NOT EXISTS FOR (n:rt__Document) ON (n.state)")
	run(t, ctx, "CREATE POINT INDEX rt_point IF NOT EXISTS FOR (n:rt__Document) ON (n.location)")
	awaitIndexes(t, ctx)

	desired, _ := a.IndexesStructured(ctx, s)
	parsed, err := n4j.ParseRemoteIndexes(query(t, ctx, n4j.IntrospectIndexesQuery()))
	if err != nil {
		t.Fatalf("ParseRemoteIndexes: %v", err)
	}
	diff := a.DiffIndexes(desired, parsed, a.OwnedLabels(ctx, s))
	for _, d := range diff.Drop {
		t.Errorf("index %q (%s) reported as a drop; the schema cannot declare that kind", d.Name, d.Type)
	}
	if len(diff.Match) != len(desired) {
		t.Errorf("matched %d of %d declared indexes alongside the undeclarable ones", len(diff.Match), len(desired))
	}
}

// The premise behind reporting a name-blocked index as drift is that
// CREATE ... IF NOT EXISTS matches on NAME, so creating over a name another
// index kind holds does not produce the declared index. Assert what the server
// actually does — the branch's reason text depends on it.
func TestNameHeldByAnotherKind_ServerBehavior(t *testing.T) {
	ctx := context.Background()
	driver(t)
	dropAll(t, ctx)
	t.Cleanup(func() { dropAll(t, ctx) })

	a := newAdapter(t, ctx)
	s := loadRoundTripSchema(t)
	desired, res := a.IndexesStructured(ctx, s)
	if res.HasErrors() {
		t.Fatalf("emitting indexes: %v", res.Err())
	}
	var rangeIdx n4j.Index
	for _, d := range desired {
		if d.Kind == n4j.IndexRange && len(d.Properties) == 1 {
			rangeIdx = d
			break
		}
	}
	if rangeIdx.Name == "" {
		t.Fatal("no single-property range index in the fixture")
	}

	// Take the name with a FULLTEXT index.
	run(t, ctx, fmt.Sprintf("CREATE FULLTEXT INDEX %s IF NOT EXISTS FOR (n:%s) ON EACH [n.%s]",
		rangeIdx.Name, rangeIdx.Label, rangeIdx.Properties[0]))
	awaitIndexes(t, ctx)

	// Now try the adapter's own statement. Record whether the server errors or
	// silently no-ops; either way the declared index must not exist afterwards.
	_, err := neo4jdriver.ExecuteQuery(ctx, driver(t), rangeIdx.Statement, nil,
		neo4jdriver.EagerResultTransformer)
	if err != nil {
		t.Logf("server REJECTS a CREATE over a name another kind holds: %v", err)
	} else {
		t.Log("server silently accepts the CREATE (IF NOT EXISTS matched on name)")
	}

	var kind string
	for _, rec := range query(t, ctx, "SHOW INDEXES YIELD name, type") {
		if n, _ := rec["name"].(string); n == rangeIdx.Name {
			kind, _ = rec["type"].(string)
		}
	}
	if kind != "FULLTEXT" {
		t.Errorf("index %q is now %q; the FULLTEXT holder should still own the name", rangeIdx.Name, kind)
	}

	// Whichever the server does, the diff must report it as actionable drift
	// rather than a Create that cannot succeed.
	parsed, err := n4j.ParseRemoteIndexes(query(t, ctx, n4j.IntrospectIndexesQuery()))
	if err != nil {
		t.Fatalf("ParseRemoteIndexes: %v", err)
	}
	diff := a.DiffIndexes([]n4j.Index{rangeIdx}, parsed, a.OwnedLabels(ctx, s))
	if len(diff.Create) != 0 {
		t.Errorf("reported %d Create(s) for a name the database already holds; the plan cannot converge", len(diff.Create))
	}
	if len(diff.Drift) != 1 {
		t.Fatalf("drift = %d, want 1", len(diff.Drift))
	}
	t.Logf("drift reason: %s", diff.Drift[0].Reason)
}

// Relationship indexes and relationship constraints live in a different
// namespace: LabelsOrTypes holds a relationship TYPE, not a label. Neither may
// be claimed as a schema-owned orphan.
func TestRelationshipObjects_AreNotClaimed(t *testing.T) {
	ctx := context.Background()
	driver(t)
	requireEnterprise(t, ctx)
	dropAll(t, ctx)
	t.Cleanup(func() { dropAll(t, ctx) })

	a := newAdapter(t, ctx)
	s := loadRoundTripSchema(t)
	applySchemaDDL(t, ctx, a, s)

	// A relationship type deliberately spelled like a label this schema owns.
	run(t, ctx, "CREATE INDEX rt_rel_idx IF NOT EXISTS FOR ()-[r:rt__Document]-() ON (r.since)")
	run(t, ctx, "CREATE CONSTRAINT rt_rel_exists IF NOT EXISTS FOR ()-[r:rt__Document]-() REQUIRE r.since IS NOT NULL")
	awaitIndexes(t, ctx)

	owned := a.OwnedLabels(ctx, s)

	desiredIdx, _ := a.IndexesStructured(ctx, s)
	parsedIdx, err := n4j.ParseRemoteIndexes(query(t, ctx, n4j.IntrospectIndexesQuery()))
	if err != nil {
		t.Fatalf("ParseRemoteIndexes: %v", err)
	}
	for _, d := range a.DiffIndexes(desiredIdx, parsedIdx, owned).Drop {
		if d.EntityType == "RELATIONSHIP" {
			t.Errorf("relationship index %q reported as a drop", d.Name)
		}
	}

	desiredCons, _ := a.ConstraintsStructured(ctx, s)
	parsedCons, err := n4j.ParseRemoteConstraints(query(t, ctx, n4j.IntrospectConstraintsQuery()))
	if err != nil {
		t.Fatalf("ParseRemoteConstraints: %v", err)
	}
	for _, d := range a.DiffConstraints(desiredCons, parsedCons, owned).Drop {
		if d.EntityType == "RELATIONSHIP" {
			t.Errorf("relationship constraint %q reported as a drop", d.Name)
		}
	}
}

// Node and relationship indexes occupy SEPARATE namespaces, so a relationship
// index over the same property list as a declared node index does not stop the
// server creating that node index. Blocking a node CREATE on it would emit no
// statement, exit non-zero forever, and tell the operator to drop an unrelated
// relationship index. The schema DDL is deliberately not applied here: the node
// index has to be genuinely absent for the classification to be a Create.
func TestRelationshipIndex_DoesNotBlockNodeIndexCreate(t *testing.T) {
	ctx := context.Background()
	driver(t)
	dropAll(t, ctx)
	t.Cleanup(func() { dropAll(t, ctx) })

	a := newAdapter(t, ctx)
	s := loadRoundTripSchema(t)

	// `state` is a property the schema declares a node @index on, and the
	// relationship type is spelled exactly like the label that index sits on.
	run(t, ctx, "CREATE INDEX rt_rel_state_idx IF NOT EXISTS FOR ()-[r:rt__Document]-() ON (r.state)")
	awaitIndexes(t, ctx)

	desired, res := a.IndexesStructured(ctx, s)
	if res.HasErrors() {
		t.Fatalf("emitting indexes: %v", res.Err())
	}
	parsed, err := n4j.ParseRemoteIndexes(query(t, ctx, n4j.IntrospectIndexesQuery()))
	if err != nil {
		t.Fatalf("ParseRemoteIndexes: %v", err)
	}

	diff := a.DiffIndexes(desired, parsed, a.OwnedLabels(ctx, s))
	var created bool
	for _, c := range diff.Create {
		if c.Label == "rt__Document" && slices.Equal(c.Properties, []string{"state"}) {
			created = true
		}
	}
	if !created {
		t.Errorf("the declared node index on rt__Document(state) was not a Create; drift=%d create=%d",
			len(diff.Drift), len(diff.Create))
		for _, d := range diff.Drift {
			t.Logf("drift: %s — %s", d.Desired.Name, d.Reason)
		}
	}
	// The precondition: the relationship index really is present and really does
	// carry the same label spelling and property list.
	var sawRel bool
	for _, ri := range parsed {
		if ri.Name == "rt_rel_state_idx" && ri.EntityType == "RELATIONSHIP" {
			sawRel = true
		}
	}
	if !sawRel {
		t.Error("the relationship index was not introspected; this test proved nothing")
	}
}

// An undeclared index on a label the schema owns IS the drift the feature
// exists to surface, so the filters above must not have made everything
// unreportable.
func TestUndeclaredSchemaOwnedIndex_IsDropped(t *testing.T) {
	ctx := context.Background()
	driver(t)
	dropAll(t, ctx)
	t.Cleanup(func() { dropAll(t, ctx) })

	a := newAdapter(t, ctx)
	s := loadRoundTripSchema(t)
	applySchemaDDL(t, ctx, a, s)

	run(t, ctx, "CREATE INDEX rt_adhoc IF NOT EXISTS FOR (n:rt__Document) ON (n.title)")
	awaitIndexes(t, ctx)

	desired, _ := a.IndexesStructured(ctx, s)
	parsed, err := n4j.ParseRemoteIndexes(query(t, ctx, n4j.IntrospectIndexesQuery()))
	if err != nil {
		t.Fatalf("ParseRemoteIndexes: %v", err)
	}
	diff := a.DiffIndexes(desired, parsed, a.OwnedLabels(ctx, s))
	if len(diff.Drop) != 1 || diff.Drop[0].Name != "rt_adhoc" {
		t.Errorf("drops = %d; want exactly the undeclared rt_adhoc", len(diff.Drop))
		for _, d := range diff.Drop {
			t.Logf("  drop: %s", d.Name)
		}
	}
}

// A second schema's objects in the same database must never be claimed.
// Ownership is set membership in the labels this schema emits, so a sibling
// whose name merely shares a prefix, extends an owned label, or differs only in
// case is outside the set by construction.
func TestSiblingSchemaInSameDatabase_IsNotClaimed(t *testing.T) {
	ctx := context.Background()
	driver(t)
	dropAll(t, ctx)
	t.Cleanup(func() { dropAll(t, ctx) })

	a := newAdapter(t, ctx)
	s := loadRoundTripSchema(t)
	applySchemaDDL(t, ctx, a, s)

	// Labels a sibling schema would emit: one lowercase-leading segment, one
	// uppercase-leading, and one that extends a label this schema owns.
	for i, label := range []string{"rt__legacy__Foo", "rt__Core__Widget", "rt__Document__Nested"} {
		run(t, ctx, fmt.Sprintf("CREATE INDEX rt_sibling_%d IF NOT EXISTS FOR (n:%s) ON (n.x)", i, label))
	}
	awaitIndexes(t, ctx)

	desired, _ := a.IndexesStructured(ctx, s)
	parsed, err := n4j.ParseRemoteIndexes(query(t, ctx, n4j.IntrospectIndexesQuery()))
	if err != nil {
		t.Fatalf("ParseRemoteIndexes: %v", err)
	}
	diff := a.DiffIndexes(desired, parsed, a.OwnedLabels(ctx, s))
	for _, d := range diff.Drop {
		t.Errorf("index %q on %v belongs to another schema and was reported as a drop", d.Name, d.LabelsOrTypes)
	}
}

// A node index reports exactly one label. The ownership code picks the owned
// label out of LabelsOrTypes, which only matters if a server can report more
// than one — record what it actually does.
func TestNodeIndex_ReportsExactlyOneLabel(t *testing.T) {
	ctx := context.Background()
	driver(t)
	dropAll(t, ctx)
	t.Cleanup(func() { dropAll(t, ctx) })

	a := newAdapter(t, ctx)
	s := loadRoundTripSchema(t)
	applySchemaDDL(t, ctx, a, s)

	// A multi-label node index, so the single-label assertion below is falsifiable.
	// Labelling a NODE with two labels does not produce one — only a FULLTEXT
	// index spans labels — so without this the loop could only ever see the
	// single-label indexes applySchemaDDL just made, and would pass whatever the
	// server did.
	run(t, ctx, "CREATE FULLTEXT INDEX rt_multi_ft IF NOT EXISTS FOR (n:rt__Document|rt__Note) ON EACH [n.title]")
	awaitIndexes(t, ctx)

	var sawMulti bool
	for _, rec := range query(t, ctx, n4j.IntrospectIndexesQuery()) {
		labels, _ := rec["labelsOrTypes"].([]any)
		entity, _ := rec["entityType"].(string)
		typ, _ := rec["type"].(string)
		if entity != "NODE" {
			continue
		}
		if typ == "FULLTEXT" {
			if len(labels) > 1 {
				sawMulti = true
			}
			continue
		}
		// The kinds the adapter itself emits report exactly one label, which is
		// what keeps a single owned label sufficient to scope them.
		if len(labels) != 1 {
			t.Errorf("%s index %v reports %d labels: %v", typ, rec["name"], len(labels), labels)
		}
	}
	if !sawMulti {
		t.Error("no multi-label index was observed; the single-label assertion above proved nothing")
	}
}

// A caller that introspects with `SHOW INDEXES YIELD *` — the obvious query,
// and the one the adapter's own filter is written to defend against — must not
// be told to drop the index behind a primary key.
func TestYieldStarCaller_ConstraintIndexesNotDropped(t *testing.T) {
	ctx := context.Background()
	driver(t)
	dropAll(t, ctx)
	t.Cleanup(func() { dropAll(t, ctx) })

	a := newAdapter(t, ctx)
	s := loadRoundTripSchema(t)
	applySchemaDDL(t, ctx, a, s)

	records := query(t, ctx, "SHOW INDEXES YIELD *")
	parsed, err := n4j.ParseRemoteIndexes(records)
	if err != nil {
		t.Fatalf("ParseRemoteIndexes on YIELD * records: %v", err)
	}
	var backing int
	for _, ri := range parsed {
		if ri.OwningConstraint != "" {
			backing++
		}
	}
	if backing == 0 {
		t.Fatal("YIELD * produced no constraint-backing index; the assertion would be vacuous")
	}

	desired, _ := a.IndexesStructured(ctx, s)
	diff := a.DiffIndexes(desired, parsed, a.OwnedLabels(ctx, s))
	for _, d := range diff.Drop {
		t.Errorf("YIELD * caller told to drop %q (owningConstraint=%q)", d.Name, d.OwningConstraint)
	}
	if len(diff.Match) != len(desired) {
		t.Errorf("matched %d of %d declared indexes from YIELD * records", len(diff.Match), len(desired))
	}
}

// A POPULATING index is classified as unverified rather than matched: it exists
// with a matching definition but serves no queries yet. Provoked by indexing a
// property across enough nodes that population is observable.
func TestPopulatingIndex_IsUnverified(t *testing.T) {
	ctx := context.Background()
	driver(t)
	dropAll(t, ctx)
	t.Cleanup(func() {
		run(t, ctx, "MATCH (n:pop__Bulk) DETACH DELETE n")
		dropAll(t, ctx)
	})

	run(t, ctx, "MATCH (n:pop__Bulk) DETACH DELETE n")
	run(t, ctx, "UNWIND range(1, 200000) AS i CREATE (:pop__Bulk {v: toString(i)})")

	const name = "pop__Bulk_v_idx"
	run(t, ctx, fmt.Sprintf("CREATE INDEX %s IF NOT EXISTS FOR (n:pop__Bulk) ON (n.v)", name))

	a := newAdapter(t, ctx)
	desired := []n4j.Index{{
		Name: name, Kind: n4j.IndexRange, Label: "pop__Bulk", Properties: []string{"v"},
	}}
	owned := ownedLabelsFor(t, ctx, a, "pop__Bulk")

	// Poll briefly for the POPULATING window, pacing the queries: issuing them
	// back to back competes with the population this loop is trying to observe,
	// and on a state the loop does not recognise it would hammer the container
	// for the whole deadline.
	deadline := time.Now().Add(30 * time.Second)
	var sawPopulating bool
	for time.Now().Before(deadline) {
		parsed, err := n4j.ParseRemoteIndexes(query(t, ctx, n4j.IntrospectIndexesQuery()))
		if err != nil {
			t.Fatalf("ParseRemoteIndexes: %v", err)
		}
		var state string
		for _, ri := range parsed {
			if ri.Name == name {
				state = ri.State
			}
		}
		if strings.EqualFold(state, "POPULATING") {
			sawPopulating = true
			diff := a.DiffIndexes(desired, parsed, owned)
			if len(diff.Unverified) != 1 {
				t.Errorf("a POPULATING index gave unverified=%d match=%d drift=%d; want unverified=1",
					len(diff.Unverified), len(diff.Match), len(diff.Drift))
			} else {
				t.Logf("POPULATING classified as unverified: %s", diff.Unverified[0].Reason)
			}
			break
		}
		// Any state other than POPULATING ends the watch: ONLINE means the window
		// closed, FAILED and an unreported state mean it will never open.
		if state != "" && !strings.EqualFold(state, "POPULATING") {
			t.Logf("index %q reached state %q without an observed POPULATING window", name, state)
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	// Missing the POPULATING window costs only the assertion that depends on it.
	// Skipping here would also discard the ONLINE check below, which does not
	// depend on the window at all — a machine fast enough to miss POPULATING would
	// silently stop testing the classification that always applies.
	if !sawPopulating {
		t.Log("the index came online too fast to observe POPULATING on this machine; the ONLINE assertion still runs")
	}

	// Once online it must match.
	awaitIndexes(t, ctx)
	parsed, err := n4j.ParseRemoteIndexes(query(t, ctx, n4j.IntrospectIndexesQuery()))
	if err != nil {
		t.Fatalf("ParseRemoteIndexes: %v", err)
	}
	if diff := a.DiffIndexes(desired, parsed, owned); len(diff.Match) != 1 {
		t.Errorf("once ONLINE the index should match; got match=%d unverified=%d",
			len(diff.Match), len(diff.Unverified))
	}
}

// ownedLabelsFor builds a label set for a synthetic label by constructing a
// throwaway schema whose emitted label is the one wanted.
func ownedLabelsFor(t *testing.T, ctx context.Context, a *n4j.Adapter, label string) *n4j.OwnedLabels {
	t.Helper()
	schemaName, typeName, found := strings.Cut(label, "__")
	if !found {
		t.Fatalf("label %q is not schema__Type shaped", label)
	}
	s, res := schema.NewBuilder().
		WithName(schemaName).
		AddType(typeName).
		WithPrimaryKey("id", schema.NewStringConstraint()).
		Done().
		Build()
	if res.HasErrors() {
		t.Fatalf("building a schema for label %q: %v", label, res.Err())
	}
	owned := a.OwnedLabels(ctx, s)
	if !owned.Contains(label) {
		t.Fatalf("constructed label set does not contain %q", label)
	}
	return owned
}

// Index names are GLOBAL: a name held by an index on a label this schema does
// not own still makes the emitted CREATE a silent no-op. Verified directly —
// creating the same name on a second label leaves the first one in place.
func TestNameHeldOnForeignLabel_BlocksCreate(t *testing.T) {
	ctx := context.Background()
	driver(t)
	dropAll(t, ctx)
	t.Cleanup(func() { dropAll(t, ctx) })

	a := newAdapter(t, ctx)
	s := loadRoundTripSchema(t)
	desired, res := a.IndexesStructured(ctx, s)
	if res.HasErrors() {
		t.Fatalf("emitting indexes: %v", res.Err())
	}
	var target n4j.Index
	for _, d := range desired {
		if d.Kind == n4j.IndexRange && len(d.Properties) == 1 {
			target = d
			break
		}
	}
	if target.Name == "" {
		t.Fatal("no single-property range index in the fixture")
	}

	// Take the name on a label this schema does not own.
	run(t, ctx, fmt.Sprintf("CREATE INDEX %s IF NOT EXISTS FOR (n:foreign__Thing) ON (n.x)", target.Name))
	awaitIndexes(t, ctx)

	// The server accepts the adapter's CREATE and does nothing.
	run(t, ctx, target.Statement)
	for _, rec := range query(t, ctx, "SHOW INDEXES YIELD name, labelsOrTypes") {
		if n, _ := rec["name"].(string); n == target.Name {
			labels, _ := rec["labelsOrTypes"].([]any)
			if len(labels) != 1 || labels[0] != "foreign__Thing" {
				t.Errorf("index %q is on %v; the foreign holder should still own the name", n, labels)
			}
		}
	}

	parsed, err := n4j.ParseRemoteIndexes(query(t, ctx, n4j.IntrospectIndexesQuery()))
	if err != nil {
		t.Fatalf("ParseRemoteIndexes: %v", err)
	}
	diff := a.DiffIndexes([]n4j.Index{target}, parsed, a.OwnedLabels(ctx, s))
	if len(diff.Create) != 0 {
		t.Errorf("reported %d Create(s) for a name a foreign label already holds; the plan cannot converge", len(diff.Create))
	}
	if len(diff.Drift) != 1 {
		t.Fatalf("drift = %d, want 1", len(diff.Drift))
	}
	t.Logf("drift reason: %s", diff.Drift[0].Reason)
}

// A declared index over a (label, properties) a constraint's backing index
// already serves is never created by the server, even under a different name.
// @@index over a primary key is explicitly legal in the DSL, so this is
// reachable from a valid schema — and the backing index does the work the
// declaration asked for, so the declaration is SATISFIED.
//
// It must be neither a Create (the server would never honour it) nor a Drift
// (nothing could clear it: dropping the backing index means dropping the primary
// key, so a deploy gate keyed on a clean diff would never pass on a valid,
// fully-applied schema).
func TestDefinitionServedByConstraintIndex_IsSatisfied(t *testing.T) {
	ctx := context.Background()
	driver(t)
	dropAll(t, ctx)
	t.Cleanup(func() { dropAll(t, ctx) })

	s, res := schema.LoadString(ctx, `schema "pk"
type Doc { doc_id String primary
           @@index(doc_id) }
`, "pk.yammm")
	if res.HasErrors() {
		t.Fatalf("load: %v", res.Err())
	}

	a := newAdapter(t, ctx)
	applySchemaDDL(t, ctx, a, s)

	desired, _ := a.IndexesStructured(ctx, s)
	if len(desired) != 1 {
		t.Fatalf("expected the one @@index, got %d", len(desired))
	}
	// The server did not create it: the constraint's backing index serves it.
	var exists bool
	for _, rec := range query(t, ctx, "SHOW INDEXES YIELD name") {
		if n, _ := rec["name"].(string); n == desired[0].Name {
			exists = true
		}
	}
	if exists {
		t.Skip("this server did create a separate index over the constrained pair; the blocker does not apply")
	}

	parsed, err := n4j.ParseRemoteIndexes(query(t, ctx, n4j.IntrospectIndexesQuery()))
	if err != nil {
		t.Fatalf("ParseRemoteIndexes: %v", err)
	}
	diff := a.DiffIndexes(desired, parsed, a.OwnedLabels(ctx, s))
	if len(diff.Create) != 0 {
		t.Errorf("reported a Create the server will never honour: %+v", diff.Create)
	}
	if len(diff.Drift) != 0 {
		t.Errorf("reported unclearable drift on a valid, fully-applied schema: %+v", diff.Drift)
	}
	if len(diff.Match) != 1 {
		t.Fatalf("match = %d, want the declaration satisfied by the backing index", len(diff.Match))
	}
	if diff.Match[0].Actual.OwningConstraint == "" {
		t.Error("the matched index should be the constraint's backing index")
	}
}

// Two types whose label and property names are underscore-ambiguous emit
// colliding constraint names. Both constraints must reach the database, or the
// second is silently skipped and the guarantee it encodes is simply absent.
func TestConstraintNameCollision_BothAreEnforced(t *testing.T) {
	ctx := context.Background()
	driver(t)
	requireEnterprise(t, ctx)
	dropAll(t, ctx)
	t.Cleanup(func() { dropAll(t, ctx) })

	s, res := schema.LoadString(ctx, `schema "cc"
type Item { item_id String primary
            a_b String required }
type Item_a { item_a_id String primary
              b String required }
`, "cc.yammm")
	if res.HasErrors() {
		t.Fatalf("load: %v", res.Err())
	}

	a := newAdapter(t, ctx)
	constraints, cRes := a.ConstraintsStructured(ctx, s)
	if cRes.HasErrors() {
		t.Fatalf("emitting constraints: %v", cRes.Err())
	}
	names := map[string]int{}
	for _, c := range constraints {
		names[c.Name]++
	}
	for n, k := range names {
		if k > 1 {
			t.Errorf("constraint name %q emitted %d times; the second CREATE would be skipped", n, k)
		}
	}

	for _, c := range constraints {
		run(t, ctx, c.Statement)
	}
	awaitIndexes(t, ctx)

	live := len(query(t, ctx, "SHOW CONSTRAINTS YIELD name"))
	if live != len(constraints) {
		t.Errorf("%d of %d constraints reached the database", live, len(constraints))
	}

	// And the NOT NULL guarantee on the second type is actually enforced.
	_, err := neo4jdriver.ExecuteQuery(ctx, driver(t),
		"CREATE (n:cc__Item_a {item_a_id: 'x'})", nil, neo4jdriver.EagerResultTransformer)
	if err == nil {
		t.Error("a node missing the required property b was accepted; Item_a.b is not NOT NULL-enforced")
	}
	t.Cleanup(func() { run(t, ctx, "MATCH (n:cc__Item_a) DETACH DELETE n") })
}
