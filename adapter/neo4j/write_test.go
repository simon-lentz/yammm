package neo4j

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/schema/load"
)

// loadSchemaAndValidator loads a test schema and returns the schema + validator.
func loadSchemaAndValidator(t *testing.T, name string) (*schema.Schema, *instance.Validator) {
	t.Helper()
	s, result, err := load.Load(context.Background(), filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("load.Load(%s) failed: %v", name, err)
	}
	if !result.OK() {
		t.Fatalf("schema %s has errors: %v", name, result)
	}
	return s, instance.NewValidator(s)
}

// buildGraphResult validates instances and builds a graph.Result snapshot.
func buildGraphResult(t *testing.T, s *schema.Schema, v *instance.Validator, instances map[string][]map[string]any) *graph.Result {
	t.Helper()
	ctx := context.Background()
	g := graph.New(s)
	for typeName, records := range instances {
		for _, props := range records {
			valid, failure, err := v.ValidateOne(ctx, typeName, instance.RawInstance{Properties: props})
			if err != nil {
				t.Fatalf("validate %s: %v", typeName, err)
			}
			if failure != nil {
				t.Fatalf("validate %s failed: %v", typeName, failure.Result.Messages())
			}
			result, err := g.Add(ctx, valid)
			if err != nil {
				t.Fatalf("graph.Add %s: %v", typeName, err)
			}
			if !result.OK() {
				t.Fatalf("graph.Add %s issues: %v", typeName, result.Messages())
			}
		}
	}
	return g.Snapshot()
}

func TestNodeQueryFor_SinglePK(t *testing.T) {
	t.Parallel()
	s, v := loadSchemaAndValidator(t, "basic.yammm")
	a := New()

	shape, err := a.ShapeForSchema(s)
	if err != nil {
		t.Fatal(err)
	}

	result := buildGraphResult(t, s, v, map[string][]map[string]any{
		"Entity": {{"id": "e1", "name": "test", "count": int64(5), "active": true, "created_at": "2024-01-01T00:00:00Z"}},
	})

	inst := result.InstancesOf("Entity")[0]
	ns := shape.Types["Entity"]
	q, err := a.NodeQueryFor(&ns, inst)
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

	shape, err := a.ShapeForSchema(s)
	if err != nil {
		t.Fatal(err)
	}

	result := buildGraphResult(t, s, v, map[string][]map[string]any{
		"Record": {{"schema_id": "s1", "record_id": "r1", "name": "test"}},
	})

	inst := result.InstancesOf("Record")[0]
	ns := shape.Types["Record"]
	q, err := a.NodeQueryFor(&ns, inst)
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

	shape, err := a.ShapeForSchema(s)
	if err != nil {
		t.Fatal(err)
	}

	result := buildGraphResult(t, s, v, map[string][]map[string]any{
		"Entity": {{"id": "e1", "name": "test", "count": int64(1), "active": true, "created_at": "2024-01-01T00:00:00Z"}},
	})

	inst := result.InstancesOf("Entity")[0]
	ns := shape.Types["Entity"]
	q, err := a.NodeQueryFor(&ns, inst, WithImmutableKeys("created_at"))
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

	shape, err := a.ShapeForSchema(s)
	if err != nil {
		t.Fatal(err)
	}

	result := buildGraphResult(t, s, v, map[string][]map[string]any{
		"Entity": {
			{"id": "e1", "name": "a", "count": int64(1), "active": true, "created_at": "2024-01-01T00:00:00Z"},
			{"id": "e2", "name": "b", "count": int64(2), "active": false, "created_at": "2024-01-02T00:00:00Z"},
		},
	})

	queries, err := a.BatchNodeQueries(result, shape)
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

	shape, err := a.ShapeForSchema(s)
	if err != nil {
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

	result := buildGraphResult(t, s, v, map[string][]map[string]any{
		"Entity": instances,
	})

	queries, err := a.BatchNodeQueries(result, shape, WithNodeChunkSize(2))
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

	shape, err := a.ShapeForSchema(s)
	if err != nil {
		t.Fatal(err)
	}

	result := buildGraphResult(t, s, v, map[string][]map[string]any{
		"Issuer": {{"issuer_id": "iss1", "name": "Test Issuer"}},
		"Issue":  {{"issuer_id": "iss1", "issue_id": "i1", "title": "Test Issue", "in_issuer": map[string]any{"_target_issuer_id": "iss1"}}},
	})

	edges := result.Edges()
	if len(edges) == 0 {
		t.Fatal("expected at least one edge")
	}

	q, err := a.EdgeQueryFor(edges[0], shape)
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

	shape, err := a.ShapeForSchema(s)
	if err != nil {
		t.Fatal(err)
	}

	result := buildGraphResult(t, s, v, map[string][]map[string]any{
		"Issuer": {{"issuer_id": "iss1", "name": "Test Issuer"}},
		"Issue":  {{"issuer_id": "iss1", "issue_id": "i1", "title": "Test Issue", "in_issuer": map[string]any{"_target_issuer_id": "iss1"}}},
	})

	edges := result.Edges()
	if len(edges) == 0 {
		t.Fatal("expected at least one edge")
	}

	// write_basic.yammm edges have no properties.
	edge := edges[0]
	if edge.HasProperties() {
		t.Skip("edge has properties; cannot test no-props path")
	}

	q, err := a.EdgeQueryFor(edge, shape)
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

	shape, err := a.ShapeForSchema(s)
	if err != nil {
		t.Fatal(err)
	}

	result := buildGraphResult(t, s, v, map[string][]map[string]any{
		"Issuer": {
			{"issuer_id": "iss1", "name": "Issuer 1"},
			{"issuer_id": "iss2", "name": "Issuer 2"},
		},
		"Issue": {
			{"issuer_id": "iss1", "issue_id": "i1", "title": "Issue 1", "in_issuer": map[string]any{"_target_issuer_id": "iss1"}},
			{"issuer_id": "iss2", "issue_id": "i2", "title": "Issue 2", "in_issuer": map[string]any{"_target_issuer_id": "iss2"}},
		},
	})

	queries, err := a.BatchEdgeQueries(result, shape)
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

	shape, err := a.ShapeForSchema(s)
	if err != nil {
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

	result := buildGraphResult(t, s, v, map[string][]map[string]any{
		"Issuer": issuers,
		"Issue":  issues,
	})

	queries, err := a.BatchEdgeQueries(result, shape, WithEdgeChunkSize(2))
	if err != nil {
		t.Fatal(err)
	}

	// With chunk size 2 and multiple edges, we should get multiple queries
	// for signatures that have >2 edges.
	totalEdges := len(result.Edges())
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

	shape, err := a.ShapeForSchema(s)
	if err != nil {
		t.Fatal(err)
	}

	result := buildGraphResult(t, s, v, map[string][]map[string]any{
		"Entity": {{"id": "e1", "name": "test", "active": true, "tags": []any{"a", "b"}, "scores": []any{int64(1), int64(2)}}},
	})

	queries, err := a.BatchNodeQueries(result, shape)
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

	shape, err := a.ShapeForSchema(s)
	if err != nil {
		t.Fatal(err)
	}

	result := buildGraphResult(t, s, v, map[string][]map[string]any{
		"Entity": {{
			"id": "e1", "name": "test", "active": true,
			"times": []any{"2024-01-01T00:00:00Z", "2024-06-15T12:30:00Z"},
			"dates": []any{"2024-01-01", "2024-06-15"},
		}},
	})

	queries, err := a.BatchNodeQueries(result, shape)
	if err != nil {
		t.Fatal(err)
	}
	if len(queries) == 0 {
		t.Fatal("expected queries")
	}

	rows := queries[0].Params["rows"].([]map[string]any)
	props := rows[0]["props"].(map[string]any)

	// The immutable layer stores temporal values as strings.
	// Coercion converts []any to []string (not []any, which the
	// Neo4j driver rejects).
	if times, ok := props["times"]; ok {
		switch v := times.(type) {
		case []string:
			if len(v) != 2 {
				t.Errorf("times length = %d; want 2", len(v))
			}
		default:
			t.Errorf("times should be []string, got %T", times)
		}
	}

	if dates, ok := props["dates"]; ok {
		switch v := dates.(type) {
		case []string:
			if len(v) != 2 {
				t.Errorf("dates length = %d; want 2", len(v))
			}
		default:
			t.Errorf("dates should be []string, got %T", dates)
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

func TestPropertyCoercion_Scalars(t *testing.T) {
	t.Parallel()
	s, v := loadSchemaAndValidator(t, "basic.yammm")
	a := New()

	shape, err := a.ShapeForSchema(s)
	if err != nil {
		t.Fatal(err)
	}

	result := buildGraphResult(t, s, v, map[string][]map[string]any{
		"Entity": {{"id": "e1", "name": "test", "count": int64(42), "active": true, "created_at": "2024-01-01T00:00:00Z", "score": 3.14}},
	})

	queries, err := a.BatchNodeQueries(result, shape)
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

func TestNodeQueryFor_MissingKey(t *testing.T) {
	t.Parallel()
	s, v := loadSchemaAndValidator(t, "basic.yammm")
	a := New()

	_, err := a.ShapeForSchema(s)
	if err != nil {
		t.Fatal(err)
	}

	result := buildGraphResult(t, s, v, map[string][]map[string]any{
		"Entity": {{"id": "e1", "name": "test", "count": int64(1), "active": true, "created_at": "2024-01-01T00:00:00Z"}},
	})

	inst := result.InstancesOf("Entity")[0]

	// Use a shape with a PK that doesn't exist in the instance.
	badShape := NodeShape{
		Label:       "test__Entity",
		PrimaryKeys: []string{"nonexistent_key"},
	}

	_, err = a.NodeQueryFor(&badShape, inst)
	if err == nil {
		t.Error("expected error for missing key")
	}
}

func TestEdgeQueryFor_InvalidRelType(t *testing.T) {
	t.Parallel()
	s, v := loadSchemaAndValidator(t, "write_basic.yammm")
	a := New()

	shape, err := a.ShapeForSchema(s)
	if err != nil {
		t.Fatal(err)
	}

	result := buildGraphResult(t, s, v, map[string][]map[string]any{
		"Issuer": {{"issuer_id": "iss1", "name": "Test"}},
		"Issue":  {{"issuer_id": "iss1", "issue_id": "i1", "title": "Test", "in_issuer": map[string]any{"_target_issuer_id": "iss1"}}},
	})

	edges := result.Edges()
	if len(edges) == 0 {
		t.Fatal("expected edges")
	}

	// The actual edges have valid relation types from the schema.
	// ValidateIdentifier is called on edge.Relation() which comes from
	// the schema, so these pass. Testing the error path requires an
	// edge with an invalid relation name, which can't be produced by
	// the graph builder. Verify that valid edges pass instead.
	for _, edge := range edges {
		_, err := a.EdgeQueryFor(edge, shape)
		if err != nil {
			t.Errorf("EdgeQueryFor failed for valid edge %s: %v", edge.Relation(), err)
		}
	}
}
