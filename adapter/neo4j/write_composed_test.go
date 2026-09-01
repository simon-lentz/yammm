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
		"DETACH DELETE c\n" +
		"RETURN count(*) AS matched_rows"
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
		"SET c = row.props\n" +
		"RETURN count(*) AS matched_rows"
	if creates[0].Statement != wantSections {
		t.Errorf("depth-1 create = %q; want %q", creates[0].Statement, wantSections)
	}

	// A depth-2 parent is itself a part, matched on its own composed key.
	wantNotes := "UNWIND $rows AS row\n" +
		"MATCH (p:nested_test__Section {_composed_key: row.parent_ck})\n" +
		"CREATE (p)-[:NOTES]->(c:nested_test__Note)\n" +
		"SET c = row.props\n" +
		"RETURN count(*) AS matched_rows"
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

// TestBatchNodeQueries_ComposedKeys pins the _composed_key values: every
// address names the owning root's type identity and the root's key, then one
// segment per composition hop. A keyed child's segment carries its own key
// values; a keyless child's is positional; and a depth-2 child APPENDS its
// segment rather than nesting the depth-1 address inside itself, so the address
// grows linearly with depth and no escape compounds.
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

	// LITERALS, not a second call to the renderer under test. Comparing the
	// adapter's output against a value computed the same way pins nothing: it
	// stays green under any format change that is applied consistently.
	const (
		s1CK = `["nested_test__Order",["o1"],["SECTIONS",["s1"]]]`
		s2CK = `["nested_test__Order",["o1"],["SECTIONS",["s2"]]]`
		n0CK = `["nested_test__Order",["o1"],["SECTIONS",["s1"]],["NOTES",0]]`
		n1CK = `["nested_test__Order",["o1"],["SECTIONS",["s1"]],["NOTES",1]]`
	)

	if got, want := cks["Section"], []string{s1CK, s2CK}; !slices.Equal(got, want) {
		t.Errorf("Section composed keys = %v; want %v", got, want)
	}
	if got, want := cks["Note"], []string{n0CK, n1CK}; !slices.Equal(got, want) {
		t.Errorf("Note composed keys = %v; want %v", got, want)
	}
	if got, want := parentCKs["Note"], []string{s1CK, s1CK}; !slices.Equal(got, want) {
		t.Errorf("Note parent_ck values = %v; want %v", got, want)
	}

	// The address carries the root's LABEL and no source path: a file-backed
	// schema renders an absolute path in [schema.TypeID], which would put the
	// writing machine's directory layout on every part node.
	for label, keys := range cks {
		for _, ck := range keys {
			if strings.Contains(ck, "/") || strings.Contains(ck, `\\`) {
				t.Errorf("%s composed key carries a path or a compounded escape: %s", label, ck)
			}
		}
	}

	// Flat, so the element count tracks depth: three at depth 1, four at depth
	// 2. A nesting form keeps the count constant while each level's rendering
	// grows.
	for _, tc := range []struct {
		name string
		ck   string
		want int
	}{{"depth 1", cks["Section"][0], 3}, {"depth 2", cks["Note"][0], 4}} {
		var arr []any
		if err := json.Unmarshal([]byte(tc.ck), &arr); err != nil {
			t.Errorf("%s composed key %q is not JSON: %v", tc.name, tc.ck, err)
			continue
		}
		if len(arr) != tc.want {
			t.Errorf("%s composed key %q has %d elements, want %d", tc.name, tc.ck, len(arr), tc.want)
		}
		if root, ok := arr[0].(string); !ok || root != "nested_test__Order" {
			t.Errorf("%s composed key does not lead with the owning root's label: %v", tc.name, arr[0])
		}
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
