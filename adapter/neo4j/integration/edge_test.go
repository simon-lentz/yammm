//go:build neo4j_integration

package integration

import (
	"context"
	"testing"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v6/neo4j"
	n4j "github.com/simon-lentz/yammm/adapter/neo4j"
)

// mergeRows runs a relationship-merge template over $rows and returns the
// matched_rows the template's always-on RETURN produces.
func mergeRows(t *testing.T, ctx context.Context, stmt string, rows []map[string]any) int64 {
	t.Helper()
	result, err := neo4jdriver.ExecuteQuery(ctx, driver(t), stmt,
		map[string]any{"rows": rows}, neo4jdriver.EagerResultTransformer)
	if err != nil {
		t.Fatalf("executing the relationship merge: %v", err)
	}
	if len(result.Records) != 1 {
		t.Fatalf("relationship merge returned %d records, want exactly 1", len(result.Records))
	}
	matched, ok := result.Records[0].AsMap()["matched_rows"].(int64)
	if !ok {
		t.Fatalf("matched_rows is %T, want int64", result.Records[0].AsMap()["matched_rows"])
	}
	return matched
}

func countRels(t *testing.T, ctx context.Context, relType string) int64 {
	t.Helper()
	rec := single(t, ctx, "MATCH ()-[r:"+relType+"]->() RETURN count(r) AS n")
	n, ok := rec["n"].(int64)
	if !ok {
		t.Fatalf("count(r) is %T, want int64", rec["n"])
	}
	return n
}

// seedEndpoints creates one node per label with the given key value.
func seedEndpoints(t *testing.T, ctx context.Context, fromLabel, toLabel string) {
	t.Helper()
	run(t, ctx, "CREATE (:"+fromLabel+" {order_id: 'o1'})")
	run(t, ctx, "CREATE (:"+toLabel+" {sku: 's1'})")
}

// The batch relationship template MATCHes its endpoints through the row's
// from_/to_ entries, so the template and the row assembler have to agree on
// those prefixes. They are exported as [n4j.RelFromRowPrefix] and
// [n4j.RelToRowPrefix] because a disagreement is silent: the MATCH binds
// nothing, the MERGE never runs, and the driver reports no error. Only a real
// server distinguishes "merged" from "matched nothing" — a string comparison
// against the template cannot.
func TestBatchRelationshipMerge_RowPrefixesMatchTemplate(t *testing.T) {
	ctx := context.Background()
	driver(t)

	const (
		fromLabel = "er__Order"
		toLabel   = "er__Item"
		relType   = "er__CONTAINS"
	)
	clean := func() {
		run(t, ctx, "MATCH (n:"+fromLabel+") DETACH DELETE n")
		run(t, ctx, "MATCH (n:"+toLabel+") DETACH DELETE n")
	}
	clean()
	t.Cleanup(clean)
	seedEndpoints(t, ctx, fromLabel, toLabel)

	stmt := n4j.BuildBatchRelationshipMergeQuery(
		fromLabel, []string{"order_id"},
		relType,
		toLabel, []string{"sku"},
		true,
	)

	row := map[string]any{
		n4j.RelFromRowPrefix + "order_id": "o1",
		n4j.RelToRowPrefix + "sku":        "s1",
		"rel_props":                       map[string]any{"quantity": int64(3)},
	}

	if got := mergeRows(t, ctx, stmt, []map[string]any{row}); got != 1 {
		t.Errorf("matched_rows = %d, want 1 — the row prefixes did not bind the endpoints", got)
	}
	if got := countRels(t, ctx, relType); got != 1 {
		t.Fatalf("relationship count = %d, want 1", got)
	}

	rec := single(t, ctx, "MATCH ()-[r:"+relType+"]->() RETURN r.quantity AS q")
	if q, ok := rec["q"].(int64); !ok || q != 3 {
		t.Errorf("r.quantity = %v (%T), want int64 3 — rel_props did not reach the relationship", rec["q"], rec["q"])
	}

	// MERGE, not CREATE: a second ingestion of the same edge updates in place.
	if got := mergeRows(t, ctx, stmt, []map[string]any{row}); got != 1 {
		t.Errorf("second merge matched_rows = %d, want 1", got)
	}
	if got := countRels(t, ctx, relType); got != 1 {
		t.Errorf("relationship count after re-merge = %d, want 1 — the template duplicated instead of merging", got)
	}
}

// A row whose endpoint keys do not carry the template's prefixes merges nothing
// and reports no error. This is the silent failure the exported prefix
// constants exist to prevent, and matched_rows = 0 is the only signal a
// consumer's safety net can read. Pinned against a real server so the signal
// itself is known to work.
func TestBatchRelationshipMerge_WrongPrefixIsSilentAndReportsZero(t *testing.T) {
	ctx := context.Background()
	driver(t)

	const (
		fromLabel = "er__Src"
		toLabel   = "er__Dst"
		relType   = "er__LINKS"
	)
	clean := func() {
		run(t, ctx, "MATCH (n:"+fromLabel+") DETACH DELETE n")
		run(t, ctx, "MATCH (n:"+toLabel+") DETACH DELETE n")
	}
	clean()
	t.Cleanup(clean)
	run(t, ctx, "CREATE (:"+fromLabel+" {order_id: 'o1'})")
	run(t, ctx, "CREATE (:"+toLabel+" {sku: 's1'})")

	stmt := n4j.BuildBatchRelationshipMergeQuery(
		fromLabel, []string{"order_id"},
		relType,
		toLabel, []string{"sku"},
		false,
	)

	// The values are right and the prefixes are wrong, which is exactly what a
	// consumer spelling the prefixes as literals gets when they drift.
	wrong := map[string]any{
		"src_order_id": "o1",
		"dst_sku":      "s1",
	}

	if got := mergeRows(t, ctx, stmt, []map[string]any{wrong}); got != 0 {
		t.Errorf("matched_rows = %d, want 0 — a non-matching row must report zero, not a match", got)
	}
	if got := countRels(t, ctx, relType); got != 0 {
		t.Errorf("relationship count = %d, want 0 — a non-matching row must not write an edge", got)
	}
}
