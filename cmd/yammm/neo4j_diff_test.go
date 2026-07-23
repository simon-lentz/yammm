package main

import (
	"bytes"
	"strings"
	"testing"

	adaptern4j "github.com/simon-lentz/yammm/adapter/neo4j"

	"github.com/stretchr/testify/assert"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
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
				Actual:  adaptern4j.RemoteConstraint{Name: "c1"},
				Reason:  "type mismatch",
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
	printIndexDiffResult(&buf, diff)

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
				Actual:  adaptern4j.RemoteIndex{Name: "i1"},
				Reason:  "vector dimension mismatch",
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
	printIndexDiffResult(&buf, diff)

	assert.Contains(t, buf.String(), "index drift: i1")
	assert.Contains(t, buf.String(), "vector dimension mismatch")
	assert.Contains(t, buf.String(), "index create: CREATE INDEX i2")
	assert.Contains(t, buf.String(), "index drop: i3")
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
	// The command progresses past URI validation but fails at connection.
	// Exit code 1 (validation) not 2 (usage) proves the env var was picked up.
	assert.NotEqual(t, 2, code)
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
// load-bearing row is "index diff unavailable": degrading to constraints-only
// keeps the printed constraint diff, but must not report success — a drift gate
// would otherwise read exit 0 as "no drift" from a comparison that never ran.
func TestNeo4jDiffExit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		constraintDrift bool
		indexes         indexDiffOutcome
		want            int
	}{
		{"all clean", false, indexDiffClean, cli.ExitOK},
		{"opted out of index diffing", false, indexDiffSkipped, cli.ExitOK},
		{"constraint drift", true, indexDiffClean, cli.ExitValidation},
		{"index drift", false, indexDiffDrifted, cli.ExitValidation},
		{"both drifted", true, indexDiffDrifted, cli.ExitValidation},
		{"index diff unavailable", false, indexDiffUnavailable, cli.ExitRuntime},
		{"constraint drift outranks unavailable", true, indexDiffUnavailable, cli.ExitValidation},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := neo4jDiffExit(tt.constraintDrift, tt.indexes); got != tt.want {
				t.Errorf("neo4jDiffExit(%v, %v) = %d, want %d", tt.constraintDrift, tt.indexes, got, tt.want)
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
	})

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
