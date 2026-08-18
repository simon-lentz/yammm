package graph_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot/snapshottest"
)

// importTestSchema creates a schema with Person (→EMPLOYER→ Company) and Company.
func importTestSchema(t *testing.T) *schema.Schema {
	t.Helper()
	s, result := schema.NewBuilder().
		WithName("import_test").
		WithSourceID(location.MustNewSourceID("test://import.yammm")).
		AddType("Person").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithProperty("name", schema.NewStringConstraint()).
		WithRelation("EMPLOYER", schema.NewTypeRef("", "Company", location.Span{}), true, false).
		Done().
		AddType("Company").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithProperty("title", schema.NewStringConstraint()).
		Done().
		Build()
	if result.HasErrors() {
		t.Fatalf("importTestSchema: %s", result)
	}
	return s
}

// importTestSchemaRequired creates a schema with required EMPLOYER relation.
func importTestSchemaRequired(t *testing.T) *schema.Schema {
	t.Helper()
	s, result := schema.NewBuilder().
		WithName("import_test_req").
		WithSourceID(location.MustNewSourceID("test://import_req.yammm")).
		AddType("Person").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithProperty("name", schema.NewStringConstraint()).
		WithRelation("EMPLOYER", schema.NewTypeRef("", "Company", location.Span{}), false, false).
		Done().
		AddType("Company").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithProperty("title", schema.NewStringConstraint()).
		Done().
		Build()
	if result.HasErrors() {
		t.Fatalf("importTestSchemaRequired: %s", result)
	}
	return s
}

func buildSnapshot(t *testing.T, s *schema.Schema, instances ...*instance.ValidInstance) *graph.Snapshot {
	t.Helper()
	return snapshottest.BuildSnapshot(t, s, instances...)
}

func TestNewFromSnapshot_NilPanics(t *testing.T) {
	t.Parallel()
	s := importTestSchema(t)
	snap := buildSnapshot(t, s)

	t.Run("nil_schema", func(t *testing.T) {
		t.Parallel()
		defer func() {
			if r := recover(); r == nil {
				t.Error("expected panic for nil schema")
			}
		}()
		graph.NewFromSnapshot(nil, snap)
	})

	t.Run("nil_snapshot", func(t *testing.T) {
		t.Parallel()
		defer func() {
			if r := recover(); r == nil {
				t.Error("expected panic for nil snapshot")
			}
		}()
		graph.NewFromSnapshot(s, nil)
	})
}

func TestNewFromSnapshot_EmptySnapshot(t *testing.T) {
	t.Parallel()
	s := importTestSchema(t)
	snap := buildSnapshot(t, s)

	g := graph.NewFromSnapshot(s, snap)
	ctx := context.Background()

	// Add a new instance to the imported graph.
	company := mustValidInstance(t, s, "Company", []any{"c1"}, map[string]any{"id": "c1", "title": "Acme"})
	result := g.Add(ctx, company)
	assert.True(t, result.OK())

	reSnap := g.Snapshot()
	assert.Len(t, reSnap.InstancesOf(mustTypeID(t, s, "Company")), 1)
}

func TestNewFromSnapshot_InstancesIndexed(t *testing.T) {
	t.Parallel()
	s := importTestSchema(t)
	company := mustValidInstance(t, s, "Company", []any{"c1"}, map[string]any{"id": "c1", "title": "Acme"})
	person := mustValidInstance(t, s, "Person", []any{"p1"}, map[string]any{"id": "p1", "name": "Alice"})
	snap := buildSnapshot(t, s, company, person)

	g := graph.NewFromSnapshot(s, snap)
	reSnap := g.Snapshot()

	assert.Len(t, reSnap.InstancesOf(mustTypeID(t, s, "Company")), 1)
	assert.Len(t, reSnap.InstancesOf(mustTypeID(t, s, "Person")), 1)

	// InstanceByKey works.
	inst, ok := reSnap.InstanceByKey(mustTypeID(t, s, "Company"), `["c1"]`)
	require.True(t, ok)
	assert.Equal(t, "Company", inst.TypeName())
}

func TestNewFromSnapshot_EdgesPreserved(t *testing.T) {
	t.Parallel()
	s := importTestSchema(t)
	company := mustValidInstance(t, s, "Company", []any{"c1"}, map[string]any{"id": "c1", "title": "Acme"})
	person := mustValidInstanceWithEdge(t, s, "Person", []any{"p1"}, map[string]any{"id": "p1", "name": "Alice"}, "EMPLOYER", [][]any{{"c1"}})
	snap := buildSnapshot(t, s, company, person)

	// Verify original has edges.
	require.Len(t, snap.Edges(), 1)

	g := graph.NewFromSnapshot(s, snap)
	reSnap := g.Snapshot()

	// Edges survive round-trip.
	require.Len(t, reSnap.Edges(), 1)
	edge := reSnap.Edges()[0]
	assert.Equal(t, "EMPLOYER", edge.Relation())
	assert.Equal(t, "Person", edge.Source().TypeName())
	assert.Equal(t, "Company", edge.Target().TypeName())

	// EdgesFrom works for imported instances.
	personInst, ok := reSnap.InstanceByKey(mustTypeID(t, s, "Person"), `["p1"]`)
	require.True(t, ok)
	edges := reSnap.EdgesFrom(personInst)
	assert.Len(t, edges, 1)
}

func TestNewFromSnapshot_UnresolvedTargetMissing(t *testing.T) {
	t.Parallel()
	s := importTestSchema(t)

	// Person references Company["c1"] which doesn't exist → unresolved.
	person := mustValidInstanceWithEdge(t, s, "Person", []any{"p1"}, map[string]any{"id": "p1", "name": "Alice"}, "EMPLOYER", [][]any{{"c1"}})
	snap := buildSnapshot(t, s, person)
	require.Len(t, snap.Unresolved(), 1)
	assert.Equal(t, "target_missing", snap.Unresolved()[0].Reason)

	// Import, then add the missing Company.
	g := graph.NewFromSnapshot(s, snap)
	ctx := context.Background()
	company := mustValidInstance(t, s, "Company", []any{"c1"}, map[string]any{"id": "c1", "title": "Acme"})
	result := g.Add(ctx, company)
	assert.True(t, result.OK())

	reSnap := g.Snapshot()

	// The edge should now be resolved.
	assert.Len(t, reSnap.Edges(), 1)
	assert.Empty(t, reSnap.Unresolved())
}

func TestNewFromSnapshot_UnresolvedAbsentEmpty(t *testing.T) {
	t.Parallel()
	s := importTestSchemaRequired(t)

	// Person without EMPLOYER field → "absent" unresolved.
	person := mustValidInstance(t, s, "Person", []any{"p1"}, map[string]any{"id": "p1", "name": "Alice"})
	snap := buildSnapshot(t, s, person)
	require.GreaterOrEqual(t, len(snap.Unresolved()), 1)

	// Find the absent/empty unresolved edge.
	var absentFound bool
	for _, u := range snap.Unresolved() {
		if u.Reason == "absent" || u.Reason == "empty" {
			absentFound = true
		}
	}
	require.True(t, absentFound, "expected absent or empty unresolved edge")

	// Import and re-snapshot — structural unresolved should persist.
	g := graph.NewFromSnapshot(s, snap)
	reSnap := g.Snapshot()

	var reAbsentFound bool
	for _, u := range reSnap.Unresolved() {
		if u.Reason == "absent" || u.Reason == "empty" {
			reAbsentFound = true
		}
	}
	assert.True(t, reAbsentFound, "absent/empty unresolved should survive import round-trip")
}

func TestNewFromSnapshot_DuplicatesPreserved(t *testing.T) {
	t.Parallel()
	s := importTestSchema(t)

	// Add two companies with same PK → one is a duplicate.
	c1a := mustValidInstance(t, s, "Company", []any{"c1"}, map[string]any{"id": "c1", "title": "Acme"})
	c1b := mustValidInstance(t, s, "Company", []any{"c1"}, map[string]any{"id": "c1", "title": "Beta"})
	snap := buildSnapshot(t, s, c1a, c1b)
	require.Len(t, snap.Duplicates(), 1)

	g := graph.NewFromSnapshot(s, snap)
	reSnap := g.Snapshot()

	assert.Len(t, reSnap.Duplicates(), 1)
	assert.Equal(t, "Company", reSnap.Duplicates()[0].Instance.TypeName())
}

func TestNewFromSnapshot_AddAfterImport_NewType(t *testing.T) {
	t.Parallel()
	s := importTestSchema(t)

	// Original snapshot has only Person.
	person := mustValidInstance(t, s, "Person", []any{"p1"}, map[string]any{"id": "p1", "name": "Alice"})
	snap := buildSnapshot(t, s, person)

	g := graph.NewFromSnapshot(s, snap)
	ctx := context.Background()

	// Add a Company — a type not in the original snapshot.
	company := mustValidInstance(t, s, "Company", []any{"c1"}, map[string]any{"id": "c1", "title": "Acme"})
	result := g.Add(ctx, company)
	assert.True(t, result.OK())

	reSnap := g.Snapshot()
	assert.Len(t, reSnap.InstancesOf(mustTypeID(t, s, "Person")), 1)
	assert.Len(t, reSnap.InstancesOf(mustTypeID(t, s, "Company")), 1)
}

func TestNewFromSnapshot_AddAfterImport_DuplicatePK(t *testing.T) {
	t.Parallel()
	s := importTestSchema(t)

	company := mustValidInstance(t, s, "Company", []any{"c1"}, map[string]any{"id": "c1", "title": "Acme"})
	snap := buildSnapshot(t, s, company)

	g := graph.NewFromSnapshot(s, snap)
	ctx := context.Background()

	// Add another Company with same PK → should be flagged as duplicate.
	dup := mustValidInstance(t, s, "Company", []any{"c1"}, map[string]any{"id": "c1", "title": "Beta"})
	result := g.Add(ctx, dup)
	assert.True(t, result.HasErrors()) // E_DUPLICATE_PK is an error

	reSnap := g.Snapshot()
	assert.Len(t, reSnap.InstancesOf(mustTypeID(t, s, "Company")), 1)
	assert.Len(t, reSnap.Duplicates(), 1)
}

func TestNewFromSnapshot_AddAfterImport_EdgeResolution(t *testing.T) {
	t.Parallel()
	s := importTestSchema(t)

	company := mustValidInstance(t, s, "Company", []any{"c1"}, map[string]any{"id": "c1", "title": "Acme"})
	snap := buildSnapshot(t, s, company)

	g := graph.NewFromSnapshot(s, snap)
	ctx := context.Background()

	// Add Person with edge to imported Company.
	person := mustValidInstanceWithEdge(t, s, "Person", []any{"p1"}, map[string]any{"id": "p1", "name": "Alice"}, "EMPLOYER", [][]any{{"c1"}})
	result := g.Add(ctx, person)
	assert.True(t, result.OK())

	reSnap := g.Snapshot()
	assert.Len(t, reSnap.Edges(), 1)
	assert.Equal(t, "EMPLOYER", reSnap.Edges()[0].Relation())
}

func TestNewFromSnapshot_CrossResolution(t *testing.T) {
	t.Parallel()
	s := importTestSchema(t)

	// Build snapshot with Person→Company unresolved (Company doesn't exist).
	person := mustValidInstanceWithEdge(t, s, "Person", []any{"p1"}, map[string]any{"id": "p1", "name": "Alice"}, "EMPLOYER", [][]any{{"c1"}})
	snap := buildSnapshot(t, s, person)
	require.Len(t, snap.Unresolved(), 1)
	require.Empty(t, snap.Edges())

	// Import, then add the Company that resolves the pending edge.
	g := graph.NewFromSnapshot(s, snap)
	ctx := context.Background()
	company := mustValidInstance(t, s, "Company", []any{"c1"}, map[string]any{"id": "c1", "title": "Acme"})
	result := g.Add(ctx, company)
	assert.True(t, result.OK())

	reSnap := g.Snapshot()
	assert.Len(t, reSnap.Edges(), 1, "pending edge should be resolved")
	assert.Empty(t, reSnap.Unresolved(), "no unresolved edges should remain")
	assert.Equal(t, "EMPLOYER", reSnap.Edges()[0].Relation())
	assert.Equal(t, `["p1"]`, reSnap.Edges()[0].Source().PrimaryKey().String())
	assert.Equal(t, `["c1"]`, reSnap.Edges()[0].Target().PrimaryKey().String())
}

func TestNewFromSnapshot_RoundTripFidelity(t *testing.T) {
	t.Parallel()
	s := importTestSchema(t)

	company := mustValidInstance(t, s, "Company", []any{"c1"}, map[string]any{"id": "c1", "title": "Acme"})
	person := mustValidInstanceWithEdge(t, s, "Person", []any{"p1"}, map[string]any{"id": "p1", "name": "Alice"}, "EMPLOYER", [][]any{{"c1"}})
	original := buildSnapshot(t, s, company, person)

	// Import and re-snapshot.
	g := graph.NewFromSnapshot(s, original)
	reconstructed := g.Snapshot()

	// Structural comparison.
	snapshottest.DiffSnapshots(t, original, reconstructed)
}

func TestNewFromSnapshot_Independence(t *testing.T) {
	t.Parallel()
	s := importTestSchema(t)

	company := mustValidInstance(t, s, "Company", []any{"c1"}, map[string]any{"id": "c1", "title": "Acme"})
	snap := buildSnapshot(t, s, company)

	g := graph.NewFromSnapshot(s, snap)
	ctx := context.Background()

	// Add a new instance to the graph.
	person := mustValidInstance(t, s, "Person", []any{"p1"}, map[string]any{"id": "p1", "name": "Alice"})
	g.Add(ctx, person)

	// Original snapshot should be unaffected.
	assert.Empty(t, snap.InstancesOf(mustTypeID(t, s, "Person")), "original snapshot should not be modified")
	assert.Len(t, snap.InstancesOf(mustTypeID(t, s, "Company")), 1)
}
