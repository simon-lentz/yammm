package neo4j

import (
	"errors"
	"testing"

	"github.com/simon-lentz/yammm/internal/yammmtest"
	"github.com/simon-lentz/yammm/schema/cypher"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/schema"
)

func rowValues(t *testing.T, params map[string]any, key string) []any {
	t.Helper()
	rows, ok := params["rows"].([]map[string]any)
	if !ok {
		t.Fatalf("rows = %T", params["rows"])
	}
	values := make([]any, len(rows))
	for i, row := range rows {
		values[i] = row[key]
	}
	return values
}

// A replace row deletes one root's stale children, so a root with no row keeps them.
func TestNodeQueries_EveryRootHoldsAReplaceRowInKeyOrder(t *testing.T) {
	t.Parallel()
	a := cypher.Config{}
	s, v := loadSchemaAndValidator(t, "composed_nested.yammm")
	snap := buildGraphResult(t, s, v, map[string][]map[string]any{
		"Order": {
			{"order_id": "o2", "customer": "c"},
			{"order_id": "o1", "customer": "c"},
			{"order_id": "o3", "customer": "c"},
		},
	})
	queries, err := NodeQueries(t.Context(), a, snap)
	if err != nil {
		t.Fatal(err)
	}
	var merged, replaced []any
	for _, q := range queries {
		switch q.Kind {
		case NodeMerge:
			merged = append(merged, rowValues(t, q.Params, batchKeyParamPrefix+"order_id")...)
		case CompositionReplace:
			replaced = append(replaced, rowValues(t, q.Params, batchKeyParamPrefix+"order_id")...)
		}
	}
	want := []any{"o1", "o2", "o3"}
	yammmtest.Diff(t, want, merged)
	yammmtest.Diff(t, want, replaced)
}

func TestEdgeQueries_RowsHoldTheSnapshotsEdgeOrder(t *testing.T) {
	t.Parallel()
	a := cypher.Config{}
	s, v := loadSchemaAndValidator(t, "write_basic.yammm")
	books := make([]map[string]any, 0, 3)
	for _, id := range []string{"b2", "b1", "b3"} {
		books = append(books, map[string]any{
			"publisher_id": "p", "book_id": id, "title": "t",
			"by_publisher": map[string]any{"_target_publisher_id": "p"},
		})
	}
	snap := buildGraphResult(t, s, v, map[string][]map[string]any{
		"Publisher": {{"publisher_id": "p", "name": "n"}},
		"Book":      books,
	})
	queries, err := EdgeQueries(t.Context(), a, snap)
	if err != nil {
		t.Fatal(err)
	}
	if len(queries) != 1 {
		t.Fatalf("queries = %d, want 1", len(queries))
	}
	yammmtest.Diff(t, []any{"b1", "b2", "b3"}, rowValues(t, queries[0].Params, RelFromRowPrefix+"book_id"))
}

func TestBatchQueries_AZeroSnapshotIsRefusedAndDoesNotPanic(t *testing.T) {
	t.Parallel()
	a := cypher.Config{}
	if _, err := NodeQueries(t.Context(), a, graph.Snapshot{}); !errors.Is(err, ErrZeroSnapshot) {
		t.Errorf("NodeQueries = %v, want ErrZeroSnapshot", err)
	}
	if _, err := EdgeQueries(t.Context(), a, graph.Snapshot{}); !errors.Is(err, ErrZeroSnapshot) {
		t.Errorf("EdgeQueries = %v, want ErrZeroSnapshot", err)
	}
}

func TestRelConstraint_AZeroRelationDeclaresNothing(t *testing.T) {
	t.Parallel()
	if got := relConstraint(schema.Relation{})("x"); got != nil {
		t.Errorf("relConstraint = %v, want nil", got)
	}
}
