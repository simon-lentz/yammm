package spec_test

import (
	"encoding/json"
	"strings"
	"testing"

	csvadapter "github.com/simon-lentz/yammm/adapter/csv"
	jsonadapter "github.com/simon-lentz/yammm/adapter/json"
	neo4jadapter "github.com/simon-lentz/yammm/adapter/neo4j"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// JSON export from loaded snapshot
// =============================================================================

// TestSnapshot_ExportJSON tests exporting a loaded snapshot to JSON.
func TestSnapshot_ExportJSON(t *testing.T) {
	t.Parallel()

	t.Run("full_pipeline", func(t *testing.T) {
		t.Parallel()
		s, snap := buildSnapshotFromDataAllTypes(t,
			"testdata/snapshot/comprehensive.yammm",
			"testdata/snapshot/comprehensive_data.json",
		)

		data := marshalSnapshot(t, snap)
		loaded := loadSnapshot(t, data, s)

		adapter, err := jsonadapter.New(nil)
		require.NoError(t, err)
		jsonBytes, err := adapter.MarshalObject(t.Context(), loaded)
		require.NoError(t, err)

		// Verify valid JSON.
		var parsed map[string]any
		require.NoError(t, json.Unmarshal(jsonBytes, &parsed))

		// Should contain both types.
		assert.Contains(t, parsed, "Department")
		assert.Contains(t, parsed, "Employee")
	})

	t.Run("edge_fk_populated", func(t *testing.T) {
		t.Parallel()
		s, snap := buildSnapshotFromDataAllTypes(t,
			"testdata/snapshot/assoc_with_props.yammm",
			"testdata/snapshot/assoc_with_props_data.json",
		)

		data := marshalSnapshot(t, snap)
		loaded := loadSnapshot(t, data, s)

		adapter, err := jsonadapter.New(nil)
		require.NoError(t, err)
		jsonBytes, err := adapter.MarshalObject(t.Context(), loaded)
		require.NoError(t, err)

		// Verify Person has works_at FK populated.
		jsonStr := string(jsonBytes)
		assert.Contains(t, jsonStr, "works_at",
			"JSON export should include works_at FK field")
		assert.Contains(t, jsonStr, "acme",
			"JSON export should include target key 'acme'")
	})

	t.Run("composition_in_output", func(t *testing.T) {
		t.Parallel()
		s, snap := buildSnapshotFromData(t,
			"testdata/snapshot/comprehensive.yammm",
			"testdata/snapshot/comprehensive_data.json",
			[]string{"Department", "Employee"},
		)

		data := marshalSnapshot(t, snap)
		loaded := loadSnapshot(t, data, s)

		adapter, err := jsonadapter.New(nil)
		require.NoError(t, err)
		jsonBytes, err := adapter.MarshalObject(t.Context(), loaded)
		require.NoError(t, err)

		jsonStr := string(jsonBytes)
		// Alice has tags composition.
		assert.Contains(t, jsonStr, "tags",
			"JSON export should include composed children")
	})
}

// =============================================================================
// CSV export from loaded snapshot
// =============================================================================

// TestSnapshot_ExportCSV tests the critical CSV FK column fix end-to-end.
func TestSnapshot_ExportCSV(t *testing.T) {
	t.Parallel()

	t.Run("fk_columns_populated", func(t *testing.T) {
		t.Parallel()
		s, snap := buildSnapshotFromDataAllTypes(t,
			"testdata/snapshot/assoc_with_props.yammm",
			"testdata/snapshot/assoc_with_props_data.json",
		)

		data := marshalSnapshot(t, snap)
		loaded := loadSnapshot(t, data, s)

		adapter := csvadapter.New()
		csvOutput, err := adapter.MarshalSnapshot(t.Context(), loaded)
		require.NoError(t, err)

		personCSV := string(csvOutput["Person"])
		// The CSV should have a works_at FK column with the target key.
		assert.Contains(t, personCSV, "works_at",
			"CSV should have works_at FK column header")
		assert.Contains(t, personCSV, "acme",
			"CSV FK column should contain the target key")
	})

	t.Run("properties_present", func(t *testing.T) {
		t.Parallel()
		s, snap := buildSnapshotFromDataAllTypes(t,
			"testdata/snapshot/simple.yammm",
			"testdata/snapshot/simple_data.json",
		)

		data := marshalSnapshot(t, snap)
		loaded := loadSnapshot(t, data, s)

		adapter := csvadapter.New()
		csvOutput, err := adapter.MarshalSnapshot(t.Context(), loaded)
		require.NoError(t, err)

		widgetCSV := string(csvOutput["Widget"])
		assert.Contains(t, widgetCSV, "Alpha Widget")
		assert.Contains(t, widgetCSV, "Beta Widget")
	})
}

// =============================================================================
// Neo4j export from loaded snapshot
// =============================================================================

// TestSnapshot_ExportNeo4j tests Cypher query generation from loaded snapshots.
func TestSnapshot_ExportNeo4j(t *testing.T) {
	t.Parallel()

	t.Run("node_queries", func(t *testing.T) {
		t.Parallel()
		s, snap := buildSnapshotFromDataAllTypes(t,
			"testdata/snapshot/assoc_with_props.yammm",
			"testdata/snapshot/assoc_with_props_data.json",
		)

		data := marshalSnapshot(t, snap)
		loaded := loadSnapshot(t, data, s)

		adapter := neo4jadapter.New()
		shapes, result := adapter.ShapeForSchema(t.Context(), loaded.Schema())
		require.True(t, result.OK(), "shape inference should succeed: %v", result.Messages())

		queries, err := adapter.BatchNodeQueries(t.Context(), loaded, shapes)
		require.NoError(t, err)
		require.NotEmpty(t, queries, "should generate node queries")

		// Verify queries contain MERGE statements for Company and Person labels.
		var foundCompany, foundPerson bool
		for _, q := range queries {
			if strings.Contains(q.Statement, "Company") {
				foundCompany = true
			}
			if strings.Contains(q.Statement, "Person") {
				foundPerson = true
			}
		}
		assert.True(t, foundCompany, "node queries should include Company label")
		assert.True(t, foundPerson, "node queries should include Person label")
	})

	t.Run("edge_queries", func(t *testing.T) {
		t.Parallel()
		s, snap := buildSnapshotFromDataAllTypes(t,
			"testdata/snapshot/assoc_with_props.yammm",
			"testdata/snapshot/assoc_with_props_data.json",
		)

		data := marshalSnapshot(t, snap)
		loaded := loadSnapshot(t, data, s)

		adapter := neo4jadapter.New()
		shapes, result := adapter.ShapeForSchema(t.Context(), loaded.Schema())
		require.True(t, result.OK(), "shape inference should succeed: %v", result.Messages())

		queries, err := adapter.BatchEdgeQueries(t.Context(), loaded, shapes)
		require.NoError(t, err)
		require.NotEmpty(t, queries, "should generate edge queries")

		// Verify WORKS_AT relationship query.
		found := false
		for _, q := range queries {
			if strings.Contains(q.Statement, "WORKS_AT") {
				found = true
				break
			}
		}
		assert.True(t, found, "should have WORKS_AT edge query")
	})
}
