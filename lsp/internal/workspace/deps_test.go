package workspace

import (
	"log/slog"
	"testing"

	"github.com/simon-lentz/yammm/lsp/internal/lsputil"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestDepGraph returns a depGraph with initialized maps, ready for testing.
func newTestDepGraph() depGraph {
	return depGraph{
		importsByEntry: make(map[string]map[string]struct{}),
		reverseDeps:    make(map[string]map[string]struct{}),
	}
}

// testLogger returns a discard logger suitable for unit tests.
func testLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func TestDepGraph_TransitiveClosureTracking(t *testing.T) {
	t.Parallel()
	dg := newTestDepGraph()
	log := testLogger()

	// Caller provides the full transitive closure for each entry.
	// A imports B and C (caller says A's closure is {B, C}).
	dg.updateDependencies("file:///A.yammm", []string{"/B.yammm", "/C.yammm"}, log)
	// B imports C (caller says B's closure is {C}).
	dg.updateDependencies("file:///B.yammm", []string{"/C.yammm"}, log)

	cURI := lsputil.PathToURI("/C.yammm")
	deps := dg.reverseDependents(cURI)

	require.NotNil(t, deps, "C should have reverse dependents")
	assert.Len(t, deps, 2, "C should be imported by both A and B")
	assert.Contains(t, deps, "file:///A.yammm")
	assert.Contains(t, deps, "file:///B.yammm")

	// B should only be depended on by A.
	bURI := lsputil.PathToURI("/B.yammm")
	bDeps := dg.reverseDependents(bURI)
	require.NotNil(t, bDeps)
	assert.Len(t, bDeps, 1)
	assert.Contains(t, bDeps, "file:///A.yammm")
}

func TestDepGraph_CascadeTrigger_ThreeLevelDAG(t *testing.T) {
	t.Parallel()
	dg := newTestDepGraph()
	log := testLogger()

	// Simulate a 3-level import DAG:
	// foundation.yammm is imported by 7 downstream entries.
	foundation := "/schemas/foundation.yammm"
	foundationURI := lsputil.PathToURI(foundation)

	downstreamEntries := []string{
		"file:///schemas/people.yammm",
		"file:///schemas/companies.yammm",
		"file:///schemas/funding.yammm",
		"file:///schemas/news.yammm",
		"file:///schemas/patents.yammm",
		"file:///schemas/products.yammm",
		"file:///schemas/research.yammm",
	}

	for _, entryURI := range downstreamEntries {
		dg.updateDependencies(entryURI, []string{foundation}, log)
	}

	deps := dg.reverseDependents(foundationURI)
	require.NotNil(t, deps, "foundation should have reverse dependents")
	assert.Len(t, deps, 7, "all 7 downstream entries should depend on foundation")

	for _, entryURI := range downstreamEntries {
		assert.Contains(t, deps, entryURI)
	}
}

func TestDepGraph_EdgeRemovalOnClose(t *testing.T) {
	t.Parallel()
	dg := newTestDepGraph()
	log := testLogger()

	// A imports B and C.
	dg.updateDependencies("file:///A.yammm", []string{"/B.yammm", "/C.yammm"}, log)
	// D imports C (so C has two dependents: A and D).
	dg.updateDependencies("file:///D.yammm", []string{"/C.yammm"}, log)

	bURI := lsputil.PathToURI("/B.yammm")
	cURI := lsputil.PathToURI("/C.yammm")

	// Preconditions.
	require.NotNil(t, dg.reverseDependents(bURI))
	require.Len(t, dg.reverseDependents(cURI), 2)

	// Close A by updating with nil imports.
	dg.updateDependencies("file:///A.yammm", nil, log)

	// A's edges should be removed: B should have no dependents.
	assert.Nil(t, dg.reverseDependents(bURI), "B should have no dependents after A is closed")

	// C should still have D as a dependent.
	cDeps := dg.reverseDependents(cURI)
	require.NotNil(t, cDeps, "C should still have a dependent (D)")
	assert.Len(t, cDeps, 1)
	assert.Contains(t, cDeps, "file:///D.yammm")

	// A should be removed from importsByEntry.
	_, exists := dg.importsByEntry["file:///A.yammm"]
	assert.False(t, exists, "A should be removed from importsByEntry after close")
}

func TestDepGraph_EdgeUpdateOnReAnalysis(t *testing.T) {
	t.Parallel()
	dg := newTestDepGraph()
	log := testLogger()

	// Initial: entry imports B and C.
	entryURI := "file:///entry.yammm"
	dg.updateDependencies(entryURI, []string{"/B.yammm", "/C.yammm"}, log)

	bURI := lsputil.PathToURI("/B.yammm")
	cURI := lsputil.PathToURI("/C.yammm")

	// Preconditions: both B and C have entry as a dependent.
	require.Contains(t, dg.reverseDependents(bURI), entryURI)
	require.Contains(t, dg.reverseDependents(cURI), entryURI)

	// Re-analysis: entry now only imports B (C was removed from import list).
	dg.updateDependencies(entryURI, []string{"/B.yammm"}, log)

	// B should still have entry as dependent.
	bDeps := dg.reverseDependents(bURI)
	require.NotNil(t, bDeps)
	assert.Contains(t, bDeps, entryURI)

	// C should no longer have any dependents.
	assert.Nil(t, dg.reverseDependents(cURI), "C should have no dependents after re-analysis removes the edge")

	// Forward map should only contain B.
	imports := dg.importsByEntry[entryURI]
	assert.Len(t, imports, 1)
	assert.Contains(t, imports, bURI)
}

func TestDepGraph_MultipleEntriesSharingImport(t *testing.T) {
	t.Parallel()
	dg := newTestDepGraph()
	log := testLogger()

	sharedPath := "/shared/common.yammm"
	sharedURI := lsputil.PathToURI(sharedPath)

	entry1 := "file:///entry1.yammm"
	entry2 := "file:///entry2.yammm"

	// Both entries import the same file.
	dg.updateDependencies(entry1, []string{sharedPath}, log)
	dg.updateDependencies(entry2, []string{sharedPath}, log)

	// Precondition: shared has both entries as dependents.
	deps := dg.reverseDependents(sharedURI)
	require.Len(t, deps, 2)

	// Close entry1 (update with nil imports).
	dg.updateDependencies(entry1, nil, log)

	// Shared should still have entry2 as a dependent.
	deps = dg.reverseDependents(sharedURI)
	require.NotNil(t, deps, "shared import should still have a dependent after closing one entry")
	assert.Len(t, deps, 1)
	assert.Contains(t, deps, entry2)
	assert.NotContains(t, deps, entry1)

	// entry2's forward deps should be unaffected.
	imports := dg.importsByEntry[entry2]
	assert.Len(t, imports, 1)
	assert.Contains(t, imports, sharedURI)
}
