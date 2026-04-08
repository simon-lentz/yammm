package spec_test

import (
	"testing"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/snapshot/snapshottest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Basic import
// =============================================================================

// TestSnapshot_Import_Basic tests NewFromSnapshot for preserving snapshot data
// across the import boundary.
func TestSnapshot_Import_Basic(t *testing.T) {
	t.Parallel()

	t.Run("add_new_data", func(t *testing.T) {
		t.Parallel()
		s, snap := buildSnapshotFromDataAllTypes(t,
			"testdata/snapshot/comprehensive.yammm",
			"testdata/snapshot/comprehensive_data.json",
		)
		origCount := len(snap.InstancesOf("Department"))

		data := marshalSnapshot(t, snap)
		loaded := loadSnapshot(t, data, s)

		// Import and add new data.
		g := graph.NewFromSnapshot(s, loaded)
		newDept := validateOne(t, instance.NewValidator(s), "Department", raw(map[string]any{
			"id":         "sales",
			"name":       "Sales",
			"created_at": "2025-01-01T00:00:00Z",
		}))
		g.Add(t.Context(), newDept)
		merged := g.Snapshot()

		assert.Len(t, merged.InstancesOf("Department"), origCount+1,
			"should have original departments plus new one")
	})

	t.Run("preserves_edges", func(t *testing.T) {
		t.Parallel()
		s, snap := buildSnapshotFromDataAllTypes(t,
			"testdata/snapshot/comprehensive.yammm",
			"testdata/snapshot/comprehensive_data.json",
		)
		origEdgeCount := len(snap.Edges())
		require.Greater(t, origEdgeCount, 0, "original should have edges")

		data := marshalSnapshot(t, snap)
		loaded := loadSnapshot(t, data, s)

		g := graph.NewFromSnapshot(s, loaded)
		merged := g.Snapshot()

		assert.Len(t, merged.Edges(), origEdgeCount,
			"imported snapshot should preserve all edges")
	})

	t.Run("preserves_compositions", func(t *testing.T) {
		t.Parallel()
		s, snap := buildSnapshotFromData(t,
			"testdata/snapshot/comprehensive.yammm",
			"testdata/snapshot/comprehensive_data.json",
			[]string{"Department", "Employee"},
		)

		data := marshalSnapshot(t, snap)
		loaded := loadSnapshot(t, data, s)

		g := graph.NewFromSnapshot(s, loaded)
		merged := g.Snapshot()

		// Check compositions survived import.
		for _, emp := range merged.InstancesOf("Employee") {
			tags := emp.Composed("TAGS")
			origEmp := findInstanceByKey(loaded, "Employee", emp.PrimaryKey().String())
			if origEmp != nil {
				assert.Len(t, tags, len(origEmp.Composed("TAGS")))
			}
		}
	})

	t.Run("preserves_duplicates", func(t *testing.T) {
		t.Parallel()
		s, snap := buildSnapshotFromData(t,
			"testdata/snapshot/comprehensive.yammm",
			"testdata/snapshot/comprehensive_data.json",
			[]string{"Department", "Employee", "Employee__dup"},
		)
		require.NotEmpty(t, snap.Duplicates())

		data := marshalSnapshot(t, snap)
		loaded := loadSnapshot(t, data, s)

		g := graph.NewFromSnapshot(s, loaded)
		merged := g.Snapshot()

		assert.Len(t, merged.Duplicates(), len(loaded.Duplicates()),
			"imported snapshot should preserve duplicates")
	})

	t.Run("preserves_unresolved", func(t *testing.T) {
		t.Parallel()
		s, v := loadSchemaRaw(t, "testdata/snapshot/required_assoc.yammm")
		ctx := t.Context()
		g := graph.New(s)

		member := validateOne(t, v, "Member", raw(map[string]any{
			"id":   "m2",
			"name": "Orphan",
		}))
		g.Add(ctx, member)
		g.Check(ctx)
		snap := g.Snapshot()
		require.NotEmpty(t, snap.Unresolved())

		data := marshalSnapshot(t, snap)
		loaded := loadSnapshot(t, data, s)

		g2 := graph.NewFromSnapshot(s, loaded)
		merged := g2.Snapshot()

		assert.NotEmpty(t, merged.Unresolved(),
			"imported snapshot should preserve unresolved edges")
	})
}

// =============================================================================
// Forward reference resolution
// =============================================================================

// TestSnapshot_Import_ForwardReference tests that previously unresolved edges
// can be resolved by adding new instances to the imported graph.
func TestSnapshot_Import_ForwardReference(t *testing.T) {
	t.Parallel()

	t.Run("resolved_by_new_add", func(t *testing.T) {
		t.Parallel()
		s, v := loadSchemaRaw(t, "testdata/graph/with_association.yammm")
		ctx := t.Context()

		// Build snapshot with unresolved edge.
		g := graph.New(s)
		person := validateOne(t, v, "Person", raw(map[string]any{
			"id": "p1", "name": "Alice",
			"works_at": map[string]any{"_target_id": "acme"},
		}))
		g.Add(ctx, person)
		snap := g.Snapshot()
		require.NotEmpty(t, snap.Unresolved(), "should have unresolved WORKS_AT edge")
		require.Empty(t, snap.Edges(), "should have no resolved edges yet")

		// Round-trip through persistence.
		data := marshalSnapshot(t, snap)
		loaded := loadSnapshot(t, data, s)

		// Import and add the missing target.
		g2 := graph.NewFromSnapshot(s, loaded)
		company := validateOne(t, v, "Company", raw(map[string]any{
			"id": "acme", "name": "Acme Corp",
		}))
		g2.Add(ctx, company)
		merged := g2.Snapshot()

		// The edge should now be resolved.
		assert.NotEmpty(t, merged.Edges(),
			"adding missing target should resolve the edge")
		assert.Empty(t, merged.Unresolved(),
			"no unresolved edges after adding target")
		assert.Equal(t, "WORKS_AT", merged.Edges()[0].Relation())
	})

	t.Run("absent_persists", func(t *testing.T) {
		t.Parallel()
		s, v := loadSchemaRaw(t, "testdata/snapshot/required_assoc.yammm")
		ctx := t.Context()

		// Build snapshot with "absent" unresolved (required relation, no field).
		g := graph.New(s)
		member := validateOne(t, v, "Member", raw(map[string]any{
			"id": "m1", "name": "Orphan",
		}))
		g.Add(ctx, member)
		g.Check(ctx)
		snap := g.Snapshot()
		require.NotEmpty(t, snap.Unresolved())

		data := marshalSnapshot(t, snap)
		loaded := loadSnapshot(t, data, s)

		// Import without adding the missing target.
		g2 := graph.NewFromSnapshot(s, loaded)
		merged := g2.Snapshot()

		require.NotEmpty(t, merged.Unresolved(),
			"absent unresolved should persist")
		assert.Equal(t, "absent", merged.Unresolved()[0].Reason)
	})
}

// =============================================================================
// Duplicate detection across import
// =============================================================================

// TestSnapshot_Import_DuplicateDetection tests that new instances with the same
// PK as imported instances are correctly flagged as duplicates.
func TestSnapshot_Import_DuplicateDetection(t *testing.T) {
	t.Parallel()

	t.Run("new_conflicts_with_imported", func(t *testing.T) {
		t.Parallel()
		s, v := loadSchemaRaw(t, "testdata/snapshot/simple.yammm")
		ctx := t.Context()

		// Build snapshot with Widget["w1"].
		g := graph.New(s)
		w1 := validateOne(t, v, "Widget", raw(map[string]any{"id": "w1", "label": "Original"}))
		g.Add(ctx, w1)
		snap := g.Snapshot()

		data := marshalSnapshot(t, snap)
		loaded := loadSnapshot(t, data, s)

		// Import and add a conflicting Widget["w1"].
		g2 := graph.NewFromSnapshot(s, loaded)
		w1dup := validateOne(t, v, "Widget", raw(map[string]any{"id": "w1", "label": "Duplicate"}))
		g2.Add(ctx, w1dup)
		merged := g2.Snapshot()

		assert.NotEmpty(t, merged.Duplicates(),
			"new instance with same PK should be flagged as duplicate")
	})
}

// =============================================================================
// Full double round-trip
// =============================================================================

// TestSnapshot_Import_FullCycle tests the definitive "nothing lost" cycle:
// build -> marshal -> load -> import -> add -> marshal -> load -> compare.
func TestSnapshot_Import_FullCycle(t *testing.T) {
	t.Parallel()

	t.Run("double_round_trip", func(t *testing.T) {
		t.Parallel()
		s, v := loadSchemaRaw(t, "testdata/graph/with_association.yammm")
		ctx := t.Context()

		// Build original graph with 2 companies and 2 persons.
		g := graph.New(s)
		c1 := validateOne(t, v, "Company", raw(map[string]any{"id": "c1", "name": "Acme"}))
		p1 := validateOne(t, v, "Person", raw(map[string]any{
			"id": "p1", "name": "Alice",
			"works_at": map[string]any{"_target_id": "c1"},
		}))
		g.Add(ctx, c1)
		g.Add(ctx, p1)
		snap1 := g.Snapshot()

		// First round-trip: marshal -> load.
		data1 := marshalSnapshot(t, snap1)
		loaded1 := loadSnapshot(t, data1, s)

		// Import and add more data.
		g2 := graph.NewFromSnapshot(s, loaded1)
		c2 := validateOne(t, v, "Company", raw(map[string]any{"id": "c2", "name": "Beta"}))
		p2 := validateOne(t, v, "Person", raw(map[string]any{
			"id": "p2", "name": "Bob",
			"works_at": map[string]any{"_target_id": "c2"},
		}))
		g2.Add(ctx, c2)
		g2.Add(ctx, p2)
		snap2 := g2.Snapshot()

		// Second round-trip: marshal -> load.
		data2 := marshalSnapshot(t, snap2)
		loaded2 := loadSnapshot(t, data2, s)

		// Verify nothing was lost.
		err := snapshottest.CompareSnapshots(snap2, loaded2)
		assert.NoError(t, err, "double round-trip should preserve all data")

		// Verify merged state.
		assert.Len(t, loaded2.InstancesOf("Company"), 2)
		assert.Len(t, loaded2.InstancesOf("Person"), 2)
		assert.Len(t, loaded2.Edges(), 2)
	})
}

// =============================================================================
// Multi-schema
// =============================================================================

// TestSnapshot_MultiSchema tests graph construction with imported schemas.
func TestSnapshot_MultiSchema(t *testing.T) {
	t.Parallel()

	t.Run("imported_type_construction", func(t *testing.T) {
		t.Parallel()
		_, snap := buildSnapshotFromDataAllTypes(t,
			"testdata/snapshot/imports/main.yammm",
			"testdata/snapshot/imports/data.json",
		)

		// Verify types from the imported schema are present in the graph.
		types := snap.Types()
		assert.Contains(t, types, "Assembly", "should have Assembly type")
		assert.Contains(t, types, "Component", "should have local Component type")

		// Verify the graph has instances from both schemas.
		assemblies := snap.InstancesOf("Assembly")
		assert.NotEmpty(t, assemblies, "should have Assembly instances")

		localComponents := snap.InstancesOf("Component")
		assert.NotEmpty(t, localComponents, "should have local Component instances")
	})

	t.Run("imported_type_round_trip", func(t *testing.T) {
		t.Parallel()
		// Build a graph from the multi-schema data, using only the types
		// that are directly accessible in the root schema.
		s, snap := buildSnapshotFromData(t,
			"testdata/snapshot/imports/main.yammm",
			"testdata/snapshot/imports/data.json",
			[]string{"Assembly", "Component"},
		)

		// Round-trip the root schema's types.
		snapshottest.AssertRoundTrip(t, snap, s)
	})
}

// findInstanceByKey looks up an instance in a snapshot by type and key string.
func findInstanceByKey(snap *graph.Snapshot, typeName, keyStr string) *graph.Instance {
	for _, inst := range snap.InstancesOf(typeName) {
		if inst.PrimaryKey().String() == keyStr {
			return inst
		}
	}
	return nil
}
