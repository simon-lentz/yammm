package neo4j

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/neo4j/neo4j-go-driver/v6/neo4j/dbtype"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/schema"
)

func TestBatchNodeQueries_DerivedPerType(t *testing.T) {
	t.Parallel()
	a, s, v, shape := setupWrite(t, "writeonce.yammm")

	graphResult := buildGraphResult(t, s, v, map[string][]map[string]any{
		"Entity": {{"id": "e1", "name": "n", "origin": "src", "first_seen": "2024-01-01T00:00:00Z"}},
		"Plain":  {{"id": "p1", "name": "n"}},
	})

	// No option: @writeOnce drives the shape per type. Entity → immutable split;
	// Plain → mutable.
	queries, err := a.BatchNodeQueries(context.Background(), graphResult, shape)
	if err != nil {
		t.Fatal(err)
	}

	byType := map[string]*BatchNodeQuery{}
	for _, q := range queries {
		if q.Kind != NodeMerge {
			continue
		}
		switch {
		case strings.Contains(q.Statement, "wo_test__Entity"):
			byType["Entity"] = q
		case strings.Contains(q.Statement, "wo_test__Plain"):
			byType["Plain"] = q
		}
	}
	entityQ, ok := byType["Entity"]
	if !ok {
		t.Fatal("no Entity query")
	}
	plainQ, ok := byType["Plain"]
	if !ok {
		t.Fatal("no Plain query")
	}

	entityNS := shape.Types[typeID(t, s, "Entity")]
	if want := buildBatchNodeMergeQuery(entityNS.Label, entityNS.PrimaryKeys, ImmutableKeys); entityQ.Statement != want {
		t.Errorf("Entity statement = %q; want immutable split %q", entityQ.Statement, want)
	}
	plainNS := shape.Types[typeID(t, s, "Plain")]
	if want := buildBatchNodeMergeQuery(plainNS.Label, plainNS.PrimaryKeys, MutableKeys); plainQ.Statement != want {
		t.Errorf("Plain statement = %q; want mutable %q", plainQ.Statement, want)
	}

	// Entity rows exclude the derived keys from update_props.
	for _, row := range entityQ.Params["rows"].([]map[string]any) {
		up, ok := row["update_props"].(map[string]any)
		if !ok {
			t.Fatal("Entity row missing update_props")
		}
		for _, k := range []string{"first_seen", "origin"} {
			if _, has := up[k]; has {
				t.Errorf("Entity update_props should exclude derived key %q", k)
			}
		}
	}
	// Plain rows carry no update_props (mutable shape).
	for _, row := range plainQ.Params["rows"].([]map[string]any) {
		if _, has := row["update_props"]; has {
			t.Error("Plain row should have no update_props")
		}
	}
}

func TestBatchNodeQueries_SingleType(t *testing.T) {
	t.Parallel()
	a, s, v, shape := setupWrite(t, "basic.yammm")

	graphResult := buildGraphResult(t, s, v, map[string][]map[string]any{
		"Entity": {
			{"id": "e1", "name": "a", "count": int64(1), "active": true, "created_at": "2024-01-01T00:00:00Z"},
			{"id": "e2", "name": "b", "count": int64(2), "active": false, "created_at": "2024-01-02T00:00:00Z"},
		},
	})

	queries, err := a.BatchNodeQueries(context.Background(), graphResult, shape)
	if err != nil {
		t.Fatal(err)
	}

	if len(queries) != 1 {
		t.Fatalf("expected 1 query, got %d", len(queries))
	}

	q := queries[0]
	ns := shape.Types[typeID(t, s, "Entity")]
	if want := buildBatchNodeMergeQuery(ns.Label, ns.PrimaryKeys, MutableKeys); q.Statement != want {
		t.Errorf("Statement = %q; want builder output %q", q.Statement, want)
	}
	rows, ok := q.Params["rows"].([]map[string]any)
	if !ok {
		t.Fatal("rows should be []map[string]any")
	}
	if len(rows) != 2 {
		t.Errorf("expected 2 rows, got %d", len(rows))
	}
}

func TestBatchNodeQueries_Chunking(t *testing.T) {
	t.Parallel()
	a, s, v, shape := setupWrite(t, "basic.yammm")

	// Create 5 instances.
	var instances []map[string]any
	for i := range 5 {
		instances = append(instances, map[string]any{
			"id": fmt.Sprintf("e%d", i), "name": "n", "count": int64(1),
			"active": true, "created_at": "2024-01-01T00:00:00Z",
		})
	}

	graphResult := buildGraphResult(t, s, v, map[string][]map[string]any{
		"Entity": instances,
	})

	queries, err := a.BatchNodeQueries(context.Background(), graphResult, shape, WithNodeChunkSize(2))
	if err != nil {
		t.Fatal(err)
	}

	// 5 instances / chunk size 2 = 3 chunks (2+2+1).
	if len(queries) != 3 {
		t.Errorf("expected 3 queries for 5 instances with chunk size 2, got %d", len(queries))
	}
}

func TestBatchEdgeQueries_GroupBySignature(t *testing.T) {
	t.Parallel()
	a, s, v, shape := setupWrite(t, "write_basic.yammm")

	graphResult := buildGraphResult(t, s, v, map[string][]map[string]any{
		"Publisher": {
			{"publisher_id": "iss1", "name": "Publisher 1"},
			{"publisher_id": "iss2", "name": "Publisher 2"},
		},
		"Book": {
			{"publisher_id": "iss1", "book_id": "i1", "title": "Book 1", "by_publisher": map[string]any{"_target_publisher_id": "iss1"}},
			{"publisher_id": "iss2", "book_id": "i2", "title": "Book 2", "by_publisher": map[string]any{"_target_publisher_id": "iss2"}},
		},
	})

	queries, err := a.BatchEdgeQueries(context.Background(), graphResult, shape)
	if err != nil {
		t.Fatal(err)
	}

	// Both edges share the (Book, BY_PUBLISHER, Publisher) signature, so they
	// group into a single batch query carrying one row per edge.
	if len(queries) != 1 {
		t.Fatalf("expected 1 batch query for one signature, got %d", len(queries))
	}
	q := queries[0]
	if q.RelationType != "BY_PUBLISHER" {
		t.Errorf("RelationType = %q; want BY_PUBLISHER", q.RelationType)
	}
	rows, ok := q.Params["rows"].([]map[string]any)
	if !ok {
		t.Fatal("rows should be []map[string]any")
	}
	if len(rows) != 2 {
		t.Errorf("expected 2 rows (one per edge), got %d", len(rows))
	}
}

func TestBatchEdgeQueries_Chunking(t *testing.T) {
	t.Parallel()
	a, s, v, shape := setupWrite(t, "write_basic.yammm")

	// Create enough edges to trigger chunking.
	var publishers []map[string]any
	var issues []map[string]any
	for i := range 5 {
		publisherID := fmt.Sprintf("iss%d", i)
		publishers = append(publishers, map[string]any{"publisher_id": publisherID, "name": fmt.Sprintf("I%d", i)})
		issues = append(issues, map[string]any{
			"publisher_id": publisherID, "book_id": fmt.Sprintf("i%d", i),
			"title": fmt.Sprintf("T%d", i), "by_publisher": map[string]any{"_target_publisher_id": publisherID},
		})
	}

	graphResult := buildGraphResult(t, s, v, map[string][]map[string]any{
		"Publisher": publishers,
		"Book":      issues,
	})

	// Precondition guard against fixture drift: one BY_PUBLISHER edge per Book.
	if totalEdges := len(graphResult.Edges()); totalEdges != 5 {
		t.Fatalf("fixture should produce 5 edges, got %d", totalEdges)
	}

	queries, err := a.BatchEdgeQueries(context.Background(), graphResult, shape, WithEdgeChunkSize(2))
	if err != nil {
		t.Fatal(err)
	}

	// 5 edges in one (Book, BY_PUBLISHER, Publisher) signature with chunk
	// size 2 → sequential chunks of 2 + 2 + 1, one query per chunk.
	if len(queries) != 3 {
		t.Fatalf("expected 3 chunked queries for 5 edges with chunk size 2, got %d", len(queries))
	}
	wantRows := []int{2, 2, 1}
	total := 0
	for i, q := range queries {
		if q.RelationType != "BY_PUBLISHER" {
			t.Errorf("query %d RelationType = %q; want BY_PUBLISHER", i, q.RelationType)
		}
		rows, ok := q.Params["rows"].([]map[string]any)
		if !ok {
			t.Fatalf("query %d rows missing or wrong type", i)
		}
		if len(rows) != wantRows[i] {
			t.Errorf("query %d rows = %d; want %d", i, len(rows), wantRows[i])
		}
		total += len(rows)
	}
	if total != 5 {
		t.Errorf("total rows across chunks = %d; want 5 (one per edge)", total)
	}
}

// TestBatchNodeQueries_PropertyCoercion pins that propsToParamMap's
// driver-type coercion is applied on the batch write path: validated values
// reach the row props as their driver types, not as raw strings or []any.
// The element-level coercion semantics themselves are unit-tested in the
// TestCoerceSlice_* and coerce_test.go cases.
func TestBatchNodeQueries_PropertyCoercion(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		fixture   string
		instance  map[string]any
		wantTypes map[string]string // prop name -> reflect type string
	}{
		{
			name:    "scalars",
			fixture: "basic.yammm",
			instance: map[string]any{
				"id": "e1", "name": "test", "count": int64(42), "active": true,
				"created_at": "2024-01-01T00:00:00Z", // Timestamp string -> time.Time
				"birth_date": "2024-06-15",           // Date string -> dbtype.Date
				"score":      int64(42),              // int under Float constraint -> float64
			},
			wantTypes: map[string]string{
				"id":         "string",
				"count":      "int64",
				"active":     "bool",
				"created_at": "time.Time",
				"birth_date": "dbtype.Date",
				"score":      "float64",
			},
		},
		{
			name:    "lists",
			fixture: "list_properties.yammm",
			instance: map[string]any{
				"id": "e1", "name": "test", "active": true,
				"tags":   []any{"a", "b"},
				"scores": []any{int64(1), int64(2)},
				"times":  []any{"2024-01-01T00:00:00Z", "2024-06-15T12:30:00Z"},
				"dates":  []any{"2024-01-01", "2024-06-15"},
			},
			wantTypes: map[string]string{
				"tags":   "[]string",
				"scores": "[]int64",
				"times":  "[]time.Time",
				"dates":  "[]dbtype.Date",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			a, s, v, shape := setupWrite(t, tc.fixture)

			graphResult := buildGraphResult(t, s, v, map[string][]map[string]any{
				"Entity": {tc.instance},
			})

			queries, err := a.BatchNodeQueries(context.Background(), graphResult, shape)
			if err != nil {
				t.Fatal(err)
			}
			if len(queries) != 1 {
				t.Fatalf("expected 1 query, got %d", len(queries))
			}

			rows := queries[0].Params["rows"].([]map[string]any)
			props := rows[0]["props"].(map[string]any)
			for prop, wantType := range tc.wantTypes {
				got, ok := props[prop]
				if !ok {
					t.Errorf("prop %q missing from row props", prop)
					continue
				}
				if gotType := reflect.TypeOf(got).String(); gotType != wantType {
					t.Errorf("prop %q coerced to %s; want %s", prop, gotType, wantType)
				}
			}
			// Value-preservation spot check for the int→Float repair.
			if tc.name == "scalars" {
				if f, _ := props["score"].(float64); f != 42.0 {
					t.Errorf("score = %v; want 42.0 preserved through int→Float repair", props["score"])
				}
			}
		})
	}
}

func TestCoerceSlice_TimeTimeElements(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, "list_properties.yammm")
	st, ok := s.Type("Entity")
	if !ok {
		t.Fatal("Entity not found")
	}
	prop, ok := st.Property("times")
	if !ok {
		t.Fatal("times property not found")
	}

	t1 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2024, 6, 15, 12, 30, 0, 0, time.UTC)
	raw := []any{t1, t2}

	result, err := coerceSlice(raw, prop.Constraint())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out, ok := result.([]time.Time)
	if !ok {
		t.Fatalf("expected []time.Time, got %T", result)
	}
	if len(out) != 2 {
		t.Fatalf("length = %d; want 2", len(out))
	}
	if !out[0].Equal(t1) || !out[1].Equal(t2) {
		t.Errorf("values mismatch: got %v", out)
	}
}

func TestCoerceSlice_DateStringElements(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, "list_properties.yammm")
	st, ok := s.Type("Entity")
	if !ok {
		t.Fatal("Entity not found")
	}
	prop, ok := st.Property("dates")
	if !ok {
		t.Fatal("dates property not found")
	}

	raw := []any{"2024-01-01", "2024-06-15"}
	result, err := coerceSlice(raw, prop.Constraint())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out, ok := result.([]dbtype.Date)
	if !ok {
		t.Fatalf("expected []dbtype.Date, got %T", result)
	}
	if len(out) != 2 {
		t.Fatalf("length = %d; want 2", len(out))
	}
	expected0 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	expected1 := time.Date(2024, 6, 15, 0, 0, 0, 0, time.UTC)
	if !time.Time(out[0]).Equal(expected0) {
		t.Errorf("out[0] = %v; want %v", out[0], expected0)
	}
	if !time.Time(out[1]).Equal(expected1) {
		t.Errorf("out[1] = %v; want %v", out[1], expected1)
	}
}

func TestCoerceSlice_TimestampStringElements(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, "list_properties.yammm")
	st, ok := s.Type("Entity")
	if !ok {
		t.Fatal("Entity not found")
	}
	prop, ok := st.Property("times")
	if !ok {
		t.Fatal("times property not found")
	}

	raw := []any{"2024-01-01T00:00:00Z", "2024-06-15T12:30:00Z"}
	result, err := coerceSlice(raw, prop.Constraint())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out, ok := result.([]time.Time)
	if !ok {
		t.Fatalf("expected []time.Time, got %T", result)
	}
	if len(out) != 2 {
		t.Fatalf("length = %d; want 2", len(out))
	}
}

func TestCoerceSlice_TemporalParseFailure(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, "list_properties.yammm")
	st, ok := s.Type("Entity")
	if !ok {
		t.Fatal("Entity not found")
	}

	t.Run("DateParseFailure", func(t *testing.T) {
		t.Parallel()
		prop, ok := st.Property("dates")
		if !ok {
			t.Fatal("dates property not found")
		}
		raw := []any{"not-a-date", "also-not"}
		if _, err := coerceSlice(raw, prop.Constraint()); err == nil {
			t.Error("expected an error on unparseable Date elements, got nil")
		}
	})

	t.Run("TimestampParseFailure", func(t *testing.T) {
		t.Parallel()
		prop, ok := st.Property("times")
		if !ok {
			t.Fatal("times property not found")
		}
		raw := []any{"not-a-timestamp", "also-not"}
		if _, err := coerceSlice(raw, prop.Constraint()); err == nil {
			t.Error("expected an error on unparseable Timestamp elements, got nil")
		}
	})
}

func TestCoerceSlice_VectorElements(t *testing.T) {
	t.Parallel()
	// A Vector property carried as []any (the shape a .ys snapshot load produces,
	// with whole numbers narrowed to int64 by NormalizeValue) must reach the
	// driver as []float64, not []any. A Vector is float-valued by definition, so
	// it coerces elementwise exactly like a List<Float>.
	s := loadSchema(t, "basic.yammm")
	st, ok := s.Type("Entity")
	if !ok {
		t.Fatal("Entity not found")
	}
	prop, ok := st.Property("embedding") // Vector[3]
	if !ok {
		t.Fatal("embedding property not found")
	}
	raw := []any{int64(1), int64(2), 3.5}
	result, err := coerceSlice(raw, prop.Constraint())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out, ok := result.([]float64)
	if !ok {
		t.Fatalf("expected []float64 for a Vector, got %T", result)
	}
	if len(out) != 3 || out[0] != 1 || out[1] != 2 || out[2] != 3.5 {
		t.Fatalf("vector elements not coerced to float64: %#v", out)
	}
}

func TestCoerceSlice_ListValueUnderScalarConstraintErrors(t *testing.T) {
	t.Parallel()
	// A []any value under a scalar (non-List, non-Vector) constraint is a shape
	// mismatch — a scalar property cannot hold a list. coerceSlice fails fast
	// rather than returning the raw []any for the driver to reject.
	s := loadSchema(t, "basic.yammm")
	st, ok := s.Type("Entity")
	if !ok {
		t.Fatal("Entity not found")
	}
	prop, ok := st.Property("score") // scalar Float
	if !ok {
		t.Fatal("score property not found")
	}
	if _, err := coerceSlice([]any{int64(1), int64(2)}, prop.Constraint()); err == nil {
		t.Error("expected an error for a []any under a scalar constraint, got nil")
	}
}

func TestPropsToParamMap_DeterministicError(t *testing.T) {
	t.Parallel()
	// When multiple node properties fail coercion — reachable when a .ys snapshot
	// is loaded under a schema whose constraints its values no longer satisfy —
	// the error names the lexicographically-first failing property on every run,
	// matching CoerceParams, so a coercion failure is reproducible rather than
	// dependent on map iteration order.
	s := loadSchema(t, "basic.yammm")
	st, ok := s.Type("Entity")
	if !ok {
		t.Fatal("Entity not found")
	}
	props := immutable.WrapProperties(map[string]any{
		"birth_date": "not-a-date",      // Date → coercion error
		"created_at": "not-a-timestamp", // Timestamp → coercion error
	})
	var first string
	for i := range 64 {
		_, err := propsToParamMap(props, st)
		if err == nil {
			t.Fatal("want a coercion error, got nil")
		}
		if i == 0 {
			first = err.Error()
			continue
		}
		if err.Error() != first {
			t.Fatalf("non-deterministic error: run %d = %q, first = %q", i, err.Error(), first)
		}
	}
	if !strings.Contains(first, "birth_date") {
		t.Errorf("error should name the lexicographically-first bad property (birth_date): %q", first)
	}
}

func TestCoerceRelProps_DeterministicError(t *testing.T) {
	t.Parallel()
	// Edge-property counterpart of TestPropsToParamMap_DeterministicError: with
	// multiple typed rel-props failing, the reported error is the
	// lexicographically-first key on every run.
	s := loadSchema(t, "edge_typed_props.yammm")
	src, ok := s.Type("Source")
	if !ok {
		t.Fatal("Source not found")
	}
	rel, ok := src.Relation("LINKED_AT")
	if !ok {
		t.Fatal("LINKED_AT relation not found")
	}
	var first string
	for i := range 64 {
		// coerceRelProps mutates props in place, so build a fresh map per run.
		props := map[string]any{
			"observed_at": "not-a-timestamp", // Timestamp → coercion error
			"weight":      "not-a-number",    // Float → coercion error (strict)
		}
		_, err := coerceRelProps(props, rel)
		if err == nil {
			t.Fatal("want a coercion error, got nil")
		}
		if i == 0 {
			first = err.Error()
			continue
		}
		if err.Error() != first {
			t.Fatalf("non-deterministic error: run %d = %q, first = %q", i, err.Error(), first)
		}
	}
	if !strings.Contains(first, "observed_at") {
		t.Errorf("error should name the lexicographically-first bad rel-prop (observed_at): %q", first)
	}
}

func TestPropsToParamMap_TemporalErrorPropagates(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, "basic.yammm")
	st, ok := s.Type("Entity")
	if !ok {
		t.Fatal("Entity not found")
	}

	// A value that bypassed validation (e.g. injected at a direct param
	// boundary) and is not a parseable Timestamp must surface as an error from
	// propsToParamMap rather than silently reaching the driver.
	props := immutable.WrapProperties(map[string]any{
		"id":         "e1",
		"created_at": "not-a-timestamp",
	})
	if _, err := propsToParamMap(props, st); err == nil {
		t.Fatal("want error from propsToParamMap on an unparseable Timestamp, got nil")
	}

	// A nil schema type short-circuits with no coercion and no error.
	clean, err := propsToParamMap(immutable.WrapProperties(map[string]any{"x": "y"}), nil)
	if err != nil || clean["x"] != "y" {
		t.Fatalf("nil schemaType should pass through unchanged: clean=%v err=%v", clean, err)
	}
}

func TestBatchNodeQueries_MissingShape(t *testing.T) {
	t.Parallel()
	s, v := loadSchemaAndValidator(t, "basic.yammm")
	a := New()

	graphResult := buildGraphResult(t, s, v, map[string][]map[string]any{
		"Entity": {{"id": "e1", "name": "test", "count": int64(1), "active": true, "created_at": "2024-01-01T00:00:00Z"}},
	})

	// Pass an empty shape map — no shape for "Entity".
	emptyShapes := &GraphShape{Types: map[schema.TypeID]NodeShape{}}
	_, err := a.BatchNodeQueries(context.Background(), graphResult, emptyShapes)
	if err == nil {
		t.Error("expected error for missing shape")
	}
	if !strings.Contains(err.Error(), "no shape for type") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestBatchEdgeQueries_MissingShape(t *testing.T) {
	t.Parallel()
	s, v := loadSchemaAndValidator(t, "write_basic.yammm")
	a := New()

	graphResult := buildGraphResult(t, s, v, map[string][]map[string]any{
		"Publisher": {{"publisher_id": "iss1", "name": "Test"}},
		"Book":      {{"publisher_id": "iss1", "book_id": "i1", "title": "Test", "by_publisher": map[string]any{"_target_publisher_id": "iss1"}}},
	})

	emptyShapes := &GraphShape{Types: map[schema.TypeID]NodeShape{}}
	_, err := a.BatchEdgeQueries(context.Background(), graphResult, emptyShapes)
	if err == nil {
		t.Error("expected error for missing shape")
	}
	if !strings.Contains(err.Error(), "no shape for") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestBatchEdgeQueries_MixedProperties(t *testing.T) {
	t.Parallel()
	a, s, v, shape := setupWrite(t, "edge_mixed.yammm")

	// Employee e1 provides the optional edge property "note";
	// Employee e2 does not. Both produce WORKS_AT edges with the
	// same (Employee, WORKS_AT, Company) signature but different
	// HasProperties() results. Before the fix, the edge without
	// properties would omit rel_props from its row, causing
	// SET r += row.rel_props to evaluate to null (Neo4j TypeError).
	graphResult := buildGraphResult(t, s, v, map[string][]map[string]any{
		"Company": {
			{"company_id": "c1", "name": "Acme"},
		},
		"Employee": {
			{"employee_id": "e1", "name": "Alice", "works_at": map[string]any{"_target_company_id": "c1", "note": "senior"}},
			{"employee_id": "e2", "name": "Bob", "works_at": map[string]any{"_target_company_id": "c1"}},
		},
	})

	queries, err := a.BatchEdgeQueries(context.Background(), graphResult, shape)
	if err != nil {
		t.Fatal(err)
	}

	// Find the WORKS_AT batch query.
	var worksAtQuery *BatchEdgeQuery
	for _, q := range queries {
		if q.RelationType == "WORKS_AT" {
			worksAtQuery = q
			break
		}
	}
	if worksAtQuery == nil {
		t.Fatal("expected a WORKS_AT batch edge query")
	}

	// If the statement references rel_props, every row must contain the key.
	if strings.Contains(worksAtQuery.Statement, "rel_props") {
		rows, ok := worksAtQuery.Params["rows"].([]map[string]any)
		if !ok {
			t.Fatal("rows should be []map[string]any")
		}
		for i, row := range rows {
			relProps, exists := row["rel_props"]
			if !exists {
				t.Errorf("row %d missing rel_props key — would cause SET r += null TypeError in Neo4j", i)
				continue
			}
			if relProps == nil {
				t.Errorf("row %d has nil rel_props — would cause SET r += null TypeError in Neo4j", i)
			}
		}
	}
}

func TestCoerceRelProps(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, "edge_typed_props.yammm")
	st, ok := s.Type("Source")
	if !ok {
		t.Fatal("Source type not found")
	}
	rels := st.AssociationsSlice()
	if len(rels) != 1 {
		t.Fatalf("want 1 association on Source, got %d", len(rels))
	}
	rel := rels[0]

	out, err := coerceRelProps(map[string]any{
		"observed_at": "2024-01-01T00:00:00Z",
		"weight":      int64(5),
		"undeclared":  "passthrough",
	}, rel)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, isTime := out["observed_at"].(time.Time); !isTime {
		t.Errorf("observed_at should be time.Time, got %T", out["observed_at"])
	}
	if _, isFloat := out["weight"].(float64); !isFloat {
		t.Errorf("weight should be float64, got %T", out["weight"])
	}
	if out["undeclared"] != "passthrough" {
		t.Errorf("undeclared property should pass through, got %#v", out["undeclared"])
	}

	// A bad temporal string on a typed edge property surfaces an error.
	if _, err := coerceRelProps(map[string]any{"observed_at": "not-a-timestamp"}, rel); err == nil {
		t.Error("want error on unparseable Timestamp edge property, got nil")
	}

	// A nil relation passes through unchanged.
	clean, err := coerceRelProps(map[string]any{"x": "y"}, nil)
	if err != nil || clean["x"] != "y" {
		t.Fatalf("nil relation should pass through: clean=%v err=%v", clean, err)
	}
}

func TestBatchEdgeQueries_CoercesTypedRelProps(t *testing.T) {
	t.Parallel()
	a, s, v, shapes := setupWrite(t, "edge_typed_props.yammm")

	graphResult := buildGraphResult(t, s, v, map[string][]map[string]any{
		"Target": {{"target_id": "t1"}},
		"Source": {{
			"source_id": "s1",
			"linked_at": map[string]any{
				"_target_target_id": "t1",
				"observed_at":       "2024-01-01T00:00:00Z",
			},
		}},
	})

	queries, err := a.BatchEdgeQueries(context.Background(), graphResult, shapes)
	if err != nil {
		t.Fatal(err)
	}
	var linked *BatchEdgeQuery
	for _, q := range queries {
		if q.RelationType == "LINKED_AT" {
			linked = q
		}
	}
	if linked == nil {
		t.Fatal("expected a LINKED_AT batch edge query")
	}
	rows := linked.Params["rows"].([]map[string]any)
	relProps, ok := rows[0]["rel_props"].(map[string]any)
	if !ok {
		t.Fatalf("rel_props missing or wrong type: %T", rows[0]["rel_props"])
	}
	if _, isTime := relProps["observed_at"].(time.Time); !isTime {
		t.Errorf("observed_at should be coerced to time.Time, got %T", relProps["observed_at"])
	}
}

func TestCoerceSlice_TypeMismatchErrors(t *testing.T) {
	t.Parallel()
	// A list element whose Go type cannot be coerced to the element type cannot
	// build a homogeneous typed slice; coerceSlice must error (naming the
	// element) rather than silently returning the raw []any to the driver.
	cases := []struct {
		name string
		c    schema.Constraint
		raw  []any
	}{
		{"String", schema.NewListConstraint(schema.NewStringConstraint()), []any{"a", int64(2)}},
		{"Integer", schema.NewListConstraint(schema.NewIntegerConstraint()), []any{int64(1), "x"}},
		{"Float", schema.NewListConstraint(schema.NewFloatConstraint()), []any{float64(1), "x"}},
		{"Boolean", schema.NewListConstraint(schema.NewBooleanConstraint()), []any{true, "x"}},
		{"Date", schema.NewListConstraint(schema.NewDateConstraint()), []any{"2024-01-01", int64(5)}},
		{"Timestamp", schema.NewListConstraint(schema.NewTimestampConstraint()), []any{"2024-01-01T00:00:00Z", int64(5)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := coerceSlice(tc.raw, tc.c); err == nil {
				t.Errorf("List<%s> with a wrong-type element: want error, got nil", tc.name)
			}
		})
	}
}

func TestCoerceSlice_IntegerWidthRepair(t *testing.T) {
	t.Parallel()
	// A List<Integer> hand-built with narrower or unsigned int widths must widen to
	// []int64 — the same width repair coerceSlice applies for Float — rather than
	// erroring on a non-int64 element (the prior strict int64-only behavior).
	raw := []any{
		int(1), int8(2), int16(3), int32(4), int64(5),
		uint(6), uint8(7), uint16(8), uint32(9), uint64(10),
	}
	got, err := coerceSlice(raw, schema.NewListConstraint(schema.NewIntegerConstraint()))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out, ok := got.([]int64)
	if !ok {
		t.Fatalf("got %T, want []int64", got)
	}
	want := []int64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	if len(out) != len(want) {
		t.Fatalf("len = %d, want %d", len(out), len(want))
	}
	for i := range want {
		if out[i] != want[i] {
			t.Errorf("out[%d] = %d, want %d", i, out[i], want[i])
		}
	}
}

func TestCoerceSlice_IntegerOverflowErrors(t *testing.T) {
	t.Parallel()
	// A uint64 beyond the int64 range cannot be represented; coerceSlice errors
	// rather than wrapping to a negative int64 (matching the validator's guard).
	// 9223372036854775808 == math.MaxInt64 + 1.
	raw := []any{uint64(9223372036854775808)}
	if _, err := coerceSlice(raw, schema.NewListConstraint(schema.NewIntegerConstraint())); err == nil {
		t.Fatal("want error for a uint64 exceeding int64 max, got nil")
	}
}

// An INHERITED primary key is the value the MERGE matches on just as an own one
// is, and an inherited property is as present in a param map as an own one, so
// both helpers must see the merged view rather than the type's own declarations.
func TestParamTypes_CoverInheritedMembers(t *testing.T) {
	t.Parallel()
	s, res := schema.LoadString(context.Background(), `schema "inh"
abstract type Base { created_on Date primary }
type Doc extends Base { title String required }
`, "p.yammm")
	if res.HasErrors() {
		t.Fatalf("load: %v", res.Err())
	}
	dt, ok := s.Type("Doc")
	if !ok {
		t.Fatal("type Doc not found")
	}

	if _, ok := ParamTypesForType(dt, "rows")["rows.created_on"]; !ok {
		t.Error("no rows.created_on entry; an inherited property belongs in a flat row too")
	}
}

// A shape that computed no @writeOnce keys must be distinguishable from one that
// never computed any, so the write path falls back to the schema type only for a
// hand-built shape — and does no per-node property walk for the overwhelmingly
// common unannotated type.
func TestShapeForSchema_ImmutableKeysNonNilWhenEmpty(t *testing.T) {
	t.Parallel()
	_, s, _, shape := setupWrite(t, "writeonce.yammm")
	_ = s
	plain := shape.Types[typeID(t, s, "Plain")]
	if plain.ImmutableKeys == nil {
		t.Error("Plain has no @writeOnce properties, but its ImmutableKeys is nil — indistinguishable from a shape that never computed them")
	}
	if len(plain.ImmutableKeys) != 0 {
		t.Errorf("Plain.ImmutableKeys = %v; want empty", plain.ImmutableKeys)
	}
}

// The batch path honours @writeOnce from a hand-built shape via the schema type,
// exactly as the single-node path does.
func TestBatchNodeQueries_HandBuiltShapeStillHonorsWriteOnce(t *testing.T) {
	t.Parallel()
	a, s, v, shape := setupWrite(t, "writeonce.yammm")

	graphResult := buildGraphResult(t, s, v, map[string][]map[string]any{
		"Entity": {{"id": "e1", "name": "n", "origin": "src", "first_seen": "2024-01-01T00:00:00Z"}},
	})

	// A shape as a pre-upgrade caller would have built it: no ImmutableKeys.
	stale := &GraphShape{Types: map[schema.TypeID]NodeShape{}}
	for id, ns := range shape.Types {
		ns.ImmutableKeys = nil
		stale.Types[id] = ns
	}

	queries, err := a.BatchNodeQueries(context.Background(), graphResult, stale)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range queries {
		if q.Kind != NodeMerge || !strings.Contains(q.Statement, "wo_test__Entity") {
			continue
		}
		if !strings.Contains(q.Statement, "ON CREATE SET") {
			t.Errorf("a hand-built shape dropped the @writeOnce split: %q", q.Statement)
		}
		for _, row := range q.Params["rows"].([]map[string]any) {
			up, ok := row["update_props"].(map[string]any)
			if !ok {
				t.Fatal("Entity row missing update_props")
			}
			if _, has := up["first_seen"]; has {
				t.Error("update_props should exclude the derived @writeOnce key")
			}
		}
	}
}

// The batch row's merge-key entries and the template that reads them are two
// halves of one wire contract, so the row must carry exactly the prefixed entry
// the template reads and no unprefixed one.
func TestBatchNodeQueries_RowCarriesTheKeyTheTemplateReads(t *testing.T) {
	t.Parallel()
	a, s, v, shape := setupWrite(t, "writeonce.yammm")

	graphResult := buildGraphResult(t, s, v, map[string][]map[string]any{
		"Plain": {{"id": "p1", "name": "n"}},
	})
	queries, err := a.BatchNodeQueries(context.Background(), graphResult, shape)
	if err != nil {
		t.Fatal(err)
	}

	ns := shape.Types[typeID(t, s, "Plain")]
	var checked int
	for _, q := range queries {
		if !strings.Contains(q.Statement, ns.Label) {
			continue
		}
		for _, pk := range ns.PrimaryKeys {
			// The template must read exactly the entry the row writes.
			want := "row." + batchKeyParamPrefix + pk
			if !strings.Contains(q.Statement, want) {
				t.Errorf("statement does not read %q:\n%s", want, q.Statement)
			}
			for _, row := range q.Params["rows"].([]map[string]any) {
				if _, has := row[batchKeyParamPrefix+pk]; !has {
					t.Errorf("row has no %q entry; the MERGE would match on null: %v",
						batchKeyParamPrefix+pk, row)
				}
				if _, unprefixed := row[pk]; unprefixed {
					t.Errorf("row carries an unprefixed %q entry, which shares the namespace with props", pk)
				}
				checked++
			}
		}
	}
	if checked == 0 {
		t.Fatal("no rows were checked; the assertion would be vacuous")
	}
}

// The batch path carries the same guarantee as the single-node path: both read
// the key out of the same instance, so both must coerce it.
func TestBatchNodeQueries_CoercesMergeKey(t *testing.T) {
	t.Parallel()
	a, s, v, shape := setupWrite(t, "temporal_pk.yammm")

	graphResult := buildGraphResult(t, s, v, map[string][]map[string]any{
		"Doc": {{"created_on": "2024-01-02", "updated_on": "2024-03-04", "title": "t"}},
	})
	queries, err := a.BatchNodeQueries(context.Background(), graphResult, shape)
	if err != nil {
		t.Fatal(err)
	}

	var checked int
	for _, q := range queries {
		if !strings.Contains(q.Statement, shape.Types[typeID(t, s, "Doc")].Label) {
			continue
		}
		for _, row := range q.Params["rows"].([]map[string]any) {
			key := row[batchKeyParamPrefix+"created_on"]
			if _, isDate := key.(dbtype.Date); !isDate {
				t.Errorf("row.%screated_on = %#v (%T); want dbtype.Date",
					batchKeyParamPrefix, key, key)
			}
			checked++
		}
	}
	if checked == 0 {
		t.Fatal("no rows were checked; the assertion would be vacuous")
	}
}

// The batch edge path must coerce both endpoint keys.
func TestEdgeQueries_CoerceEndpointKeysFromAGraph(t *testing.T) {
	t.Parallel()
	a, s, v, shapes := setupWrite(t, "temporal_pk.yammm")

	graphResult := buildGraphResult(t, s, v, map[string][]map[string]any{
		"Doc":      {{"created_on": "2024-01-02", "updated_on": "2024-03-04", "title": "t"}},
		"Citation": {{"citation_id": "c1", "cites": map[string]any{"_target_created_on": "2024-01-02"}}},
	})

	if edges := graphResult.Edges(); len(edges) != 1 {
		t.Fatalf("got %d edges; want 1", len(edges))
	}

	batches, err := a.BatchEdgeQueries(context.Background(), graphResult, shapes)
	if err != nil {
		t.Fatal(err)
	}
	if len(batches) != 1 {
		t.Fatalf("got %d batch queries; want 1", len(batches))
	}
	rows := batches[0].Params["rows"].([]map[string]any)
	if to := rows[0][RelToRowPrefix+"created_on"]; !isDateValue(to) {
		t.Errorf("BatchEdgeQueries row.%screated_on = %#v (%T); want dbtype.Date",
			RelToRowPrefix, to, to)
	}
}

func isDateValue(v any) bool {
	_, ok := v.(dbtype.Date)
	return ok
}
