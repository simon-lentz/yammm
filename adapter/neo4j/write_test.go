package neo4j

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/neo4j/neo4j-go-driver/v6/neo4j/dbtype"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// Compile-time interface assertions.
var (
	_ NodeSource = (*graph.Instance)(nil)
	_ NodeSource = (*instance.ValidInstance)(nil)
)

func TestNodeQueryFor_SinglePK(t *testing.T) {
	t.Parallel()
	a, s, v, shape := setupWrite(t, "basic.yammm")

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

	// The statement must be exactly the builder output for this shape — the
	// Cypher text itself is pinned by the builder tests; this test owns the
	// write-layer wiring (shape-derived label/keys, mutable-key mode).
	if want := BuildNodeMergeQuery(ns.Label, ns.PrimaryKeys, MutableKeys); q.Statement != want {
		t.Errorf("Statement = %q; want builder output %q", q.Statement, want)
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
	a, s, v, shape := setupWrite(t, "composite_pk.yammm")

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

	if want := BuildNodeMergeQuery(ns.Label, ns.PrimaryKeys, MutableKeys); q.Statement != want {
		t.Errorf("Statement = %q; want builder output %q", q.Statement, want)
	}
	if q.Params["key_schema_id"] != "s1" || q.Params["key_record_id"] != "r1" {
		t.Errorf("composite key params = %v; want key_schema_id=s1, key_record_id=r1", q.Params)
	}
}

func TestNodeQueryFor_ImmutableKeys(t *testing.T) {
	t.Parallel()
	a, s, v, shape := setupWrite(t, "basic.yammm")

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

	// WithImmutableKeys must select the immutable-key builder variant.
	if want := BuildNodeMergeQuery(ns.Label, ns.PrimaryKeys, ImmutableKeys); q.Statement != want {
		t.Errorf("Statement = %q; want builder output %q", q.Statement, want)
	}

	updateProps, ok := q.Params["update_props"].(map[string]any)
	if !ok {
		t.Fatal("update_props should be map[string]any")
	}
	if _, has := updateProps["created_at"]; has {
		t.Error("update_props should not contain immutable key 'created_at'")
	}
}

func TestNodeQueryFor_ImmutableKeyUnknown(t *testing.T) {
	t.Parallel()
	a, s, v, shape := setupWrite(t, "basic.yammm")

	graphResult := buildGraphResult(t, s, v, map[string][]map[string]any{
		"Entity": {{"id": "e1", "name": "test", "count": int64(1), "active": true, "created_at": "2024-01-01T00:00:00Z"}},
	})

	inst := graphResult.InstancesOf("Entity")[0]
	ns := shape.Types["Entity"]
	st, _ := s.Type("Entity")

	// A mistyped immutable key must fail loudly: honored silently, the real
	// property would stay in update_props and be rewritten on every re-MERGE,
	// defeating write-once. A valid key alongside it must not mask the error.
	_, err := a.NodeQueryFor(context.Background(), &ns, inst, st, WithImmutableKeys("created_at", "craeted_at"))
	if err == nil {
		t.Fatal("expected error for unknown immutable key")
	}
	if !strings.Contains(err.Error(), "craeted_at") {
		t.Errorf("error should name the unknown key: %v", err)
	}
	if !strings.Contains(err.Error(), "Entity") {
		t.Errorf("error should name the type: %v", err)
	}
}

func TestNodeQueryFor_ImmutableKeyInherited(t *testing.T) {
	t.Parallel()
	a, s, v, shape := setupWrite(t, "inheritance.yammm")

	graphResult := buildGraphResult(t, s, v, map[string][]map[string]any{
		"Entity": {{"id": "e1", "name": "test", "run_id": "r1", "source_fetched_at": "2024-01-01T00:00:00Z"}},
	})

	inst := graphResult.InstancesOf("Entity")[0]
	ns := shape.Types["Entity"]
	st, _ := s.Type("Entity")

	// An inherited property is a real property of the type and must validate.
	q, err := a.NodeQueryFor(context.Background(), &ns, inst, st, WithImmutableKeys("source_fetched_at"))
	if err != nil {
		t.Fatal(err)
	}
	updateProps, ok := q.Params["update_props"].(map[string]any)
	if !ok {
		t.Fatal("update_props should be map[string]any")
	}
	if _, has := updateProps["source_fetched_at"]; has {
		t.Error("update_props should not contain immutable key 'source_fetched_at'")
	}
}

func TestNodeQueryFor_ImmutableKeysNilSchemaType(t *testing.T) {
	t.Parallel()
	a, s, v, shape := setupWrite(t, "basic.yammm")

	graphResult := buildGraphResult(t, s, v, map[string][]map[string]any{
		"Entity": {{"id": "e1", "name": "test", "count": int64(1), "active": true, "created_at": "2024-01-01T00:00:00Z"}},
	})

	inst := graphResult.InstancesOf("Entity")[0]
	ns := shape.Types["Entity"]

	// With no schema type there is nothing to validate against; the key is
	// honored unvalidated, matching the nil pass-through coercion contract.
	q, err := a.NodeQueryFor(context.Background(), &ns, inst, nil, WithImmutableKeys("created_at"))
	if err != nil {
		t.Fatal(err)
	}
	updateProps, ok := q.Params["update_props"].(map[string]any)
	if !ok {
		t.Fatal("update_props should be map[string]any")
	}
	if _, has := updateProps["created_at"]; has {
		t.Error("update_props should not contain immutable key 'created_at'")
	}
}

func TestNodeQueryFor_DerivedImmutableKeys(t *testing.T) {
	t.Parallel()
	a, s, v, shape := setupWrite(t, "writeonce.yammm")

	graphResult := buildGraphResult(t, s, v, map[string][]map[string]any{
		"Entity": {{"id": "e1", "name": "n", "origin": "src", "first_seen": "2024-01-01T00:00:00Z"}},
	})

	inst := graphResult.InstancesOf("Entity")[0]
	ns := shape.Types["Entity"]
	st, _ := s.Type("Entity")

	// No WithImmutableKeys option: the @writeOnce annotations alone drive the
	// immutable-key split.
	q, err := a.NodeQueryFor(context.Background(), &ns, inst, st)
	if err != nil {
		t.Fatal(err)
	}
	if want := BuildNodeMergeQuery(ns.Label, ns.PrimaryKeys, ImmutableKeys); q.Statement != want {
		t.Errorf("Statement = %q; want immutable-key builder output %q", q.Statement, want)
	}
	updateProps, ok := q.Params["update_props"].(map[string]any)
	if !ok {
		t.Fatal("update_props should be map[string]any")
	}
	for _, k := range []string{"first_seen", "origin"} {
		if _, has := updateProps[k]; has {
			t.Errorf("update_props should not contain derived immutable key %q", k)
		}
	}
	if _, has := updateProps["name"]; !has {
		t.Error("update_props should retain the mutable 'name' property")
	}
}

func TestNodeQueryFor_DerivedUnionExplicit(t *testing.T) {
	t.Parallel()
	a, s, v, shape := setupWrite(t, "writeonce.yammm")

	graphResult := buildGraphResult(t, s, v, map[string][]map[string]any{
		"Entity": {{"id": "e1", "name": "n", "origin": "src", "first_seen": "2024-01-01T00:00:00Z"}},
	})

	inst := graphResult.InstancesOf("Entity")[0]
	ns := shape.Types["Entity"]
	st, _ := s.Type("Entity")

	// Explicit "name" unions with derived first_seen/origin: all three excluded.
	q, err := a.NodeQueryFor(context.Background(), &ns, inst, st, WithImmutableKeys("name"))
	if err != nil {
		t.Fatal(err)
	}
	updateProps, ok := q.Params["update_props"].(map[string]any)
	if !ok {
		t.Fatal("update_props should be map[string]any")
	}
	for _, k := range []string{"first_seen", "origin", "name"} {
		if _, has := updateProps[k]; has {
			t.Errorf("update_props should exclude union member %q", k)
		}
	}
}

func TestNodeQueryFor_UnannotatedTypeUnchanged(t *testing.T) {
	t.Parallel()
	a, s, v, shape := setupWrite(t, "writeonce.yammm")

	graphResult := buildGraphResult(t, s, v, map[string][]map[string]any{
		"Plain": {{"id": "p1", "name": "n"}},
	})

	inst := graphResult.InstancesOf("Plain")[0]
	ns := shape.Types["Plain"]
	st, _ := s.Type("Plain")

	// No annotations and no explicit keys → mutable shape, no update_props.
	q, err := a.NodeQueryFor(context.Background(), &ns, inst, st)
	if err != nil {
		t.Fatal(err)
	}
	if want := BuildNodeMergeQuery(ns.Label, ns.PrimaryKeys, MutableKeys); q.Statement != want {
		t.Errorf("Statement = %q; want mutable builder output %q", q.Statement, want)
	}
	if _, has := q.Params["update_props"]; has {
		t.Error("update_props should be absent for an unannotated type with no explicit keys")
	}
}

// A nil schemaType is the documented streaming call shape — a caller holding an
// instance but not the *schema.Type. It skips $props coercion and explicit-key
// validation, but it must NOT skip the @writeOnce guarantee: the immutable keys
// come from the NodeShape, which was built from the schema and is always in
// hand. A caller that got the mutable shape here would overwrite every
// @writeOnce property on each re-ingestion MERGE — destroying exactly the
// first-observation values the annotation exists to protect, with no error and
// no warning.
func TestNodeQueryFor_NilSchemaTypeStillHonorsWriteOnce(t *testing.T) {
	t.Parallel()
	a, s, v, shape := setupWrite(t, "writeonce.yammm")

	graphResult := buildGraphResult(t, s, v, map[string][]map[string]any{
		"Entity": {{"id": "e1", "name": "n", "origin": "src", "first_seen": "2024-01-01T00:00:00Z"}},
	})

	inst := graphResult.InstancesOf("Entity")[0]
	ns := shape.Types["Entity"]

	q, err := a.NodeQueryFor(context.Background(), &ns, inst, nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := BuildNodeMergeQuery(ns.Label, ns.PrimaryKeys, ImmutableKeys); q.Statement != want {
		t.Errorf("Statement = %q; want immutable split %q even with a nil schemaType", q.Statement, want)
	}
	updateProps, ok := q.Params["update_props"].(map[string]any)
	if !ok {
		t.Fatal("update_props should be present when the shape carries @writeOnce keys")
	}
	for _, k := range []string{"first_seen", "origin"} {
		if _, has := updateProps[k]; has {
			t.Errorf("update_props should exclude derived immutable key %q", k)
		}
	}
	if _, has := updateProps["name"]; !has {
		t.Error("update_props should retain the mutable 'name' property")
	}
}

// An unannotated type still gets the mutable shape through the same nil-type
// path, so the fix above turns on the schema's annotations rather than on the
// nil argument itself.
func TestNodeQueryFor_NilSchemaTypeUnannotatedStaysMutable(t *testing.T) {
	t.Parallel()
	a, s, v, shape := setupWrite(t, "writeonce.yammm")

	graphResult := buildGraphResult(t, s, v, map[string][]map[string]any{
		"Plain": {{"id": "p1", "name": "n"}},
	})

	inst := graphResult.InstancesOf("Plain")[0]
	ns := shape.Types["Plain"]

	q, err := a.NodeQueryFor(context.Background(), &ns, inst, nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := BuildNodeMergeQuery(ns.Label, ns.PrimaryKeys, MutableKeys); q.Statement != want {
		t.Errorf("Statement = %q; want mutable builder output %q", q.Statement, want)
	}
	if _, has := q.Params["update_props"]; has {
		t.Error("update_props should be absent for an unannotated type with no explicit keys")
	}
}

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

	entityNS := shape.Types["Entity"]
	if want := BuildBatchNodeMergeQuery(entityNS.Label, entityNS.PrimaryKeys, ImmutableKeys); entityQ.Statement != want {
		t.Errorf("Entity statement = %q; want immutable split %q", entityQ.Statement, want)
	}
	plainNS := shape.Types["Plain"]
	if want := BuildBatchNodeMergeQuery(plainNS.Label, plainNS.PrimaryKeys, MutableKeys); plainQ.Statement != want {
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

// TestBatchNodeQueries_ExplicitKeysStillSplitEveryType pins the back-compat
// contract: a non-empty explicit key list produces the split shape for every
// type, including an unannotated one — byte-for-byte today's behavior.
func TestBatchNodeQueries_ExplicitKeysStillSplitEveryType(t *testing.T) {
	t.Parallel()
	a, s, v, shape := setupWrite(t, "writeonce.yammm")

	graphResult := buildGraphResult(t, s, v, map[string][]map[string]any{
		"Entity": {{"id": "e1", "name": "n", "origin": "src", "first_seen": "2024-01-01T00:00:00Z"}},
		"Plain":  {{"id": "p1", "name": "n"}},
	})

	queries, err := a.BatchNodeQueries(context.Background(), graphResult, shape, WithImmutableKeys("name"))
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range queries {
		if !strings.Contains(q.Statement, "ON CREATE SET") {
			t.Errorf("an explicit key should force the split shape for every type: %q", q.Statement)
		}
	}
}

func TestBatchNodeQueries_ImmutableKeyUnknown(t *testing.T) {
	t.Parallel()
	a, s, v, shape := setupWrite(t, "basic.yammm")

	graphResult := buildGraphResult(t, s, v, map[string][]map[string]any{
		"Entity": {{"id": "e1", "name": "test", "count": int64(1), "active": true, "created_at": "2024-01-01T00:00:00Z"}},
	})

	_, err := a.BatchNodeQueries(context.Background(), graphResult, shape, WithImmutableKeys("frist_seen_at"))
	if err == nil {
		t.Fatal("expected error for unknown immutable key")
	}
	if !strings.Contains(err.Error(), "frist_seen_at") {
		t.Errorf("error should name the unknown key: %v", err)
	}
	if !strings.Contains(err.Error(), "Entity") {
		t.Errorf("error should name the written type(s): %v", err)
	}
}

func TestBatchNodeQueries_ImmutableKeyPartialTypeMatch(t *testing.T) {
	t.Parallel()
	a, s, v, shape := setupWrite(t, "multiple_types.yammm")

	graphResult := buildGraphResult(t, s, v, map[string][]map[string]any{
		"Widget": {{"id": "w1", "code": "c1"}},
		"Gadget": {{"uid": "u1", "sku": "s1", "optional_note": "n"}},
	})

	// optional_note is a property of Gadget but not Widget: real for at least
	// one written type, so the batch is accepted; removeKeys no-ops on the
	// Widget rows.
	queries, err := a.BatchNodeQueries(context.Background(), graphResult, shape, WithImmutableKeys("optional_note"))
	if err != nil {
		t.Fatal(err)
	}
	if len(queries) != 2 {
		t.Fatalf("expected 2 queries, got %d", len(queries))
	}

	// Regression on the valid-key behavior: rows carrying the property keep it
	// in props and exclude it from update_props.
	for _, q := range queries {
		rows, ok := q.Params["rows"].([]map[string]any)
		if !ok {
			t.Fatal("rows should be []map[string]any")
		}
		for _, row := range rows {
			props, ok := row["props"].(map[string]any)
			if !ok {
				t.Fatal("props should be map[string]any")
			}
			updateProps, ok := row["update_props"].(map[string]any)
			if !ok {
				t.Fatal("update_props should be map[string]any")
			}
			if _, has := props["optional_note"]; !has {
				continue
			}
			if _, still := updateProps["optional_note"]; still {
				t.Error("update_props should not contain immutable key 'optional_note'")
			}
		}
	}
}

func TestBatchNodeQueries_ImmutableKeyMatchesNoType(t *testing.T) {
	t.Parallel()
	a, s, v, shape := setupWrite(t, "multiple_types.yammm")

	graphResult := buildGraphResult(t, s, v, map[string][]map[string]any{
		"Widget": {{"id": "w1", "code": "c1"}},
		"Gadget": {{"uid": "u1", "sku": "s1"}},
	})

	_, err := a.BatchNodeQueries(context.Background(), graphResult, shape, WithImmutableKeys("optional_note", "does_not_exist"))
	if err == nil {
		t.Fatal("expected error for immutable key naming no written type's property")
	}
	if !strings.Contains(err.Error(), "does_not_exist") {
		t.Errorf("error should name the unknown key: %v", err)
	}
	if strings.Contains(err.Error(), "optional_note") {
		t.Errorf("error should not name the key that matched a written type: %v", err)
	}
	if !strings.Contains(err.Error(), "Widget") || !strings.Contains(err.Error(), "Gadget") {
		t.Errorf("error should name the written types: %v", err)
	}
}

func TestBatchNodeQueries_ImmutableKeysEmptySnapshot(t *testing.T) {
	t.Parallel()
	a, s, v, shape := setupWrite(t, "basic.yammm")

	graphResult := buildGraphResult(t, s, v, nil)

	// An empty snapshot generates no queries, so there is no write-once
	// invariant to defeat; a legitimately-empty pipeline run must not fail.
	queries, err := a.BatchNodeQueries(context.Background(), graphResult, shape, WithImmutableKeys("anything"))
	if err != nil {
		t.Fatal(err)
	}
	if len(queries) != 0 {
		t.Errorf("expected no queries for empty snapshot, got %d", len(queries))
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
	ns := shape.Types["Entity"]
	if want := BuildBatchNodeMergeQuery(ns.Label, ns.PrimaryKeys, MutableKeys); q.Statement != want {
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

func TestEdgeQueryFor_Basic(t *testing.T) {
	t.Parallel()
	a, s, v, shape := setupWrite(t, "write_basic.yammm")

	graphResult := buildGraphResult(t, s, v, map[string][]map[string]any{
		"Publisher": {{"publisher_id": "iss1", "name": "Test Publisher"}},
		"Book":      {{"publisher_id": "iss1", "book_id": "i1", "title": "Test Book", "by_publisher": map[string]any{"_target_publisher_id": "iss1"}}},
	})

	edges := graphResult.Edges()
	if len(edges) != 1 {
		t.Fatalf("expected exactly the BY_PUBLISHER edge, got %d edges", len(edges))
	}

	q, err := a.EdgeQueryFor(context.Background(), edges[0], shape)
	if err != nil {
		t.Fatal(err)
	}

	// Exactly the builder output for Book→BY_PUBLISHER→Publisher with no edge
	// properties (the fixture's relation declares none) — which also pins
	// that no SET r += clause is emitted for a propertyless edge.
	book, publisher := shape.Types["Book"], shape.Types["Publisher"]
	want := BuildRelationshipMergeQuery(
		book.Label, book.PrimaryKeys,
		"BY_PUBLISHER",
		publisher.Label, publisher.PrimaryKeys,
		false,
	)
	if q.Statement != want {
		t.Errorf("Statement = %q; want builder output %q", q.Statement, want)
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

func TestNodeQueryFor_MissingKey(t *testing.T) {
	t.Parallel()
	a, s, v, _ := setupWrite(t, "basic.yammm")

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
		"Publisher": {{"publisher_id": "iss1", "name": "Test"}},
		"Book":      {{"publisher_id": "iss1", "book_id": "i1", "title": "Test", "by_publisher": map[string]any{"_target_publisher_id": "iss1"}}},
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
		"Publisher": {{"publisher_id": "iss1", "name": "Test"}},
		"Book":      {{"publisher_id": "iss1", "book_id": "i1", "title": "Test", "by_publisher": map[string]any{"_target_publisher_id": "iss1"}}},
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

func TestEdgeQueriesFor_CoercesTypedRelProps(t *testing.T) {
	t.Parallel()
	a, s, v, shapes := setupWrite(t, "edge_typed_props.yammm")

	valid := validateInstance(t, v, "Source", map[string]any{
		"source_id": "s1",
		"linked_at": map[string]any{
			"_target_target_id": "t1",
			"observed_at":       "2024-01-01T00:00:00Z",
			"weight":            int64(5),
		},
	})

	st, _ := s.Type("Source")
	queries, err := a.EdgeQueriesFor(context.Background(), valid, st, shapes)
	if err != nil {
		t.Fatal(err)
	}
	if len(queries) != 1 {
		t.Fatalf("got %d queries; want 1", len(queries))
	}
	relProps, ok := queries[0].Params["rel_props"].(map[string]any)
	if !ok {
		t.Fatalf("rel_props missing or wrong type: %T", queries[0].Params["rel_props"])
	}
	if _, isTime := relProps["observed_at"].(time.Time); !isTime {
		t.Errorf("observed_at should be coerced to time.Time, got %T", relProps["observed_at"])
	}
	if _, isFloat := relProps["weight"].(float64); !isFloat {
		t.Errorf("weight should be coerced to float64, got %T", relProps["weight"])
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

func TestEdgeQueryFor_InvalidRelType(t *testing.T) {
	t.Parallel()
	// A relation named with a Cypher reserved word produces edges whose
	// relation type fails ValidateIdentifier. Both edge entry points must
	// refuse to emit Cypher for it rather than hand the driver a statement
	// with a bare reserved word in the MERGE.
	s, result := schema.NewBuilder().
		WithName("badrel").
		AddType("Target").WithPrimaryKey("id", schema.NewStringConstraint()).Done().
		AddType("Source").WithPrimaryKey("id", schema.NewStringConstraint()).
		WithRelation("MATCH", schema.NewTypeRef("", "Target", location.Span{}), false, false).Done().
		Build()
	if err := result.Err(); err != nil {
		t.Fatalf("build schema: %v", err)
	}

	a := New()
	shape, shapeResult := a.ShapeForSchema(context.Background(), s)
	if err := shapeResult.Err(); err != nil {
		t.Fatal(err)
	}
	v := instance.NewValidator(s)

	graphResult := buildGraphResult(t, s, v, map[string][]map[string]any{
		"Target": {{"id": "t1"}},
		"Source": {{"id": "s1", "match": map[string]any{"_target_id": "t1"}}},
	})

	edges := graphResult.Edges()
	if len(edges) != 1 {
		t.Fatalf("expected the MATCH edge, got %d edges", len(edges))
	}

	if _, err := a.EdgeQueryFor(context.Background(), edges[0], shape); !errors.Is(err, ErrReservedKeyword) {
		t.Errorf("EdgeQueryFor error = %v; want ErrReservedKeyword", err)
	}
	if _, err := a.BatchEdgeQueries(context.Background(), graphResult, shape); !errors.Is(err, ErrReservedKeyword) {
		t.Errorf("BatchEdgeQueries error = %v; want ErrReservedKeyword", err)
	}
}

// validateInstance validates a single instance without building a graph.
func validateInstance(t *testing.T, v *instance.Validator, typeName string, props map[string]any) *instance.ValidInstance {
	t.Helper()
	valid, result := v.ValidateOne(context.Background(), typeName, instance.RawInstance{Properties: props})
	if !result.OK() {
		t.Fatalf("validate %s: %v", typeName, result)
	}
	return valid
}

func TestEdgeQueriesFor_Basic(t *testing.T) {
	t.Parallel()
	a, s, v, shapes := setupWrite(t, "write_basic.yammm")

	// Create a Book that references a Publisher via BY_PUBLISHER.
	valid := validateInstance(t, v, "Book", map[string]any{
		"publisher_id": "iss1",
		"book_id":      "book1",
		"title":        "Test Book",
		"by_publisher": map[string]any{"_target_publisher_id": "iss1"},
	})

	st, _ := s.Type("Book")
	queries, err := a.EdgeQueriesFor(context.Background(), valid, st, shapes)
	if err != nil {
		t.Fatal(err)
	}

	if len(queries) != 1 {
		t.Fatalf("got %d queries; want 1", len(queries))
	}

	q := queries[0]
	book, publisher := shapes.Types["Book"], shapes.Types["Publisher"]
	want := BuildRelationshipMergeQuery(
		book.Label, book.PrimaryKeys,
		"BY_PUBLISHER",
		publisher.Label, publisher.PrimaryKeys,
		false,
	)
	if q.Statement != want {
		t.Errorf("Statement = %q; want builder output %q", q.Statement, want)
	}
	// Source keys use from_key_ prefix.
	if q.Params["from_key_publisher_id"] != "iss1" || q.Params["from_key_book_id"] != "book1" {
		t.Errorf("unexpected source key params: %v", q.Params)
	}
	// Target key uses to_key_ prefix.
	if q.Params["to_key_publisher_id"] != "iss1" {
		t.Errorf("unexpected target key params: %v", q.Params)
	}
}

func TestEdgeQueriesFor_MultiTarget(t *testing.T) {
	t.Parallel()
	a, s, v, shapes := setupWrite(t, "write_basic.yammm")

	// Publisher with PUBLISHES pointing to multiple Books.
	valid := validateInstance(t, v, "Publisher", map[string]any{
		"publisher_id": "iss1",
		"name":         "Test Publisher",
		"publishes": []any{
			map[string]any{"_target_publisher_id": "iss1", "_target_book_id": "book1"},
			map[string]any{"_target_publisher_id": "iss1", "_target_book_id": "book2"},
		},
	})

	st, _ := s.Type("Publisher")
	queries, err := a.EdgeQueriesFor(context.Background(), valid, st, shapes)
	if err != nil {
		t.Fatal(err)
	}

	if len(queries) != 2 {
		t.Fatalf("got %d queries; want 2", len(queries))
	}
	publisher, book := shapes.Types["Publisher"], shapes.Types["Book"]
	want := BuildRelationshipMergeQuery(
		publisher.Label, publisher.PrimaryKeys,
		"PUBLISHES",
		book.Label, book.PrimaryKeys,
		false,
	)
	for i, q := range queries {
		if q.Statement != want {
			t.Errorf("query %d Statement = %q; want builder output %q", i, q.Statement, want)
		}
	}
}

func TestEdgeQueriesFor_EdgeProperties(t *testing.T) {
	t.Parallel()
	a, s, v, shapes := setupWrite(t, "edge_mixed.yammm")

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
	employee, company := shapes.Types["Employee"], shapes.Types["Company"]
	// hasProps=true selects the SET r += $rel_props variant.
	want := BuildRelationshipMergeQuery(
		employee.Label, employee.PrimaryKeys,
		"WORKS_AT",
		company.Label, company.PrimaryKeys,
		true,
	)
	if q.Statement != want {
		t.Errorf("Statement = %q; want builder output %q", q.Statement, want)
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
	a, s, v, shapes := setupWrite(t, "basic.yammm")

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

	valid := validateInstance(t, v, "Book", map[string]any{
		"publisher_id": "iss1",
		"book_id":      "book1",
		"title":        "Test",
		"by_publisher": map[string]any{"_target_publisher_id": "iss1"},
	})

	// Provide shapes with only Book (no Publisher shape).
	incompleteShapes := &GraphShape{
		Types: map[string]NodeShape{
			"Book": {
				Type:        "Book",
				Label:       "write_test__Book",
				PrimaryKeys: []string{"publisher_id", "book_id"},
			},
		},
	}

	st, _ := s.Type("Book")
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
	a, s, v, shape := setupWrite(t, "basic.yammm")

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

	if want := BuildNodeMergeQuery(ns.Label, ns.PrimaryKeys, MutableKeys); q.Statement != want {
		t.Errorf("Statement = %q; want builder output %q", q.Statement, want)
	}
	if q.Params["key_id"] != "e1" {
		t.Errorf("key_id = %v; want e1", q.Params["key_id"])
	}
}

func TestExtractKeyFromImmutableKey(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		components []any
		keyNames   []string
		want       map[string]any
		wantErr    string // substring of the expected error; empty = success
	}{
		{
			name:       "single component",
			components: []any{"val1"},
			keyNames:   []string{"id"},
			want:       map[string]any{"id": "val1"},
		},
		{
			name:       "composite",
			components: []any{"s1", "r1"},
			keyNames:   []string{"schema_id", "record_id"},
			want:       map[string]any{"schema_id": "s1", "record_id": "r1"},
		},
		{
			name:       "length mismatch",
			components: []any{"val1"},
			keyNames:   []string{"a", "b"},
			wantErr:    "1 components but 2 key names",
		},
		{
			name:       "no key names",
			components: []any{"val1"},
			keyNames:   nil,
			wantErr:    "no",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := extractKeyFromImmutableKey(
				immutable.WrapKey(tc.components), &NodeShape{PrimaryKeys: tc.keyNames},
			)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("want error containing %q, got nil", tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("error = %v; want substring %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			for k, want := range tc.want {
				if got[k] != want {
					t.Errorf("%s = %v; want %v", k, got[k], want)
				}
			}
		})
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

	if _, ok := ParamTypesForMergeKeys(dt, "rows")["rows.key_created_on"]; !ok {
		t.Error("no rows.key_created_on entry; the batch template matches on row.key_created_on")
	}
	if _, ok := ParamTypesForMergeKeys(dt, "")["key_created_on"]; !ok {
		t.Error("no key_created_on entry; the single-node template binds $key_created_on")
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
	plain := shape.Types["Plain"]
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
	stale := &GraphShape{Types: map[string]NodeShape{}}
	for name, ns := range shape.Types {
		ns.ImmutableKeys = nil
		stale.Types[name] = ns
	}

	queries, err := a.BatchNodeQueries(context.Background(), graphResult, stale)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range queries {
		if !strings.Contains(q.Statement, "wo_test__Entity") {
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

	ns := shape.Types["Plain"]
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

// A MERGE key must reach the driver as the same Neo4j-native type $props stores
// the property as. A Date primary key bound as a string matches no node whose
// property is a DATE, so each write inserts another duplicate.
func TestNodeQueryFor_CoercesMergeKey(t *testing.T) {
	t.Parallel()
	a, s, v, shape := setupWrite(t, "temporal_pk.yammm")

	valid := validateInstance(t, v, "Doc", map[string]any{
		"created_on": "2024-01-02",
		"updated_on": "2024-03-04",
		"title":      "t",
	})

	ns := shape.Types["Doc"]
	st, _ := s.Type("Doc")
	q, err := a.NodeQueryFor(context.Background(), &ns, valid, st)
	if err != nil {
		t.Fatal(err)
	}

	key := q.Params[batchKeyParamPrefix+"created_on"]
	if _, isDate := key.(dbtype.Date); !isDate {
		t.Errorf("$%screated_on = %#v (%T); want dbtype.Date", batchKeyParamPrefix, key, key)
	}
	// Control: the property the MERGE key is drawn from carries the same type,
	// which is the invariant — the two maps must agree.
	props := q.Params["props"].(map[string]any)
	if props["created_on"] != key {
		t.Errorf("key %#v and property %#v disagree; the MERGE matches nothing",
			key, props["created_on"])
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
		if !strings.Contains(q.Statement, shape.Types["Doc"].Label) {
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

// An endpoint key is MATCHed against nodes the node path already wrote, so it
// must be coerced the same way. Bound raw it matches nothing, the MERGE never
// executes, and matched_rows is a legitimate-looking 0 with no error.
func TestEdgeQueriesFor_CoercesEndpointKeys(t *testing.T) {
	t.Parallel()
	a, s, v, shapes := setupWrite(t, "temporal_pk.yammm")

	valid := validateInstance(t, v, "Citation", map[string]any{
		"citation_id": "c1",
		"cites":       map[string]any{"_target_created_on": "2024-01-02"},
	})

	st, _ := s.Type("Citation")
	queries, err := a.EdgeQueriesFor(context.Background(), valid, st, shapes)
	if err != nil {
		t.Fatal(err)
	}
	if len(queries) != 1 {
		t.Fatalf("got %d queries; want 1", len(queries))
	}

	to := queries[0].Params[relToKeyParamPrefix+"created_on"]
	if _, isDate := to.(dbtype.Date); !isDate {
		t.Errorf("$%screated_on = %#v (%T); want dbtype.Date", relToKeyParamPrefix, to, to)
	}
	// Control: the source key is a String, which coerces to itself — so the
	// assertion above turns on the Date endpoint, not on coercion running at all.
	from := queries[0].Params[relFromKeyParamPrefix+"citation_id"]
	if from != "c1" {
		t.Errorf("$%scitation_id = %#v; want the String key unchanged", relFromKeyParamPrefix, from)
	}
}

// Both edge paths that hold a resolved graph must coerce both endpoints.
func TestEdgeQueries_CoerceEndpointKeysFromAGraph(t *testing.T) {
	t.Parallel()
	a, s, v, shapes := setupWrite(t, "temporal_pk.yammm")

	graphResult := buildGraphResult(t, s, v, map[string][]map[string]any{
		"Doc":      {{"created_on": "2024-01-02", "updated_on": "2024-03-04", "title": "t"}},
		"Citation": {{"citation_id": "c1", "cites": map[string]any{"_target_created_on": "2024-01-02"}}},
	})

	edges := graphResult.Edges()
	if len(edges) != 1 {
		t.Fatalf("got %d edges; want 1", len(edges))
	}

	single, err := a.EdgeQueryFor(context.Background(), edges[0], shapes)
	if err != nil {
		t.Fatal(err)
	}
	if to := single.Params[relToKeyParamPrefix+"created_on"]; !isDateValue(to) {
		t.Errorf("EdgeQueryFor $%screated_on = %#v (%T); want dbtype.Date",
			relToKeyParamPrefix, to, to)
	}

	batches, err := a.BatchEdgeQueries(context.Background(), graphResult, shapes)
	if err != nil {
		t.Fatal(err)
	}
	if len(batches) != 1 {
		t.Fatalf("got %d batch queries; want 1", len(batches))
	}
	rows := batches[0].Params["rows"].([]map[string]any)
	if to := rows[0][relToRowPrefix+"created_on"]; !isDateValue(to) {
		t.Errorf("BatchEdgeQueries row.%screated_on = %#v (%T); want dbtype.Date",
			relToRowPrefix, to, to)
	}
}

func isDateValue(v any) bool {
	_, ok := v.(dbtype.Date)
	return ok
}

// A shape carrying no key constraints — hand-built, or restored from a
// serialization that cannot hold one — passes keys through rather than failing,
// which is the behaviour of a shape that never declared their types.
func TestNodeQueryFor_ShapeWithoutKeyConstraintsPassesKeysThrough(t *testing.T) {
	t.Parallel()
	a, s, v, shape := setupWrite(t, "temporal_pk.yammm")

	valid := validateInstance(t, v, "Doc", map[string]any{
		"created_on": "2024-01-02",
		"updated_on": "2024-03-04",
		"title":      "t",
	})

	handBuilt := shape.Types["Doc"]
	handBuilt.keyConstraints = nil
	st, _ := s.Type("Doc")
	q, err := a.NodeQueryFor(context.Background(), &handBuilt, valid, st)
	if err != nil {
		t.Fatal(err)
	}
	if got := q.Params[batchKeyParamPrefix+"created_on"]; got != "2024-01-02" {
		t.Errorf("$%screated_on = %#v; want the raw value passed through",
			batchKeyParamPrefix, got)
	}
}
