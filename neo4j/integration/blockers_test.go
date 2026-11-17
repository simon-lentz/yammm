//go:build neo4j_integration

package integration

import (
	"context"
	"slices"
	"strings"
	"testing"

	n4j "github.com/simon-lentz/yammm/neo4j"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/schema/cypher"
)

// applyPlan runs a plan in the order its contract states: every object a drift
// on either side names and every Drop, each dropped once, then every constraint
// and then every index created.
func applyPlan(t *testing.T, ctx context.Context, plan n4j.Plan) {
	t.Helper()
	dropped := make(map[string]struct{})
	drop := func(stmt string) {
		t.Helper()
		if _, done := dropped[stmt]; done {
			return
		}
		dropped[stmt] = struct{}{}
		run(t, ctx, stmt)
	}
	dropBlockers := func(b n4j.Blockers) {
		for _, rc := range b.Constraints {
			drop(cypher.DropConstraintStatement(rc.Name))
		}
		for _, ri := range b.Indexes {
			if ri.OwningConstraint != "" {
				drop(cypher.DropConstraintStatement(ri.OwningConstraint))
				continue
			}
			drop(cypher.DropIndexStatement(ri.Name))
		}
	}
	for _, d := range plan.Constraints.Drift {
		dropBlockers(d.Blockers)
	}
	for _, d := range plan.Indexes.Drift {
		dropBlockers(d.Blockers)
	}
	for _, rc := range plan.Constraints.Drop {
		drop(cypher.DropConstraintStatement(rc.Name))
	}
	for _, ri := range plan.Indexes.Drop {
		drop(cypher.DropIndexStatement(ri.Name))
	}
	for _, c := range plan.Constraints.Create {
		run(t, ctx, c.Statement)
	}
	for _, d := range plan.Constraints.Drift {
		run(t, ctx, d.Desired.Statement)
	}
	for _, i := range plan.Indexes.Create {
		run(t, ctx, i.Statement)
	}
	for _, d := range plan.Indexes.Drift {
		run(t, ctx, d.Desired.Statement)
	}
	awaitIndexes(t, ctx)
}

// liveDiff plans every constraint and index the schema declares against the
// database's.
func liveDiff(t *testing.T, ctx context.Context, a cypher.Config, s schema.Schema) n4j.Plan {
	t.Helper()
	return planAgainst(t, a, s, readConstraints(t, ctx), readIndexes(t, ctx))
}

// planAgainst plans s under c against the given database rows, failing the
// test when c refuses s.
func planAgainst(t *testing.T, c cypher.Config, s schema.Schema, constraints []n4j.RemoteConstraint, indexes []n4j.RemoteIndex) n4j.Plan {
	t.Helper()
	plan, res := n4j.Diff(c, s, constraints, indexes)
	if res.HasErrors() {
		t.Fatalf("planning: %v", res.Err())
	}
	return plan
}

// readConstraints returns the database's constraints as SHOW CONSTRAINTS
// reports them.
func readConstraints(t *testing.T, ctx context.Context) []n4j.RemoteConstraint {
	t.Helper()
	constraints, err := n4j.ParseConstraints(query(t, ctx, n4j.ShowConstraints))
	if err != nil {
		t.Fatalf("ParseConstraints: %v", err)
	}
	return constraints
}

// readIndexes returns the database's indexes as SHOW INDEXES reports them.
func readIndexes(t *testing.T, ctx context.Context) []n4j.RemoteIndex {
	t.Helper()
	indexes, err := n4j.ParseIndexes(query(t, ctx, n4j.ShowIndexes))
	if err != nil {
		t.Fatalf("ParseIndexes: %v", err)
	}
	return indexes
}

// requireCleanPlan fails unless the plan holds nothing to do on either side.
func requireCleanPlan(t *testing.T, plan n4j.Plan) {
	t.Helper()
	c, i := plan.Constraints, plan.Indexes
	if len(c.Drift)+len(c.Create)+len(c.Drop) != 0 {
		t.Errorf("the second diff's constraints are not clean: drift %+v, create %+v, drop %+v",
			c.Drift, c.Create, c.Drop)
	}
	if len(i.Drift)+len(i.Create)+len(i.Drop)+len(i.Unverified) != 0 {
		t.Errorf("the second diff's indexes are not clean: drift %+v, create %+v, drop %+v, unverified %+v",
			i.Drift, i.Create, i.Drop, i.Unverified)
	}
}

func loadBlockerSchema(t *testing.T, ctx context.Context, src string) schema.Schema {
	t.Helper()
	s, res := schema.LoadString(ctx, src, "blk.yammm")
	if res.HasErrors() {
		t.Fatalf("load: %v", res.Err())
	}
	return s
}

// named returns the one declaration of kind among desired.
func named[T any](t *testing.T, desired []T, is func(T) bool) T {
	t.Helper()
	i := slices.IndexFunc(desired, is)
	if i < 0 {
		t.Fatalf("no declaration of the kind the test needs: %+v", desired)
	}
	return desired[i]
}

// propertiesOf returns the properties SHOW reports for the object called name,
// and whether one exists.
func propertiesOf(t *testing.T, ctx context.Context, show, name string) ([]string, bool) {
	t.Helper()
	for _, rec := range query(t, ctx, show+" YIELD name, properties") {
		if rec["name"] != name {
			continue
		}
		raw, _ := rec["properties"].([]any)
		props := make([]string, 0, len(raw))
		for _, p := range raw {
			props = append(props, p.(string))
		}
		return props, true
	}
	return nil, false
}

// A key-backed constraint whose name an index holds, and whose definition a
// RANGE index serves, takes effect only once both are gone: each alone keeps
// the create from taking effect, so a drift must name both.
func TestConstraintCreateBesideEitherOfTwoBlockers_ServerResponse(t *testing.T) {
	ctx := context.Background()
	driver(t)
	t.Cleanup(func() { dropAll(t, ctx) })

	const create = "CREATE CONSTRAINT blk_key IF NOT EXISTS FOR (n:blk__Doc) REQUIRE n.id IS UNIQUE"
	for _, tc := range []struct{ name, blocker string }{
		{"an index holding the name", "CREATE INDEX blk_key FOR (n:blk__Doc) ON (n.z)"},
		{"a RANGE index serving the definition", "CREATE INDEX blk_range FOR (n:blk__Doc) ON (n.id)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dropAll(t, ctx)
			run(t, ctx, tc.blocker)
			t.Logf("server answered %v", answer(t, ctx, create))
			if _, created := propertiesOf(t, ctx, "SHOW CONSTRAINTS", "blk_key"); created {
				t.Errorf("the server created blk_key beside %s", tc.name)
			}
		})
	}
}

// A drift naming an index holding its name and a RANGE index serving its
// definition converges in one apply.
func TestConstraintDriftWithTwoBlockers_ConvergesInOneApply(t *testing.T) {
	ctx := context.Background()
	driver(t)
	t.Cleanup(func() { dropAll(t, ctx) })
	dropAll(t, ctx)

	s := loadBlockerSchema(t, ctx, "schema \"blk\"\n\ntype Doc {\n\tid String primary\n}\n")
	a := cypher.Config{Edition: cypher.Community}
	desired, res := a.Constraints(s)
	if res.HasErrors() {
		t.Fatalf("emitting constraints: %v", res.Err())
	}
	key := named(t, desired, func(c cypher.Constraint) bool { return c.Kind == cypher.ConstraintUnique })
	run(t, ctx, "CREATE INDEX "+key.Name+" FOR (n:"+key.Label+") ON (n.z)")
	run(t, ctx, "CREATE INDEX blk_range FOR (n:"+key.Label+") ON (n.id)")
	awaitIndexes(t, ctx)

	plan := liveDiff(t, ctx, a, s)
	if d := plan.Constraints.Drift; len(d) != 1 || len(d[0].Blockers.Constraints)+len(d[0].Blockers.Indexes) != 2 {
		t.Fatalf("drift = %+v, want one drift naming both blockers", d)
	}
	applyPlan(t, ctx, plan)
	requireCleanPlan(t, liveDiff(t, ctx, a, s))
}

// vectorIndexStatement returns the library's statement for the one vector index
// a schema declares, and that index's name.
func vectorIndexStatement(t *testing.T, a cypher.Config, s schema.Schema) (stmt, name string) {
	t.Helper()
	desired, res := a.Indexes(s)
	if res.HasErrors() {
		t.Fatalf("emitting indexes: %v", res.Err())
	}
	idx := named(t, desired, func(i cypher.Index) bool { return i.Kind == cypher.IndexVector })
	return idx.Statement, idx.Name
}

const (
	vectorSchema4 = "schema \"blk\"\n\ntype Doc {\n\tid String primary\n\tv Vector[4] @vector(cosine)\n}\n"
	vectorSchema3 = "schema \"blk\"\n\ntype Doc {\n\tid String primary\n\tv Vector[3] @vector(cosine)\n}\n"
)

// A vector index whose name another index holds, and whose definition a vector
// index of another dimension holds, takes effect only once both are gone.
func TestVectorIndexCreateBesideEitherOfTwoBlockers_ServerResponse(t *testing.T) {
	ctx := context.Background()
	driver(t)
	t.Cleanup(func() { dropAll(t, ctx) })

	a := cypher.Config{}
	want, name := vectorIndexStatement(t, a, loadBlockerSchema(t, ctx, vectorSchema4))
	other, otherName := vectorIndexStatement(t, a, loadBlockerSchema(t, ctx, vectorSchema3))
	for _, tc := range []struct{ name, blocker string }{
		{"an index holding the name", "CREATE INDEX " + name + " FOR (n:blk__Doc) ON (n.q)"},
		{"a vector index of another dimension", strings.Replace(other, otherName, "blk_vec3", 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dropAll(t, ctx)
			run(t, ctx, tc.blocker)
			awaitIndexes(t, ctx)
			t.Logf("server answered %v", answer(t, ctx, want))
			if props, held := propertiesOf(t, ctx, "SHOW INDEXES", name); held && slices.Equal(props, []string{"v"}) {
				t.Errorf("the server created %s beside %s", name, tc.name)
			}
		})
	}
}

// A vector drift whose realisation sits under another name, and whose name an
// unrelated index holds, converges in one apply: the drift names both.
func TestVectorDriftWithANameHolder_ConvergesInOneApply(t *testing.T) {
	ctx := context.Background()
	driver(t)
	t.Cleanup(func() { dropAll(t, ctx) })
	dropAll(t, ctx)

	a := newConfig(t, ctx, cypher.Config{})
	s := loadBlockerSchema(t, ctx, vectorSchema4)
	_, name := vectorIndexStatement(t, a, s)
	other, otherName := vectorIndexStatement(t, a, loadBlockerSchema(t, ctx, vectorSchema3))
	run(t, ctx, strings.Replace(other, otherName, "blk_vec3", 1))
	run(t, ctx, "CREATE INDEX "+name+" FOR (n:blk__Doc) ON (n.q)")
	awaitIndexes(t, ctx)

	plan := liveDiff(t, ctx, a, s)
	var vector *n4j.IndexDrift
	for i, d := range plan.Indexes.Drift {
		if d.Desired.Name == name {
			vector = &plan.Indexes.Drift[i]
		}
	}
	if vector == nil || len(vector.Blockers.Constraints)+len(vector.Blockers.Indexes) != 2 {
		t.Fatalf("drift = %+v, want the vector drift naming its realisation and the name holder", plan.Indexes.Drift)
	}
	applyPlan(t, ctx, plan)
	requireCleanPlan(t, liveDiff(t, ctx, a, s))
}

// A VECTOR index created without vector.dimensions is accepted and reported
// with no dimension, so the declaration it realises is Unverified, never a
// Match or a drift on a dimension nobody read.
func TestVectorIndexWithoutADimension_IsUnverified(t *testing.T) {
	ctx := context.Background()
	driver(t)
	t.Cleanup(func() { dropAll(t, ctx) })
	dropAll(t, ctx)

	a := newConfig(t, ctx, cypher.Config{})
	s := loadBlockerSchema(t, ctx, vectorSchema4)
	_, name := vectorIndexStatement(t, a, s)
	run(t, ctx, "CREATE VECTOR INDEX "+name+" FOR (n:blk__Doc) ON (n.v) OPTIONS {indexConfig: {`vector.similarity_function`: 'cosine'}}")
	awaitIndexes(t, ctx)

	plan := liveDiff(t, ctx, a, s)
	var got *n4j.IndexUnverified
	for i, u := range plan.Indexes.Unverified {
		if u.Desired.Name == name {
			got = &plan.Indexes.Unverified[i]
		}
	}
	if got == nil {
		t.Fatalf("unverified = %+v, want %s unverified", plan.Indexes.Unverified, name)
	}
	if dim, ok := got.Actual.VectorDimensions(); ok {
		t.Errorf("VectorDimensions() = %d, true; want the server to report none", dim)
	}
	if want := "database reported no vector dimension; it was not compared"; got.Reason != want {
		t.Errorf("reason = %q, want %q", got.Reason, want)
	}
}

// A TYPE drift whose name another object holds converges in one apply: the
// holder is an orphan NOT NULL, or a UNIQUE that realises the primary key, whose
// declaration is then created again under its own name.
func TestTypeDriftWhoseNameIsHeld_ConvergesInOneApply(t *testing.T) {
	ctx := context.Background()
	driver(t)
	requireEnterprise(t, ctx)
	t.Cleanup(func() { dropAll(t, ctx) })

	s := loadBlockerSchema(t, ctx, "schema \"flip\"\n\ntype Doc {\n\tid String primary\n\tz Integer\n}\n")
	for _, tc := range []struct{ name, holder string }{
		{"an orphan NOT NULL", "CREATE CONSTRAINT flip__Doc_z_type FOR (n:flip__Doc) REQUIRE n.q IS NOT NULL"},
		{"the primary key's UNIQUE", "CREATE CONSTRAINT flip__Doc_z_type FOR (n:flip__Doc) REQUIRE n.id IS UNIQUE"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dropAll(t, ctx)
			run(t, ctx, "CREATE CONSTRAINT a FOR (n:flip__Doc) REQUIRE n.z IS :: STRING")
			run(t, ctx, tc.holder)

			to := cypher.Config{}
			applyPlan(t, ctx, liveDiff(t, ctx, to, s))
			requireCleanPlan(t, liveDiff(t, ctx, to, s))
		})
	}
}

const crossSideSchema = "schema \"cx\"\n\ntype Doc {\n\tid String primary\n\ttitle String required @index\n\tbody String @index\n\tz Integer\n}\n"

// crossSideNames holds the names crossSideSchema declares. Names do not depend
// on the server, so an Enterprise Config reads them on either edition.
type crossSideNames struct {
	label, idUnique, titleNotNull, zType, titleIdx, bodyIdx string
}

func readCrossSideNames(t *testing.T, s schema.Schema) crossSideNames {
	t.Helper()
	a := cypher.Config{}
	constraints, res := a.Constraints(s)
	if res.HasErrors() {
		t.Fatalf("emitting constraints: %v", res.Err())
	}
	indexes, res := a.Indexes(s)
	if res.HasErrors() {
		t.Fatalf("emitting indexes: %v", res.Err())
	}
	on := func(prop string) func([]string) bool {
		return func(props []string) bool { return slices.Equal(props, []string{prop}) }
	}
	notNull := named(t, constraints, func(c cypher.Constraint) bool { return c.Kind == cypher.ConstraintNotNull && on("title")(c.Properties) })
	return crossSideNames{
		label:        notNull.Label,
		idUnique:     named(t, constraints, func(c cypher.Constraint) bool { return c.Kind == cypher.ConstraintUnique && on("id")(c.Properties) }).Name,
		titleNotNull: notNull.Name,
		zType:        named(t, constraints, func(c cypher.Constraint) bool { return c.Kind == cypher.ConstraintType && on("z")(c.Properties) }).Name,
		titleIdx:     named(t, indexes, func(i cypher.Index) bool { return on("title")(i.Properties) }).Name,
		bodyIdx:      named(t, indexes, func(i cypher.Index) bool { return on("body")(i.Properties) }).Name,
	}
}

func indexIn(indexes []cypher.Index, name string) bool {
	return slices.ContainsFunc(indexes, func(i cypher.Index) bool { return i.Name == name })
}

func indexDriftOn(drift []n4j.IndexDrift, name string) bool {
	return slices.ContainsFunc(drift, func(d n4j.IndexDrift) bool { return d.Desired.Name == name })
}

func constraintDriftOn(drift []n4j.ConstraintDrift, name string) bool {
	return slices.ContainsFunc(drift, func(d n4j.ConstraintDrift) bool { return d.Desired.Name == name })
}

// A drop that one side of the plan needs can remove an object the other side
// matched. Each case builds such a state, and one apply of the joint plan in
// its contract order must leave nothing to do.
func TestCrossSidePlan_ConvergesInOneApply(t *testing.T) {
	ctx := context.Background()
	driver(t)
	t.Cleanup(func() { dropAll(t, ctx) })

	s := loadBlockerSchema(t, ctx, crossSideSchema)
	n := readCrossSideNames(t, s)
	for _, tc := range []struct {
		name       string
		enterprise bool
		state      string
		// shape reports whether the first plan holds the cross-side move the
		// case exists for.
		shape func(n4j.Plan) bool
	}{
		{
			name:  "a Drop whose backing index serves a declared index",
			state: "CREATE CONSTRAINT cx_orphan FOR (n:" + n.label + ") REQUIRE n.body IS UNIQUE",
			shape: func(p n4j.Plan) bool {
				return len(p.Constraints.Drop) == 1 && indexIn(p.Indexes.Create, n.bodyIdx)
			},
		},
		{
			name:       "a constraint drift's blocker whose backing index serves a declared index",
			enterprise: true,
			state:      "CREATE CONSTRAINT " + n.zType + " FOR (n:" + n.label + ") REQUIRE n.body IS UNIQUE",
			shape: func(p n4j.Plan) bool {
				return constraintDriftOn(p.Constraints.Drift, n.zType) && indexIn(p.Indexes.Create, n.bodyIdx)
			},
		},
		{
			name:       "an index realisation that holds a declared constraint's name",
			enterprise: true,
			state:      "CREATE INDEX " + n.titleNotNull + " FOR (n:" + n.label + ") ON (n.title)",
			shape: func(p n4j.Plan) bool {
				return constraintDriftOn(p.Constraints.Drift, n.titleNotNull) && indexDriftOn(p.Indexes.Drift, n.titleIdx)
			},
		},
		{
			name:       "a NOT NULL realisation that holds a declared index's name",
			enterprise: true,
			state:      "CREATE CONSTRAINT " + n.bodyIdx + " FOR (n:" + n.label + ") REQUIRE n.title IS NOT NULL",
			shape: func(p n4j.Plan) bool {
				return constraintDriftOn(p.Constraints.Drift, n.titleNotNull) && indexDriftOn(p.Indexes.Drift, n.bodyIdx)
			},
		},
		{
			name:  "a UNIQUE realisation that holds a declared index's name",
			state: "CREATE CONSTRAINT " + n.bodyIdx + " FOR (n:" + n.label + ") REQUIRE n.id IS UNIQUE",
			shape: func(p n4j.Plan) bool {
				return len(p.Constraints.Drift) == 1 && indexDriftOn(p.Indexes.Drift, n.bodyIdx)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.enterprise {
				requireEnterprise(t, ctx)
			}
			dropAll(t, ctx)
			run(t, ctx, tc.state)
			awaitIndexes(t, ctx)

			a := newConfig(t, ctx, cypher.Config{})
			plan := liveDiff(t, ctx, a, s)
			if !tc.shape(plan) {
				t.Fatalf("the first plan does not hold the case's move: constraints %+v, indexes %+v", plan.Constraints, plan.Indexes)
			}
			applyPlan(t, ctx, plan)
			requireCleanPlan(t, liveDiff(t, ctx, a, s))
		})
	}
}

// nodeLookupIndex returns the name of the database's node LOOKUP index, or ""
// when it has none.
func nodeLookupIndex(t *testing.T, ctx context.Context) string {
	t.Helper()
	for _, rec := range query(t, ctx, "SHOW INDEXES YIELD name, type, entityType WHERE type = 'LOOKUP' AND entityType = 'NODE'") {
		if name, _ := rec["name"].(string); name != "" {
			return name
		}
	}
	return ""
}

// A LOOKUP index can hold any name, and its name blocks a create like any
// other index's: the server refuses a constraint over it and takes no effect
// from an index over it. A database holds one node LOOKUP index, so each case
// renames it and the cleanup restores the original.
func TestLookupIndexHoldingADeclaredName_ConvergesInOneApply(t *testing.T) {
	ctx := context.Background()
	driver(t)
	t.Cleanup(func() { dropAll(t, ctx) })

	s := loadBlockerSchema(t, ctx, crossSideSchema)
	n := readCrossSideNames(t, s)
	for _, held := range []string{n.idUnique, n.bodyIdx} {
		t.Run(held, func(t *testing.T) {
			dropAll(t, ctx)
			original := nodeLookupIndex(t, ctx)
			if original == "" {
				t.Fatal("the database has no node LOOKUP index")
			}
			t.Cleanup(func() {
				dropAll(t, ctx)
				run(t, ctx, "DROP INDEX "+quoteIdentifier(held)+" IF EXISTS")
				if nodeLookupIndex(t, ctx) == "" {
					run(t, ctx, "CREATE LOOKUP INDEX "+quoteIdentifier(original)+" FOR (n) ON EACH labels(n)")
				}
			})
			run(t, ctx, "DROP INDEX "+quoteIdentifier(original))
			run(t, ctx, "CREATE LOOKUP INDEX "+quoteIdentifier(held)+" FOR (n) ON EACH labels(n)")
			awaitIndexes(t, ctx)

			a := newConfig(t, ctx, cypher.Config{})
			plan := liveDiff(t, ctx, a, s)
			if !constraintDriftOn(plan.Constraints.Drift, held) && !indexDriftOn(plan.Indexes.Drift, held) {
				t.Fatalf("the first plan does not drift %s on the LOOKUP index: constraints %+v, indexes %+v", held, plan.Constraints, plan.Indexes)
			}
			applyPlan(t, ctx, plan)
			requireCleanPlan(t, liveDiff(t, ctx, a, s))
		})
	}
}
