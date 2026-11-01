package neo4j

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/internal/yammmtest"
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
func TestBatchNodeQueries_EveryRootHoldsAReplaceRowInKeyOrder(t *testing.T) {
	t.Parallel()
	a, s, v, shape := setupWrite(t, "composed_nested.yammm")
	snap := buildGraphResult(t, s, v, map[string][]map[string]any{
		"Order": {
			{"order_id": "o2", "customer": "c"},
			{"order_id": "o1", "customer": "c"},
			{"order_id": "o3", "customer": "c"},
		},
	})
	queries, err := a.BatchNodeQueries(t.Context(), snap, shape)
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

func TestBatchEdgeQueries_RowsHoldTheSnapshotsEdgeOrder(t *testing.T) {
	t.Parallel()
	a, s, v, shape := setupWrite(t, "write_basic.yammm")
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
	queries, err := a.BatchEdgeQueries(t.Context(), snap, shape)
	if err != nil {
		t.Fatal(err)
	}
	if len(queries) != 1 {
		t.Fatalf("queries = %d, want 1", len(queries))
	}
	yammmtest.Diff(t, []any{"b1", "b2", "b3"}, rowValues(t, queries[0].Params, RelFromRowPrefix+"book_id"))
}

func TestConstraintsForSchema_TheEntrySchemasTypesComeBeforeAnImports(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, res := schema.LoadSourcesWithEntry(ctx, map[string][]byte{
		"geo.yammm":    []byte("schema \"geo\"\n\nimport \"./common\" as common\n\ntype Region {\n\tid String primary\n}\n\ntype County {\n\tfips String primary\n\t--> IN_REGION (one) common.Region\n}\n"),
		"common.yammm": []byte("schema \"common\"\n\ntype Region {\n\tid String primary\n}\n"),
	}, "geo.yammm", ".", schema.WithSourcesOnly(true))
	if res.HasErrors() {
		t.Fatal(res)
	}
	constraints, cres := New().ConstraintsForSchema(ctx, s)
	if cres.HasErrors() {
		t.Fatal(cres)
	}
	entry := slices.IndexFunc(constraints, func(c string) bool { return strings.Contains(c, "geo__Region") })
	imported := slices.IndexFunc(constraints, func(c string) bool { return strings.Contains(c, "common__Region") })
	if entry < 0 || imported < 0 || entry > imported {
		t.Errorf("geo__Region at %d, common__Region at %d, want the entry first in %q", entry, imported, constraints)
	}
}

func TestBatchQueries_AZeroSnapshotIsRefusedAndDoesNotPanic(t *testing.T) {
	t.Parallel()
	a, _, _, shape := setupWrite(t, "write_basic.yammm")
	const want = "snapshot carries no schema"
	if _, err := a.BatchNodeQueries(t.Context(), graph.Snapshot{}, shape); err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("BatchNodeQueries = %v, want %q", err, want)
	}
	if _, err := a.BatchEdgeQueries(t.Context(), graph.Snapshot{}, shape); err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("BatchEdgeQueries = %v, want %q", err, want)
	}
}

func TestTargetScope_AnUnknownPropertyNamesTheAnnotatedType(t *testing.T) {
	t.Parallel()
	s, _ := loadSchemaAndValidator(t, "write_basic.yammm")
	book, ok := s.Type("Book")
	if !ok {
		t.Fatal("no type Book")
	}
	if got := targetScope(book, schema.Property{}); got != (identifierSubject{TypeName: "Book"}) {
		t.Errorf("targetScope = %+v", got)
	}
}

func TestRelConstraint_AZeroRelationDeclaresNothing(t *testing.T) {
	t.Parallel()
	if got := relConstraint(schema.Relation{})("x"); got != nil {
		t.Errorf("relConstraint = %v, want nil", got)
	}
}
