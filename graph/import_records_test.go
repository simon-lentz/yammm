package graph_test

import (
	"slices"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot/snapshottest"
)

const importRecordsSchema = `schema "records"

type Company {
	id String primary
	name String
}

type Employee {
	id String primary
	--> WORKS_AT (_:one) Company
	--> AUDITS (one:many) Company
	--> KNOWS (_:many) Company {
		since Integer
	}
}
`

// importRecordsSnapshot holds every kind of record Add makes: edges with and
// without properties, a forward reference resolved later, target_missing and
// absent records, and a duplicate.
func importRecordsSnapshot(t *testing.T) graph.Snapshot {
	t.Helper()
	s, res := schema.LoadString(t.Context(), importRecordsSchema, "records.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res)
	}
	ref := func(key string) map[string]any { return map[string]any{"_target_id": key} }
	v := instance.NewValidator(s)
	g := graph.New(s)
	for _, row := range []struct {
		typeName string
		props    map[string]any
	}{
		{"Employee", map[string]any{
			"id": "e1", "works_at": ref("c1"), "audits": []any{ref("c1")},
			"knows": []any{map[string]any{"_target_id": "c1", "since": 2020}, map[string]any{"_target_id": "c9", "since": 2021}},
		}},
		{"Employee", map[string]any{"id": "e2", "audits": []any{ref("c2")}}},
		{"Company", map[string]any{"id": "c1", "name": "one"}},
		{"Company", map[string]any{"id": "c2", "name": "two"}},
		{"Company", map[string]any{"id": "c1", "name": "one again"}},
		{"Employee", map[string]any{"id": "e3", "works_at": ref("c9")}},
		{"Employee", map[string]any{"id": "e4", "audits": []any{}}},
	} {
		vi, res := v.ValidateOne(t.Context(), row.typeName, instance.RawInstance{Properties: row.props})
		if res.HasErrors() {
			t.Fatalf("validate %s %v: %s", row.typeName, row.props["id"], res)
		}
		if res := g.Add(t.Context(), vi); res.HasErrors() && (row.props["name"] != "one again" || !res.HasCode(diag.E_DUPLICATE_PK)) {
			t.Fatalf("Add %s %v: %s", row.typeName, row.props["id"], res)
		}
	}
	return g.Snapshot()
}

// An import installs the records as they stand and yields the same snapshot.
func TestNewFromSnapshot_InstallsTheRecordsAsTheyStand(t *testing.T) {
	t.Parallel()
	built := importRecordsSnapshot(t)
	reasons := map[string]int{}
	for u := range built.Unresolved() {
		reasons[u.Reason()]++
	}
	withProps := 0
	for e := range built.Edges() {
		if e.HasProperties() {
			withProps++
		}
	}
	if reasons["target_missing"] == 0 || reasons["absent"] == 0 || withProps == 0 || len(slices.Collect(built.Duplicates())) != 1 {
		t.Fatalf("the fixture lacks a kind of record: reasons %v, %d edges with properties, duplicates %d",
			reasons, withProps, len(slices.Collect(built.Duplicates())))
	}
	snapshottest.DiffSnapshots(t, built, graph.NewFromSnapshot(built).Snapshot())
}
