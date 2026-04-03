package workspace

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/simon-lentz/yammm/lsp/internal/protocol"

	"github.com/simon-lentz/yammm/location"

	"github.com/simon-lentz/yammm/lsp/internal/analysis"
	"github.com/simon-lentz/yammm/lsp/internal/docstate"
	"github.com/simon-lentz/yammm/lsp/internal/lsputil"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestMapper() uriMapper {
	return uriMapper{
		publishedByEntry: make(map[string]map[string]struct{}),
	}
}

// makeDoc creates a docstate.Document with the given URI, SourceID, and OpenOrder.
func makeDoc(uri string, sourceID location.SourceID, openOrder int) *docstate.Document {
	return &docstate.Document{
		URI:       uri,
		SourceID:  sourceID,
		Version:   1,
		OpenOrder: openOrder,
	}
}

func TestMapper_invalidateCache(t *testing.T) {
	t.Parallel()
	m := newTestMapper()

	// Populate cache
	open := map[string]*docstate.Document{
		"file:///a.yammm": makeDoc("file:///a.yammm", location.SourceID{}, 0),
	}
	m.ensureCache(open)
	assert.NotNil(t, m.canonicalToURI, "cache should be populated after ensureCache")

	m.invalidateCache()
	assert.Nil(t, m.canonicalToURI, "cache should be nil after invalidation")
}

func TestMapper_ensureCache_lazyRebuild(t *testing.T) {
	t.Parallel()
	m := newTestMapper()

	open := map[string]*docstate.Document{
		"file:///a.yammm": makeDoc("file:///a.yammm", location.SourceID{}, 0),
	}

	// First call builds cache
	cache1 := m.ensureCache(open)
	assert.NotNil(t, cache1)

	// Second call returns same cache (no rebuild)
	cache2 := m.ensureCache(open)
	assert.Equal(t, cache1, cache2)

	// After invalidation, rebuilds
	m.invalidateCache()
	cache3 := m.ensureCache(open)
	assert.NotNil(t, cache3)
}

func TestMapper_buildCanonicalToURIMap_prefersSourceID(t *testing.T) {
	t.Parallel()
	m := newTestMapper()

	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "test.yammm")
	require.NoError(t, os.WriteFile(filePath, []byte("content"), 0o600))

	sourceID, err := location.SourceIDFromAbsolutePath(filePath)
	require.NoError(t, err)

	uri := lsputil.PathToURI(filePath)
	open := map[string]*docstate.Document{
		uri: makeDoc(uri, sourceID, 0),
	}

	cache := m.buildCanonicalToURIMap(open)

	// Should use SourceID.String() as key
	assert.Equal(t, uri, cache[sourceID.String()])
}

func TestMapper_buildCanonicalToURIMap_fallbackWithoutSourceID(t *testing.T) {
	t.Parallel()
	m := newTestMapper()

	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "test.yammm")
	require.NoError(t, os.WriteFile(filePath, []byte("content"), 0o600))

	uri := lsputil.PathToURI(filePath)
	open := map[string]*docstate.Document{
		uri: makeDoc(uri, location.SourceID{}, 0), // zero SourceID
	}

	cache := m.buildCanonicalToURIMap(open)

	// Should fall back to resolved path
	resolved, err := filepath.EvalSymlinks(filePath)
	require.NoError(t, err)
	lookupKey := filepath.ToSlash(resolved)
	assert.Equal(t, uri, cache[lookupKey])
}

func TestMapper_buildCanonicalToURIMap_symlinkResolution(t *testing.T) {
	t.Parallel()
	m := newTestMapper()

	tmpDir := t.TempDir()
	realFile := filepath.Join(tmpDir, "real.yammm")
	linkFile := filepath.Join(tmpDir, "link.yammm")
	require.NoError(t, os.WriteFile(realFile, []byte("content"), 0o600))
	require.NoError(t, os.Symlink(realFile, linkFile))

	// Open via symlink with zero SourceID to exercise fallback path
	linkURI := lsputil.PathToURI(linkFile)
	open := map[string]*docstate.Document{
		linkURI: makeDoc(linkURI, location.SourceID{}, 0),
	}

	cache := m.buildCanonicalToURIMap(open)

	// Cache key should be the resolved (real) path
	resolved, err := filepath.EvalSymlinks(linkFile)
	require.NoError(t, err)
	lookupKey := filepath.ToSlash(resolved)
	assert.Equal(t, linkURI, cache[lookupKey])
}

func TestMapper_buildCanonicalToURIMap_prefersEarlierOpenOrder(t *testing.T) {
	t.Parallel()
	m := newTestMapper()

	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "test.yammm")
	require.NoError(t, os.WriteFile(filePath, []byte("content"), 0o600))

	sourceID, err := location.SourceIDFromAbsolutePath(filePath)
	require.NoError(t, err)

	uri1 := lsputil.PathToURI(filePath)
	uri2 := "file:///alias/test.yammm"

	open := map[string]*docstate.Document{
		uri1: makeDoc(uri1, sourceID, 1), // opened second
		uri2: makeDoc(uri2, sourceID, 0), // opened first
	}

	cache := m.buildCanonicalToURIMap(open)

	// uri2 has lower OpenOrder, so it should win
	assert.Equal(t, uri2, cache[sourceID.String()])
}

func TestMapper_remapToOpenDocURI_matchesCache(t *testing.T) {
	t.Parallel()
	m := newTestMapper()

	cache := map[string]string{
		"/path/to/test.yammm": "file:///client/test.yammm",
	}

	result := m.remapToOpenDocURI("file:///path/to/test.yammm", cache)
	assert.Equal(t, "file:///client/test.yammm", result)
}

func TestMapper_remapToOpenDocURI_noMatch(t *testing.T) {
	t.Parallel()
	m := newTestMapper()

	cache := map[string]string{
		"/path/to/other.yammm": "file:///client/other.yammm",
	}

	// Should return original URI when no cache match
	result := m.remapToOpenDocURI("file:///path/to/test.yammm", cache)
	assert.Equal(t, "file:///path/to/test.yammm", result)
}

func TestMapper_remapToOpenDocURI_nonFileScheme(t *testing.T) {
	t.Parallel()
	m := newTestMapper()

	cache := map[string]string{}

	// Non-file scheme URIs should be returned as-is
	result := m.remapToOpenDocURI("http://example.com/test", cache)
	assert.Equal(t, "http://example.com/test", result)
}

func TestMapper_remapToOpenDocURI_barePath(t *testing.T) {
	t.Parallel()
	m := newTestMapper()

	cache := map[string]string{
		"/path/to/test.yammm": "file:///client/test.yammm",
	}

	// Bare path (no scheme) should use the path directly for lookup
	result := m.remapToOpenDocURI("/path/to/test.yammm", cache)
	assert.Equal(t, "file:///client/test.yammm", result)
}

func TestMapper_clearEntryPublications(t *testing.T) {
	t.Parallel()
	m := newTestMapper()

	entryURI := "file:///entry.yammm"
	pubURIs := map[string]struct{}{
		"file:///a.yammm": {},
		"file:///b.yammm": {},
	}
	m.publishedByEntry[entryURI] = pubURIs

	result := m.clearEntryPublications(entryURI)

	assert.Equal(t, pubURIs, result)
	_, exists := m.publishedByEntry[entryURI]
	assert.False(t, exists, "entry should be removed from publishedByEntry")
}

func TestMapper_clearEntryPublications_noPriorPublications(t *testing.T) {
	t.Parallel()
	m := newTestMapper()

	result := m.clearEntryPublications("file:///nonexistent.yammm")
	assert.Nil(t, result)
}

func TestMapper_uriStillPublishedByOthers(t *testing.T) {
	t.Parallel()
	m := newTestMapper()

	m.publishedByEntry["file:///entry1.yammm"] = map[string]struct{}{
		"file:///shared.yammm": {},
	}
	m.publishedByEntry["file:///entry2.yammm"] = map[string]struct{}{
		"file:///shared.yammm": {},
	}

	assert.True(t, m.uriStillPublishedByOthers("file:///shared.yammm"))
	assert.False(t, m.uriStillPublishedByOthers("file:///unique.yammm"))
}

func TestMapper_uriStillPublishedByOthers_empty(t *testing.T) {
	t.Parallel()
	m := newTestMapper()
	assert.False(t, m.uriStillPublishedByOthers("file:///any.yammm"))
}

func TestMapper_remapPathToURI_fileURI(t *testing.T) {
	t.Parallel()
	m := newTestMapper()

	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "test.yammm")
	require.NoError(t, os.WriteFile(filePath, []byte("content"), 0o600))

	sourceID, err := location.SourceIDFromAbsolutePath(filePath)
	require.NoError(t, err)

	clientURI := lsputil.PathToURI(filePath)
	open := map[string]*docstate.Document{
		clientURI: makeDoc(clientURI, sourceID, 0),
	}

	result := m.remapPathToURI(lsputil.PathToURI(filePath), open)
	assert.Equal(t, clientURI, result)
}

func TestMapper_remapPathToURI_barePath(t *testing.T) {
	t.Parallel()
	m := newTestMapper()

	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "test.yammm")
	require.NoError(t, os.WriteFile(filePath, []byte("content"), 0o600))

	sourceID, err := location.SourceIDFromAbsolutePath(filePath)
	require.NoError(t, err)

	clientURI := lsputil.PathToURI(filePath)
	open := map[string]*docstate.Document{
		clientURI: makeDoc(clientURI, sourceID, 0),
	}

	// Input is a bare path (SourceID.String() format)
	result := m.remapPathToURI(sourceID.String(), open)
	assert.Equal(t, clientURI, result)
}

func TestMapper_remapPathToURI_symlink(t *testing.T) {
	t.Parallel()
	m := newTestMapper()

	tmpDir := t.TempDir()
	// Canonicalize tmpDir to handle macOS /var -> /private/var symlink
	tmpDir, err := filepath.EvalSymlinks(tmpDir)
	require.NoError(t, err)

	realFile := filepath.Join(tmpDir, "real.yammm")
	linkFile := filepath.Join(tmpDir, "link.yammm")
	require.NoError(t, os.WriteFile(realFile, []byte("content"), 0o600))
	require.NoError(t, os.Symlink(realFile, linkFile))

	sourceID, err := location.SourceIDFromAbsolutePath(realFile)
	require.NoError(t, err)

	clientURI := lsputil.PathToURI(realFile)
	open := map[string]*docstate.Document{
		clientURI: makeDoc(clientURI, sourceID, 0),
	}

	// Input is the symlink path; should resolve to the real file's URI
	result := m.remapPathToURI(linkFile, open)
	assert.Equal(t, clientURI, result)
}

func TestMapper_remapPathToURI_nonFileScheme(t *testing.T) {
	t.Parallel()
	m := newTestMapper()

	open := map[string]*docstate.Document{}

	result := m.remapPathToURI("http://example.com/test", open)
	assert.Equal(t, "http://example.com/test", result)
}

func TestMapper_remapPathToURI_unknownPath(t *testing.T) {
	t.Parallel()
	m := newTestMapper()

	open := map[string]*docstate.Document{}

	// Non-existent path should return a file URI for the cleaned path
	result := m.remapPathToURI("/nonexistent/path/test.yammm", open)
	assert.Equal(t, lsputil.PathToURI("/nonexistent/path/test.yammm"), result)
}

func TestMapper_computePublicationPlan_groupsDiagsByURI(t *testing.T) {
	t.Parallel()
	m := newTestMapper()

	entryURI := "file:///entry.yammm"
	snapshot := &analysis.Snapshot{
		LSPDiagnostics: []analysis.URIDiagnostic{
			{URI: "file:///a.yammm", Diagnostic: protocol.Diagnostic{Message: "err1"}},
			{URI: "file:///a.yammm", Diagnostic: protocol.Diagnostic{Message: "err2"}},
			{URI: "file:///b.yammm", Diagnostic: protocol.Diagnostic{Message: "err3"}},
		},
	}

	open := map[string]*docstate.Document{
		"file:///a.yammm": makeDoc("file:///a.yammm", location.SourceID{}, 0),
		"file:///b.yammm": makeDoc("file:///b.yammm", location.SourceID{}, 1),
	}

	diagsByURI, staleURIs, versionsByURI := m.computePublicationPlan(entryURI, snapshot, open)

	assert.Len(t, diagsByURI["file:///a.yammm"], 2)
	assert.Len(t, diagsByURI["file:///b.yammm"], 1)
	assert.Empty(t, staleURIs)
	assert.NotNil(t, versionsByURI["file:///a.yammm"])
	assert.NotNil(t, versionsByURI["file:///b.yammm"])
}

func TestMapper_computePublicationPlan_tracksStaleURIs(t *testing.T) {
	t.Parallel()
	m := newTestMapper()

	entryURI := "file:///entry.yammm"

	// First publication includes a.yammm and b.yammm
	m.publishedByEntry[entryURI] = map[string]struct{}{
		"file:///a.yammm": {},
		"file:///b.yammm": {},
	}

	// Second publication only includes a.yammm
	snapshot := &analysis.Snapshot{
		LSPDiagnostics: []analysis.URIDiagnostic{
			{URI: "file:///a.yammm", Diagnostic: protocol.Diagnostic{Message: "err1"}},
		},
	}

	open := map[string]*docstate.Document{
		"file:///a.yammm": makeDoc("file:///a.yammm", location.SourceID{}, 0),
		"file:///b.yammm": makeDoc("file:///b.yammm", location.SourceID{}, 1),
	}

	_, staleURIs, versionsByURI := m.computePublicationPlan(entryURI, snapshot, open)

	assert.Contains(t, staleURIs, "file:///b.yammm")
	assert.NotNil(t, versionsByURI["file:///b.yammm"], "stale URI should include version")
}

func TestMapper_computePublicationPlan_remapsRelatedInfo(t *testing.T) {
	t.Parallel()
	m := newTestMapper()

	entryURI := "file:///entry.yammm"
	snapshot := &analysis.Snapshot{
		LSPDiagnostics: []analysis.URIDiagnostic{
			{
				URI: "file:///a.yammm",
				Diagnostic: protocol.Diagnostic{
					Message: "err with related",
					RelatedInformation: []protocol.DiagnosticRelatedInformation{
						{
							Location: protocol.Location{
								URI:   "file:///b.yammm",
								Range: protocol.Range{},
							},
							Message: "related here",
						},
					},
				},
			},
		},
	}

	open := map[string]*docstate.Document{
		"file:///a.yammm": makeDoc("file:///a.yammm", location.SourceID{}, 0),
		"file:///b.yammm": makeDoc("file:///b.yammm", location.SourceID{}, 1),
	}

	diagsByURI, _, _ := m.computePublicationPlan(entryURI, snapshot, open)

	diags := diagsByURI["file:///a.yammm"]
	require.Len(t, diags, 1)
	require.Len(t, diags[0].RelatedInformation, 1)
	assert.Equal(t, "file:///b.yammm", diags[0].RelatedInformation[0].Location.URI)
}
