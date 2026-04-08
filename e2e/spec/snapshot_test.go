package spec_test

import (
	"math"
	"testing"
	"time"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
	"github.com/simon-lentz/yammm/snapshot/snapshottest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Full pipeline round-trip
// =============================================================================

// TestSnapshot_RoundTrip_FullPipeline exercises the complete e2e pipeline:
// schema.Load -> JSON parse -> validate -> graph -> snapshot -> marshal -> load -> compare.
func TestSnapshot_RoundTrip_FullPipeline(t *testing.T) {
	t.Parallel()

	t.Run("comprehensive", func(t *testing.T) {
		t.Parallel()
		s, snap := buildSnapshotFromDataAllTypes(t,
			"testdata/snapshot/comprehensive.yammm",
			"testdata/snapshot/comprehensive_data.json",
		)

		// Verify graph was built correctly.
		require.Contains(t, snap.Types(), "Department")
		require.Contains(t, snap.Types(), "Employee")
		require.Len(t, snap.InstancesOf("Department"), 2)
		require.Len(t, snap.InstancesOf("Employee"), 2)
		require.NotEmpty(t, snap.Edges(), "should have WORKS_IN edges")

		// Round-trip: marshal -> load -> compare.
		snapshottest.AssertRoundTrip(t, snap, s)
	})

	t.Run("empty_snapshot", func(t *testing.T) {
		t.Parallel()
		s, _ := loadSchemaRaw(t, "testdata/snapshot/simple.yammm")
		g := graph.New(s)
		snap := g.Snapshot()

		data := marshalSnapshot(t, snap)
		loaded := loadSnapshot(t, data, s)

		assert.Empty(t, loaded.Types())
		assert.Nil(t, loaded.Edges())
	})

	t.Run("no_relationships", func(t *testing.T) {
		t.Parallel()
		s, v := loadSchemaRaw(t, "testdata/snapshot/simple.yammm")
		inst := validateOne(t, v, "Widget", raw(map[string]any{
			"id":    "w1",
			"label": "Test Widget",
		}))
		snap := buildGraph(t, s, inst)

		snapshottest.AssertRoundTrip(t, snap, s)
	})

	t.Run("all_nil_optionals", func(t *testing.T) {
		t.Parallel()
		// Employee with only required fields — all optional properties are absent.
		s, snap := buildSnapshotFromData(t,
			"testdata/snapshot/comprehensive.yammm",
			"testdata/snapshot/comprehensive_data.json",
			[]string{"Employee__minimal"},
		)

		data := marshalSnapshot(t, snap)
		loaded := loadSnapshot(t, data, s)

		employees := loaded.InstancesOf("Employee")
		require.Len(t, employees, 1)

		props := employees[0].Properties().Clone()
		// Optional properties should not be present.
		_, hasEmail := props["email"]
		assert.False(t, hasEmail, "optional email should be absent")
		_, hasSalary := props["salary"]
		assert.False(t, hasSalary, "optional salary should be absent")
		_, hasSkills := props["skills"]
		assert.False(t, hasSkills, "optional skills should be absent")
	})
}

// =============================================================================
// Property fidelity
// =============================================================================

// TestSnapshot_RoundTrip_PropertyFidelity tests that all property types survive
// the marshal -> load round-trip with correct values.
func TestSnapshot_RoundTrip_PropertyFidelity(t *testing.T) {
	t.Parallel()

	// Helper: inline schema with one type having a single property of the given type.
	type schemaPair struct {
		v *instance.Validator
		s *schema.Schema
	}
	propSchema := func(t *testing.T, propDecl string) schemaPair {
		t.Helper()
		content := `schema "PropTest"
type Item {
    id String primary
    val ` + propDecl + `
}`
		s, v := loadSchemaStringRaw(t, content, "proptest")
		return schemaPair{v: v, s: s}
	}

	// Helper: build, marshal, load, return loaded property value.
	roundTripProp := func(t *testing.T, propDecl string, val any) map[string]any {
		t.Helper()
		sp := propSchema(t, propDecl)
		inst := validateOne(t, sp.v, "Item", raw(map[string]any{"id": "x", "val": val}))
		snap := buildGraph(t, sp.s, inst)

		data := marshalSnapshot(t, snap)
		loaded := loadSnapshot(t, data, sp.s)

		items := loaded.InstancesOf("Item")
		require.Len(t, items, 1)
		return items[0].Properties().Clone()
	}

	t.Run("int64_large_value", func(t *testing.T) {
		t.Parallel()
		props := roundTripProp(t, "Integer", int64(9007199254740993))
		assert.Equal(t, int64(9007199254740993), props["val"])
	})

	t.Run("int64_boundary", func(t *testing.T) {
		t.Parallel()
		props := roundTripProp(t, "Integer", int64(math.MaxInt64))
		assert.Equal(t, int64(math.MaxInt64), props["val"])
	})

	t.Run("float_scientific_notation", func(t *testing.T) {
		t.Parallel()
		props := roundTripProp(t, "Float", 1.5e10)
		assert.InDelta(t, 1.5e10, props["val"], 1.0)
	})

	t.Run("float_type_narrowing", func(t *testing.T) {
		t.Parallel()
		// float64(1.0) marshals as JSON "1", normalizes to int64(1) on load.
		// The typed accessor should still return 1.0 via Float().
		sp := propSchema(t, "Float")
		inst := validateOne(t, sp.v, "Item", raw(map[string]any{"id": "x", "val": float64(1.0)}))
		snap := buildGraph(t, sp.s, inst)

		data := marshalSnapshot(t, snap)
		loaded := loadSnapshot(t, data, sp.s)
		items := loaded.InstancesOf("Item")
		require.Len(t, items, 1)

		// The raw value may be int64(1) due to JSON type narrowing.
		// But functionally it represents 1.0.
		val, ok := items[0].Properties().Get("val")
		require.True(t, ok)
		f, fok := val.Float()
		require.True(t, fok)
		assert.Equal(t, float64(1.0), f)
	})

	t.Run("boolean_true_false", func(t *testing.T) {
		t.Parallel()
		props := roundTripProp(t, "Boolean", true)
		assert.Equal(t, true, props["val"])

		props = roundTripProp(t, "Boolean", false)
		assert.Equal(t, false, props["val"])
	})

	t.Run("string_unicode", func(t *testing.T) {
		t.Parallel()
		props := roundTripProp(t, "String", "café ☕ 你好")
		assert.Equal(t, "café ☕ 你好", props["val"])
	})

	t.Run("list_string", func(t *testing.T) {
		t.Parallel()
		props := roundTripProp(t, "List<String>", []any{"a", "b", "c"})
		list, ok := props["val"].([]any)
		require.True(t, ok, "val should be []any")
		assert.Equal(t, []any{"a", "b", "c"}, list)
	})

	t.Run("list_integer", func(t *testing.T) {
		t.Parallel()
		props := roundTripProp(t, "List<Integer[0, 100]>", []any{int64(10), int64(20), int64(30)})
		list, ok := props["val"].([]any)
		require.True(t, ok, "val should be []any")
		require.Len(t, list, 3)
	})

	t.Run("list_empty", func(t *testing.T) {
		t.Parallel()
		props := roundTripProp(t, "List<String>", []any{})
		list, ok := props["val"].([]any)
		require.True(t, ok, "val should be []any, not nil")
		assert.Empty(t, list)
	})

	t.Run("vector", func(t *testing.T) {
		t.Parallel()
		props := roundTripProp(t, "Vector[3]", []any{0.1, 0.2, 0.3})
		list, ok := props["val"].([]any)
		require.True(t, ok, "val should be []any")
		assert.Len(t, list, 3)
	})

	t.Run("enum", func(t *testing.T) {
		t.Parallel()
		props := roundTripProp(t, `Enum["active", "inactive"]`, "active")
		assert.Equal(t, "active", props["val"])
	})

	t.Run("date", func(t *testing.T) {
		t.Parallel()
		props := roundTripProp(t, "Date", "2026-03-15")
		assert.Equal(t, "2026-03-15", props["val"])
	})

	t.Run("timestamp", func(t *testing.T) {
		t.Parallel()
		props := roundTripProp(t, "Timestamp", "2026-01-01T00:00:00Z")
		assert.Equal(t, "2026-01-01T00:00:00Z", props["val"])
	})

	t.Run("pattern", func(t *testing.T) {
		t.Parallel()
		props := roundTripProp(t, `Pattern["^[^@]+@[^@]+$"]`, "test@example.com")
		assert.Equal(t, "test@example.com", props["val"])
	})
}

// =============================================================================
// Edge round-trip
// =============================================================================

// TestSnapshot_RoundTrip_Edges tests edge serialization and reconstruction.
func TestSnapshot_RoundTrip_Edges(t *testing.T) {
	t.Parallel()

	t.Run("basic_association", func(t *testing.T) {
		t.Parallel()
		s, snap := buildSnapshotFromDataAllTypes(t,
			"testdata/snapshot/comprehensive.yammm",
			"testdata/snapshot/comprehensive_data.json",
		)

		data := marshalSnapshot(t, snap)
		loaded := loadSnapshot(t, data, s)

		// Find an employee with an edge.
		employees := loaded.InstancesOf("Employee")
		var foundEdge bool
		for _, emp := range employees {
			edges := loaded.EdgesFrom(emp)
			if len(edges) > 0 {
				assert.Equal(t, "WORKS_IN", edges[0].Relation())
				assert.Equal(t, "Department", edges[0].Target().TypeName())
				foundEdge = true
				break
			}
		}
		assert.True(t, foundEdge, "should find at least one employee with WORKS_IN edge")
	})

	t.Run("multiple_edges_per_instance", func(t *testing.T) {
		t.Parallel()
		content := `schema "MultiEdge"
type Company {
    id String primary
    name String required
}

type Team {
    id String primary
    name String required
}

type Person {
    id String primary
    name String required
    --> WORKS_AT Company
    --> MEMBER_OF Team
}`
		s, v := loadSchemaStringRaw(t, content, "multiedge")
		ctx := t.Context()
		g := graph.New(s)

		company := validateOne(t, v, "Company", raw(map[string]any{"id": "c1", "name": "Acme"}))
		team := validateOne(t, v, "Team", raw(map[string]any{"id": "t1", "name": "Alpha"}))
		person := validateOne(t, v, "Person", raw(map[string]any{
			"id": "p1", "name": "Alice",
			"works_at":  map[string]any{"_target_id": "c1"},
			"member_of": map[string]any{"_target_id": "t1"},
		}))
		g.Add(ctx, company)
		g.Add(ctx, team)
		g.Add(ctx, person)
		snap := g.Snapshot()

		data := marshalSnapshot(t, snap)
		loaded := loadSnapshot(t, data, s)

		persons := loaded.InstancesOf("Person")
		require.Len(t, persons, 1)
		edges := loaded.EdgesFrom(persons[0])
		assert.Len(t, edges, 2, "person should have 2 edges after round-trip")
	})

	t.Run("edge_with_properties", func(t *testing.T) {
		t.Parallel()
		s, snap := buildSnapshotFromDataAllTypes(t,
			"testdata/snapshot/assoc_with_props.yammm",
			"testdata/snapshot/assoc_with_props_data.json",
		)

		data := marshalSnapshot(t, snap)
		loaded := loadSnapshot(t, data, s)

		persons := loaded.InstancesOf("Person")
		require.Len(t, persons, 1)
		edges := loaded.EdgesFrom(persons[0])
		require.Len(t, edges, 1)

		edgeProps := edges[0].Properties().Clone()
		assert.Equal(t, "Engineer", edgeProps["role"])
		assert.Equal(t, true, edgeProps["active"])
	})

	t.Run("edge_ordering", func(t *testing.T) {
		t.Parallel()
		s, v := loadSchemaRaw(t, "testdata/graph/with_association.yammm")
		ctx := t.Context()
		g := graph.New(s)

		company := validateOne(t, v, "Company", raw(map[string]any{"id": "acme", "name": "Acme"}))
		g.Add(ctx, company)

		// Add persons in reverse alphabetical order.
		for _, name := range []string{"charlie", "bob", "alice"} {
			p := validateOne(t, v, "Person", raw(map[string]any{
				"id": name, "name": name,
				"works_at": map[string]any{"_target_id": "acme"},
			}))
			g.Add(ctx, p)
		}
		snap := g.Snapshot()

		data := marshalSnapshot(t, snap)
		loaded := loadSnapshot(t, data, s)

		edges := loaded.Edges()
		require.Len(t, edges, 3)
		// Edges should be sorted by source key: ["alice"] < ["bob"] < ["charlie"].
		pk0 := edges[0].Source().PrimaryKey().String()
		pk1 := edges[1].Source().PrimaryKey().String()
		pk2 := edges[2].Source().PrimaryKey().String()
		assert.True(t, pk0 < pk1 && pk1 < pk2,
			"edges should be sorted by source key: got %s, %s, %s", pk0, pk1, pk2)
	})
}

// =============================================================================
// Composition round-trip
// =============================================================================

// TestSnapshot_RoundTrip_Compositions tests keyed and keyless composition round-trip.
func TestSnapshot_RoundTrip_Compositions(t *testing.T) {
	t.Parallel()

	t.Run("keyed_composition", func(t *testing.T) {
		t.Parallel()
		s, snap := buildSnapshotFromData(t,
			"testdata/snapshot/comprehensive.yammm",
			"testdata/snapshot/comprehensive_data.json",
			[]string{"Department", "Employee"},
		)

		data := marshalSnapshot(t, snap)
		loaded := loadSnapshot(t, data, s)

		// Alice (first Employee alphabetically by UUID) has HOME_ADDRESS.
		employees := loaded.InstancesOf("Employee")
		var foundComposition bool
		for _, emp := range employees {
			composed := emp.Composed("HOME_ADDRESS")
			if len(composed) > 0 {
				props := composed[0].Properties().Clone()
				assert.Equal(t, "123 Main St", props["street"])
				assert.Equal(t, "Springfield", props["city"])
				foundComposition = true
				break
			}
		}
		assert.True(t, foundComposition, "should find employee with HOME_ADDRESS composition")
	})

	t.Run("keyless_composition", func(t *testing.T) {
		t.Parallel()
		s, snap := buildSnapshotFromData(t,
			"testdata/snapshot/comprehensive.yammm",
			"testdata/snapshot/comprehensive_data.json",
			[]string{"Department", "Employee"},
		)

		data := marshalSnapshot(t, snap)
		loaded := loadSnapshot(t, data, s)

		employees := loaded.InstancesOf("Employee")
		var foundTags bool
		for _, emp := range employees {
			tags := emp.Composed("TAGS")
			if len(tags) > 0 {
				assert.Len(t, tags, 2, "Alice should have 2 tags")
				foundTags = true
				break
			}
		}
		assert.True(t, foundTags, "should find employee with TAGS composition")
	})

	t.Run("empty_composition", func(t *testing.T) {
		t.Parallel()
		// Bob has no compositions.
		s, snap := buildSnapshotFromData(t,
			"testdata/snapshot/comprehensive.yammm",
			"testdata/snapshot/comprehensive_data.json",
			[]string{"Department", "Employee"},
		)

		data := marshalSnapshot(t, snap)
		loaded := loadSnapshot(t, data, s)

		employees := loaded.InstancesOf("Employee")
		var foundEmpty bool
		for _, emp := range employees {
			if len(emp.Composed("HOME_ADDRESS")) == 0 && len(emp.Composed("TAGS")) == 0 {
				foundEmpty = true
				break
			}
		}
		assert.True(t, foundEmpty, "should find employee with no compositions")
	})
}

// =============================================================================
// Diagnostics round-trip (duplicates and unresolved)
// =============================================================================

// TestSnapshot_RoundTrip_Diagnostics tests duplicate and unresolved edge
// preservation across the persistence boundary.
func TestSnapshot_RoundTrip_Diagnostics(t *testing.T) {
	t.Parallel()

	t.Run("duplicates", func(t *testing.T) {
		t.Parallel()
		s, snap := buildSnapshotFromData(t,
			"testdata/snapshot/comprehensive.yammm",
			"testdata/snapshot/comprehensive_data.json",
			[]string{"Department", "Employee", "Employee__dup"},
		)
		require.NotEmpty(t, snap.Duplicates(), "should have duplicates from __dup data")

		data := marshalSnapshot(t, snap)
		loaded := loadSnapshot(t, data, s)

		require.Len(t, loaded.Duplicates(), len(snap.Duplicates()))
		assert.Equal(t, "Employee", loaded.Duplicates()[0].Instance.TypeName())
	})

	t.Run("duplicate_has_no_diagnostic", func(t *testing.T) {
		t.Parallel()
		s, snap := buildSnapshotFromData(t,
			"testdata/snapshot/comprehensive.yammm",
			"testdata/snapshot/comprehensive_data.json",
			[]string{"Department", "Employee", "Employee__dup"},
		)

		data := marshalSnapshot(t, snap)
		loaded := loadSnapshot(t, data, s)

		for _, dup := range loaded.Duplicates() {
			assert.False(t, dup.HasDiagnostic(), "loaded duplicate should not have diagnostic")
		}
	})

	t.Run("unresolved_target_missing", func(t *testing.T) {
		t.Parallel()
		// Employee referencing nonexistent Department.
		s, snap := buildSnapshotFromData(t,
			"testdata/snapshot/comprehensive.yammm",
			"testdata/snapshot/comprehensive_data.json",
			[]string{"Employee__no_dept"},
		)

		data := marshalSnapshot(t, snap)
		loaded := loadSnapshot(t, data, s)

		// No WORKS_IN edge exists for the no_dept employee, so it should have
		// zero edges and zero unresolved (works_in is optional).
		employees := loaded.InstancesOf("Employee")
		require.Len(t, employees, 1)
		assert.Nil(t, loaded.EdgesFrom(employees[0]))
	})

	t.Run("unresolved_absent", func(t *testing.T) {
		t.Parallel()
		// Member without required BELONGS_TO field -> absent unresolved edge.
		s, v := loadSchemaRaw(t, "testdata/snapshot/required_assoc.yammm")
		ctx := t.Context()
		g := graph.New(s)

		member := validateOne(t, v, "Member", raw(map[string]any{
			"id":   "m2",
			"name": "Orphan Bob",
		}))
		g.Add(ctx, member)
		checkResult := g.Check(ctx)
		_ = checkResult // may have errors from required association

		snap := g.Snapshot()
		require.NotEmpty(t, snap.Unresolved(), "should have unresolved from absent required assoc")

		data := marshalSnapshot(t, snap)
		loaded := loadSnapshot(t, data, s)

		require.NotEmpty(t, loaded.Unresolved())
		assert.Equal(t, "absent", loaded.Unresolved()[0].Reason)
	})

	t.Run("mixed", func(t *testing.T) {
		t.Parallel()
		s, v := loadSchemaRaw(t, "testdata/graph/with_association.yammm")
		ctx := t.Context()
		g := graph.New(s)

		// Add two persons with same key (duplicate) and one with unresolved edge.
		p1 := validateOne(t, v, "Person", raw(map[string]any{
			"id": "p1", "name": "Alice",
			"works_at": map[string]any{"_target_id": "ghost"},
		}))
		p1dup := validateOne(t, v, "Person", raw(map[string]any{
			"id": "p1", "name": "Alice Dup",
		}))
		g.Add(ctx, p1)
		g.Add(ctx, p1dup)
		snap := g.Snapshot()

		require.NotEmpty(t, snap.Duplicates())
		require.NotEmpty(t, snap.Unresolved())

		data := marshalSnapshot(t, snap)
		loaded := loadSnapshot(t, data, s)

		assert.Len(t, loaded.Duplicates(), len(snap.Duplicates()))
		assert.Len(t, loaded.Unresolved(), len(snap.Unresolved()))
	})
}

// =============================================================================
// Provenance round-trip
// =============================================================================

// TestSnapshot_RoundTrip_Provenance tests provenance metadata preservation.
func TestSnapshot_RoundTrip_Provenance(t *testing.T) {
	t.Parallel()

	t.Run("source_name_and_path", func(t *testing.T) {
		t.Parallel()
		s, snap := buildSnapshotFromDataAllTypes(t,
			"testdata/snapshot/simple.yammm",
			"testdata/snapshot/simple_data.json",
		)

		// Verify provenance is set on the original snapshot instances.
		origWidgets := snap.InstancesOf("Widget")
		require.NotEmpty(t, origWidgets)

		// Provenance may or may not be set depending on the adapter.
		// If it's set on the original, verify it survives round-trip.
		origProv := origWidgets[0].Provenance()
		data := marshalSnapshot(t, snap)
		loaded := loadSnapshot(t, data, s)

		widgets := loaded.InstancesOf("Widget")
		require.NotEmpty(t, widgets)
		loadedProv := widgets[0].Provenance()

		if origProv != nil {
			require.NotNil(t, loadedProv, "provenance should survive round-trip")
			assert.Equal(t, origProv.SourceName(), loadedProv.SourceName())
		} else {
			assert.Nil(t, loadedProv, "nil provenance should stay nil")
		}
	})

	t.Run("nil_provenance", func(t *testing.T) {
		t.Parallel()
		s, v := loadSchemaRaw(t, "testdata/snapshot/simple.yammm")
		// Build programmatically without provenance.
		inst := validateOne(t, v, "Widget", raw(map[string]any{"id": "w1", "label": "test"}))
		snap := buildGraph(t, s, inst)

		data := marshalSnapshot(t, snap)
		loaded := loadSnapshot(t, data, s)

		widgets := loaded.InstancesOf("Widget")
		require.Len(t, widgets, 1)
		assert.Nil(t, widgets[0].Provenance(), "nil provenance should survive round-trip")
	})
}

// =============================================================================
// Inheritance round-trip
// =============================================================================

// TestSnapshot_RoundTrip_Inheritance tests inherited properties across the
// persistence boundary.
func TestSnapshot_RoundTrip_Inheritance(t *testing.T) {
	t.Parallel()

	t.Run("inherited_properties", func(t *testing.T) {
		t.Parallel()
		s, snap := buildSnapshotFromDataAllTypes(t,
			"testdata/snapshot/inheritance.yammm",
			"testdata/snapshot/inheritance_data.json",
		)

		data := marshalSnapshot(t, snap)
		loaded := loadSnapshot(t, data, s)

		docs := loaded.InstancesOf("Document")
		require.Len(t, docs, 1)
		props := docs[0].Properties().Clone()

		// Inherited from Named.
		assert.Equal(t, "Design Spec", props["name"])
		// Inherited from Timestamped.
		assert.Equal(t, "2024-01-01T00:00:00Z", props["created_at"])
		// Own property.
		assert.Equal(t, "The system shall...", props["content"])
	})

	t.Run("multiple_concrete_types", func(t *testing.T) {
		t.Parallel()
		s, snap := buildSnapshotFromDataAllTypes(t,
			"testdata/snapshot/inheritance.yammm",
			"testdata/snapshot/inheritance_data.json",
		)

		snapshottest.AssertRoundTrip(t, snap, s)

		data := marshalSnapshot(t, snap)
		loaded := loadSnapshot(t, data, s)

		assert.Len(t, loaded.InstancesOf("Document"), 1)
		assert.Len(t, loaded.InstancesOf("Report"), 1)

		reports := loaded.InstancesOf("Report")
		props := reports[0].Properties().Clone()
		assert.Equal(t, "Q4 Summary", props["name"])
	})
}

// =============================================================================
// Determinism
// =============================================================================

// TestSnapshot_Determinism verifies byte-level determinism guarantees.
func TestSnapshot_Determinism(t *testing.T) {
	t.Parallel()

	t.Run("default_no_timestamp", func(t *testing.T) {
		t.Parallel()
		s, snap := buildSnapshotFromDataAllTypes(t,
			"testdata/snapshot/comprehensive.yammm",
			"testdata/snapshot/comprehensive_data.json",
		)
		snapshottest.AssertDeterministic(t, snap, s)
	})

	t.Run("with_indent", func(t *testing.T) {
		t.Parallel()
		s, snap := buildSnapshotFromDataAllTypes(t,
			"testdata/snapshot/comprehensive.yammm",
			"testdata/snapshot/comprehensive_data.json",
		)
		snapshottest.AssertDeterministic(t, snap, s, snapshot.WithIndent("\t"))
	})

	t.Run("with_metadata", func(t *testing.T) {
		t.Parallel()
		s, snap := buildSnapshotFromDataAllTypes(t,
			"testdata/snapshot/comprehensive.yammm",
			"testdata/snapshot/comprehensive_data.json",
		)
		snapshottest.AssertDeterministic(t, snap, s, snapshot.WithMetadata(map[string]string{
			"env": "staging", "pipeline": "test",
		}))
	})

	t.Run("compact_vs_indented_both_loadable", func(t *testing.T) {
		t.Parallel()
		s, snap := buildSnapshotFromDataAllTypes(t,
			"testdata/snapshot/comprehensive.yammm",
			"testdata/snapshot/comprehensive_data.json",
		)

		compact := marshalSnapshot(t, snap)
		indented := marshalSnapshot(t, snap, snapshot.WithIndent("\t"))
		assert.NotEqual(t, compact, indented, "compact and indented should differ")

		loadedCompact := loadSnapshot(t, compact, s)
		loadedIndented := loadSnapshot(t, indented, s)

		err := snapshottest.CompareSnapshots(loadedCompact, loadedIndented)
		assert.NoError(t, err, "compact and indented should produce equivalent snapshots")
	})
}

// =============================================================================
// Options
// =============================================================================

// TestSnapshot_Options tests marshal options via Info inspection.
func TestSnapshot_Options(t *testing.T) {
	t.Parallel()

	t.Run("metadata_round_trip", func(t *testing.T) {
		t.Parallel()
		_, snap := buildSnapshotFromDataAllTypes(t,
			"testdata/snapshot/simple.yammm",
			"testdata/snapshot/simple_data.json",
		)

		data := marshalSnapshot(t, snap, snapshot.WithMetadata(map[string]string{
			"env": "staging", "pipeline": "test",
		}))
		info := snapshotInfo(t, data)
		assert.Equal(t, "staging", info.Metadata["env"])
		assert.Equal(t, "test", info.Metadata["pipeline"])
	})

	t.Run("created_at_round_trip", func(t *testing.T) {
		t.Parallel()
		_, snap := buildSnapshotFromDataAllTypes(t,
			"testdata/snapshot/simple.yammm",
			"testdata/snapshot/simple_data.json",
		)

		fixedTime := time.Date(2026, 4, 7, 12, 0, 0, 0, time.UTC)
		data := marshalSnapshot(t, snap, snapshot.WithCreatedAt(fixedTime))
		info := snapshotInfo(t, data)
		assert.Equal(t, "2026-04-07T12:00:00Z", info.CreatedAt)
	})

	t.Run("indented_contains_newlines", func(t *testing.T) {
		t.Parallel()
		_, snap := buildSnapshotFromDataAllTypes(t,
			"testdata/snapshot/simple.yammm",
			"testdata/snapshot/simple_data.json",
		)

		data := marshalSnapshot(t, snap, snapshot.WithIndent("\t"))
		assert.Contains(t, string(data), "\n")
	})

	t.Run("compact_no_newlines", func(t *testing.T) {
		t.Parallel()
		_, snap := buildSnapshotFromDataAllTypes(t,
			"testdata/snapshot/simple.yammm",
			"testdata/snapshot/simple_data.json",
		)

		data := marshalSnapshot(t, snap)
		assert.NotContains(t, string(data), "\n")
	})
}
