package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/neo4j"
	"github.com/simon-lentz/yammm/schema/cypher"

	"github.com/stretchr/testify/assert"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
)

func TestPrintConstraintDiff_AllMatch(t *testing.T) {
	t.Parallel()

	diff := &neo4j.ConstraintDiff{
		Match: []neo4j.ConstraintMatch{
			{Desired: cypher.Constraint{Name: "c1"}, Actual: neo4j.RemoteConstraint{Name: "c1"}},
		},
	}

	var buf bytes.Buffer
	printConstraintDiff(&buf, diff)

	assert.Contains(t, buf.String(), "matched: 1")
	assert.Contains(t, buf.String(), "0 drifted")
	assert.Contains(t, buf.String(), "0 to create")
	assert.Contains(t, buf.String(), "0 to drop")
}

func TestPrintConstraintDiff_WithDrift(t *testing.T) {
	t.Parallel()

	diff := &neo4j.ConstraintDiff{
		Drift: []neo4j.ConstraintDrift{
			{
				Desired: cypher.Constraint{Name: "c1"},
				Blockers: neo4j.Blockers{
					Constraints: []neo4j.RemoteConstraint{{Name: "c1", Type: "NODE_PROPERTY_TYPE", LabelsOrTypes: []string{"app__T"}, Properties: []string{"x"}}},
					Indexes:     []neo4j.RemoteIndex{{Name: "held", Type: "TEXT", LabelsOrTypes: []string{"app__T"}, Properties: []string{"x"}}},
				},
				Reason: "type mismatch",
			},
		},
		Create: []cypher.Constraint{
			{Name: "c2", Statement: "CREATE CONSTRAINT c2 ..."},
		},
	}

	var buf bytes.Buffer
	printConstraintDiff(&buf, diff)

	assert.Contains(t, buf.String(), "drift: c1")
	assert.Contains(t, buf.String(), "type mismatch")
	assert.Contains(t, buf.String(), "  blocker: NODE_PROPERTY_TYPE constraint \"c1\" on app__T(x)\n")
	assert.Contains(t, buf.String(), "  blocker: TEXT index \"held\" on app__T(x)\n")
	assert.Contains(t, buf.String(), "create: CREATE CONSTRAINT c2")
}

func TestPrintIndexDiff_AllMatch(t *testing.T) {
	t.Parallel()

	diff := &neo4j.IndexDiff{
		Match: []neo4j.IndexMatch{
			{Desired: cypher.Index{Name: "i1"}, Actual: neo4j.RemoteIndex{Name: "i1"}},
		},
	}

	var buf bytes.Buffer
	printIndexDiff(&buf, diff, nil)

	assert.Contains(t, buf.String(), "matched: 1 indexes")
	assert.Contains(t, buf.String(), "0 to create")
	assert.Contains(t, buf.String(), "0 to drop")
}

func TestPrintIndexDiff_DriftCreateDrop(t *testing.T) {
	t.Parallel()

	diff := &neo4j.IndexDiff{
		Drift: []neo4j.IndexDrift{
			{
				Desired: cypher.Index{Name: "i1"},
				Blockers: neo4j.Blockers{
					Constraints: []neo4j.RemoteConstraint{{Name: "nn", Type: "NODE_PROPERTY_EXISTENCE", LabelsOrTypes: []string{"test__Foo"}, Properties: []string{"v"}}},
					Indexes: []neo4j.RemoteIndex{
						{Name: "i1", Type: "VECTOR", LabelsOrTypes: []string{"test__Foo"}, Properties: []string{"v"}},
						{Name: "pk", Type: "RANGE", LabelsOrTypes: []string{"test__Foo"}, Properties: []string{"id"}, OwningConstraint: "pk"},
					},
				},
				Reason: "vector dimension mismatch",
			},
		},
		Create: []cypher.Index{
			{Name: "i2", Statement: "CREATE INDEX i2 IF NOT EXISTS ..."},
		},
		Drop: []neo4j.RemoteIndex{
			{Name: "i3", Type: "RANGE", LabelsOrTypes: []string{"test__Foo"}},
		},
	}

	var buf bytes.Buffer
	printIndexDiff(&buf, diff, nil)

	assert.Contains(t, buf.String(), "index drift: i1")
	assert.Contains(t, buf.String(), "vector dimension mismatch")
	assert.Contains(t, buf.String(), "  index blocker: VECTOR index \"i1\" on test__Foo(v)\n")
	assert.Contains(t, buf.String(), "  index blocker: RANGE index \"pk\" on test__Foo(id), which backs constraint \"pk\": drop the constraint\n")
	assert.Contains(t, buf.String(), "  index blocker: NODE_PROPERTY_EXISTENCE constraint \"nn\" on test__Foo(v)\n")
	assert.Contains(t, buf.String(), "index create: CREATE INDEX i2")
	assert.Contains(t, buf.String(), "index drop: i3")
}

// TestPrintConstraintDiff_BlockersFollowTheirDrift pins each blocker line under its
// own drift, constraints first, with every label and property joined, and names
// a row whose type is unknown by its own kind.
func TestPrintConstraintDiff_BlockersFollowTheirDrift(t *testing.T) {
	t.Parallel()

	diff := &neo4j.ConstraintDiff{
		Drift: []neo4j.ConstraintDrift{
			{
				Desired: cypher.Constraint{Name: "c1"},
				Blockers: neo4j.Blockers{
					Constraints: []neo4j.RemoteConstraint{{Name: "c1", Type: "NODE_KEY", LabelsOrTypes: []string{"app__T"}, Properties: []string{"a", "b"}}},
					Indexes:     []neo4j.RemoteIndex{{Name: "multi", Type: "TEXT", LabelsOrTypes: []string{"app__T", "app__U"}, Properties: []string{"a"}}},
				},
				Reason: "r1",
			},
			{
				Desired: cypher.Constraint{Name: "c2"},
				Blockers: neo4j.Blockers{
					Constraints: []neo4j.RemoteConstraint{{Name: "untyped", LabelsOrTypes: []string{"app__T"}, Properties: []string{"b"}}},
					Indexes:     []neo4j.RemoteIndex{{Name: "range", LabelsOrTypes: []string{"app__T"}, Properties: []string{"b"}, OwningConstraint: "k"}},
				},
				Reason: "r2",
			},
		},
	}

	var buf bytes.Buffer
	printConstraintDiff(&buf, diff)

	assert.Contains(t, buf.String(), "drift: c1 (r1)\n"+
		"  blocker: NODE_KEY constraint \"c1\" on app__T(a, b)\n"+
		"  blocker: TEXT index \"multi\" on app__T|app__U(a)\n"+
		"drift: c2 (r2)\n"+
		"  blocker: constraint \"untyped\" on app__T(b)\n"+
		"  blocker: index \"range\" on app__T(b), which backs constraint \"k\": drop the constraint\n")
}

// TestPrintIndexDiff_BlockersFollowTheirDrift pins each index blocker line
// under its own drift, constraints first, with every label and property joined,
// names the owning constraint of a backing index, and names a row whose type is
// unknown by its own kind.
func TestPrintIndexDiff_BlockersFollowTheirDrift(t *testing.T) {
	t.Parallel()

	diff := &neo4j.IndexDiff{
		Drift: []neo4j.IndexDrift{
			{
				Desired: cypher.Index{Name: "i1"},
				Blockers: neo4j.Blockers{Indexes: []neo4j.RemoteIndex{
					{Name: "idx", Type: "RANGE", LabelsOrTypes: []string{"app__T"}, Properties: []string{"a", "b"}, OwningConstraint: "c"},
					{Name: "multi", Type: "TEXT", LabelsOrTypes: []string{"app__T", "app__U"}, Properties: []string{"a"}},
				}},
				Reason: "r1",
			},
			{
				Desired: cypher.Index{Name: "i2"},
				Blockers: neo4j.Blockers{
					Constraints: []neo4j.RemoteConstraint{{Name: "nn", Type: "NODE_PROPERTY_EXISTENCE", LabelsOrTypes: []string{"app__T"}, Properties: []string{"b"}}},
					Indexes:     []neo4j.RemoteIndex{{Name: "untyped", LabelsOrTypes: []string{"app__T"}, Properties: []string{"b"}}},
				},
				Reason: "r2",
			},
		},
	}

	var buf bytes.Buffer
	printIndexDiff(&buf, diff, nil)

	assert.Contains(t, buf.String(), "index drift: i1 (r1)\n"+
		"  index blocker: RANGE index \"idx\" on app__T(a, b), which backs constraint \"c\": drop the constraint\n"+
		"  index blocker: TEXT index \"multi\" on app__T|app__U(a)\n"+
		"index drift: i2 (r2)\n"+
		"  index blocker: NODE_PROPERTY_EXISTENCE constraint \"nn\" on app__T(b)\n"+
		"  index blocker: index \"untyped\" on app__T(b)\n")
}

func TestNeo4jDiff_EnvVarSatisfiesURI(t *testing.T) {
	// Cannot run in parallel due to env var mutation.
	//
	// Loopback port 1 is essentially never listening (binding it requires
	// root) and refuses connections immediately, so the connection failure
	// is deterministic and fast — unlike a real localhost:7687, where a
	// developer's running Neo4j would change the outcome, or an unroutable
	// TEST-NET address, where the dial would hang until timeout.
	t.Setenv("YAMMM_NEO4J_URI", "neo4j://127.0.0.1:1")

	code := executeCmd(t, "neo4j", "diff", "testdata/valid.yammm")
	// The command passes the --uri guard and fails at the connection: exit 3
	// (runtime), where a URI it never read is exit 2 (usage).
	assert.Equal(t, cli.ExitRuntime, code)
}

// TestNeo4jDiff_AcceptsConfigFlags proves the --edition and --separator flags
// are wired (mirroring the constraints command): the command
// proceeds past flag parsing to the connection attempt (which fails on the
// refused loopback port), so the exit code is not the usage code. The targeted
// "invalid edition" message (written to os.Stderr, which in-process cobra
// buffers cannot capture) is asserted by the neo4j_diff_bad_edition.txtar
// script instead.
func TestNeo4jDiff_AcceptsConfigFlags(t *testing.T) {
	t.Parallel()
	code := executeCmd(t, "neo4j", "diff", "--uri", "neo4j://127.0.0.1:1",
		"--edition", "community", "--separator", "__", "testdata/valid.yammm")
	assert.NotEqual(t, 2, code)
}

// An index whose configuration the database did not report is printed as
// unverified, never folded into the matched count: "we could not check this" and
// "this is in sync" are different claims.
func TestPrintIndexDiff_Unverified(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	printIndexDiff(&buf, &neo4j.IndexDiff{
		Unverified: []neo4j.IndexUnverified{{
			Desired: cypher.Index{Name: "test__Entity_embedding_vector_idx"},
			Reason:  "database reported no vector configuration",
		}},
	}, nil)

	out := buf.String()
	if !strings.Contains(out, "index unverified: test__Entity_embedding_vector_idx") {
		t.Errorf("unverified index should be listed; got:\n%s", out)
	}
	if !strings.Contains(out, "index note: 1 index(es) could not be fully verified") {
		t.Errorf("unverified count should be noted; got:\n%s", out)
	}
	if strings.Contains(out, "matched: 1") {
		t.Errorf("an unverified index must not be counted as matched; got:\n%s", out)
	}
}

// TestNeo4jDiffExit pins the exit-code contract of the plan. A change on
// either half outranks an unverified object, and an unverified object on either
// half must not report success: a drift gate would otherwise read exit 0 as "no
// drift" from a comparison that never ran.
func TestNeo4jDiffExit(t *testing.T) {
	t.Parallel()

	var (
		cDrift      = neo4j.ConstraintDiff{Drift: []neo4j.ConstraintDrift{{}}}
		cCreate     = neo4j.ConstraintDiff{Create: []cypher.Constraint{{}}}
		cDrop       = neo4j.ConstraintDiff{Drop: []neo4j.RemoteConstraint{{}}}
		cUnverified = neo4j.ConstraintDiff{Unverified: []neo4j.ConstraintUnverified{{}}}
		iDrift      = neo4j.IndexDiff{Drift: []neo4j.IndexDrift{{}}}
		iCreate     = neo4j.IndexDiff{Create: []cypher.Index{{}}}
		iDrop       = neo4j.IndexDiff{Drop: []neo4j.RemoteIndex{{}}}
		iUnverified = neo4j.IndexDiff{Unverified: []neo4j.IndexUnverified{{}}}
	)
	tests := []struct {
		name string
		plan neo4j.Plan
		want int
	}{
		{"all clean", neo4j.Plan{}, cli.ExitOK},
		{"excluded objects alone", neo4j.Plan{
			Constraints: neo4j.ConstraintDiff{Excluded: 2},
			Indexes:     neo4j.IndexDiff{Excluded: 3},
		}, cli.ExitOK},
		{"constraint drift", neo4j.Plan{Constraints: cDrift}, cli.ExitValidation},
		{"constraint create", neo4j.Plan{Constraints: cCreate}, cli.ExitValidation},
		{"constraint drop", neo4j.Plan{Constraints: cDrop}, cli.ExitValidation},
		{"index drift", neo4j.Plan{Indexes: iDrift}, cli.ExitValidation},
		{"index create", neo4j.Plan{Indexes: iCreate}, cli.ExitValidation},
		{"index drop", neo4j.Plan{Indexes: iDrop}, cli.ExitValidation},
		{"constraint unverified", neo4j.Plan{Constraints: cUnverified}, cli.ExitRuntime},
		{"index unverified", neo4j.Plan{Indexes: iUnverified}, cli.ExitRuntime},
		{"constraint drift outranks index unverified", neo4j.Plan{Constraints: cDrift, Indexes: iUnverified}, cli.ExitValidation},
		{"index drop outranks constraint unverified", neo4j.Plan{Constraints: cUnverified, Indexes: iDrop}, cli.ExitValidation},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := neo4jDiffExit(tt.plan); got != tt.want {
				t.Errorf("neo4jDiffExit(%s) = %d, want %d", tt.name, got, tt.want)
			}
		})
	}
}

// TestReportPlan_WritesBothHalves pins that the report names the index half
// beside the constraint half: a plan whose only change is an index must not
// print as a constraint report alone.
func TestReportPlan_WritesBothHalves(t *testing.T) {
	t.Parallel()

	plan := neo4j.Plan{
		Constraints: neo4j.ConstraintDiff{Create: []cypher.Constraint{{Name: "c_new", Statement: "CREATE CONSTRAINT c_new"}}},
		Indexes:     neo4j.IndexDiff{Create: []cypher.Index{{Name: "i_new", Statement: "CREATE INDEX i_new"}}},
	}

	var buf bytes.Buffer
	reportPlan(&buf, plan, nil)
	out := buf.String()

	for _, want := range []string{"c_new", "i_new", "\nsummary: ", "\nindex summary: "} {
		if !strings.Contains(out, want) {
			t.Errorf("report lacks %q; got:\n%s", want, out)
		}
	}
	if c, i := strings.Index(out, "c_new"), strings.Index(out, "i_new"); c > i {
		t.Errorf("the constraint half must come first; got:\n%s", out)
	}
}
