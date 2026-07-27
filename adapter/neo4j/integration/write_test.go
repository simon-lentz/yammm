//go:build neo4j_integration

package integration

import (
	"context"
	"testing"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v6/neo4j"
	n4j "github.com/simon-lentz/yammm/adapter/neo4j"
)

// execWithParams runs a parameterised statement, which the write tests need and
// the DDL helpers do not.
func execWithParams(t *testing.T, ctx context.Context, cypher string, params map[string]any) {
	t.Helper()
	if _, err := neo4jdriver.ExecuteQuery(ctx, driver(t), cypher, params,
		neo4jdriver.EagerResultTransformer); err != nil {
		t.Fatalf("executing %q: %v", cypher, err)
	}
}

func countNodes(t *testing.T, ctx context.Context, label string) int64 {
	t.Helper()
	rec := single(t, ctx, "MATCH (n:"+label+") RETURN count(n) AS n")
	n, ok := rec["n"].(int64)
	if !ok {
		t.Fatalf("count(n) is %T, want int64", rec["n"])
	}
	return n
}

// The batch template must MERGE on the row's key entry. This is the whole point
// of namespacing merge keys as key_<name>: the template and the row assembler
// have to agree, and if they do not the MERGE matches nothing and every
// re-ingestion inserts a duplicate instead of updating in place. Only a real
// MERGE can tell those apart — a string comparison against the template cannot.
func TestBatchMerge_KeyNamespaceMatchesTemplate(t *testing.T) {
	ctx := context.Background()
	driver(t)
	dropAll(t, ctx)
	t.Cleanup(func() {
		run(t, ctx, "MATCH (n:wr__Thing) DETACH DELETE n")
		dropAll(t, ctx)
	})
	run(t, ctx, "MATCH (n:wr__Thing) DETACH DELETE n")

	const label = "wr__Thing"
	stmt := n4j.BuildBatchNodeMergeQuery(label, []string{"thing_id"}, n4j.MutableKeys)

	row := func(title string) map[string]any {
		return map[string]any{
			// The documented row shape: merge keys under the key_ prefix.
			"key_thing_id": "t1",
			"props":        map[string]any{"thing_id": "t1", "title": title},
		}
	}

	execWithParams(t, ctx, stmt, map[string]any{"rows": []map[string]any{row("first")}})
	if n := countNodes(t, ctx, label); n != 1 {
		t.Fatalf("after the first batch there are %d nodes, want 1", n)
	}

	// A second batch with the same key must UPDATE, not insert.
	execWithParams(t, ctx, stmt, map[string]any{"rows": []map[string]any{row("second")}})
	if n := countNodes(t, ctx, label); n != 1 {
		t.Errorf("after re-merging the same key there are %d nodes, want 1 — the MERGE did not match", n)
	}
	rec := single(t, ctx, "MATCH (n:"+label+") RETURN n.title AS title")
	if title, _ := rec["title"].(string); title != "second" {
		t.Errorf("title = %q after re-merge, want \"second\"", title)
	}
}

// The immutable split must actually preserve values on MATCH. The adapter's own
// tests assert the emitted Cypher's shape; only the server can confirm the shape
// has the effect the whole @writeOnce feature is for.
func TestBatchMerge_WriteOncePreservedOnMatch(t *testing.T) {
	ctx := context.Background()
	driver(t)
	dropAll(t, ctx)
	t.Cleanup(func() {
		run(t, ctx, "MATCH (n:wr__Audited) DETACH DELETE n")
		dropAll(t, ctx)
	})
	run(t, ctx, "MATCH (n:wr__Audited) DETACH DELETE n")

	const label = "wr__Audited"
	stmt := n4j.BuildBatchNodeMergeQuery(label, []string{"aid"}, n4j.ImmutableKeys)

	// first_seen is the immutable key: present in props (used ON CREATE) and
	// absent from update_props (used ON MATCH).
	row := func(firstSeen, title string) map[string]any {
		return map[string]any{
			"key_aid":      "a1",
			"props":        map[string]any{"aid": "a1", "first_seen": firstSeen, "title": title},
			"update_props": map[string]any{"aid": "a1", "title": title},
		}
	}

	execWithParams(t, ctx, stmt, map[string]any{"rows": []map[string]any{row("2024-01-01", "first")}})
	execWithParams(t, ctx, stmt, map[string]any{"rows": []map[string]any{row("2025-06-30", "second")}})

	if n := countNodes(t, ctx, label); n != 1 {
		t.Fatalf("there are %d nodes, want 1", n)
	}
	rec := single(t, ctx, "MATCH (n:"+label+") RETURN n.first_seen AS first_seen, n.title AS title")
	if got, _ := rec["first_seen"].(string); got != "2024-01-01" {
		t.Errorf("first_seen = %q after re-merge; the write-once value was overwritten", got)
	}
	if got, _ := rec["title"].(string); got != "second" {
		t.Errorf("title = %q after re-merge, want \"second\" — the mutable property should update", got)
	}
}

// The single-node template's parameter namespace must match what NodeQueryFor
// builds, the same agreement the batch path needs.
func TestSingleNodeMerge_KeyParamNamespace(t *testing.T) {
	ctx := context.Background()
	driver(t)
	dropAll(t, ctx)
	t.Cleanup(func() {
		run(t, ctx, "MATCH (n:wr__Single) DETACH DELETE n")
		dropAll(t, ctx)
	})
	run(t, ctx, "MATCH (n:wr__Single) DETACH DELETE n")

	const label = "wr__Single"
	stmt := n4j.BuildNodeMergeQuery(label, []string{"sid"}, n4j.MutableKeys)

	for _, title := range []string{"first", "second"} {
		execWithParams(t, ctx, stmt, map[string]any{
			"key_sid": "s1",
			"props":   map[string]any{"sid": "s1", "title": title},
		})
	}
	if n := countNodes(t, ctx, label); n != 1 {
		t.Errorf("there are %d nodes after two merges on one key, want 1", n)
	}
}

// A primary key literally named update_props is a legal DSL property name, and
// the key_ namespace is what stops it colliding with the row's own update_props
// map. Sharing one namespace, the property map would take the key's place and
// every MERGE in the batch would match on a map instead of a key — which the
// server rejects outright, so this is the case that proves the namespace is
// load-bearing rather than tidy.
func TestBatchMerge_KeyNamedUpdateProps(t *testing.T) {
	ctx := context.Background()
	driver(t)
	dropAll(t, ctx)
	t.Cleanup(func() {
		run(t, ctx, "MATCH (n:wr__Collide) DETACH DELETE n")
		dropAll(t, ctx)
	})
	run(t, ctx, "MATCH (n:wr__Collide) DETACH DELETE n")

	const label = "wr__Collide"
	stmt := n4j.BuildBatchNodeMergeQuery(label, []string{"update_props"}, n4j.ImmutableKeys)

	row := func(title string) map[string]any {
		return map[string]any{
			"key_update_props": "u1",
			"props":            map[string]any{"update_props": "u1", "title": title},
			"update_props":     map[string]any{"update_props": "u1", "title": title},
		}
	}

	execWithParams(t, ctx, stmt, map[string]any{"rows": []map[string]any{row("first")}})
	execWithParams(t, ctx, stmt, map[string]any{"rows": []map[string]any{row("second")}})

	if n := countNodes(t, ctx, label); n != 1 {
		t.Errorf("there are %d nodes, want 1 — a key named update_props collided with the row's property map", n)
	}
}
