package neo4j

import (
	"strings"
	"testing"
)

// The prefix values are the published row contract, so they are pinned against
// literals rather than against themselves. Spelling the constant on both sides
// of the comparison would restate it, not pin it: a rename would move both and
// the test would stay green.
func TestRowPrefixes_ArePublishedValues(t *testing.T) {
	t.Parallel()
	if RelFromRowPrefix != "from_" {
		t.Errorf("RelFromRowPrefix = %q, want %q; consumers assemble rows on this value", RelFromRowPrefix, "from_")
	}
	if RelToRowPrefix != "to_" {
		t.Errorf("RelToRowPrefix = %q, want %q; consumers assemble rows on this value", RelToRowPrefix, "to_")
	}
}

// The template must read the keys the prefixes name, so a consumer that
// assembles rows from the constants builds rows the query can read.
func TestBatchRelationshipMergeQuery_ComposesFromTheExportedRowPrefixes(t *testing.T) {
	t.Parallel()
	got := BuildBatchRelationshipMergeQuery(
		"A", []string{"src_id", "src_region"},
		"KNOWS",
		"B", []string{"dst_id"},
		true,
	)
	for _, want := range []string{
		"row." + RelFromRowPrefix + "src_id",
		"row." + RelFromRowPrefix + "src_region",
		"row." + RelToRowPrefix + "dst_id",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("query does not read %q:\n%s", want, got)
		}
	}
}

// The rows BatchEdgeQueries assembles must key on the same constants the
// template reads — the other half of the contract, from the writer's side.
func TestBatchEdgeQueries_KeysRowsOnTheExportedRowPrefixes(t *testing.T) {
	t.Parallel()
	a, s, v, shapes := setupWrite(t, "edge_mixed.yammm")
	snap := buildGraphResult(t, s, v, map[string][]map[string]any{
		"Company": {{"company_id": "c1", "name": "Acme"}},
		"Employee": {{
			"employee_id": "e1",
			"name":        "Ada",
			"works_at":    map[string]any{"_target_company_id": "c1", "note": "since 2020"},
		}},
	})

	batches, err := a.BatchEdgeQueries(t.Context(), snap, shapes)
	if err != nil {
		t.Fatalf("BatchEdgeQueries: %v", err)
	}
	if len(batches) == 0 {
		t.Fatal("no edge batches, so this asserts nothing")
	}
	for _, b := range batches {
		rows, ok := b.Params["rows"].([]map[string]any)
		if !ok || len(rows) == 0 {
			t.Fatalf("batch params hold no rows: %#v", b.Params)
		}
		for _, row := range rows {
			var from, to int
			for k := range row {
				switch {
				case strings.HasPrefix(k, RelFromRowPrefix):
					from++
				case strings.HasPrefix(k, RelToRowPrefix):
					to++
				case k == "rel_props":
				default:
					t.Errorf("row key %q carries neither exported prefix", k)
				}
			}
			if from == 0 || to == 0 {
				t.Errorf("row %#v has %d from-keys and %d to-keys; want at least one of each", row, from, to)
			}
			// Every key the statement reads must be present in the row.
			for k := range row {
				if k == "rel_props" {
					continue
				}
				if !strings.Contains(b.Statement, "row."+k) {
					t.Errorf("row carries %q but the statement never reads it:\n%s", k, b.Statement)
				}
			}
		}
	}
}
