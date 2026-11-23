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
	"github.com/simon-lentz/yammm/diag"
	n4j "github.com/simon-lentz/yammm/neo4j"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/schema/cypher"
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

// Every constraint kind the library can emit must execute. Three of the four —
// NOT NULL, TYPE, and NODE KEY — are Enterprise-only, so on Community this
// assertion covers a quarter of the surface.
func TestAllConstraintKindsExecute(t *testing.T) {
	ctx := context.Background()
	driver(t)
	requireEnterprise(t, ctx)
	dropAll(t, ctx)
	t.Cleanup(func() { dropAll(t, ctx) })

	a := newConfig(t, ctx, cypher.Config{NodeKeys: true})
	s := loadRoundTripSchema(t)

	constraints, res := a.Constraints(s)
	if res != nil {
		t.Fatalf("emitting constraints: %v", res)
	}
	for _, c := range constraints {
		run(t, ctx, c.Statement)
	}
	awaitIndexes(t, ctx)

	// Every kind the emitter produced must be present on the server.
	emitted := map[cypher.ConstraintKind]bool{}
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
	// any kind the library stopped emitting — the exact regression it exists to
	// catch would silently remove its own coverage.
	for kind, want := range map[cypher.ConstraintKind]string{
		cypher.ConstraintNotNull: "NODE_PROPERTY_EXISTENCE",
		cypher.ConstraintType:    "NODE_PROPERTY_TYPE",
		cypher.ConstraintNodeKey: "NODE_KEY",
	} {
		if !emitted[kind] {
			t.Errorf("the library emitted no %v constraint for this schema; that kind is now unproven against the server", kind)
			continue
		}
		if !live[want] {
			t.Errorf("the library emitted %v but no %s constraint exists on the server", kind, want)
		}
	}
	if !emitted[cypher.ConstraintNodeKey] {
		t.Error("NodeKeys emitted no NODE KEY constraint")
	}
	t.Logf("constraint kinds live on the server: %v", slices.Sorted(maps.Keys(live)))
}

// TestNodeKeyRequest_EnforcesPrimaryKeyOnEitherEdition runs on BOTH editions by
// design, and is the only capabilities test that does.
//
// NodeKeys asks for a kind Community cannot hold. The
// invariant that must survive the request either way is that the primary key is
// ENFORCED — as NODE KEY where the edition affords it, as UNIQUE where it does
// not. It regressed precisely because every existing NODE KEY assertion sits
// behind requireEnterprise: the library emitted a NODE KEY, the edition filter
// discarded it as undeclarable, the primary key's NOT NULL had already been
// skipped as covered by it, and a Community user who asked for STRONGER keys got
// no primary-key constraint at all. Nothing skipped here, so nothing hides that.
//
// Executing the statements is half the value: on Community the server itself
// rejects a CREATE CONSTRAINT ... IS NODE KEY, so a regression that reinstated
// the un-degraded kind would fail here even if the emitter's own assertions were
// somehow satisfied.
func TestNodeKeyRequest_EnforcesPrimaryKeyOnEitherEdition(t *testing.T) {
	ctx := context.Background()
	driver(t)
	dropAll(t, ctx)
	t.Cleanup(func() { dropAll(t, ctx) })

	enterprise := isEnterprise(t, ctx)
	a := newConfig(t, ctx, cypher.Config{NodeKeys: true})
	s := loadRoundTripSchema(t)

	var warnings []diag.Issue
	constraints, err := a.Constraints(s, cypher.WithWarnings(func(w diag.Issue) { warnings = append(warnings, w) }))
	if err != nil {
		t.Fatalf("emitting constraints: %v", err)
	}

	// The warning is the Community half's other half: the substitution must be
	// announced there and stay silent on Enterprise.
	gotWarning := slices.ContainsFunc(warnings, func(w diag.Issue) bool { return w.Code() == cypher.W_NEO4J_NODE_KEY_UNSUPPORTED })
	switch {
	case enterprise && gotWarning:
		t.Errorf("Enterprise honored the request but still warned: %v", warnings)
	case !enterprise && !gotWarning:
		t.Errorf("Community substituted UNIQUE for NODE KEY without warning: %v", warnings)
	}

	wantKind := cypher.ConstraintNodeKey
	if !enterprise {
		wantKind = cypher.ConstraintUnique
	}

	var emittedPK int
	for _, c := range constraints {
		if c.Kind == wantKind {
			emittedPK++
		}
		if !enterprise && c.Kind == cypher.ConstraintNodeKey {
			t.Errorf("Community emitted a NODE KEY the edition cannot hold: %s", c.Statement)
		}
		run(t, ctx, c.Statement)
	}
	awaitIndexes(t, ctx)

	// Asserted emitted before asserted live, so a regression that stops emitting
	// cannot pass this vacuously.
	if emittedPK == 0 {
		t.Fatalf("no %v constraint emitted for the schema's primary keys; primary keys are unenforced", wantKind)
	}

	// Liveness is asserted through the library's own diff rather than against a
	// hand-written server type name. The server's spelling follows the Cypher
	// language (a uniqueness constraint reports UNIQUENESS under CYPHER 5 and
	// NODE_PROPERTY_UNIQUENESS under CYPHER 25) and the library already canonicalises it;
	// re-encoding that knowledge here would make the test assert the fixture
	// rather than the behaviour. A clean diff is also the operator-visible claim:
	// what was emitted is present, owned, and needs no further action.
	actual, err := n4j.ParseConstraints(query(t, ctx, n4j.ShowConstraints))
	if err != nil {
		t.Fatalf("parsing constraints: %v", err)
	}
	cd := planAgainst(t, a, s, actual, readIndexes(t, ctx)).Constraints
	if len(cd.Match) != len(constraints) {
		t.Errorf("matched %d of %d emitted constraints; drift=%d create=%d drop=%d",
			len(cd.Match), len(constraints),
			len(cd.Drift), len(cd.Create), len(cd.Drop))
		for _, d := range cd.Drift {
			t.Logf("  drift: %s", d.Reason)
		}
		for _, c := range cd.Create {
			t.Logf("  create: %s", c.Statement)
		}
	}
}

// A TYPE constraint's propertyType must be reported, and must match what the
// library emitted: ParseConstraints refuses a TYPE record without one, and
// `yammm neo4j diff` would exit 3 on every run. Community holds no TYPE
// constraint, so this runs on Enterprise alone.
func TestTypeConstraint_PropertyTypeRoundTrips(t *testing.T) {
	ctx := context.Background()
	driver(t)
	requireEnterprise(t, ctx)
	dropAll(t, ctx)
	t.Cleanup(func() { dropAll(t, ctx) })

	a := newConfig(t, ctx, cypher.Config{})
	s := loadRoundTripSchema(t)
	applySchemaDDL(t, ctx, a, s)

	desired, _ := a.Constraints(s)
	var wantType int
	for _, c := range desired {
		if c.Kind == cypher.ConstraintType {
			wantType++
		}
	}
	if wantType == 0 {
		t.Fatal("the fixture emitted no TYPE constraints; this test would prove nothing")
	}

	records := query(t, ctx, n4j.ShowConstraints)
	parsed, err := n4j.ParseConstraints(records)
	if err != nil {
		t.Fatalf("ParseConstraints: %v", err)
	}
	var withPropertyType int
	for _, rc := range parsed {
		if rc.Type == "NODE_PROPERTY_TYPE" && rc.PropertyType != "" {
			withPropertyType++
		}
	}
	if withPropertyType != wantType {
		t.Errorf("%d of %d TYPE constraints reported a propertyType", withPropertyType, wantType)
	}

	// And they must compare equal, not drift.
	diff := planAgainst(t, a, s, parsed, readIndexes(t, ctx)).Constraints
	for _, d := range diff.Drift {
		t.Errorf("drift on a constraint the library itself applied: %s (%s)", d.Desired.Name, d.Reason)
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

	a := newConfig(t, ctx, cypher.Config{})
	s := loadRoundTripSchema(t)
	desired, _ := a.Constraints(s)

	// Find a TYPE constraint and apply a deliberately different one under the
	// same label and property.
	var target cypher.Constraint
	for _, c := range desired {
		if c.Kind == cypher.ConstraintType && c.TypeExpr != "STRING" && len(c.Properties) == 1 {
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

	parsed, err := n4j.ParseConstraints(query(t, ctx, n4j.ShowConstraints))
	if err != nil {
		t.Fatalf("ParseConstraints: %v", err)
	}
	diff := planAgainst(t, a, s, parsed, readIndexes(t, ctx)).Constraints
	if len(diff.Drift) != 1 {
		t.Fatalf("drift = %d, want 1 (schema declares %s, database enforces STRING)", len(diff.Drift), target.TypeExpr)
	}
	t.Logf("drift reason: %s", diff.Drift[0].Reason)
	if !strings.Contains(diff.Drift[0].Reason, target.TypeExpr) {
		t.Errorf("drift reason %q does not name the schema's type %q", diff.Drift[0].Reason, target.TypeExpr)
	}
}

// A caller whose own introspection omits propertyType gets an error from
// ParseConstraints, never a plan that reads an uncompared TYPE constraint as
// matched.
func TestParseConstraints_RefusesALiveTypeRecordWithNoPropertyType(t *testing.T) {
	ctx := context.Background()
	driver(t)
	requireEnterprise(t, ctx)
	dropAll(t, ctx)
	t.Cleanup(func() { dropAll(t, ctx) })

	a := newConfig(t, ctx, cypher.Config{})
	s := loadRoundTripSchema(t)
	applySchemaDDL(t, ctx, a, s)

	desired, _ := a.Constraints(s)
	var typeConstraints []cypher.Constraint
	for _, c := range desired {
		if c.Kind == cypher.ConstraintType {
			typeConstraints = append(typeConstraints, c)
		}
	}
	if len(typeConstraints) == 0 {
		t.Skip("no TYPE constraints in the fixture")
	}

	// A caller's own query that does not yield propertyType.
	records := query(t, ctx,
		"SHOW CONSTRAINTS YIELD name, type, entityType, labelsOrTypes, properties, createStatement")
	parsed, err := n4j.ParseConstraints(records)
	if err == nil || !strings.Contains(err.Error(), "reports no propertyType") {
		t.Fatalf("ParseConstraints = %d records, %v; want the TYPE record refused", len(parsed), err)
	}
}

// Index shapes the DSL cannot express must not be reported as drops: no schema
// edit could resolve one, so the diff would never converge. Single-label
// FULLTEXT is deliberately absent — it IS declarable now, and an undeclared one
// is a drop ([TestUndeclaredFulltextIndex_IsDropped]); the fulltext shape the
// DSL still cannot express is the multi-label form.
func TestUndeclarableIndexKinds_AreNotDropped(t *testing.T) {
	ctx := context.Background()
	driver(t)
	dropAll(t, ctx)
	t.Cleanup(func() { dropAll(t, ctx) })

	a := newConfig(t, ctx, cypher.Config{})
	s := loadRoundTripSchema(t)
	applySchemaDDL(t, ctx, a, s)

	// Hand-create shapes the schema has no vocabulary for, on a label it owns.
	run(t, ctx, "CREATE FULLTEXT INDEX rt_ft IF NOT EXISTS FOR (n:rt__Document|rt__Note) ON EACH [n.body]")
	run(t, ctx, "CREATE TEXT INDEX rt_text IF NOT EXISTS FOR (n:rt__Document) ON (n.state)")
	run(t, ctx, "CREATE POINT INDEX rt_point IF NOT EXISTS FOR (n:rt__Document) ON (n.location)")
	awaitIndexes(t, ctx)

	desired, _ := a.Indexes(s)
	parsed, err := n4j.ParseIndexes(query(t, ctx, n4j.ShowIndexes))
	if err != nil {
		t.Fatalf("ParseIndexes: %v", err)
	}
	diff := planAgainst(t, a, s, readConstraints(t, ctx), parsed).Indexes
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

	a := newConfig(t, ctx, cypher.Config{})
	s := loadRoundTripSchema(t)
	desired, res := a.Indexes(s)
	if res != nil {
		t.Fatalf("emitting indexes: %v", res)
	}
	var rangeIdx cypher.Index
	for _, d := range desired {
		if d.Kind == cypher.IndexRange && len(d.Properties) == 1 {
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

	// Now try the library's own statement. Record whether the server errors or
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
	parsed, err := n4j.ParseIndexes(query(t, ctx, n4j.ShowIndexes))
	if err != nil {
		t.Fatalf("ParseIndexes: %v", err)
	}
	diff := planAgainst(t, a, s, readConstraints(t, ctx), parsed).Indexes
	for _, c := range diff.Create {
		if c.Name == rangeIdx.Name {
			t.Errorf("reported a Create of %s, a name the database already holds; the plan cannot converge", c.Name)
		}
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

	a := newConfig(t, ctx, cypher.Config{})
	s := loadRoundTripSchema(t)
	applySchemaDDL(t, ctx, a, s)

	// A relationship type deliberately spelled like a label this schema owns.
	run(t, ctx, "CREATE INDEX rt_rel_idx IF NOT EXISTS FOR ()-[r:rt__Document]-() ON (r.since)")
	run(t, ctx, "CREATE CONSTRAINT rt_rel_exists IF NOT EXISTS FOR ()-[r:rt__Document]-() REQUIRE r.since IS NOT NULL")
	awaitIndexes(t, ctx)

	parsedIdx, err := n4j.ParseIndexes(query(t, ctx, n4j.ShowIndexes))
	if err != nil {
		t.Fatalf("ParseIndexes: %v", err)
	}
	for _, d := range planAgainst(t, a, s, readConstraints(t, ctx), parsedIdx).Indexes.Drop {
		if d.EntityType == "RELATIONSHIP" {
			t.Errorf("relationship index %q reported as a drop", d.Name)
		}
	}

	parsedCons, err := n4j.ParseConstraints(query(t, ctx, n4j.ShowConstraints))
	if err != nil {
		t.Fatalf("ParseConstraints: %v", err)
	}
	for _, d := range planAgainst(t, a, s, parsedCons, readIndexes(t, ctx)).Constraints.Drop {
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

	a := newConfig(t, ctx, cypher.Config{})
	s := loadRoundTripSchema(t)

	// `state` is a property the schema declares a node @index on, and the
	// relationship type is spelled exactly like the label that index sits on.
	run(t, ctx, "CREATE INDEX rt_rel_state_idx IF NOT EXISTS FOR ()-[r:rt__Document]-() ON (r.state)")
	awaitIndexes(t, ctx)

	_, res := a.Indexes(s)
	if res != nil {
		t.Fatalf("emitting indexes: %v", res)
	}
	parsed, err := n4j.ParseIndexes(query(t, ctx, n4j.ShowIndexes))
	if err != nil {
		t.Fatalf("ParseIndexes: %v", err)
	}

	diff := planAgainst(t, a, s, readConstraints(t, ctx), parsed).Indexes
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

	a := newConfig(t, ctx, cypher.Config{})
	s := loadRoundTripSchema(t)
	applySchemaDDL(t, ctx, a, s)

	run(t, ctx, "CREATE INDEX rt_adhoc IF NOT EXISTS FOR (n:rt__Document) ON (n.title)")
	awaitIndexes(t, ctx)

	parsed, err := n4j.ParseIndexes(query(t, ctx, n4j.ShowIndexes))
	if err != nil {
		t.Fatalf("ParseIndexes: %v", err)
	}
	diff := planAgainst(t, a, s, readConstraints(t, ctx), parsed).Indexes
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

	a := newConfig(t, ctx, cypher.Config{})
	s := loadRoundTripSchema(t)
	applySchemaDDL(t, ctx, a, s)

	// Labels a sibling schema would emit: one lowercase-leading segment, one
	// uppercase-leading, and one that extends a label this schema owns.
	for i, label := range []string{"rt__legacy__Foo", "rt__Core__Widget", "rt__Document__Nested"} {
		run(t, ctx, fmt.Sprintf("CREATE INDEX rt_sibling_%d IF NOT EXISTS FOR (n:%s) ON (n.x)", i, label))
	}
	awaitIndexes(t, ctx)

	parsed, err := n4j.ParseIndexes(query(t, ctx, n4j.ShowIndexes))
	if err != nil {
		t.Fatalf("ParseIndexes: %v", err)
	}
	diff := planAgainst(t, a, s, readConstraints(t, ctx), parsed).Indexes
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

	a := newConfig(t, ctx, cypher.Config{})
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
	for _, rec := range query(t, ctx, n4j.ShowIndexes) {
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
		// A LOOKUP index covers every label and reports none.
		if typ == "LOOKUP" {
			if rec["labelsOrTypes"] != nil {
				t.Errorf("LOOKUP index %v reports labels %v, want null", rec["name"], rec["labelsOrTypes"])
			}
			continue
		}
		// The kinds the library itself emits report exactly one label, which is
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
// and the one the library's own filter is written to defend against — must not
// be told to drop the index behind a primary key.
func TestYieldStarCaller_ConstraintIndexesNotDropped(t *testing.T) {
	ctx := context.Background()
	driver(t)
	dropAll(t, ctx)
	t.Cleanup(func() { dropAll(t, ctx) })

	a := newConfig(t, ctx, cypher.Config{})
	s := loadRoundTripSchema(t)
	applySchemaDDL(t, ctx, a, s)

	records := query(t, ctx, "SHOW INDEXES YIELD *")
	parsed, err := n4j.ParseIndexes(records)
	if err != nil {
		t.Fatalf("ParseIndexes on YIELD * records: %v", err)
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

	desired, _ := a.Indexes(s)
	diff := planAgainst(t, a, s, readConstraints(t, ctx), parsed).Indexes
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

	a := newConfig(t, ctx, cypher.Config{})
	s, res := schema.LoadString(ctx, "schema \"pop\"\n\ntype Bulk {\n\tid String primary\n\tv String @index\n}\n", "pop.yammm")
	if res != nil {
		t.Fatalf("load: %v", res)
	}
	if desired, _ := a.Indexes(s); len(desired) != 1 || desired[0].Name != name {
		t.Fatalf("the schema declares %+v, want the one index %s", desired, name)
	}

	// Poll briefly for the POPULATING window, pacing the queries: issuing them
	// back to back competes with the population this loop is trying to observe,
	// and on a state the loop does not recognise it would hammer the container
	// for the whole deadline.
	deadline := time.Now().Add(30 * time.Second)
	var sawPopulating bool
	for time.Now().Before(deadline) {
		parsed, err := n4j.ParseIndexes(query(t, ctx, n4j.ShowIndexes))
		if err != nil {
			t.Fatalf("ParseIndexes: %v", err)
		}
		var state string
		for _, ri := range parsed {
			if ri.Name == name {
				state = ri.State
			}
		}
		if strings.EqualFold(state, "POPULATING") {
			sawPopulating = true
			diff := planAgainst(t, a, s, readConstraints(t, ctx), parsed).Indexes
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
	parsed, err := n4j.ParseIndexes(query(t, ctx, n4j.ShowIndexes))
	if err != nil {
		t.Fatalf("ParseIndexes: %v", err)
	}
	if diff := planAgainst(t, a, s, readConstraints(t, ctx), parsed).Indexes; len(diff.Match) != 1 {
		t.Errorf("once ONLINE the index should match; got match=%d unverified=%d",
			len(diff.Match), len(diff.Unverified))
	}
}

// Index names are GLOBAL: a name held by an index on a label this schema does
// not own still makes the emitted CREATE a silent no-op. Verified directly —
// creating the same name on a second label leaves the first one in place.
func TestNameHeldOnForeignLabel_BlocksCreate(t *testing.T) {
	ctx := context.Background()
	driver(t)
	dropAll(t, ctx)
	t.Cleanup(func() { dropAll(t, ctx) })

	a := newConfig(t, ctx, cypher.Config{})
	s := loadRoundTripSchema(t)
	desired, res := a.Indexes(s)
	if res != nil {
		t.Fatalf("emitting indexes: %v", res)
	}
	var target cypher.Index
	for _, d := range desired {
		if d.Kind == cypher.IndexRange && len(d.Properties) == 1 {
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

	// The server accepts the library's CREATE and does nothing.
	run(t, ctx, target.Statement)
	for _, rec := range query(t, ctx, "SHOW INDEXES YIELD name, labelsOrTypes") {
		if n, _ := rec["name"].(string); n == target.Name {
			labels, _ := rec["labelsOrTypes"].([]any)
			if len(labels) != 1 || labels[0] != "foreign__Thing" {
				t.Errorf("index %q is on %v; the foreign holder should still own the name", n, labels)
			}
		}
	}

	parsed, err := n4j.ParseIndexes(query(t, ctx, n4j.ShowIndexes))
	if err != nil {
		t.Fatalf("ParseIndexes: %v", err)
	}
	diff := planAgainst(t, a, s, readConstraints(t, ctx), parsed).Indexes
	for _, c := range diff.Create {
		if c.Name == target.Name {
			t.Errorf("reported a Create of %s, a name a foreign label already holds; the plan cannot converge", c.Name)
		}
	}
	if len(diff.Drift) != 1 {
		t.Fatalf("drift = %d, want 1", len(diff.Drift))
	}
	t.Logf("drift reason: %s", diff.Drift[0].Reason)
}

// The server never creates a declared index over a (label, properties) that a
// constraint's backing index already serves. A valid schema reaches this when
// one @@index's emitters disagree: Doc keys on doc_id alone and Rev on (doc_id,
// rev), so the load accepts it, and Doc's declaration is SATISFIED by its
// constraint's backing index. It is neither a Create, which the server never
// honours, nor a Drift, which only dropping the primary key could clear.
func TestDefinitionServedByConstraintIndex_IsSatisfied(t *testing.T) {
	ctx := context.Background()
	driver(t)
	t.Cleanup(func() { dropAll(t, ctx) })

	s, res := schema.LoadString(ctx, `schema "pk"
abstract type Base { doc_id String primary
                     @@index(doc_id) }
type Doc extends Base { }
type Rev extends Base { rev String primary }
`, "pk.yammm")
	if res != nil {
		t.Fatalf("load: %v", res)
	}

	for _, tc := range []struct {
		name    string
		nodeKey bool
	}{
		{"UNIQUE", false},
		{"NODE KEY", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.nodeKey {
				requireEnterprise(t, ctx)
			}
			dropAll(t, ctx)
			a := newConfig(t, ctx, cypher.Config{NodeKeys: tc.nodeKey})
			applySchemaDDL(t, ctx, a, s)

			desired, _ := a.Indexes(s)
			doc, _ := s.Type("Doc")
			rev, _ := s.Type("Rev")
			docLabel, revLabel := a.Label(doc), a.Label(rev)
			byLabel := map[string]cypher.Index{}
			for _, ix := range desired {
				byLabel[ix.Label] = ix
			}
			if len(desired) != 2 || byLabel[docLabel].Name == "" || byLabel[revLabel].Name == "" {
				t.Fatalf("expected the @@index on %s and on %s, got %+v", docLabel, revLabel, desired)
			}
			created := map[string]bool{}
			for _, rec := range query(t, ctx, "SHOW INDEXES YIELD name") {
				if n, ok := rec["name"].(string); ok {
					created[n] = true
				}
			}
			if n := byLabel[docLabel].Name; created[n] {
				t.Fatalf("the server created %s beside the constraint's backing index", n)
			}
			if n := byLabel[revLabel].Name; !created[n] {
				t.Fatalf("the server did not create %s, which no backing index serves", n)
			}

			parsed, err := n4j.ParseIndexes(query(t, ctx, n4j.ShowIndexes))
			if err != nil {
				t.Fatalf("ParseIndexes: %v", err)
			}
			diff := planAgainst(t, a, s, readConstraints(t, ctx), parsed).Indexes
			if len(diff.Create) != 0 {
				t.Errorf("reported a Create the server will never honour: %+v", diff.Create)
			}
			if len(diff.Drift) != 0 {
				t.Errorf("reported unclearable drift on a valid, fully-applied schema: %+v", diff.Drift)
			}
			if len(diff.Match) != 2 {
				t.Fatalf("match = %d, want both declarations satisfied", len(diff.Match))
			}
			for _, m := range diff.Match {
				switch m.Desired.Label {
				case docLabel:
					if m.Actual.OwningConstraint == "" {
						t.Errorf("Doc's declaration matched %s; it should be the constraint's backing index", m.Actual.Name)
					}
				case revLabel:
					if m.Actual.OwningConstraint != "" || m.Actual.Name != m.Desired.Name {
						t.Errorf("Rev's declaration matched %s (constraint %q); it should be its own index", m.Actual.Name, m.Actual.OwningConstraint)
					}
				default:
					t.Errorf("a match on an unexpected label: %+v", m)
				}
			}
		})
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
	if res != nil {
		t.Fatalf("load: %v", res)
	}

	a := newConfig(t, ctx, cypher.Config{})
	constraints, cRes := a.Constraints(s)
	if cRes != nil {
		t.Fatalf("emitting constraints: %v", cRes)
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

// A hand-named fulltext index whose definition equals a declaration is claimed
// by identity pairing under its legacy name: Match, no rename, no DDL. This is
// the zero-op adoption a schema-first consumer migrating a hand-maintained
// fulltext registry relies on — the live counterpart of the operator's-ad-hoc-
// name case the pairing documents.
func TestHandNamedFulltextIndex_IsClaimedByIdentity(t *testing.T) {
	ctx := context.Background()
	driver(t)
	dropAll(t, ctx)
	t.Cleanup(func() { dropAll(t, ctx) })

	a := newConfig(t, ctx, cypher.Config{})
	s := loadRoundTripSchema(t)

	// The operator's index exists FIRST, under a hand-picked name, exactly as a
	// migration from a hand-maintained registry finds it.
	run(t, ctx, "CREATE FULLTEXT INDEX legacy_title_ft IF NOT EXISTS FOR (n:rt__Document) ON EACH [n.title]")
	awaitIndexes(t, ctx)

	desired, res := a.Indexes(s)
	if res != nil {
		t.Fatalf("emitting indexes: %v", res)
	}
	var ft cypher.Index
	for _, d := range desired {
		if d.Kind == cypher.IndexFulltext && len(d.Properties) == 1 && d.Properties[0] == "title" {
			ft = d
			break
		}
	}
	if ft.Name == "" {
		t.Fatal("no single-property fulltext declaration in the fixture")
	}

	parsed, err := n4j.ParseIndexes(query(t, ctx, n4j.ShowIndexes))
	if err != nil {
		t.Fatalf("ParseIndexes: %v", err)
	}
	diff := planAgainst(t, a, s, readConstraints(t, ctx), parsed).Indexes
	if len(diff.Match) != 1 || diff.Match[0].Desired.Name != ft.Name {
		t.Fatalf("match = %+v, want %s alone (the legacy index claimed by identity)", diff.Match, ft.Name)
	}
	if got := diff.Match[0].Actual.Name; got != "legacy_title_ft" {
		t.Errorf("claimed remote name = %q; want the legacy name kept as-is", got)
	}
	for _, c := range diff.Create {
		if c.Name == ft.Name {
			t.Errorf("reported a Create of %s, which the legacy index realises", c.Name)
		}
	}
	if len(diff.Drift)+len(diff.Drop) != 0 {
		t.Errorf("drift %d, drop %d, want 0", len(diff.Drift), len(diff.Drop))
	}
}

// The declarability flip, live: an owned single-label FULLTEXT row with no
// declaration is a Drop — the definition-drift authority the fulltext
// annotations exist to provide.
func TestUndeclaredFulltextIndex_IsDropped(t *testing.T) {
	ctx := context.Background()
	driver(t)
	dropAll(t, ctx)
	t.Cleanup(func() { dropAll(t, ctx) })

	a := newConfig(t, ctx, cypher.Config{})
	s := loadRoundTripSchema(t)
	applySchemaDDL(t, ctx, a, s)

	// The schema declares fulltext over [title] and [title, state]; [state]
	// alone is undeclared.
	run(t, ctx, "CREATE FULLTEXT INDEX rt_adhoc_ft IF NOT EXISTS FOR (n:rt__Document) ON EACH [n.state]")
	awaitIndexes(t, ctx)

	parsed, err := n4j.ParseIndexes(query(t, ctx, n4j.ShowIndexes))
	if err != nil {
		t.Fatalf("ParseIndexes: %v", err)
	}
	diff := planAgainst(t, a, s, readConstraints(t, ctx), parsed).Indexes
	if len(diff.Drop) != 1 || diff.Drop[0].Name != "rt_adhoc_ft" {
		t.Errorf("drops = %d; want exactly the undeclared rt_adhoc_ft", len(diff.Drop))
		for _, d := range diff.Drop {
			t.Logf("  drop: %s (%s)", d.Name, d.Type)
		}
	}
}

// A multi-label FULLTEXT index does not stop the server from creating a
// single-label fulltext index over the same property — they are different
// objects — which is why the diff keeps multi-label rows out of the
// definition-blocker map and out of declarability, while their names keep
// blocking. Assert what the server actually does; the exclusion's safety
// argument depends on it.
func TestMultiLabelFulltext_DoesNotBlockSingleLabelCreate(t *testing.T) {
	ctx := context.Background()
	driver(t)
	dropAll(t, ctx)
	t.Cleanup(func() { dropAll(t, ctx) })

	a := newConfig(t, ctx, cypher.Config{})
	s := loadRoundTripSchema(t)

	desired, res := a.Indexes(s)
	if res != nil {
		t.Fatalf("emitting indexes: %v", res)
	}
	var ft cypher.Index
	for _, d := range desired {
		if d.Kind == cypher.IndexFulltext && len(d.Properties) == 1 && d.Properties[0] == "title" {
			ft = d
			break
		}
	}
	if ft.Name == "" {
		t.Fatal("no single-property fulltext declaration in the fixture")
	}

	// The multi-label object exists first; the declared statement must still
	// create the single-label index beside it.
	run(t, ctx, "CREATE FULLTEXT INDEX rt_multi_ft IF NOT EXISTS FOR (n:rt__Document|rt__Note) ON EACH [n.title]")
	run(t, ctx, ft.Statement)
	awaitIndexes(t, ctx)

	present := make(map[string]bool)
	for _, rec := range query(t, ctx, "SHOW INDEXES YIELD name, type") {
		if name, _ := rec["name"].(string); name != "" {
			present[name] = true
		}
	}
	if !present["rt_multi_ft"] || !present[ft.Name] {
		t.Fatalf("want both the multi-label index and the declared one to exist; got %v", present)
	}

	parsed, err := n4j.ParseIndexes(query(t, ctx, n4j.ShowIndexes))
	if err != nil {
		t.Fatalf("ParseIndexes: %v", err)
	}
	diff := planAgainst(t, a, s, readConstraints(t, ctx), parsed).Indexes
	if len(diff.Match) != 1 {
		t.Errorf("match = %d, want 1 (the declared single-label index)", len(diff.Match))
	}
	if len(diff.Drop) != 0 {
		t.Errorf("drops = %d, want 0: the multi-label object is excluded, not dropped", len(diff.Drop))
	}
	lookups := 0
	for _, ri := range parsed {
		if ri.Type == "LOOKUP" {
			lookups++
		}
	}
	if diff.Excluded != 1+lookups {
		t.Errorf("Excluded = %d; want the multi-label row and the %d LOOKUP rows counted", diff.Excluded, lookups)
	}
}

// One namespace holds index and constraint names, and IF NOT EXISTS answers for a
// name of its own kind alone: over the other kind's name the server refuses the
// statement, so reading both kinds' rows separates a no-op from a failure.
func TestNameHeldAcrossKinds_ServerResponse(t *testing.T) {
	ctx := context.Background()
	driver(t)
	t.Cleanup(func() { dropAll(t, ctx) })

	const (
		uniqueOnA  = "CREATE CONSTRAINT held FOR (n:A) REQUIRE n.x IS UNIQUE"
		notNullOnA = "CREATE CONSTRAINT held FOR (n:A) REQUIRE n.x IS NOT NULL"
		typeOnA    = "CREATE CONSTRAINT held FOR (n:A) REQUIRE n.x IS :: STRING"
		indexOnA   = "CREATE INDEX held FOR (n:A) ON (n.x)"
		indexOnB   = "CREATE INDEX held IF NOT EXISTS FOR (n:B) ON (n.y)"
		uniqueOnB  = "CREATE CONSTRAINT held IF NOT EXISTS FOR (n:B) REQUIRE n.y IS UNIQUE"
		notNullOnB = "CREATE CONSTRAINT held IF NOT EXISTS FOR (n:B) REQUIRE n.y IS NOT NULL"
		nodeKeyOnA = "CREATE CONSTRAINT held FOR (n:A) REQUIRE n.x IS NODE KEY"
		nodeKeyOnB = "CREATE CONSTRAINT held IF NOT EXISTS FOR (n:B) REQUIRE n.y IS NODE KEY"
	)
	tests := []struct {
		name       string
		holder     string
		statement  string
		enterprise bool
		wantCode   string
	}{
		{name: "index over a backing index's name", holder: uniqueOnA, statement: indexOnB},
		{name: "index over a not-null constraint's name", holder: notNullOnA, statement: indexOnB, enterprise: true, wantCode: "ConstraintWithNameAlreadyExists"},
		{name: "index over a type constraint's name", holder: typeOnA, statement: indexOnB, enterprise: true, wantCode: "ConstraintWithNameAlreadyExists"},
		{name: "constraint over a constraint's name", holder: notNullOnA, statement: uniqueOnB, enterprise: true},
		{name: "unique constraint over an index's name", holder: indexOnA, statement: uniqueOnB, wantCode: "IndexWithNameAlreadyExists"},
		{name: "not-null constraint over an index's name", holder: indexOnA, statement: notNullOnB, enterprise: true, wantCode: "IndexWithNameAlreadyExists"},
		{name: "index over a node key's backing index's name", holder: nodeKeyOnA, statement: indexOnB, enterprise: true},
		{name: "node key constraint over an index's name", holder: indexOnA, statement: nodeKeyOnB, enterprise: true, wantCode: "IndexWithNameAlreadyExists"},
		{name: "node key constraint over a unique constraint's name", holder: uniqueOnA, statement: nodeKeyOnB, enterprise: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.enterprise {
				requireEnterprise(t, ctx)
			}
			dropAll(t, ctx)
			if _, err := neo4jdriver.ExecuteQuery(ctx, driver(t), tt.holder, nil, neo4jdriver.EagerResultTransformer); err != nil {
				t.Fatalf("creating the holder: %v", err)
			}
			_, err := neo4jdriver.ExecuteQuery(ctx, driver(t), tt.statement, nil, neo4jdriver.EagerResultTransformer)
			switch {
			case tt.wantCode == "" && err != nil:
				t.Fatalf("server refused the statement: %v", err)
			case tt.wantCode != "" && (err == nil || !strings.Contains(err.Error(), tt.wantCode)):
				t.Fatalf("server answered %v, want a refusal with %s", err, tt.wantCode)
			}
			for _, show := range []string{"SHOW INDEXES YIELD name, labelsOrTypes", "SHOW CONSTRAINTS YIELD name, labelsOrTypes"} {
				for _, rec := range query(t, ctx, show) {
					labels, _ := rec["labelsOrTypes"].([]any)
					if rec["name"] == "held" && !slices.Contains(labels, any("A")) {
						t.Errorf("%s: the name moved off its holder: %v", show, labels)
					}
				}
			}
		})
	}
}

// constraintDefinitionKey keeps a composite's property order because the server
// refuses a key constraint only over an index in that same order.
func TestConstraintOverACompositeIndex_ServerResponse(t *testing.T) {
	ctx := context.Background()
	driver(t)
	t.Cleanup(func() { dropAll(t, ctx) })

	tests := []struct {
		name       string
		constraint string
		wantCode   string
	}{
		{name: "same property order", constraint: "CREATE CONSTRAINT c FOR (n:A) REQUIRE (n.x, n.y) IS UNIQUE", wantCode: "IndexAlreadyExists"},
		{name: "reversed property order", constraint: "CREATE CONSTRAINT c FOR (n:A) REQUIRE (n.y, n.x) IS UNIQUE"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dropAll(t, ctx)
			if _, err := neo4jdriver.ExecuteQuery(ctx, driver(t), "CREATE INDEX i FOR (n:A) ON (n.x, n.y)", nil, neo4jdriver.EagerResultTransformer); err != nil {
				t.Fatalf("creating the index: %v", err)
			}
			_, err := neo4jdriver.ExecuteQuery(ctx, driver(t), tt.constraint, nil, neo4jdriver.EagerResultTransformer)
			switch {
			case tt.wantCode == "" && err != nil:
				t.Fatalf("server refused the constraint: %v", err)
			case tt.wantCode != "" && (err == nil || !strings.Contains(err.Error(), tt.wantCode)):
				t.Fatalf("server answered %v, want a refusal with %s", err, tt.wantCode)
			}
		})
	}
}
