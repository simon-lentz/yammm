package main

import (
	"bytes"
	"strings"
	"testing"

	adaptern4j "github.com/simon-lentz/yammm/adapter/neo4j"

	"github.com/stretchr/testify/assert"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
	"github.com/simon-lentz/yammm/schema"
)

func TestPrintDiffResult_AllMatch(t *testing.T) {
	t.Parallel()

	diff := &adaptern4j.ConstraintDiffResult{
		Match: []adaptern4j.ConstraintMatch{
			{Desired: adaptern4j.Constraint{Name: "c1"}, Actual: adaptern4j.RemoteConstraint{Name: "c1"}},
		},
	}

	var buf bytes.Buffer
	printDiffResult(&buf, diff)

	assert.Contains(t, buf.String(), "matched: 1")
	assert.Contains(t, buf.String(), "0 drifted")
	assert.Contains(t, buf.String(), "0 to create")
	assert.Contains(t, buf.String(), "0 to drop")
}

func TestPrintDiffResult_WithDrift(t *testing.T) {
	t.Parallel()

	diff := &adaptern4j.ConstraintDiffResult{
		Drift: []adaptern4j.ConstraintDrift{
			{
				Desired: adaptern4j.Constraint{Name: "c1"},
				Blockers: adaptern4j.Blockers{
					Constraints: []adaptern4j.RemoteConstraint{{Name: "c1", Type: "NODE_PROPERTY_TYPE", LabelsOrTypes: []string{"app__T"}, Properties: []string{"x"}}},
					Indexes:     []adaptern4j.RemoteIndex{{Name: "held", Type: "TEXT", LabelsOrTypes: []string{"app__T"}, Properties: []string{"x"}}},
				},
				Reason: "type mismatch",
			},
		},
		Create: []adaptern4j.Constraint{
			{Name: "c2", Statement: "CREATE CONSTRAINT c2 ..."},
		},
	}

	var buf bytes.Buffer
	printDiffResult(&buf, diff)

	assert.Contains(t, buf.String(), "drift: c1")
	assert.Contains(t, buf.String(), "type mismatch")
	assert.Contains(t, buf.String(), "  blocker: NODE_PROPERTY_TYPE constraint \"c1\" on app__T(x)\n")
	assert.Contains(t, buf.String(), "  blocker: TEXT index \"held\" on app__T(x)\n")
	assert.Contains(t, buf.String(), "create: CREATE CONSTRAINT c2")
}

func TestPrintIndexDiffResult_AllMatch(t *testing.T) {
	t.Parallel()

	diff := &adaptern4j.IndexDiffResult{
		Match: []adaptern4j.IndexMatch{
			{Desired: adaptern4j.Index{Name: "i1"}, Actual: adaptern4j.RemoteIndex{Name: "i1"}},
		},
	}

	var buf bytes.Buffer
	printIndexDiffResult(&buf, diff, nil)

	assert.Contains(t, buf.String(), "matched: 1 indexes")
	assert.Contains(t, buf.String(), "0 to create")
	assert.Contains(t, buf.String(), "0 to drop")
}

func TestPrintIndexDiffResult_DriftCreateDrop(t *testing.T) {
	t.Parallel()

	diff := &adaptern4j.IndexDiffResult{
		Drift: []adaptern4j.IndexDrift{
			{
				Desired: adaptern4j.Index{Name: "i1"},
				Blockers: adaptern4j.Blockers{
					Constraints: []adaptern4j.RemoteConstraint{{Name: "nn", Type: "NODE_PROPERTY_EXISTENCE", LabelsOrTypes: []string{"test__Foo"}, Properties: []string{"v"}}},
					Indexes: []adaptern4j.RemoteIndex{
						{Name: "i1", Type: "VECTOR", LabelsOrTypes: []string{"test__Foo"}, Properties: []string{"v"}},
						{Name: "pk", Type: "RANGE", LabelsOrTypes: []string{"test__Foo"}, Properties: []string{"id"}, OwningConstraint: "pk"},
					},
				},
				Reason: "vector dimension mismatch",
			},
		},
		Create: []adaptern4j.Index{
			{Name: "i2", Statement: "CREATE INDEX i2 IF NOT EXISTS ..."},
		},
		Drop: []adaptern4j.RemoteIndex{
			{Name: "i3", Type: "RANGE", LabelsOrTypes: []string{"test__Foo"}},
		},
	}

	var buf bytes.Buffer
	printIndexDiffResult(&buf, diff, nil)

	assert.Contains(t, buf.String(), "index drift: i1")
	assert.Contains(t, buf.String(), "vector dimension mismatch")
	assert.Contains(t, buf.String(), "  index blocker: VECTOR index \"i1\" on test__Foo(v)\n")
	assert.Contains(t, buf.String(), "  index blocker: RANGE index \"pk\" on test__Foo(id), which backs constraint \"pk\": drop the constraint\n")
	assert.Contains(t, buf.String(), "  index blocker: NODE_PROPERTY_EXISTENCE constraint \"nn\" on test__Foo(v)\n")
	assert.Contains(t, buf.String(), "index create: CREATE INDEX i2")
	assert.Contains(t, buf.String(), "index drop: i3")
}

// TestPrintDiffResult_BlockersFollowTheirDrift pins each blocker line under its
// own drift, constraints first, with every label and property joined, and names
// a row whose type is unknown by its own kind.
func TestPrintDiffResult_BlockersFollowTheirDrift(t *testing.T) {
	t.Parallel()

	diff := &adaptern4j.ConstraintDiffResult{
		Drift: []adaptern4j.ConstraintDrift{
			{
				Desired: adaptern4j.Constraint{Name: "c1"},
				Blockers: adaptern4j.Blockers{
					Constraints: []adaptern4j.RemoteConstraint{{Name: "c1", Type: "NODE_KEY", LabelsOrTypes: []string{"app__T"}, Properties: []string{"a", "b"}}},
					Indexes:     []adaptern4j.RemoteIndex{{Name: "multi", Type: "TEXT", LabelsOrTypes: []string{"app__T", "app__U"}, Properties: []string{"a"}}},
				},
				Reason: "r1",
			},
			{
				Desired: adaptern4j.Constraint{Name: "c2"},
				Blockers: adaptern4j.Blockers{
					Constraints: []adaptern4j.RemoteConstraint{{Name: "untyped", LabelsOrTypes: []string{"app__T"}, Properties: []string{"b"}}},
					Indexes:     []adaptern4j.RemoteIndex{{Name: "range", LabelsOrTypes: []string{"app__T"}, Properties: []string{"b"}, OwningConstraint: "k"}},
				},
				Reason: "r2",
			},
		},
	}

	var buf bytes.Buffer
	printDiffResult(&buf, diff)

	assert.Contains(t, buf.String(), "drift: c1 (r1)\n"+
		"  blocker: NODE_KEY constraint \"c1\" on app__T(a, b)\n"+
		"  blocker: TEXT index \"multi\" on app__T|app__U(a)\n"+
		"drift: c2 (r2)\n"+
		"  blocker: constraint \"untyped\" on app__T(b)\n"+
		"  blocker: index \"range\" on app__T(b), which backs constraint \"k\": drop the constraint\n")
}

// TestPrintIndexDiffResult_BlockersFollowTheirDrift pins each index blocker line
// under its own drift, constraints first, with every label and property joined,
// names the owning constraint of a backing index, and names a row whose type is
// unknown by its own kind.
func TestPrintIndexDiffResult_BlockersFollowTheirDrift(t *testing.T) {
	t.Parallel()

	diff := &adaptern4j.IndexDiffResult{
		Drift: []adaptern4j.IndexDrift{
			{
				Desired: adaptern4j.Index{Name: "i1"},
				Blockers: adaptern4j.Blockers{Indexes: []adaptern4j.RemoteIndex{
					{Name: "idx", Type: "RANGE", LabelsOrTypes: []string{"app__T"}, Properties: []string{"a", "b"}, OwningConstraint: "c"},
					{Name: "multi", Type: "TEXT", LabelsOrTypes: []string{"app__T", "app__U"}, Properties: []string{"a"}},
				}},
				Reason: "r1",
			},
			{
				Desired: adaptern4j.Index{Name: "i2"},
				Blockers: adaptern4j.Blockers{
					Constraints: []adaptern4j.RemoteConstraint{{Name: "nn", Type: "NODE_PROPERTY_EXISTENCE", LabelsOrTypes: []string{"app__T"}, Properties: []string{"b"}}},
					Indexes:     []adaptern4j.RemoteIndex{{Name: "untyped", LabelsOrTypes: []string{"app__T"}, Properties: []string{"b"}}},
				},
				Reason: "r2",
			},
		},
	}

	var buf bytes.Buffer
	printIndexDiffResult(&buf, diff, nil)

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

// TestNeo4jDiff_AcceptsConfigFlags proves the --edition, --separator, and
// --named flags are wired (mirroring the constraints command): the command
// proceeds past flag parsing to the connection attempt (which fails on the
// refused loopback port), so the exit code is not the usage code. The targeted
// "invalid edition" message (written to os.Stderr, which in-process cobra
// buffers cannot capture) is asserted by the neo4j_diff_bad_edition.txtar
// script instead.
func TestNeo4jDiff_AcceptsConfigFlags(t *testing.T) {
	t.Parallel()
	code := executeCmd(t, "neo4j", "diff", "--uri", "neo4j://127.0.0.1:1",
		"--edition", "community", "--separator", "__", "--named=false", "testdata/valid.yammm")
	assert.NotEqual(t, 2, code)
}

// TestNeo4jDiff_IndexesFlag proves the --indexes opt-out is wired: with
// --indexes=false the command parses the flag and proceeds to the connection
// attempt (which fails on the refused loopback port), so the exit code is not
// the usage code.
func TestNeo4jDiff_IndexesFlag(t *testing.T) {
	t.Parallel()
	code := executeCmd(t, "neo4j", "diff", "--uri", "neo4j://127.0.0.1:1",
		"--indexes=false", "testdata/valid.yammm")
	assert.NotEqual(t, 2, code)
}

// TestNeo4jDiffExit pins the exit-code contract of the two diff halves. The
// load-bearing rows are the two incomplete comparisons — "unavailable"
// (introspection failed) and "unverified" (an index whose configuration the
// server would not disclose). Both keep the printed constraint diff but must not
// report success: a drift gate would otherwise read exit 0 as "no drift" from a
// comparison that never ran.
// TestIndexOutcomeBeforeDiff pins the step between the read and the exit table:
// only a read that was ASKED FOR and failed is unavailable, so --indexes=false
// over a server that cannot report indexes still exits 0.
func TestIndexOutcomeBeforeDiff(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		enabled, failed bool
		want            indexDiffOutcome
	}{
		{"asked for and read", true, false, indexDiffSkipped},
		{"asked for and unreadable", true, true, indexDiffUnavailable},
		{"opted out, read fine", false, false, indexDiffSkipped},
		{"opted out, unreadable", false, true, indexDiffSkipped},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := indexOutcomeBeforeDiff(tt.enabled, tt.failed); got != tt.want {
				t.Errorf("indexOutcomeBeforeDiff(%v, %v) = %v, want %v",
					tt.enabled, tt.failed, got, tt.want)
			}
		})
	}
}

func TestNeo4jDiffExit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                 string
		constraintDrift      bool
		constraintUnverified bool
		indexes              indexDiffOutcome
		want                 int
	}{
		{"all clean", false, false, indexDiffClean, cli.ExitOK},
		{"opted out of index diffing", false, false, indexDiffSkipped, cli.ExitOK},
		{"constraint drift", true, false, indexDiffClean, cli.ExitValidation},
		{"index drift", false, false, indexDiffDrifted, cli.ExitValidation},
		{"both drifted", true, false, indexDiffDrifted, cli.ExitValidation},
		{"index diff unavailable", false, false, indexDiffUnavailable, cli.ExitRuntime},
		{"constraint drift outranks unavailable", true, false, indexDiffUnavailable, cli.ExitValidation},
		{"index unverified", false, false, indexDiffUnverified, cli.ExitRuntime},
		{"constraint drift outranks unverified", true, false, indexDiffUnverified, cli.ExitValidation},
		// A constraint the server would not fully describe is unverified in the
		// same sense an index is, and must not exit 0 — even when the index half
		// is clean, and even when the caller opted out of index diffing entirely.
		{"constraint unverified", false, true, indexDiffClean, cli.ExitRuntime},
		{"constraint unverified with indexes skipped", false, true, indexDiffSkipped, cli.ExitRuntime},
		{"constraint drift outranks constraint unverified", true, true, indexDiffClean, cli.ExitValidation},
		{"index drift outranks constraint unverified", false, true, indexDiffDrifted, cli.ExitValidation},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := neo4jDiffExit(tt.constraintDrift, tt.constraintUnverified, tt.indexes); got != tt.want {
				t.Errorf("neo4jDiffExit(%v, %v, %v) = %d, want %d",
					tt.constraintDrift, tt.constraintUnverified, tt.indexes, got, tt.want)
			}
		})
	}
}

// An index whose configuration the database did not report is printed as
// unverified, never folded into the matched count: "we could not check this" and
// "this is in sync" are different claims.
func TestPrintIndexDiffResult_Unverified(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	printIndexDiffResult(&buf, &adaptern4j.IndexDiffResult{
		Unverified: []adaptern4j.IndexUnverified{{
			Desired: adaptern4j.Index{Name: "test__Entity_embedding_vector_idx"},
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

// A constraint that realises the primary key and holds a declared index's name
// drifts, so the index can be created. After a failed index read the plan
// declares no index, and the printed constraint half carries no such move.
func TestPlanRemoteState_AFailedIndexReadDeclaresNoIndex(t *testing.T) {
	t.Parallel()
	s, loaded := schema.LoadString(t.Context(), "schema \"cx\"\n\ntype Doc {\n\tid String primary\n\tbody String @index\n}\n", "cx.yammm")
	if err := loaded.Err(); err != nil {
		t.Fatal(err)
	}
	adapter := adaptern4j.New(adaptern4j.WithEdition(adaptern4j.Community))
	desired, result := adapter.ConstraintsStructured(t.Context(), s)
	if err := result.Err(); err != nil || len(desired) != 1 {
		t.Fatalf("%d declared constraints, want 1: %v", len(desired), err)
	}
	desiredIndexes, result := adapter.IndexesStructured(t.Context(), s)
	if err := result.Err(); err != nil || len(desiredIndexes) != 1 {
		t.Fatalf("%d declared indexes, want 1: %v", len(desiredIndexes), err)
	}
	holder := desiredIndexes[0].Name
	key := adaptern4j.RemoteConstraint{Name: holder, Type: "UNIQUENESS", EntityType: "NODE", LabelsOrTypes: []string{desired[0].Label}, Properties: []string{"id"}}
	backing := adaptern4j.RemoteIndex{Name: holder, Type: "RANGE", EntityType: "NODE", State: "ONLINE", LabelsOrTypes: key.LabelsOrTypes, Properties: key.Properties, OwningConstraint: holder}
	owned := adapter.OwnedLabels(t.Context(), s)

	read := planRemoteState(adapter, desired, desiredIndexes,
		remoteState{constraints: []adaptern4j.RemoteConstraint{key}, indexes: []adaptern4j.RemoteIndex{backing}}, indexDiffSkipped, owned)
	if len(read.Constraints.Drift) != 1 {
		t.Fatalf("after a read, constraints = %+v; want the key drifted so the index can take its name", read.Constraints)
	}
	failed := planRemoteState(adapter, desired, desiredIndexes,
		remoteState{constraints: []adaptern4j.RemoteConstraint{key}, indexFailed: true}, indexDiffUnavailable, owned)
	if len(failed.Constraints.Match) != 1 || len(failed.Constraints.Drift) != 0 {
		t.Errorf("after a failed read, constraints = %+v; want the key matched", failed.Constraints)
	}
	if n := len(failed.Indexes.Drift) + len(failed.Indexes.Create); n != 0 {
		t.Errorf("after a failed read, indexes = %+v; want no index planned", failed.Indexes)
	}
}
