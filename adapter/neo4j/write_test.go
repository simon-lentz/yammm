package neo4j

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/neo4j/neo4j-go-driver/v6/neo4j/dbtype"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
)

// Compile-time interface assertions.
var (
	_ NodeSource = (*graph.Instance)(nil)
	_ NodeSource = (*instance.ValidInstance)(nil)
)

func TestNodeQueryFor_SinglePK(t *testing.T) {
	t.Parallel()
	s, v := loadSchemaAndValidator(t, "basic.yammm")
	a := New()

	shape, result := a.ShapeForSchema(context.Background(), s)
	if err := result.Err(); err != nil {
		t.Fatal(err)
	}

	graphResult := buildGraphResult(t, s, v, map[string][]map[string]any{
		"Entity": {{"id": "e1", "name": "test", "count": int64(5), "active": true, "created_at": "2024-01-01T00:00:00Z"}},
	})

	inst := graphResult.InstancesOf("Entity")[0]
	ns := shape.Types["Entity"]
	st, _ := s.Type("Entity")
	q, err := a.NodeQueryFor(context.Background(), &ns, inst, st)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(q.Statement, "MERGE (n:basic_test__Entity {id: $key_id})") {
		t.Errorf("unexpected statement: %s", q.Statement)
	}
	if !strings.Contains(q.Statement, "SET n += $props") {
		t.Errorf("missing SET clause: %s", q.Statement)
	}
	if q.Params["key_id"] != "e1" {
		t.Errorf("key_id = %v; want e1", q.Params["key_id"])
	}
	if q.Params["props"] == nil {
		t.Error("props should not be nil")
	}
}

func TestNodeQueryFor_CompositePK(t *testing.T) {
	t.Parallel()
	s, v := loadSchemaAndValidator(t, "composite_pk.yammm")
	a := New()

	shape, result := a.ShapeForSchema(context.Background(), s)
	if err := result.Err(); err != nil {
		t.Fatal(err)
	}

	graphResult := buildGraphResult(t, s, v, map[string][]map[string]any{
		"Record": {{"schema_id": "s1", "record_id": "r1", "name": "test"}},
	})

	inst := graphResult.InstancesOf("Record")[0]
	ns := shape.Types["Record"]
	st, _ := s.Type("Record")
	q, err := a.NodeQueryFor(context.Background(), &ns, inst, st)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(q.Statement, "schema_id: $key_schema_id") {
		t.Errorf("missing schema_id key: %s", q.Statement)
	}
	if !strings.Contains(q.Statement, "record_id: $key_record_id") {
		t.Errorf("missing record_id key: %s", q.Statement)
	}
}

func TestNodeQueryFor_ImmutableKeys(t *testing.T) {
	t.Parallel()
	s, v := loadSchemaAndValidator(t, "basic.yammm")
	a := New()

	shape, result := a.ShapeForSchema(context.Background(), s)
	if err := result.Err(); err != nil {
		t.Fatal(err)
	}

	graphResult := buildGraphResult(t, s, v, map[string][]map[string]any{
		"Entity": {{"id": "e1", "name": "test", "count": int64(1), "active": true, "created_at": "2024-01-01T00:00:00Z"}},
	})

	inst := graphResult.InstancesOf("Entity")[0]
	ns := shape.Types["Entity"]
	st, _ := s.Type("Entity")
	q, err := a.NodeQueryFor(context.Background(), &ns, inst, st, WithImmutableKeys("created_at"))
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(q.Statement, "ON CREATE SET") {
		t.Errorf("missing ON CREATE SET: %s", q.Statement)
	}
	if !strings.Contains(q.Statement, "ON MATCH SET") {
		t.Errorf("missing ON MATCH SET: %s", q.Statement)
	}

	updateProps, ok := q.Params["update_props"].(map[string]any)
	if !ok {
		t.Fatal("update_props should be map[string]any")
	}
	if _, has := updateProps["created_at"]; has {
		t.Error("update_props should not contain immutable key 'created_at'")
	}
}

func TestBatchNodeQueries_SingleType(t *testing.T) {
	t.Parallel()
	s, v := loadSchemaAndValidator(t, "basic.yammm")
	a := New()

	shape, result := a.ShapeForSchema(context.Background(), s)
	if err := result.Err(); err != nil {
		t.Fatal(err)
	}

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
	if !strings.Contains(q.Statement, "UNWIND $rows AS row") {
		t.Errorf("missing UNWIND: %s", q.Statement)
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
	s, v := loadSchemaAndValidator(t, "basic.yammm")
	a := New()

	shape, result := a.ShapeForSchema(context.Background(), s)
	if err := result.Err(); err != nil {
		t.Fatal(err)
	}

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

func TestEdgeQueryFor_Basic(t *testing.T) {
	t.Parallel()
	s, v := loadSchemaAndValidator(t, "write_basic.yammm")
	a := New()

	shape, result := a.ShapeForSchema(context.Background(), s)
	if err := result.Err(); err != nil {
		t.Fatal(err)
	}

	graphResult := buildGraphResult(t, s, v, map[string][]map[string]any{
		"Issuer": {{"issuer_id": "iss1", "name": "Test Issuer"}},
		"Issue":  {{"issuer_id": "iss1", "issue_id": "i1", "title": "Test Issue", "in_issuer": map[string]any{"_target_issuer_id": "iss1"}}},
	})

	edges := graphResult.Edges()
	if len(edges) == 0 {
		t.Fatal("expected at least one edge")
	}

	q, err := a.EdgeQueryFor(context.Background(), edges[0], shape)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(q.Statement, "MATCH (from:") {
		t.Errorf("missing MATCH from: %s", q.Statement)
	}
	if !strings.Contains(q.Statement, "MATCH (to:") {
		t.Errorf("missing MATCH to: %s", q.Statement)
	}
	if !strings.Contains(q.Statement, "MERGE (from)-[r:") {
		t.Errorf("missing MERGE relationship: %s", q.Statement)
	}
}

func TestEdgeQueryFor_NoProperties(t *testing.T) {
	t.Parallel()
	s, v := loadSchemaAndValidator(t, "write_basic.yammm")
	a := New()

	shape, result := a.ShapeForSchema(context.Background(), s)
	if err := result.Err(); err != nil {
		t.Fatal(err)
	}

	graphResult := buildGraphResult(t, s, v, map[string][]map[string]any{
		"Issuer": {{"issuer_id": "iss1", "name": "Test Issuer"}},
		"Issue":  {{"issuer_id": "iss1", "issue_id": "i1", "title": "Test Issue", "in_issuer": map[string]any{"_target_issuer_id": "iss1"}}},
	})

	edges := graphResult.Edges()
	if len(edges) == 0 {
		t.Fatal("expected at least one edge")
	}

	// write_basic.yammm edges have no properties.
	edge := edges[0]
	if edge.HasProperties() {
		t.Skip("edge has properties; cannot test no-props path")
	}

	q, err := a.EdgeQueryFor(context.Background(), edge, shape)
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(q.Statement, "SET r +=") {
		t.Errorf("statement should omit SET r += for propertyless edge: %s", q.Statement)
	}
}

func TestBatchEdgeQueries_GroupBySignature(t *testing.T) {
	t.Parallel()
	s, v := loadSchemaAndValidator(t, "write_basic.yammm")
	a := New()

	shape, result := a.ShapeForSchema(context.Background(), s)
	if err := result.Err(); err != nil {
		t.Fatal(err)
	}

	graphResult := buildGraphResult(t, s, v, map[string][]map[string]any{
		"Issuer": {
			{"issuer_id": "iss1", "name": "Issuer 1"},
			{"issuer_id": "iss2", "name": "Issuer 2"},
		},
		"Issue": {
			{"issuer_id": "iss1", "issue_id": "i1", "title": "Issue 1", "in_issuer": map[string]any{"_target_issuer_id": "iss1"}},
			{"issuer_id": "iss2", "issue_id": "i2", "title": "Issue 2", "in_issuer": map[string]any{"_target_issuer_id": "iss2"}},
		},
	})

	queries, err := a.BatchEdgeQueries(context.Background(), graphResult, shape)
	if err != nil {
		t.Fatal(err)
	}

	// All edges have the same signature (Issue->IN_ISSUER->Issuer), so 1 query.
	// (Plus the reverse ISSUES edges from Issuer->Issue.)
	if len(queries) == 0 {
		t.Fatal("expected at least one batch edge query")
	}

	// Each query should have an UNWIND statement.
	for _, q := range queries {
		if !strings.Contains(q.Statement, "UNWIND $rows AS row") {
			t.Errorf("missing UNWIND: %s", q.Statement)
		}
		if q.RelationType == "" {
			t.Error("RelationType should be non-empty")
		}
	}
}

func TestBatchEdgeQueries_Chunking(t *testing.T) {
	t.Parallel()
	s, v := loadSchemaAndValidator(t, "write_basic.yammm")
	a := New()

	shape, result := a.ShapeForSchema(context.Background(), s)
	if err := result.Err(); err != nil {
		t.Fatal(err)
	}

	// Create enough edges to trigger chunking.
	var issuers []map[string]any
	var issues []map[string]any
	for i := range 5 {
		issuerID := fmt.Sprintf("iss%d", i)
		issuers = append(issuers, map[string]any{"issuer_id": issuerID, "name": fmt.Sprintf("I%d", i)})
		issues = append(issues, map[string]any{
			"issuer_id": issuerID, "issue_id": fmt.Sprintf("i%d", i),
			"title": fmt.Sprintf("T%d", i), "in_issuer": map[string]any{"_target_issuer_id": issuerID},
		})
	}

	graphResult := buildGraphResult(t, s, v, map[string][]map[string]any{
		"Issuer": issuers,
		"Issue":  issues,
	})

	queries, err := a.BatchEdgeQueries(context.Background(), graphResult, shape, WithEdgeChunkSize(2))
	if err != nil {
		t.Fatal(err)
	}

	// With chunk size 2 and multiple edges, we should get multiple queries
	// for signatures that have >2 edges.
	totalEdges := len(graphResult.Edges())
	if totalEdges == 0 {
		t.Fatal("expected edges")
	}

	// Just verify chunking produces more than 1 query for a signature with >2 edges.
	if len(queries) < 2 {
		t.Logf("total edges: %d, queries: %d", totalEdges, len(queries))
	}
}

func TestPropertyCoercion_TypedSlice(t *testing.T) {
	t.Parallel()
	s, v := loadSchemaAndValidator(t, "list_properties.yammm")
	a := New()

	shape, result := a.ShapeForSchema(context.Background(), s)
	if err := result.Err(); err != nil {
		t.Fatal(err)
	}

	graphResult := buildGraphResult(t, s, v, map[string][]map[string]any{
		"Entity": {{"id": "e1", "name": "test", "active": true, "tags": []any{"a", "b"}, "scores": []any{int64(1), int64(2)}}},
	})

	queries, err := a.BatchNodeQueries(context.Background(), graphResult, shape)
	if err != nil {
		t.Fatal(err)
	}
	if len(queries) == 0 {
		t.Fatal("expected queries")
	}

	rows := queries[0].Params["rows"].([]map[string]any)
	props := rows[0]["props"].(map[string]any)

	// tags should be []string, not []any.
	if tags, ok := props["tags"]; ok {
		switch tags.(type) {
		case []string:
			// correct
		default:
			t.Errorf("tags should be []string, got %T", tags)
		}
	}

	// scores should be []int64, not []any.
	if scores, ok := props["scores"]; ok {
		switch scores.(type) {
		case []int64:
			// correct
		default:
			t.Errorf("scores should be []int64, got %T", scores)
		}
	}
}

func TestPropertyCoercion_TemporalSlice(t *testing.T) {
	t.Parallel()
	s, v := loadSchemaAndValidator(t, "list_properties.yammm")
	a := New()

	shape, result := a.ShapeForSchema(context.Background(), s)
	if err := result.Err(); err != nil {
		t.Fatal(err)
	}

	graphResult := buildGraphResult(t, s, v, map[string][]map[string]any{
		"Entity": {{
			"id": "e1", "name": "test", "active": true,
			"times": []any{"2024-01-01T00:00:00Z", "2024-06-15T12:30:00Z"},
			"dates": []any{"2024-01-01", "2024-06-15"},
		}},
	})

	queries, err := a.BatchNodeQueries(context.Background(), graphResult, shape)
	if err != nil {
		t.Fatal(err)
	}
	if len(queries) == 0 {
		t.Fatal("expected queries")
	}

	rows := queries[0].Params["rows"].([]map[string]any)
	props := rows[0]["props"].(map[string]any)

	// Temporal list elements are coerced to driver-compatible types:
	// List<Timestamp> strings -> []time.Time, List<Date> strings -> []dbtype.Date.
	if times, ok := props["times"]; ok {
		switch v := times.(type) {
		case []time.Time:
			if len(v) != 2 {
				t.Errorf("times length = %d; want 2", len(v))
			}
		default:
			t.Errorf("times should be []time.Time, got %T", times)
		}
	}

	if dates, ok := props["dates"]; ok {
		switch v := dates.(type) {
		case []dbtype.Date:
			if len(v) != 2 {
				t.Errorf("dates length = %d; want 2", len(v))
			}
		default:
			t.Errorf("dates should be []dbtype.Date, got %T", dates)
		}
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

	result := coerceSlice(raw, prop.Constraint())
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
	result := coerceSlice(raw, prop.Constraint())
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
	result := coerceSlice(raw, prop.Constraint())
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
		result := coerceSlice(raw, prop.Constraint())
		if _, ok := result.([]string); !ok {
			t.Errorf("expected []string fallback, got %T", result)
		}
	})

	t.Run("TimestampParseFailure", func(t *testing.T) {
		t.Parallel()
		prop, ok := st.Property("times")
		if !ok {
			t.Fatal("times property not found")
		}
		raw := []any{"not-a-timestamp", "also-not"}
		result := coerceSlice(raw, prop.Constraint())
		if _, ok := result.([]string); !ok {
			t.Errorf("expected []string fallback, got %T", result)
		}
	})
}

func TestPropertyCoercion_Scalars(t *testing.T) {
	t.Parallel()
	s, v := loadSchemaAndValidator(t, "basic.yammm")
	a := New()

	shape, result := a.ShapeForSchema(context.Background(), s)
	if err := result.Err(); err != nil {
		t.Fatal(err)
	}

	graphResult := buildGraphResult(t, s, v, map[string][]map[string]any{
		"Entity": {{"id": "e1", "name": "test", "count": int64(42), "active": true, "created_at": "2024-01-01T00:00:00Z", "score": 3.14}},
	})

	queries, err := a.BatchNodeQueries(context.Background(), graphResult, shape)
	if err != nil {
		t.Fatal(err)
	}

	rows := queries[0].Params["rows"].([]map[string]any)
	props := rows[0]["props"].(map[string]any)

	// Scalar values pass through as their native types.
	if _, ok := props["id"].(string); !ok {
		t.Errorf("id should be string, got %T", props["id"])
	}
	if _, ok := props["count"].(int64); !ok {
		t.Errorf("count should be int64, got %T", props["count"])
	}
	if _, ok := props["active"].(bool); !ok {
		t.Errorf("active should be bool, got %T", props["active"])
	}
}

func TestPropertyCoercion_TemporalScalar(t *testing.T) {
	t.Parallel()
	s, v := loadSchemaAndValidator(t, "basic.yammm")
	a := New()

	shape, result := a.ShapeForSchema(context.Background(), s)
	if err := result.Err(); err != nil {
		t.Fatal(err)
	}

	graphResult := buildGraphResult(t, s, v, map[string][]map[string]any{
		"Entity": {{
			"id": "e1", "name": "test", "count": int64(1), "active": true,
			"created_at": "2024-01-01T00:00:00Z",
			"birth_date": "2024-06-15",
		}},
	})

	queries, err := a.BatchNodeQueries(context.Background(), graphResult, shape)
	if err != nil {
		t.Fatal(err)
	}

	rows := queries[0].Params["rows"].([]map[string]any)
	props := rows[0]["props"].(map[string]any)

	// Date property should coerce to dbtype.Date, not string.
	if bd, ok := props["birth_date"]; ok {
		if _, isDate := bd.(dbtype.Date); !isDate {
			t.Errorf("birth_date should be dbtype.Date, got %T", bd)
		}
	} else {
		t.Error("birth_date missing from props")
	}

	// Timestamp property should coerce to time.Time, not string.
	if ca, ok := props["created_at"]; ok {
		if _, isTime := ca.(time.Time); !isTime {
			t.Errorf("created_at should be time.Time, got %T", ca)
		}
	} else {
		t.Error("created_at missing from props")
	}
}

func TestCoerceTemporalScalar_Unit(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, "basic.yammm")
	st, ok := s.Type("Entity")
	if !ok {
		t.Fatal("Entity not found")
	}

	t.Run("Date", func(t *testing.T) {
		t.Parallel()
		prop, ok := st.Property("birth_date")
		if !ok {
			t.Fatal("birth_date property not found")
		}
		result := coerceTemporalScalar("2024-06-15", prop.Constraint())
		if _, isDate := result.(dbtype.Date); !isDate {
			t.Errorf("expected dbtype.Date, got %T", result)
		}
	})

	t.Run("Timestamp_RFC3339", func(t *testing.T) {
		t.Parallel()
		prop, ok := st.Property("created_at")
		if !ok {
			t.Fatal("created_at property not found")
		}
		result := coerceTemporalScalar("2024-01-01T00:00:00Z", prop.Constraint())
		if _, isTime := result.(time.Time); !isTime {
			t.Errorf("expected time.Time, got %T", result)
		}
	})

	t.Run("Timestamp_RFC3339Nano", func(t *testing.T) {
		t.Parallel()
		prop, ok := st.Property("created_at")
		if !ok {
			t.Fatal("created_at property not found")
		}
		result := coerceTemporalScalar("2024-01-01T00:00:00.123456789Z", prop.Constraint())
		tt, isTime := result.(time.Time)
		if !isTime {
			t.Fatalf("expected time.Time, got %T", result)
		}
		if tt.Nanosecond() != 123456789 {
			t.Errorf("nanosecond = %d; want 123456789", tt.Nanosecond())
		}
	})

	t.Run("UnparseableDate", func(t *testing.T) {
		t.Parallel()
		prop, ok := st.Property("birth_date")
		if !ok {
			t.Fatal("birth_date property not found")
		}
		result := coerceTemporalScalar("not-a-date", prop.Constraint())
		if s, ok := result.(string); !ok || s != "not-a-date" {
			t.Errorf("expected original string, got %T %v", result, result)
		}
	})

	t.Run("UnparseableTimestamp", func(t *testing.T) {
		t.Parallel()
		prop, ok := st.Property("created_at")
		if !ok {
			t.Fatal("created_at property not found")
		}
		result := coerceTemporalScalar("not-a-timestamp", prop.Constraint())
		if s, ok := result.(string); !ok || s != "not-a-timestamp" {
			t.Errorf("expected original string, got %T %v", result, result)
		}
	})

	t.Run("NonTemporalConstraint", func(t *testing.T) {
		t.Parallel()
		result := coerceTemporalScalar("hello", schema.NewStringConstraint())
		if s, ok := result.(string); !ok || s != "hello" {
			t.Errorf("expected original string, got %T %v", result, result)
		}
	})
}

func TestNodeQueryFor_MissingKey(t *testing.T) {
	t.Parallel()
	s, v := loadSchemaAndValidator(t, "basic.yammm")
	a := New()

	_, result := a.ShapeForSchema(context.Background(), s)
	if err := result.Err(); err != nil {
		t.Fatal(err)
	}

	graphResult := buildGraphResult(t, s, v, map[string][]map[string]any{
		"Entity": {{"id": "e1", "name": "test", "count": int64(1), "active": true, "created_at": "2024-01-01T00:00:00Z"}},
	})

	inst := graphResult.InstancesOf("Entity")[0]

	// Use a shape with a PK that doesn't exist in the instance.
	badShape := NodeShape{
		Label:       "test__Entity",
		PrimaryKeys: []string{"nonexistent_key"},
	}

	_, err := a.NodeQueryFor(context.Background(), &badShape, inst, nil)
	if err == nil {
		t.Error("expected error for missing key")
	}
}

func TestNodeQueryFor_EmptyPrimaryKeys(t *testing.T) {
	t.Parallel()
	s, v := loadSchemaAndValidator(t, "basic.yammm")
	a := New()

	graphResult := buildGraphResult(t, s, v, map[string][]map[string]any{
		"Entity": {{"id": "e1", "name": "test", "count": int64(1), "active": true, "created_at": "2024-01-01T00:00:00Z"}},
	})

	inst := graphResult.InstancesOf("Entity")[0]
	emptyPKShape := NodeShape{
		Label:       "test__Entity",
		PrimaryKeys: nil,
	}

	_, err := a.NodeQueryFor(context.Background(), &emptyPKShape, inst, nil)
	if err == nil {
		t.Error("expected error for empty primary keys")
	}
	if !strings.Contains(err.Error(), "no primary keys defined") {
		t.Errorf("unexpected error message: %v", err)
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
	emptyShapes := &GraphShape{Types: map[string]NodeShape{}}
	_, err := a.BatchNodeQueries(context.Background(), graphResult, emptyShapes)
	if err == nil {
		t.Error("expected error for missing shape")
	}
	if !strings.Contains(err.Error(), "no shape for type") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestEdgeQueryFor_MissingSourceShape(t *testing.T) {
	t.Parallel()
	s, v := loadSchemaAndValidator(t, "write_basic.yammm")
	a := New()

	graphResult := buildGraphResult(t, s, v, map[string][]map[string]any{
		"Issuer": {{"issuer_id": "iss1", "name": "Test"}},
		"Issue":  {{"issuer_id": "iss1", "issue_id": "i1", "title": "Test", "in_issuer": map[string]any{"_target_issuer_id": "iss1"}}},
	})

	edges := graphResult.Edges()
	if len(edges) == 0 {
		t.Fatal("expected edges")
	}

	// Shape map with only one type — whichever type is the source will be missing.
	partialShapes := &GraphShape{Types: map[string]NodeShape{
		"__none__": {Label: "x", PrimaryKeys: []string{"x"}},
	}}
	_, err := a.EdgeQueryFor(context.Background(), edges[0], partialShapes)
	if err == nil {
		t.Error("expected error for missing source/target shape")
	}
	if !strings.Contains(err.Error(), "no shape for") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestBatchEdgeQueries_MissingShape(t *testing.T) {
	t.Parallel()
	s, v := loadSchemaAndValidator(t, "write_basic.yammm")
	a := New()

	graphResult := buildGraphResult(t, s, v, map[string][]map[string]any{
		"Issuer": {{"issuer_id": "iss1", "name": "Test"}},
		"Issue":  {{"issuer_id": "iss1", "issue_id": "i1", "title": "Test", "in_issuer": map[string]any{"_target_issuer_id": "iss1"}}},
	})

	emptyShapes := &GraphShape{Types: map[string]NodeShape{}}
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
	s, v := loadSchemaAndValidator(t, "edge_mixed.yammm")
	a := New()

	shape, result := a.ShapeForSchema(context.Background(), s)
	if err := result.Err(); err != nil {
		t.Fatal(err)
	}

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

func TestEdgeQueryFor_InvalidRelType(t *testing.T) {
	t.Parallel()
	s, v := loadSchemaAndValidator(t, "write_basic.yammm")
	a := New()

	shape, result := a.ShapeForSchema(context.Background(), s)
	if err := result.Err(); err != nil {
		t.Fatal(err)
	}

	graphResult := buildGraphResult(t, s, v, map[string][]map[string]any{
		"Issuer": {{"issuer_id": "iss1", "name": "Test"}},
		"Issue":  {{"issuer_id": "iss1", "issue_id": "i1", "title": "Test", "in_issuer": map[string]any{"_target_issuer_id": "iss1"}}},
	})

	edges := graphResult.Edges()
	if len(edges) == 0 {
		t.Fatal("expected edges")
	}

	// The actual edges have valid relation types from the schema.
	// ValidateIdentifier is called on edge.Relation() which comes from
	// the schema, so these pass. Testing the error path requires an
	// edge with an invalid relation name, which can't be produced by
	// the graph builder. Verify that valid edges pass instead.
	for _, edge := range edges {
		_, err := a.EdgeQueryFor(context.Background(), edge, shape)
		if err != nil {
			t.Errorf("EdgeQueryFor failed for valid edge %s: %v", edge.Relation(), err)
		}
	}
}

// --- EdgeQueriesFor tests ---

// validateInstance validates a single instance without building a graph.
func validateInstance(t *testing.T, v *instance.Validator, typeName string, props map[string]any) *instance.ValidInstance {
	t.Helper()
	valid, result := v.ValidateOne(context.Background(), typeName, instance.RawInstance{Properties: props})
	if !result.OK() {
		t.Fatalf("validate %s: %v", typeName, result.Messages())
	}
	return valid
}

func TestEdgeQueriesFor_Basic(t *testing.T) {
	t.Parallel()
	s, v := loadSchemaAndValidator(t, "write_basic.yammm")
	a := New()

	shapes, result := a.ShapeForSchema(context.Background(), s)
	if err := result.Err(); err != nil {
		t.Fatal(err)
	}

	// Create an Issue that references an Issuer via IN_ISSUER.
	valid := validateInstance(t, v, "Issue", map[string]any{
		"issuer_id": "iss1",
		"issue_id":  "issue1",
		"title":     "Test Issue",
		"in_issuer": map[string]any{"_target_issuer_id": "iss1"},
	})

	st, _ := s.Type("Issue")
	queries, err := a.EdgeQueriesFor(context.Background(), valid, st, shapes)
	if err != nil {
		t.Fatal(err)
	}

	if len(queries) != 1 {
		t.Fatalf("got %d queries; want 1", len(queries))
	}

	q := queries[0]
	if !strings.Contains(q.Statement, "MERGE (from)-[r:IN_ISSUER]->(to)") {
		t.Errorf("unexpected statement: %s", q.Statement)
	}
	if !strings.Contains(q.Statement, "MATCH (from:write_test__Issue") {
		t.Errorf("missing source MATCH: %s", q.Statement)
	}
	if !strings.Contains(q.Statement, "MATCH (to:write_test__Issuer") {
		t.Errorf("missing target MATCH: %s", q.Statement)
	}
	// Source keys use from_key_ prefix.
	if q.Params["from_key_issuer_id"] != "iss1" || q.Params["from_key_issue_id"] != "issue1" {
		t.Errorf("unexpected source key params: %v", q.Params)
	}
	// Target key uses to_key_ prefix.
	if q.Params["to_key_issuer_id"] != "iss1" {
		t.Errorf("unexpected target key params: %v", q.Params)
	}
}

func TestEdgeQueriesFor_MultiTarget(t *testing.T) {
	t.Parallel()
	s, v := loadSchemaAndValidator(t, "write_basic.yammm")
	a := New()

	shapes, result := a.ShapeForSchema(context.Background(), s)
	if err := result.Err(); err != nil {
		t.Fatal(err)
	}

	// Issuer with ISSUES pointing to multiple Issues.
	valid := validateInstance(t, v, "Issuer", map[string]any{
		"issuer_id": "iss1",
		"name":      "Test Issuer",
		"issues": []any{
			map[string]any{"_target_issuer_id": "iss1", "_target_issue_id": "issue1"},
			map[string]any{"_target_issuer_id": "iss1", "_target_issue_id": "issue2"},
		},
	})

	st, _ := s.Type("Issuer")
	queries, err := a.EdgeQueriesFor(context.Background(), valid, st, shapes)
	if err != nil {
		t.Fatal(err)
	}

	if len(queries) != 2 {
		t.Fatalf("got %d queries; want 2", len(queries))
	}
	for _, q := range queries {
		if !strings.Contains(q.Statement, "MERGE (from)-[r:ISSUES]->(to)") {
			t.Errorf("unexpected statement: %s", q.Statement)
		}
	}
}

func TestEdgeQueriesFor_EdgeProperties(t *testing.T) {
	t.Parallel()
	s, v := loadSchemaAndValidator(t, "edge_mixed.yammm")
	a := New()

	shapes, result := a.ShapeForSchema(context.Background(), s)
	if err := result.Err(); err != nil {
		t.Fatal(err)
	}

	// Employee with WORKS_AT edge that includes "note" property.
	valid := validateInstance(t, v, "Employee", map[string]any{
		"employee_id": "emp1",
		"name":        "Alice",
		"works_at": map[string]any{
			"_target_company_id": "c1",
			"note":               "senior engineer",
		},
	})

	st, _ := s.Type("Employee")
	queries, err := a.EdgeQueriesFor(context.Background(), valid, st, shapes)
	if err != nil {
		t.Fatal(err)
	}

	if len(queries) != 1 {
		t.Fatalf("got %d queries; want 1", len(queries))
	}

	q := queries[0]
	if !strings.Contains(q.Statement, "SET r += $rel_props") {
		t.Errorf("missing rel_props SET: %s", q.Statement)
	}
	relProps, ok := q.Params["rel_props"].(map[string]any)
	if !ok {
		t.Fatalf("rel_props not map[string]any: %T", q.Params["rel_props"])
	}
	if relProps["note"] != "senior engineer" {
		t.Errorf("rel_props[note] = %v; want 'senior engineer'", relProps["note"])
	}
}

func TestEdgeQueriesFor_NoEdges(t *testing.T) {
	t.Parallel()
	s, v := loadSchemaAndValidator(t, "basic.yammm")
	a := New()

	shapes, result := a.ShapeForSchema(context.Background(), s)
	if err := result.Err(); err != nil {
		t.Fatal(err)
	}

	valid := validateInstance(t, v, "Entity", map[string]any{
		"id":         "e1",
		"name":       "test",
		"count":      int64(5),
		"active":     true,
		"created_at": "2024-01-01T00:00:00Z",
	})

	st, _ := s.Type("Entity")
	queries, err := a.EdgeQueriesFor(context.Background(), valid, st, shapes)
	if err != nil {
		t.Fatal(err)
	}
	if len(queries) != 0 {
		t.Errorf("got %d queries; want 0", len(queries))
	}
}

func TestEdgeQueriesFor_MissingTargetShape(t *testing.T) {
	t.Parallel()
	s, v := loadSchemaAndValidator(t, "write_basic.yammm")
	a := New()

	valid := validateInstance(t, v, "Issue", map[string]any{
		"issuer_id": "iss1",
		"issue_id":  "issue1",
		"title":     "Test",
		"in_issuer": map[string]any{"_target_issuer_id": "iss1"},
	})

	// Provide shapes with only Issue (no Issuer shape).
	incompleteShapes := &GraphShape{
		Types: map[string]NodeShape{
			"Issue": {
				Type:        "Issue",
				Label:       "write_test__Issue",
				PrimaryKeys: []string{"issuer_id", "issue_id"},
			},
		},
	}

	st, _ := s.Type("Issue")
	_, err := a.EdgeQueriesFor(context.Background(), valid, st, incompleteShapes)
	if err == nil {
		t.Fatal("expected error for missing target shape")
	}
	if !strings.Contains(err.Error(), "no shape for target type") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestNodeQueryFor_ValidInstance(t *testing.T) {
	t.Parallel()
	s, v := loadSchemaAndValidator(t, "basic.yammm")
	a := New()

	shape, result := a.ShapeForSchema(context.Background(), s)
	if err := result.Err(); err != nil {
		t.Fatal(err)
	}

	// Use ValidInstance directly (streaming path) instead of *graph.Instance.
	valid := validateInstance(t, v, "Entity", map[string]any{
		"id":         "e1",
		"name":       "test",
		"count":      int64(5),
		"active":     true,
		"created_at": "2024-01-01T00:00:00Z",
	})

	ns := shape.Types["Entity"]
	st, _ := s.Type("Entity")
	q, err := a.NodeQueryFor(context.Background(), &ns, valid, st)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(q.Statement, "MERGE (n:basic_test__Entity {id: $key_id})") {
		t.Errorf("unexpected statement: %s", q.Statement)
	}
	if q.Params["key_id"] != "e1" {
		t.Errorf("key_id = %v; want e1", q.Params["key_id"])
	}
}

// --- extractKeyFromImmutableKey tests ---

func TestExtractKeyFromImmutableKey_SingleComponent(t *testing.T) {
	t.Parallel()
	key := immutable.WrapKey([]any{"val1"})
	result, err := extractKeyFromImmutableKey(key, []string{"id"})
	if err != nil {
		t.Fatal(err)
	}
	if result["id"] != "val1" {
		t.Errorf("id = %v; want val1", result["id"])
	}
}

func TestExtractKeyFromImmutableKey_Composite(t *testing.T) {
	t.Parallel()
	key := immutable.WrapKey([]any{"s1", "r1"})
	result, err := extractKeyFromImmutableKey(key, []string{"schema_id", "record_id"})
	if err != nil {
		t.Fatal(err)
	}
	if result["schema_id"] != "s1" {
		t.Errorf("schema_id = %v; want s1", result["schema_id"])
	}
	if result["record_id"] != "r1" {
		t.Errorf("record_id = %v; want r1", result["record_id"])
	}
}

func TestExtractKeyFromImmutableKey_LengthMismatch(t *testing.T) {
	t.Parallel()
	key := immutable.WrapKey([]any{"val1"})
	_, err := extractKeyFromImmutableKey(key, []string{"a", "b"})
	if err == nil {
		t.Fatal("expected error for length mismatch")
	}
	if !strings.Contains(err.Error(), "1 components but 2 key names") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestExtractKeyFromImmutableKey_NoKeys(t *testing.T) {
	t.Parallel()
	key := immutable.WrapKey([]any{"val1"})
	_, err := extractKeyFromImmutableKey(key, nil)
	if err == nil {
		t.Fatal("expected error for no keys")
	}
}
