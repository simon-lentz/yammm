package snapshot_test

import (
	"context"
	"testing"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/snapshot"
)

// composedPKCollision builds a snapshot holding a composed-child primary-key
// collision: two Child instances with the same key under one Parent's CHILDREN
// relation. The second is rejected and recorded as a duplicate.
func composedPKCollision(t *testing.T) *graph.Snapshot {
	t.Helper()
	ctx := context.Background()
	s := testSchemaWithComposition(t)

	g := graph.New(s)
	if res := g.Add(ctx, mustValidInstance(t, s, "Parent", []any{"p1"}, map[string]any{"name": "Root"})); res.HasErrors() {
		t.Fatalf("Add parent: %v", res)
	}
	first := mustValidInstance(t, s, "Child", []any{"c1"}, map[string]any{"value": "first"})
	if res := g.AddComposed(ctx, "Parent", graph.FormatKey("p1"), "CHILDREN", first); res.HasErrors() {
		t.Fatalf("AddComposed first child: %v", res)
	}
	second := mustValidInstance(t, s, "Child", []any{"c1"}, map[string]any{"value": "second"})
	if res := g.AddComposed(ctx, "Parent", graph.FormatKey("p1"), "CHILDREN", second); !res.HasErrors() {
		t.Fatal("AddComposed with a colliding child key must report a duplicate")
	}

	snap := g.Snapshot()
	if len(snap.Duplicates()) != 1 {
		t.Fatalf("snapshot holds %d duplicates, want 1", len(snap.Duplicates()))
	}
	return snap
}

// TestRoundTrip_ComposedChildDuplicate pins that a snapshot carrying a
// composed-child duplicate survives Marshal and Load. Composed children never
// appear in the instances section, so a duplicate naming one has no entry for
// the reader's existence check to find — and rejecting there loses the whole
// graph, not only the duplicate record.
func TestRoundTrip_ComposedChildDuplicate(t *testing.T) {
	ctx := context.Background()
	s := testSchemaWithComposition(t)
	snap := composedPKCollision(t)

	data, res := snapshot.Marshal(ctx, snap)
	if res.HasErrors() {
		t.Fatalf("Marshal: %v", res)
	}

	loaded, loadRes := snapshot.Load(ctx, data, s)
	if loadRes.HasErrors() {
		t.Fatalf("Load rejected a document Marshal produced: %v", loadRes)
	}
	if got := len(loaded.Duplicates()); got != 1 {
		t.Fatalf("loaded snapshot holds %d duplicates, want 1", got)
	}

	dup := loaded.Duplicates()[0]
	if dup.Conflict == nil {
		t.Fatal("duplicate conflict pointer did not resolve")
	}
	if got := dup.Conflict.PrimaryKey().String(); got != graph.FormatKey("c1") {
		t.Errorf("conflict primary key = %s, want %s", got, graph.FormatKey("c1"))
	}
	v, ok := dup.Conflict.Property("value")
	if !ok {
		t.Fatal("conflict instance has no value property")
	}
	if got, _ := v.String(); got != "first" {
		t.Errorf("conflict resolved to the wrong child: value = %q, want %q", got, "first")
	}
}

// TestRoundTrip_ComposedChildDuplicateAmongSiblings pins conflict resolution
// in a slot holding more than one child: the loaded conflict must be the
// sibling whose key collided, which slot addressing alone cannot select.
func TestRoundTrip_ComposedChildDuplicateAmongSiblings(t *testing.T) {
	ctx := context.Background()
	s := testSchemaWithComposition(t)

	g := graph.New(s)
	if res := g.Add(ctx, mustValidInstance(t, s, "Parent", []any{"p1"}, map[string]any{"name": "Root"})); res.HasErrors() {
		t.Fatalf("Add parent: %v", res)
	}
	for key, value := range map[string]string{"c1": "first", "c2": "second"} {
		child := mustValidInstance(t, s, "Child", []any{key}, map[string]any{"value": value})
		if res := g.AddComposed(ctx, "Parent", graph.FormatKey("p1"), "CHILDREN", child); res.HasErrors() {
			t.Fatalf("AddComposed %s: %v", key, res)
		}
	}
	colliding := mustValidInstance(t, s, "Child", []any{"c2"}, map[string]any{"value": "third"})
	if res := g.AddComposed(ctx, "Parent", graph.FormatKey("p1"), "CHILDREN", colliding); !res.HasErrors() {
		t.Fatal("AddComposed with a colliding child key must report a duplicate")
	}

	data, res := snapshot.Marshal(ctx, g.Snapshot())
	if res.HasErrors() {
		t.Fatalf("Marshal: %v", res)
	}
	loaded, loadRes := snapshot.Load(ctx, data, s)
	if loadRes.HasErrors() {
		t.Fatalf("Load rejected a document Marshal produced: %v", loadRes)
	}

	dups := loaded.Duplicates()
	if len(dups) != 1 {
		t.Fatalf("loaded snapshot holds %d duplicates, want 1", len(dups))
	}
	conflict := dups[0].Conflict
	if conflict == nil {
		t.Fatal("duplicate conflict pointer did not resolve")
	}
	if got, want := conflict.PrimaryKey().String(), graph.FormatKey("c2"); got != want {
		t.Errorf("conflict primary key = %s, want %s", got, want)
	}
	v, ok := conflict.Property("value")
	if !ok {
		t.Fatal("conflict instance has no value property")
	}
	if got, _ := v.String(); got != "second" {
		t.Errorf("conflict resolved to the wrong sibling: value = %q, want %q", got, "second")
	}
}

// TestVerify_ComposedChildDuplicate pins the same document against Verify,
// which runs the identical existence check on a path that materializes nothing.
func TestVerify_ComposedChildDuplicate(t *testing.T) {
	ctx := context.Background()
	s := testSchemaWithComposition(t)

	data, res := snapshot.Marshal(ctx, composedPKCollision(t))
	if res.HasErrors() {
		t.Fatalf("Marshal: %v", res)
	}
	if verRes := snapshot.Verify(ctx, data, s); verRes.HasErrors() {
		t.Errorf("Verify rejected a document Marshal produced: %v", verRes)
	}
}

// TestSnapshot_ComposedDuplicateCarriesParent pins that the composing
// coordinates reach the snapshot, which is what lets the conflict resolve
// through a relation rather than the instance index.
func TestSnapshot_ComposedDuplicateCarriesParent(t *testing.T) {
	snap := composedPKCollision(t)
	dup := snap.Duplicates()[0]

	if dup.Parent == nil {
		t.Fatal("composed-child duplicate carries no parent")
	}
	if got := dup.Parent.PrimaryKey().String(); got != graph.FormatKey("p1") {
		t.Errorf("parent primary key = %s, want %s", got, graph.FormatKey("p1"))
	}
	if dup.Relation != "CHILDREN" {
		t.Errorf("relation = %q, want %q", dup.Relation, "CHILDREN")
	}
}
