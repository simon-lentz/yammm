package spec_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Operations — 4 claims
// =============================================================================

// TestGraph_NewCreatesGraph verifies that graph.New(schema) creates a non-nil graph.
// Source: SPEC.md, "Graph Construction" — graph.New(schema) creates graph.
func TestGraph_NewCreatesGraph(t *testing.T) {
	t.Parallel()
	s, _ := loadSchemaRaw(t, "testdata/graph/basic.yammm")
	g := graph.New(s)
	assert.NotNil(t, g, "graph.New should return a non-nil graph")
}

// TestGraph_AddReturnsResult verifies that g.Add returns (diag.Result, error)
// and that the result is OK for a valid instance addition.
// Source: SPEC.md, "Graph Construction" — g.Add(ctx, validInstance) returns (diag.Result, error).
func TestGraph_AddReturnsResult(t *testing.T) {
	t.Parallel()
	s, v := loadSchemaRaw(t, "testdata/graph/basic.yammm")
	ctx := context.Background()
	g := graph.New(s)

	inst := validateOne(t, v, "Apple", raw(map[string]any{
		"id":   "a1",
		"name": "Fuji",
	}))

	result, err := g.Add(ctx, inst)
	require.NoError(t, err, "g.Add should not return an error for a valid instance")
	assert.True(t, result.OK(), "g.Add result should be OK: %v", result.Messages())
}

// TestGraph_CheckRequiredAssociation verifies that g.Check reports errors
// when required associations are missing.
// Source: SPEC.md, "Graph Construction" — g.Check(ctx) checks required association completeness.
func TestGraph_CheckRequiredAssociation(t *testing.T) {
	t.Parallel()
	s, v := loadSchemaRaw(t, "testdata/graph/with_required.yammm")
	ctx := context.Background()
	g := graph.New(s)

	// Add Employee without satisfying the required BELONGS_TO association
	inst := validateOne(t, v, "Employee", raw(map[string]any{
		"id":   "emp1",
		"name": "Alice",
	}))
	addResult, err := g.Add(ctx, inst)
	require.NoError(t, err)
	// Add itself may succeed (the edge is unresolved/pending)
	_ = addResult

	// Check should report unresolved required association
	checkResult, err := g.Check(ctx)
	require.NoError(t, err, "g.Check should not return an error")
	assert.False(t, checkResult.OK(), "Check should report errors for missing required association")
	assertDiagHasCode(t, checkResult, diag.E_UNRESOLVED_REQUIRED)
}

// TestGraph_CheckRequiredAssociationSatisfied verifies that g.Check passes
// when required associations are satisfied.
// Source: SPEC.md, "Graph Construction" — Check passes when all required edges are resolved.
func TestGraph_CheckRequiredAssociationSatisfied(t *testing.T) {
	t.Parallel()
	s, v := loadSchemaRaw(t, "testdata/graph/with_required.yammm")
	ctx := context.Background()
	g := graph.New(s)

	// Add Department first
	dept := validateOne(t, v, "Department", raw(map[string]any{
		"id":   "eng",
		"name": "Engineering",
	}))
	_, err := g.Add(ctx, dept)
	require.NoError(t, err)

	// Add Employee with resolved BELONGS_TO edge
	emp := validateOne(t, v, "Employee", raw(map[string]any{
		"id":         "emp2",
		"name":       "Bob",
		"belongs_to": map[string]any{"_target_id": "eng"},
	}))
	_, err = g.Add(ctx, emp)
	require.NoError(t, err)

	// Check should pass
	checkResult, err := g.Check(ctx)
	require.NoError(t, err)
	assert.True(t, checkResult.OK(), "Check should pass with resolved required association: %v", checkResult.Messages())
}

// TestGraph_SnapshotBasic verifies that g.Snapshot() returns a result with
// Types() and InstancesOf() reflecting the added instances.
// Source: SPEC.md, "Graph Construction" — g.Snapshot() returns immutable snapshot
// with Types(), InstancesOf().
func TestGraph_SnapshotBasic(t *testing.T) {
	t.Parallel()
	s, v := loadSchemaRaw(t, "testdata/graph/basic.yammm")

	apple := validateOne(t, v, "Apple", raw(map[string]any{
		"id":   "a1",
		"name": "Fuji",
	}))

	snap := buildGraph(t, s, apple)
	require.NotNil(t, snap, "Snapshot should not be nil")

	types := snap.Types()
	require.Contains(t, types, "Apple", "Types() should include Apple")

	instances := snap.InstancesOf("Apple")
	require.Len(t, instances, 1, "should have 1 Apple instance")
	assert.Equal(t, "Apple", instances[0].TypeName())
}

// =============================================================================
// Ordering — 5 claims
// =============================================================================

// TestGraph_OrderingTypes verifies that Types() returns type names in
// lexicographic order regardless of insertion order.
// Source: SPEC.md, "Graph Construction" — Types(): lexicographic by type name.
func TestGraph_OrderingTypes(t *testing.T) {
	t.Parallel()
	s, v := loadSchemaRaw(t, "testdata/graph/basic.yammm")

	// Add in non-alphabetical order: Zebra, Mango, Apple
	zebra := validateOne(t, v, "Zebra", raw(map[string]any{
		"id":   "z1",
		"name": "Zara",
	}))
	mango := validateOne(t, v, "Mango", raw(map[string]any{
		"id":   "m1",
		"name": "Alfonso",
	}))
	apple := validateOne(t, v, "Apple", raw(map[string]any{
		"id":   "a1",
		"name": "Fuji",
	}))

	snap := buildGraph(t, s, zebra, mango, apple)

	types := snap.Types()
	require.Len(t, types, 3, "should have 3 types")
	assert.Equal(t, []string{"Apple", "Mango", "Zebra"}, types,
		"Types() should be sorted lexicographically")
}

// TestGraph_OrderingInstancesOf verifies that InstancesOf() returns instances
// sorted by primary key string.
// Source: SPEC.md, "Graph Construction" — InstancesOf(): lexicographic by primary key.
func TestGraph_OrderingInstancesOf(t *testing.T) {
	t.Parallel()
	s, v := loadSchemaRaw(t, "testdata/graph/basic.yammm")

	// Add in reverse order: z2, z1
	z2 := validateOne(t, v, "Zebra", raw(map[string]any{
		"id":   "z2",
		"name": "Zach",
	}))
	z1 := validateOne(t, v, "Zebra", raw(map[string]any{
		"id":   "z1",
		"name": "Zara",
	}))

	snap := buildGraph(t, s, z2, z1)

	instances := snap.InstancesOf("Zebra")
	require.Len(t, instances, 2, "should have 2 Zebra instances")

	// Sorted by PK string: ["z1"] < ["z2"]
	pk0 := instances[0].PrimaryKey().String()
	pk1 := instances[1].PrimaryKey().String()
	assert.True(t, pk0 < pk1,
		"InstancesOf should be sorted by primary key: got %s, %s", pk0, pk1)
}

// TestGraph_OrderingEdges verifies that Edges() returns edges sorted by
// (sourceType, sourceKey, relation, targetType, targetKey).
// Source: SPEC.md, "Graph Construction" — Edges(): lexicographic tuple.
func TestGraph_OrderingEdges(t *testing.T) {
	t.Parallel()
	s, v := loadSchemaRaw(t, "testdata/graph/with_association.yammm")
	ctx := context.Background()
	g := graph.New(s)

	// Add Company target first
	company := validateOne(t, v, "Company", raw(map[string]any{
		"id":   "acme",
		"name": "Acme Corp",
	}))
	_, err := g.Add(ctx, company)
	require.NoError(t, err)

	// Add two Persons with edges, in reverse order by key: bob, then alice
	bob := validateOne(t, v, "Person", raw(map[string]any{
		"id":       "bob",
		"name":     "Bob",
		"works_at": map[string]any{"_target_id": "acme"},
	}))
	_, err = g.Add(ctx, bob)
	require.NoError(t, err)

	alice := validateOne(t, v, "Person", raw(map[string]any{
		"id":       "alice",
		"name":     "Alice",
		"works_at": map[string]any{"_target_id": "acme"},
	}))
	_, err = g.Add(ctx, alice)
	require.NoError(t, err)

	snap := g.Snapshot()
	edges := snap.Edges()
	require.Len(t, edges, 2, "should have 2 edges")

	// Sorted by (sourceType, sourceKey, relation, targetType, targetKey)
	// Both edges: (Person, ?, WORKS_AT, Company, acme)
	// alice < bob by sourceKey
	assert.Equal(t, "Person", edges[0].Source().TypeName())
	assert.Equal(t, "Person", edges[1].Source().TypeName())

	pk0 := edges[0].Source().PrimaryKey().String()
	pk1 := edges[1].Source().PrimaryKey().String()
	assert.True(t, pk0 < pk1,
		"Edges should be sorted by source key: got %s, %s", pk0, pk1)
}

// TestGraph_OrderingDuplicates verifies that Duplicates() returns duplicate
// records sorted by (typeName, primaryKey).
// Source: SPEC.md, "Graph Construction" — Duplicates(): lexicographic by (typeName, primaryKey).
func TestGraph_OrderingDuplicates(t *testing.T) {
	t.Parallel()
	s, v := loadSchemaRaw(t, "testdata/graph/basic.yammm")
	ctx := context.Background()
	g := graph.New(s)

	// Add two different types, then add duplicates in reverse order
	zebraOrig := validateOne(t, v, "Zebra", raw(map[string]any{
		"id":   "z1",
		"name": "Zara",
	}))
	appleOrig := validateOne(t, v, "Apple", raw(map[string]any{
		"id":   "a1",
		"name": "Fuji",
	}))

	_, err := g.Add(ctx, zebraOrig)
	require.NoError(t, err)
	_, err = g.Add(ctx, appleOrig)
	require.NoError(t, err)

	// Now add duplicates in reverse type order: Zebra first, then Apple
	zebraDup := validateOne(t, v, "Zebra", raw(map[string]any{
		"id":   "z1",
		"name": "Zara Dup",
	}))
	appleDup := validateOne(t, v, "Apple", raw(map[string]any{
		"id":   "a1",
		"name": "Fuji Dup",
	}))

	// Add duplicates
	_, err = g.Add(ctx, zebraDup)
	require.NoError(t, err)
	_, err = g.Add(ctx, appleDup)
	require.NoError(t, err)

	snap := g.Snapshot()
	dups := snap.Duplicates()
	require.Len(t, dups, 2, "should have 2 duplicate records")

	// Sorted by (typeName, primaryKey): Apple < Zebra
	assert.Equal(t, "Apple", dups[0].Instance.TypeName(),
		"first duplicate should be Apple (lexicographic order)")
	assert.Equal(t, "Zebra", dups[1].Instance.TypeName(),
		"second duplicate should be Zebra (lexicographic order)")
}

// TestGraph_OrderingUnresolved verifies that Unresolved() returns unresolved
// edge records sorted by (sourceType, sourceKey, relation, targetType, targetKey).
// Source: SPEC.md, "Graph Construction" — Unresolved(): lexicographic by tuple.
func TestGraph_OrderingUnresolved(t *testing.T) {
	t.Parallel()
	s, v := loadSchemaRaw(t, "testdata/graph/with_association.yammm")
	ctx := context.Background()
	g := graph.New(s)

	// Add persons with edges pointing to non-existent companies (forward references
	// that will never be resolved). Add in reverse order to test sorting.
	bob := validateOne(t, v, "Person", raw(map[string]any{
		"id":       "bob",
		"name":     "Bob",
		"works_at": map[string]any{"_target_id": "ghost-corp"},
	}))
	_, err := g.Add(ctx, bob)
	require.NoError(t, err)

	alice := validateOne(t, v, "Person", raw(map[string]any{
		"id":       "alice",
		"name":     "Alice",
		"works_at": map[string]any{"_target_id": "phantom-inc"},
	}))
	_, err = g.Add(ctx, alice)
	require.NoError(t, err)

	snap := g.Snapshot()
	unresolved := snap.Unresolved()
	require.Len(t, unresolved, 2, "should have 2 unresolved edges")

	// Sorted by (sourceType, sourceKey, ...): alice < bob by sourceKey
	pk0 := unresolved[0].Source.PrimaryKey().String()
	pk1 := unresolved[1].Source.PrimaryKey().String()
	assert.True(t, pk0 < pk1,
		"Unresolved should be sorted by source key: got %s, %s", pk0, pk1)

	// Verify structure of unresolved edges
	assert.Equal(t, "WORKS_AT", unresolved[0].Relation)
	assert.Equal(t, "Company", unresolved[0].TargetType)
	assert.Equal(t, "WORKS_AT", unresolved[1].Relation)
	assert.Equal(t, "Company", unresolved[1].TargetType)
}

// =============================================================================
// Duplicates
// =============================================================================

// TestGraph_Duplicates verifies that adding the same instance twice (same
// type + primary key) produces a duplicate record in the snapshot.
// Source: SPEC.md, "Graph Construction" — duplicate PK detection.
func TestGraph_Duplicates(t *testing.T) {
	t.Parallel()
	s, v := loadSchemaRaw(t, "testdata/graph/basic.yammm")
	ctx := context.Background()
	g := graph.New(s)

	inst1 := validateOne(t, v, "Apple", raw(map[string]any{
		"id":   "a1",
		"name": "Fuji",
	}))
	inst2 := validateOne(t, v, "Apple", raw(map[string]any{
		"id":   "a1",
		"name": "Fuji (duplicate)",
	}))

	result1, err := g.Add(ctx, inst1)
	require.NoError(t, err)
	assert.True(t, result1.OK(), "first Add should succeed")

	result2, err := g.Add(ctx, inst2)
	require.NoError(t, err)
	// The second Add returns a result with E_DUPLICATE_PK
	assert.False(t, result2.OK(), "second Add should report duplicate")
	assertDiagHasCode(t, result2, diag.E_DUPLICATE_PK)

	snap := g.Snapshot()
	dups := snap.Duplicates()
	require.NotEmpty(t, dups, "Duplicates() should be non-empty")
	assert.Equal(t, "Apple", dups[0].Instance.TypeName())
}

// =============================================================================
// Thread safety — 3 claims
// =============================================================================

// TestGraph_ConcurrentAdd verifies that concurrent Add calls do not race.
// Run with: go test -race -run TestGraph_ConcurrentAdd
// Source: SPEC.md, "Graph Construction" — concurrent Add is safe.
func TestGraph_ConcurrentAdd(t *testing.T) {
	t.Parallel()
	s, v := loadSchemaRaw(t, "testdata/graph/basic.yammm")
	ctx := context.Background()
	g := graph.New(s)

	var wg sync.WaitGroup
	for i := range 10 {
		wg.Go(func() {
			inst := validateOne(t, v, "Apple", raw(map[string]any{ //nolint:contextcheck // test helper uses internal context
				"id":   fmt.Sprintf("item-%d", i),
				"name": fmt.Sprintf("Item %d", i),
			}))
			_, _ = g.Add(ctx, inst)
		})
	}
	wg.Wait()

	snap := g.Snapshot()
	instances := snap.InstancesOf("Apple")
	assert.NotEmpty(t, instances, "should have Apple instances after concurrent Add")
	assert.Len(t, instances, 10, "all 10 concurrent adds should succeed with unique keys")
}

// TestGraph_ConcurrentAddComposed verifies that concurrent AddComposed calls
// do not race when adding children to the same parent.
// Run with: go test -race -run TestGraph_ConcurrentAddComposed
// Source: SPEC.md, "Graph Construction" — concurrent AddComposed is safe.
func TestGraph_ConcurrentAddComposed(t *testing.T) {
	t.Parallel()
	s, v := loadSchemaRaw(t, "testdata/graph/with_composition.yammm")
	ctx := context.Background()
	g := graph.New(s)

	// Add parent Order first
	order := validateOne(t, v, "Order", raw(map[string]any{
		"id": "order-1",
	}))
	_, err := g.Add(ctx, order)
	require.NoError(t, err)

	parentKey := graph.FormatKey("order-1")

	// Pre-validate all children sequentially using ValidateForComposition
	// (part types cannot be validated with ValidateOne).
	var childRaws []instance.RawInstance
	for i := range 10 {
		childRaws = append(childRaws, raw(map[string]any{
			"description": fmt.Sprintf("Item %d", i),
			"quantity":    i + 1,
		}))
	}
	validChildren, failures, err := v.ValidateForComposition(ctx, "Order", "ITEMS", childRaws)
	require.NoError(t, err)
	require.Empty(t, failures, "all children should validate")
	require.Len(t, validChildren, 10)

	// Concurrently add all pre-validated children
	var wg sync.WaitGroup
	for i := range 10 {
		wg.Go(func() {
			_, _ = g.AddComposed(ctx, "Order", parentKey, "ITEMS", validChildren[i])
		})
	}
	wg.Wait()

	snap := g.Snapshot()
	orders := snap.InstancesOf("Order")
	require.Len(t, orders, 1, "should have 1 Order instance")
	composed := orders[0].Composed("ITEMS")
	assert.NotEmpty(t, composed, "Order should have composed LineItem children")
	assert.Len(t, composed, 10, "all 10 concurrent AddComposed calls should attach children")
}

// TestGraph_SnapshotImmutableConcurrentReads verifies that a snapshot can be
// read concurrently from multiple goroutines without races.
// Run with: go test -race -run TestGraph_SnapshotImmutableConcurrentReads
// Source: SPEC.md, "Graph Construction" — snapshot is immutable for concurrent reads.
func TestGraph_SnapshotImmutableConcurrentReads(t *testing.T) {
	t.Parallel()
	s, v := loadSchemaRaw(t, "testdata/graph/basic.yammm")

	inst := validateOne(t, v, "Apple", raw(map[string]any{
		"id":   "a1",
		"name": "Fuji",
	}))
	snap := buildGraph(t, s, inst)

	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() {
			// Read from snapshot concurrently
			types := snap.Types()
			assert.NotEmpty(t, types)
			instances := snap.InstancesOf("Apple")
			assert.NotEmpty(t, instances)
			edges := snap.Edges()
			_ = edges // may be nil, that's fine
			dups := snap.Duplicates()
			_ = dups // may be nil, that's fine
			unresolved := snap.Unresolved()
			_ = unresolved // may be nil, that's fine
		})
	}
	wg.Wait()
}

// =============================================================================
// Snapshot defensive copies
// =============================================================================

// TestGraph_SnapshotDefensiveCopies verifies that slice-returning methods on
// Result return nil when empty, and that returned slices are defensive copies.
// Source: SPEC.md, "Graph Construction" — all slice-returning methods return
// defensive copies, nil if empty.
func TestGraph_SnapshotDefensiveCopies(t *testing.T) {
	t.Parallel()
	s, v := loadSchemaRaw(t, "testdata/graph/basic.yammm")

	inst := validateOne(t, v, "Apple", raw(map[string]any{
		"id":   "a1",
		"name": "Fuji",
	}))
	snap := buildGraph(t, s, inst)

	// Verify nil for empty collections
	assert.Nil(t, snap.Edges(), "Edges() should be nil when no edges exist")
	assert.Nil(t, snap.Duplicates(), "Duplicates() should be nil when no duplicates exist")
	assert.Nil(t, snap.Unresolved(), "Unresolved() should be nil when no unresolved edges exist")
	assert.Nil(t, snap.InstancesOf("NonExistent"), "InstancesOf() should be nil for unknown type")

	// Verify defensive copy: modifying returned slice should not affect snapshot
	types1 := snap.Types()
	types2 := snap.Types()
	require.Len(t, types1, 1)
	types1[0] = "MODIFIED"
	assert.Equal(t, "Apple", types2[0],
		"modifying Types() return value should not affect subsequent calls")

	instances1 := snap.InstancesOf("Apple")
	instances2 := snap.InstancesOf("Apple")
	require.Len(t, instances1, 1)
	instances1[0] = nil
	assert.NotNil(t, instances2[0],
		"modifying InstancesOf() return value should not affect subsequent calls")
}

// =============================================================================
// AddComposed — basic operation
// =============================================================================

// TestGraph_AddComposedBasic verifies that AddComposed attaches a composed child
// to an existing parent instance.
// Source: SPEC.md, "Graph Construction" — AddComposed attaches children.
func TestGraph_AddComposedBasic(t *testing.T) {
	t.Parallel()
	s, v := loadSchemaRaw(t, "testdata/graph/with_composition.yammm")
	ctx := context.Background()
	g := graph.New(s)

	// Add parent Order
	order := validateOne(t, v, "Order", raw(map[string]any{
		"id": "order-1",
	}))
	_, err := g.Add(ctx, order)
	require.NoError(t, err)

	parentKey := graph.FormatKey("order-1")

	// Validate composed child via ValidateForComposition (part types cannot
	// be validated with ValidateOne).
	childRaws := []instance.RawInstance{raw(map[string]any{
		"description": "Widget",
		"quantity":    5,
	})}
	validChildren, failures, err := v.ValidateForComposition(ctx, "Order", "ITEMS", childRaws)
	require.NoError(t, err)
	require.Empty(t, failures, "child should validate")
	require.Len(t, validChildren, 1)

	result, err := g.AddComposed(ctx, "Order", parentKey, "ITEMS", validChildren[0])
	require.NoError(t, err)
	assert.True(t, result.OK(), "AddComposed should succeed: %v", result.Messages())

	// Verify in snapshot
	snap := g.Snapshot()
	orders := snap.InstancesOf("Order")
	require.Len(t, orders, 1)
	composed := orders[0].Composed("ITEMS")
	require.Len(t, composed, 1, "should have 1 composed child")
	assert.Equal(t, "LineItem", composed[0].TypeName())
}

// =============================================================================
// Edge resolution — forward references
// =============================================================================

// TestGraph_ForwardReferenceResolution verifies that edges are resolved when
// the target instance is added after the source.
// Source: SPEC.md, "Graph Construction" — forward references are resolved.
func TestGraph_ForwardReferenceResolution(t *testing.T) {
	t.Parallel()
	s, v := loadSchemaRaw(t, "testdata/graph/with_association.yammm")
	ctx := context.Background()
	g := graph.New(s)

	// Add Person first (before its WORKS_AT target Company)
	person := validateOne(t, v, "Person", raw(map[string]any{
		"id":       "alice",
		"name":     "Alice",
		"works_at": map[string]any{"_target_id": "acme"},
	}))
	_, err := g.Add(ctx, person)
	require.NoError(t, err)

	// At this point, edge is unresolved (forward reference)
	snap1 := g.Snapshot()
	assert.NotEmpty(t, snap1.Unresolved(), "should have unresolved edge before target is added")
	assert.Empty(t, snap1.Edges(), "should have no resolved edges yet")

	// Now add the target Company
	company := validateOne(t, v, "Company", raw(map[string]any{
		"id":   "acme",
		"name": "Acme Corp",
	}))
	_, err = g.Add(ctx, company)
	require.NoError(t, err)

	// Forward reference should now be resolved
	snap2 := g.Snapshot()
	assert.Empty(t, snap2.Unresolved(), "all edges should be resolved after target is added")
	require.Len(t, snap2.Edges(), 1, "should have 1 resolved edge")
	assert.Equal(t, "WORKS_AT", snap2.Edges()[0].Relation())
	assert.Equal(t, "Person", snap2.Edges()[0].Source().TypeName())
	assert.Equal(t, "Company", snap2.Edges()[0].Target().TypeName())
}

// =============================================================================
// Inline composition extraction
// =============================================================================

// TestGraph_InlineCompositionExtraction verifies that compositions included
// inline in instance data are automatically extracted during Add.
// Source: SPEC.md, "Graph Construction" — Add extracts composed children.
func TestGraph_InlineCompositionExtraction(t *testing.T) {
	t.Parallel()
	s, v := loadSchemaRaw(t, "testdata/graph/with_composition.yammm")
	ctx := context.Background()
	g := graph.New(s)

	// Add Order with inline composed LineItem children
	order := validateOne(t, v, "Order", raw(map[string]any{
		"id": "order-1",
		"items": []any{
			map[string]any{"description": "Widget", "quantity": 5},
			map[string]any{"description": "Gadget", "quantity": 3},
		},
	}))
	result, err := g.Add(ctx, order)
	require.NoError(t, err)
	assert.True(t, result.OK(), "Add with inline compositions should succeed: %v", result.Messages())

	snap := g.Snapshot()
	orders := snap.InstancesOf("Order")
	require.Len(t, orders, 1)
	composed := orders[0].Composed("ITEMS")
	assert.Len(t, composed, 2, "should have 2 composed LineItem children extracted from inline data")
}

// =============================================================================
// Snapshot OK and diagnostics
// =============================================================================

// TestGraph_SnapshotOK verifies that Result.OK() returns true for a clean graph
// and false for a graph with errors (e.g., duplicates).
// Source: SPEC.md, "Graph Construction" — Result.OK() checks construction success.
func TestGraph_SnapshotOK(t *testing.T) {
	t.Parallel()
	s, v := loadSchemaRaw(t, "testdata/graph/basic.yammm")
	ctx := context.Background()

	// Clean graph
	g1 := graph.New(s)
	inst := validateOne(t, v, "Apple", raw(map[string]any{
		"id":   "a1",
		"name": "Fuji",
	}))
	_, _ = g1.Add(ctx, inst)
	snap1 := g1.Snapshot()
	assert.True(t, snap1.OK(), "clean graph snapshot should be OK")

	// Graph with duplicate
	g2 := graph.New(s)
	inst1 := validateOne(t, v, "Apple", raw(map[string]any{
		"id":   "a1",
		"name": "Fuji",
	}))
	inst2 := validateOne(t, v, "Apple", raw(map[string]any{
		"id":   "a1",
		"name": "Fuji Again",
	}))
	_, _ = g2.Add(ctx, inst1)
	_, _ = g2.Add(ctx, inst2)
	snap2 := g2.Snapshot()
	assert.False(t, snap2.OK(), "graph with duplicates should not be OK")
}

// =============================================================================
// Data-driven edge and unresolved tests via loadTestData
// =============================================================================

// TestGraph_DataDrivenEdges verifies edge creation and resolution using
// test data loaded from JSON, matching the existing testdata conventions.
// Source: SPEC.md, "Graph Construction" — edges from association data.
func TestGraph_DataDrivenEdges(t *testing.T) {
	t.Parallel()
	s, v := loadSchemaRaw(t, "testdata/graph/with_association.yammm")

	data := "testdata/graph/data.json"
	companies := loadTestData(t, data, "Company")
	companyInst := validateOne(t, v, "Company", companies[0])

	persons := loadTestData(t, data, "Person")
	var personInsts []*instance.ValidInstance
	for _, p := range persons {
		personInsts = append(personInsts, validateOne(t, v, "Person", p))
	}

	// Build graph with all instances
	allInsts := append([]*instance.ValidInstance{companyInst}, personInsts...)
	snap := buildGraph(t, s, allInsts...)

	// Should have 2 edges (alice->acme, bob->acme)
	edges := snap.Edges()
	require.Len(t, edges, 2, "should have 2 resolved edges")

	// All edges should point to Company
	for _, e := range edges {
		assert.Equal(t, "WORKS_AT", e.Relation())
		assert.Equal(t, "Company", e.Target().TypeName())
	}
}
