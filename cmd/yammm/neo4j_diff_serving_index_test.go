package main

import (
	"bytes"
	"strings"
	"testing"

	adaptern4j "github.com/simon-lentz/yammm/adapter/neo4j"
	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
	"github.com/simon-lentz/yammm/schema"
)

// A composite key's uniqueness constraint is backed by an index that already
// serves a `@@index` declared over the same properties, so the server creates
// no second index and the diff reads the backing index as the declaration's.
// While that index does not serve queries the report says so once: the index
// is the declaration's drift or its unverified, and the note that counts the
// indexes the diff did not compare does not count it again.
func TestNeo4jDiff_ABackingIndexThatServesADeclarationIsReportedOnce(t *testing.T) {
	t.Parallel()
	s, loaded := schema.LoadString(t.Context(),
		"schema \"gs\"\n\ntype T {\n\ta String primary\n\tb String primary\n\t@@index(a, b)\n}\n", "serving.yammm")
	if err := loaded.Err(); err != nil {
		t.Fatal(err)
	}
	adapter := adaptern4j.New()
	desired, result := adapter.IndexesStructured(t.Context(), s)
	if err := result.Err(); err != nil || len(desired) != 1 {
		t.Fatalf("%d declared indexes, want 1: %v", len(desired), err)
	}

	for _, c := range []struct {
		state string
		line  string
	}{
		{"ONLINE", "matched: 1 indexes"},
		{"POPULATING", "index unverified: gs__T_a_b_idx"},
		{"FAILED", "index drift: gs__T_a_b_idx"},
	} {
		t.Run(c.state, func(t *testing.T) {
			t.Parallel()
			rows := []map[string]any{{
				"name": "gs__T_a_b_unique", "type": "RANGE", "entityType": "NODE", "state": c.state,
				"labelsOrTypes": []any{"gs__T"}, "properties": []any{"a", "b"}, "owningConstraint": "gs__T_a_b_unique",
			}}
			log := &queryLog{replies: []queryReply{{}, {records: rows}}}
			var diagnostics bytes.Buffer
			state, err := fetchRemoteState(t.Context(), log.run, "neo4j", cli.NewDiagnosticSink(&diagnostics, cli.FormatText, true, false))
			if err != nil || state.indexFailed || len(state.indexes) != 1 {
				t.Fatalf("fetchRemoteState: %v, index read failed: %v, %d indexes", err, state.indexFailed, len(state.indexes))
			}

			owned := adapter.OwnedLabels(t.Context(), s)
			var out bytes.Buffer
			printIndexDiffResult(&out, adapter.DiffIndexes(desired, state.indexes, owned), owned)
			if !strings.Contains(out.String(), c.line) {
				t.Errorf("the report lacks %q:\n%s", c.line, out.String())
			}
			if strings.Contains(out.String(), "were not compared") {
				t.Errorf("the report counts the index it compared among those it did not:\n%s", out.String())
			}
		})
	}
}
