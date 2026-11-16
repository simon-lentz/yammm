//go:build neo4j_integration

package integration

import (
	"context"
	"strings"
	"testing"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v6/neo4j"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/schema/cypher"
)

// answer runs one statement and returns the server's refusal, or nil.
func answer(t *testing.T, ctx context.Context, stmt string) error {
	t.Helper()
	_, err := neo4jdriver.ExecuteQuery(ctx, driver(t), stmt, nil, neo4jdriver.EagerResultTransformer)
	return err
}

// wantAnswer fails unless the server accepted stmt (code empty) or refused it
// with an error naming code.
func wantAnswer(t *testing.T, ctx context.Context, stmt, code string) {
	t.Helper()
	err := answer(t, ctx, stmt)
	switch {
	case code == "" && err != nil:
		t.Fatalf("%s: server refused it: %v", stmt, err)
	case code != "" && (err == nil || !strings.Contains(err.Error(), code)):
		t.Fatalf("%s: server answered %v, want a refusal with %s", stmt, err, code)
	}
}

// A primary key flipped between UNIQUE and NODE KEY, by an option or an older
// database, converges in one apply in the consumer's order.
func TestKeyKindFlip_ConvergesInOneApply(t *testing.T) {
	ctx := context.Background()
	driver(t)
	requireEnterprise(t, ctx)
	t.Cleanup(func() { dropAll(t, ctx) })

	s, res := schema.LoadString(ctx, "schema \"flip\"\n\ntype Doc {\n\tid String primary\n}\n", "flip.yammm")
	if res.HasErrors() {
		t.Fatalf("load: %v", res.Err())
	}
	for _, tc := range []struct {
		name     string
		from, to cypher.Config
	}{
		{"UNIQUE and NOT NULL to NODE KEY", cypher.Config{}, cypher.Config{NodeKeys: true}},
		{"NODE KEY to UNIQUE and NOT NULL", cypher.Config{NodeKeys: true}, cypher.Config{}},
		{"NODE KEY to a Community configuration's UNIQUE", cypher.Config{NodeKeys: true}, cypher.Config{Edition: cypher.Community}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dropAll(t, ctx)
			applySchemaDDL(t, ctx, tc.from, s)

			applyPlan(t, ctx, liveDiff(t, ctx, tc.to, s))
			requireCleanPlan(t, liveDiff(t, ctx, tc.to, s))
		})
	}
}

// A UNIQUE that name pairing gives to another declaration still blocks the NODE
// KEY over its key: two drifts name it, and it is dropped once.
func TestKeyKindHolderPairedByName_ConvergesInOneApply(t *testing.T) {
	ctx := context.Background()
	driver(t)
	requireEnterprise(t, ctx)
	t.Cleanup(func() { dropAll(t, ctx) })
	dropAll(t, ctx)

	s, res := schema.LoadString(ctx, "schema \"flip\"\n\ntype Doc {\n\tid String primary\n\tname String required\n}\n", "flip.yammm")
	if res.HasErrors() {
		t.Fatalf("load: %v", res.Err())
	}
	applySchemaDDL(t, ctx, cypher.Config{}, s)
	run(t, ctx, "DROP CONSTRAINT flip__Doc_id_unique")
	run(t, ctx, "DROP CONSTRAINT flip__Doc_name_not_null")
	run(t, ctx, "CREATE CONSTRAINT flip__Doc_name_not_null FOR (n:flip__Doc) REQUIRE n.id IS UNIQUE")

	to := cypher.Config{NodeKeys: true}
	applyPlan(t, ctx, liveDiff(t, ctx, to, s))
	requireCleanPlan(t, liveDiff(t, ctx, to, s))
}

// A TYPE drift whose Desired takes the name a key-kind drift drops converges:
// every drop runs before any create, so the UNIQUE no longer holds the name.
func TestDriftThatTakesAnotherDriftsName_ConvergesInOneApply(t *testing.T) {
	ctx := context.Background()
	driver(t)
	requireEnterprise(t, ctx)
	t.Cleanup(func() { dropAll(t, ctx) })
	dropAll(t, ctx)

	s, res := schema.LoadString(ctx, "schema \"flip\"\n\ntype Doc {\n\tid String primary\n\tz Integer\n}\n", "flip.yammm")
	if res.HasErrors() {
		t.Fatalf("load: %v", res.Err())
	}
	run(t, ctx, "CREATE CONSTRAINT a FOR (n:flip__Doc) REQUIRE n.z IS :: STRING")
	run(t, ctx, "CREATE CONSTRAINT flip__Doc_z_type FOR (n:flip__Doc) REQUIRE n.id IS UNIQUE")

	to := cypher.Config{NodeKeys: true}
	applyPlan(t, ctx, liveDiff(t, ctx, to, s))
	requireCleanPlan(t, liveDiff(t, ctx, to, s))
}

// constraintDefinitionKey keeps a composite's property order because the server
// refuses a UNIQUE and a NODE KEY constraint over one label only in the same
// property order.
func TestKeyConstraintBesideTheOtherKind_ServerResponse(t *testing.T) {
	ctx := context.Background()
	driver(t)
	requireEnterprise(t, ctx)
	t.Cleanup(func() { dropAll(t, ctx) })

	const (
		uniqueX     = "CREATE CONSTRAINT u IF NOT EXISTS FOR (n:A) REQUIRE n.x IS UNIQUE"
		nodeKeyX    = "CREATE CONSTRAINT k IF NOT EXISTS FOR (n:A) REQUIRE n.x IS NODE KEY"
		uniqueXY    = "CREATE CONSTRAINT u IF NOT EXISTS FOR (n:A) REQUIRE (n.x, n.y) IS UNIQUE"
		nodeKeyXY   = "CREATE CONSTRAINT k IF NOT EXISTS FOR (n:A) REQUIRE (n.x, n.y) IS NODE KEY"
		nodeKeyYX   = "CREATE CONSTRAINT k IF NOT EXISTS FOR (n:A) REQUIRE (n.y, n.x) IS NODE KEY"
		uniqueYX    = "CREATE CONSTRAINT u IF NOT EXISTS FOR (n:A) REQUIRE (n.y, n.x) IS UNIQUE"
		refusedHere = "ConstraintAlreadyExists"
	)
	for _, tc := range []struct {
		name, holder, statement, wantCode string
	}{
		{"NODE KEY beside a UNIQUE", uniqueX, nodeKeyX, refusedHere},
		{"UNIQUE beside a NODE KEY", nodeKeyX, uniqueX, refusedHere},
		{"a composite NODE KEY beside a UNIQUE in the same order", uniqueXY, nodeKeyXY, refusedHere},
		{"a composite UNIQUE beside a NODE KEY in the same order", nodeKeyXY, uniqueXY, refusedHere},
		{"a composite NODE KEY beside a UNIQUE in the reversed order", uniqueXY, nodeKeyYX, ""},
		{"a composite UNIQUE beside a NODE KEY in the reversed order", nodeKeyXY, uniqueYX, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dropAll(t, ctx)
			wantAnswer(t, ctx, tc.holder, "")
			wantAnswer(t, ctx, tc.statement, tc.wantCode)
		})
	}
}

// An index declared over a primary key meets the key's constraint on the
// server. Applied after the constraint, its CREATE INDEX ... IF NOT EXISTS
// creates nothing and the backing index serves it; applied first, it makes the
// server refuse the constraint. The package docs state the order from these rows.
func TestIndexOverAKeyConstraint_ApplyOrder(t *testing.T) {
	ctx := context.Background()
	driver(t)
	t.Cleanup(func() { dropAll(t, ctx) })

	const index = "CREATE INDEX ord_idx IF NOT EXISTS FOR (n:A) ON (n.x)"
	for _, tc := range []struct {
		name, constraint string
		enterprise       bool
	}{
		{"UNIQUE", "CREATE CONSTRAINT ord_key IF NOT EXISTS FOR (n:A) REQUIRE n.x IS UNIQUE", false},
		{"NODE KEY", "CREATE CONSTRAINT ord_key IF NOT EXISTS FOR (n:A) REQUIRE n.x IS NODE KEY", true},
	} {
		t.Run(tc.name+", the constraint first", func(t *testing.T) {
			if tc.enterprise {
				requireEnterprise(t, ctx)
			}
			dropAll(t, ctx)
			wantAnswer(t, ctx, tc.constraint, "")
			wantAnswer(t, ctx, index, "")
			for _, rec := range query(t, ctx, "SHOW INDEXES YIELD name") {
				if rec["name"] == "ord_idx" {
					t.Errorf("the server created ord_idx beside the constraint's backing index")
				}
			}
		})
		t.Run(tc.name+", the index first", func(t *testing.T) {
			if tc.enterprise {
				requireEnterprise(t, ctx)
			}
			dropAll(t, ctx)
			wantAnswer(t, ctx, index, "")
			wantAnswer(t, ctx, tc.constraint, "IndexAlreadyExists")
		})
	}
}

// A NODE KEY requires every node to hold each key property, so the server refuses
// it over a node that lacks one. The diff's drift Reason and the package docs
// state this: a consumer that drops the other key kind first is left with no
// uniqueness constraint when the create is refused.
func TestNodeKeyOverANodeWithoutTheKey_ServerResponse(t *testing.T) {
	ctx := context.Background()
	driver(t)
	requireEnterprise(t, ctx)
	dropAll(t, ctx)
	t.Cleanup(func() {
		run(t, ctx, "MATCH (n:A) DETACH DELETE n")
		dropAll(t, ctx)
	})

	run(t, ctx, "CREATE (:A {y: 1})")
	wantAnswer(t, ctx, "CREATE CONSTRAINT k IF NOT EXISTS FOR (n:A) REQUIRE n.x IS UNIQUE", "")
	wantAnswer(t, ctx, "DROP CONSTRAINT k", "")
	wantAnswer(t, ctx, "CREATE CONSTRAINT k IF NOT EXISTS FOR (n:A) REQUIRE n.x IS NODE KEY", "ConstraintCreationFailed")
}
