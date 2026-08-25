package neo4j

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/graph"
)

func nestedGraphResult(t *testing.T) (*Adapter, *GraphShape, *graph.Snapshot) {
	t.Helper()
	a, s, v, shape := setupWrite(t, "composed_nested.yammm")
	snap := buildGraphResult(t, s, v, map[string][]map[string]any{
		"Order": {{
			"order_id": "o1",
			"customer": "c",
			"sections": []any{
				map[string]any{
					"section_id": "s1",
					"heading":    "h1",
					"notes": []any{
						map[string]any{"body": "n1"},
						map[string]any{"body": "n2"},
					},
				},
				map[string]any{
					"section_id": "s2",
					"heading":    "h2",
				},
			},
		}},
	})
	return a, shape, snap
}

func TestBatchNodeQueries_ComposedPhaseOrder(t *testing.T) {
	t.Parallel()
	a, shape, snap := nestedGraphResult(t)

	queries, err := a.BatchNodeQueries(context.Background(), snap, shape)
	if err != nil {
		t.Fatal(err)
	}

	var kinds []NodeQueryKind
	for _, q := range queries {
		kinds = append(kinds, q.Kind)
	}
	want := []NodeQueryKind{NodeMerge, CompositionReplace, CompositionCreate, CompositionCreate}
	if len(kinds) != len(want) {
		t.Fatalf("got %d queries (%v); want %v", len(kinds), kinds, want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("query %d Kind = %s; want %s (full order %v)", i, kinds[i], want[i], kinds)
		}
	}

	// Parent-first by depth: the SECTIONS create precedes the NOTES create.
	if !strings.Contains(queries[2].Statement, "[:SECTIONS]") {
		t.Errorf("first create statement %q is not the depth-1 SECTIONS group", queries[2].Statement)
	}
	if !strings.Contains(queries[3].Statement, "[:NOTES]") {
		t.Errorf("second create statement %q is not the depth-2 NOTES group", queries[3].Statement)
	}
}

// TestBatchNodeQueries_ComposedReplaceStatement pins the rendered subtree
// delete: every hop anchored by the closure's relation names and part
// labels, so the path cannot escape the composed subtree.
func TestBatchNodeQueries_ComposedReplaceStatement(t *testing.T) {
	t.Parallel()
	a, shape, snap := nestedGraphResult(t)

	queries, err := a.BatchNodeQueries(context.Background(), snap, shape)
	if err != nil {
		t.Fatal(err)
	}

	var replace *BatchNodeQuery
	for _, q := range queries {
		if q.Kind == CompositionReplace {
			replace = q
			break
		}
	}
	if replace == nil {
		t.Fatal("no CompositionReplace query")
	}

	want := "UNWIND $rows AS row\n" +
		"MATCH (p:nested_test__Order {order_id: row.key_order_id})\n" +
		"MATCH (p) (()-[:NOTES|SECTIONS]->(:nested_test__Note|nested_test__Section)){1,} (c)\n" +
		"DETACH DELETE c"
	if replace.Statement != want {
		t.Errorf("replace statement = %q; want %q", replace.Statement, want)
	}

	rows := replace.Params["rows"].([]map[string]any)
	if len(rows) != 1 || rows[0]["key_order_id"] != "o1" {
		t.Errorf("replace rows = %v; want one row keyed key_order_id=o1", rows)
	}
}

func TestBatchNodeQueries_ComposedCreateStatements(t *testing.T) {
	t.Parallel()
	a, shape, snap := nestedGraphResult(t)

	queries, err := a.BatchNodeQueries(context.Background(), snap, shape)
	if err != nil {
		t.Fatal(err)
	}

	var creates []*BatchNodeQuery
	for _, q := range queries {
		if q.Kind == CompositionCreate {
			creates = append(creates, q)
		}
	}
	if len(creates) != 2 {
		t.Fatalf("got %d create queries; want 2", len(creates))
	}

	wantSections := "UNWIND $rows AS row\n" +
		"MATCH (p:nested_test__Order {order_id: row.key_order_id})\n" +
		"CREATE (p)-[:SECTIONS]->(c:nested_test__Section)\n" +
		"SET c = row.props"
	if creates[0].Statement != wantSections {
		t.Errorf("depth-1 create = %q; want %q", creates[0].Statement, wantSections)
	}

	// A depth-2 parent is itself a part, matched on its own composed key.
	wantNotes := "UNWIND $rows AS row\n" +
		"MATCH (p:nested_test__Section {_composed_key: row.parent_ck})\n" +
		"CREATE (p)-[:NOTES]->(c:nested_test__Note)\n" +
		"SET c = row.props"
	if creates[1].Statement != wantNotes {
		t.Errorf("depth-2 create = %q; want %q", creates[1].Statement, wantNotes)
	}

	// No bookkeeping entry reaches the driver.
	for _, q := range creates {
		for _, row := range q.Params["rows"].([]map[string]any) {
			if _, has := row["_parent_keys"]; has {
				t.Error("create row carries the _parent_keys bookkeeping entry")
			}
		}
	}
}

// TestBatchNodeQueries_ComposedKeys pins the _composed_key values: a keyed
// child's key carries its own primary-key values; a keyless child's is
// positional; a depth-2 child's parent component is the parent's composed
// key string.
func TestBatchNodeQueries_ComposedKeys(t *testing.T) {
	t.Parallel()
	a, shape, snap := nestedGraphResult(t)

	queries, err := a.BatchNodeQueries(context.Background(), snap, shape)
	if err != nil {
		t.Fatal(err)
	}

	cks := map[string][]string{}
	parentCKs := map[string][]string{}
	for _, q := range queries {
		if q.Kind != CompositionCreate {
			continue
		}
		label := "Section"
		if strings.Contains(q.Statement, "__Note)") {
			label = "Note"
		}
		for _, row := range q.Params["rows"].([]map[string]any) {
			props := row["props"].(map[string]any)
			cks[label] = append(cks[label], props[composedKeyProp].(string))
			if pck, ok := row["parent_ck"].(string); ok {
				parentCKs[label] = append(parentCKs[label], pck)
			}
		}
	}

	s1CK, err := graph.FormatComposedKey([]any{"o1"}, "SECTIONS", []any{"s1"})
	if err != nil {
		t.Fatal(err)
	}
	s2CK, err := graph.FormatComposedKey([]any{"o1"}, "SECTIONS", []any{"s2"})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := cks["Section"], []string{s1CK, s2CK}; !slices.Equal(got, want) {
		t.Errorf("Section composed keys = %v; want %v", got, want)
	}

	// The keyless Note children key positionally under s1's composed key.
	n0CK, err := graph.FormatComposedKey([]any{s1CK}, "NOTES", 0)
	if err != nil {
		t.Fatal(err)
	}
	n1CK, err := graph.FormatComposedKey([]any{s1CK}, "NOTES", 1)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := cks["Note"], []string{n0CK, n1CK}; !slices.Equal(got, want) {
		t.Errorf("Note composed keys = %v; want %v", got, want)
	}
	if got, want := parentCKs["Note"], []string{s1CK, s1CK}; !slices.Equal(got, want) {
		t.Errorf("Note parent_ck values = %v; want %v", got, want)
	}

	// The composed key is the documented JSON array form.
	var arr []any
	if err := json.Unmarshal([]byte(n0CK), &arr); err != nil || len(arr) != 3 {
		t.Errorf("composed key %q is not a 3-element JSON array (%v)", n0CK, err)
	}
}

func TestBatchNodeQueries_ComposedChunkStraddle(t *testing.T) {
	t.Parallel()
	a, shape, snap := nestedGraphResult(t)

	queries, err := a.BatchNodeQueries(context.Background(), snap, shape, WithNodeChunkSize(1))
	if err != nil {
		t.Fatal(err)
	}

	// The two Section rows straddle the chunk boundary: one create query per
	// row, both in the depth-1 group, before every depth-2 query.
	perStmt := map[string]int{}
	var order []NodeQueryKind
	for _, q := range queries {
		order = append(order, q.Kind)
		if q.Kind == CompositionCreate {
			perStmt[q.Statement]++
			if n := len(q.Params["rows"].([]map[string]any)); n != 1 {
				t.Errorf("chunked create query carries %d rows; want 1", n)
			}
		}
	}
	want := []NodeQueryKind{NodeMerge, CompositionReplace, CompositionCreate, CompositionCreate, CompositionCreate, CompositionCreate}
	if len(order) != len(want) {
		t.Fatalf("got %d queries (%v); want %v", len(order), order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("query %d Kind = %s; want %s (full order %v)", i, order[i], want[i], order)
		}
	}
	// Each group carries two rows, so each statement renders two chunks.
	for stmt, n := range perStmt {
		if n != 2 {
			t.Errorf("create statement %q appears %d times; want 2 chunks", stmt, n)
		}
	}
}
