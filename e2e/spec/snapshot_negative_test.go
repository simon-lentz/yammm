package spec_test

import (
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/snapshot"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Schema hash verification
// =============================================================================

// TestSnapshot_SchemaHashVerification tests that Load/Verify reject snapshots
// from incompatible schemas.
func TestSnapshot_SchemaHashVerification(t *testing.T) {
	t.Parallel()

	t.Run("wrong_schema", func(t *testing.T) {
		t.Parallel()
		// Build snapshot with comprehensive schema.
		_, snap := buildSnapshotFromDataAllTypes(t,
			"testdata/snapshot/comprehensive.yammm",
			"testdata/snapshot/comprehensive_data.json",
		)
		data := marshalSnapshot(t, snap)

		// Load with a different schema.
		wrongSchema, _ := loadSchemaRaw(t, "testdata/snapshot/simple.yammm")
		_, result := snapshot.Load(t.Context(), data, wrongSchema)
		require.True(t, result.HasErrors(), "loading with wrong schema should fail")
		assertDiagHasCode(t, result, diag.E_SNAPSHOT_INCOMPATIBLE_SCHEMA)
	})

	t.Run("verify_detects_mismatch", func(t *testing.T) {
		t.Parallel()
		_, snap := buildSnapshotFromDataAllTypes(t,
			"testdata/snapshot/comprehensive.yammm",
			"testdata/snapshot/comprehensive_data.json",
		)
		data := marshalSnapshot(t, snap)

		wrongSchema, _ := loadSchemaRaw(t, "testdata/snapshot/simple.yammm")
		verifySnapshotFails(t, data, wrongSchema, diag.E_SNAPSHOT_INCOMPATIBLE_SCHEMA)
	})

	t.Run("correct_schema_passes", func(t *testing.T) {
		t.Parallel()
		s, snap := buildSnapshotFromDataAllTypes(t,
			"testdata/snapshot/comprehensive.yammm",
			"testdata/snapshot/comprehensive_data.json",
		)
		data := marshalSnapshot(t, snap)
		verifySnapshot(t, data, s)
	})
}

// =============================================================================
// Integrity verification
// =============================================================================

// TestSnapshot_IntegrityVerification tests that corrupted .ys files are
// detected by the integrity hash.
func TestSnapshot_IntegrityVerification(t *testing.T) {
	t.Parallel()

	t.Run("corrupted_payload", func(t *testing.T) {
		t.Parallel()
		s, snap := buildSnapshotFromDataAllTypes(t,
			"testdata/snapshot/simple.yammm",
			"testdata/snapshot/simple_data.json",
		)
		data := marshalSnapshot(t, snap)

		// Corrupt a byte in the middle of the payload (instances region).
		mid := len(data) / 2
		corrupted := corruptBytesAt(data, mid)

		result := snapshot.Verify(t.Context(), corrupted, s)
		require.True(t, result.HasErrors(), "corrupted file should fail verification")
		// May be E_SNAPSHOT_INTEGRITY_MISMATCH or E_SNAPSHOT_MALFORMED depending on corruption.
		hasIntegrity := false
		hasMalformed := false
		for issue := range result.Issues() {
			if issue.Code() == diag.E_SNAPSHOT_INTEGRITY_MISMATCH {
				hasIntegrity = true
			}
			if issue.Code() == diag.E_SNAPSHOT_MALFORMED {
				hasMalformed = true
			}
		}
		assert.True(t, hasIntegrity || hasMalformed,
			"should have integrity mismatch or malformed error, got: %v", result.Messages())
	})

	t.Run("skip_integrity_bypasses", func(t *testing.T) {
		t.Parallel()
		s, snap := buildSnapshotFromDataAllTypes(t,
			"testdata/snapshot/simple.yammm",
			"testdata/snapshot/simple_data.json",
		)
		data := marshalSnapshot(t, snap)

		// Corrupt data but skip integrity check.
		mid := len(data) / 2
		corrupted := corruptBytesAt(data, mid)

		result := snapshot.Verify(t.Context(), corrupted, s, snapshot.WithSkipIntegrityCheck())
		// Should NOT have E_SNAPSHOT_INTEGRITY_MISMATCH specifically.
		for issue := range result.Issues() {
			assert.NotEqual(t, diag.E_SNAPSHOT_INTEGRITY_MISMATCH, issue.Code(),
				"skip integrity should not produce integrity mismatch")
		}
	})
}

// =============================================================================
// Malformed input
// =============================================================================

// TestSnapshot_Malformed tests that non-JSON and structurally wrong input
// produces appropriate diagnostics.
func TestSnapshot_Malformed(t *testing.T) {
	t.Parallel()

	t.Run("not_json", func(t *testing.T) {
		t.Parallel()
		s, _ := loadSchemaRaw(t, "testdata/snapshot/simple.yammm")
		result := snapshot.Verify(t.Context(), []byte("not json at all"), s)
		require.True(t, result.HasErrors())
		assertDiagHasCode(t, result, diag.E_SNAPSHOT_MALFORMED)
	})

	t.Run("missing_header", func(t *testing.T) {
		t.Parallel()
		s, _ := loadSchemaRaw(t, "testdata/snapshot/simple.yammm")
		result := snapshot.Verify(t.Context(), []byte(`{"types":[]}`), s)
		require.True(t, result.HasErrors())
		assertDiagHasCode(t, result, diag.E_SNAPSHOT_MALFORMED)
	})
}

// =============================================================================
// Info validation
// =============================================================================

// TestSnapshot_Info tests that Info returns consistent metadata.
func TestSnapshot_Info(t *testing.T) {
	t.Parallel()

	t.Run("matches_loaded", func(t *testing.T) {
		t.Parallel()
		s, snap := buildSnapshotFromDataAllTypes(t,
			"testdata/snapshot/comprehensive.yammm",
			"testdata/snapshot/comprehensive_data.json",
		)
		data := marshalSnapshot(t, snap, snapshot.WithMetadata(map[string]string{
			"env": "test",
		}))

		info := snapshotInfo(t, data)
		loaded := loadSnapshot(t, data, s)

		assert.Equal(t, loaded.Schema().Name(), info.SchemaName)

		// Count instances from loaded snapshot.
		totalInstances := 0
		for _, typeName := range loaded.Types() {
			totalInstances += len(loaded.InstancesOf(typeName))
		}
		assert.Equal(t, totalInstances, info.TotalInstances)
		assert.Equal(t, len(loaded.Edges()), info.TotalEdges)
		assert.Equal(t, len(loaded.Duplicates()), info.DuplicateCount)
		assert.Equal(t, len(loaded.Unresolved()), info.UnresolvedCount)
	})

	t.Run("integrity_ok", func(t *testing.T) {
		t.Parallel()
		_, snap := buildSnapshotFromDataAllTypes(t,
			"testdata/snapshot/simple.yammm",
			"testdata/snapshot/simple_data.json",
		)
		data := marshalSnapshot(t, snap)
		info := snapshotInfo(t, data)
		assert.Equal(t, "ok", info.IntegrityStatus)
	})

	t.Run("file_size", func(t *testing.T) {
		t.Parallel()
		_, snap := buildSnapshotFromDataAllTypes(t,
			"testdata/snapshot/simple.yammm",
			"testdata/snapshot/simple_data.json",
		)
		data := marshalSnapshot(t, snap)
		info := snapshotInfo(t, data)
		assert.Equal(t, int64(len(data)), info.FileSize)
	})
}

// =============================================================================
// Empty / edge cases
// =============================================================================

// TestSnapshot_EmptyEdgeCases tests boundary conditions.
func TestSnapshot_EmptyEdgeCases(t *testing.T) {
	t.Parallel()

	t.Run("empty_round_trip", func(t *testing.T) {
		t.Parallel()
		s, v := loadSchemaRaw(t, "testdata/snapshot/simple.yammm")
		_ = v
		g := graph.New(s)
		snap := g.Snapshot()

		data := marshalSnapshot(t, snap)
		loaded := loadSnapshot(t, data, s)

		assert.Empty(t, loaded.Types())
		assert.Nil(t, loaded.Edges())
		assert.Nil(t, loaded.Duplicates())
		assert.Nil(t, loaded.Unresolved())
	})

	t.Run("info_on_empty", func(t *testing.T) {
		t.Parallel()
		s, _ := loadSchemaRaw(t, "testdata/snapshot/simple.yammm")
		g := graph.New(s)
		snap := g.Snapshot()

		data := marshalSnapshot(t, snap)
		info := snapshotInfo(t, data)

		assert.Equal(t, 0, info.TotalInstances)
		assert.Equal(t, 0, info.TotalEdges)
		assert.Equal(t, 0, info.DuplicateCount)
		assert.Equal(t, 0, info.UnresolvedCount)
	})

	t.Run("loaded_diagnostics_ok", func(t *testing.T) {
		t.Parallel()
		// Build snapshot WITH duplicates, load it, verify Diagnostics() is OK.
		s, snap := buildSnapshotFromData(t,
			"testdata/snapshot/comprehensive.yammm",
			"testdata/snapshot/comprehensive_data.json",
			[]string{"Department", "Employee", "Employee__dup"},
		)
		require.NotEmpty(t, snap.Duplicates())

		data := marshalSnapshot(t, snap)
		loaded := loadSnapshot(t, data, s)

		// Loaded snapshot always reports OK diagnostics.
		assert.True(t, loaded.Diagnostics().OK(),
			"loaded snapshot Diagnostics() should be OK")
		assert.True(t, loaded.OK(),
			"loaded snapshot OK() should be true")
		assert.False(t, loaded.HasErrors(),
			"loaded snapshot HasErrors() should be false")
		// But duplicates are still present.
		assert.NotEmpty(t, loaded.Duplicates())
	})
}
