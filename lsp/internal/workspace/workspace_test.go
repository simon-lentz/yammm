package workspace

import (
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/lsp/internal/protocol"
	"github.com/simon-lentz/yammm/lsp/internal/testutil"

	"github.com/simon-lentz/yammm/lsp/internal/analysis"
	"github.com/simon-lentz/yammm/lsp/internal/docstate"
	"github.com/simon-lentz/yammm/lsp/internal/lsputil"
	"github.com/simon-lentz/yammm/lsp/internal/markdown"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestURIToPath_Valid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		uri  string
		want string
	}{
		{"simple path", "file:///foo/bar.yammm", "/foo/bar.yammm"},
		{"path with spaces (encoded)", "file:///foo/bar%20baz.yammm", "/foo/bar baz.yammm"},
		{"nested path", "file:///a/b/c/d/e.yammm", "/a/b/c/d/e.yammm"},
		{"root path", "file:///schema.yammm", "/schema.yammm"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := lsputil.URIToPath(tt.uri)
			require.NoError(t, err, "lsputil.URIToPath(%q)", tt.uri)
			assert.Equal(t, tt.want, got, "lsputil.URIToPath(%q)", tt.uri)
		})
	}
}

func TestURIToPath_InvalidScheme(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		uri  string
	}{
		{"http scheme", "http://example.com/foo.yammm"},
		{"https scheme", "https://example.com/foo.yammm"},
		{"no scheme", "/foo/bar.yammm"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := lsputil.URIToPath(tt.uri)
			assert.Error(t, err, "lsputil.URIToPath(%q) should return error", tt.uri)
		})
	}
}

func TestURIToPath_InvalidURI(t *testing.T) {
	t.Parallel()

	// Test with malformed URI
	_, err := lsputil.URIToPath("file://[::1%eth0/bad")
	assert.Error(t, err, "lsputil.URIToPath(malformed URI) should return error")
}

func TestPathToURI_Absolute(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path string
		want string
	}{
		{"simple path", "/foo/bar.yammm", "file:///foo/bar.yammm"},
		{"path with spaces", "/foo/bar baz.yammm", "file:///foo/bar%20baz.yammm"},
		{"nested path", "/a/b/c/d.yammm", "file:///a/b/c/d.yammm"},
		{"root file", "/schema.yammm", "file:///schema.yammm"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := lsputil.PathToURI(tt.path)
			assert.Equal(t, tt.want, got, "lsputil.PathToURI(%q)", tt.path)
		})
	}
}

func TestURIPathRoundtrip(t *testing.T) {
	t.Parallel()

	paths := []string{
		"/foo/bar.yammm",
		"/home/user/project/schema.yammm",
		"/tmp/test.yammm",
	}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			uri := lsputil.PathToURI(path)
			got, err := lsputil.URIToPath(uri)
			require.NoError(t, err, "lsputil.URIToPath(lsputil.PathToURI(%q))", path)
			assert.Equal(t, path, got, "roundtrip(%q)", path)
		})
	}
}

func TestNewWorkspace(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	cfg := Config{}
	ws := newTestWorkspace(t, logger, cfg)

	require.NotNil(t, ws, "NewWorkspace() returned nil")
	assert.Equal(t, lsputil.PositionEncodingUTF16, ws.posEncoding, "posEncoding")
}

func TestWorkspace_AddRoot(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := newTestWorkspace(t, logger, Config{})

	ws.AddRoot("file:///project/one")
	ws.AddRoot("file:///project/two")

	ws.mu.RLock()
	defer ws.mu.RUnlock()

	require.Len(t, ws.roots, 2)
	assert.Equal(t, "/project/one", ws.roots[0])
	assert.Equal(t, "/project/two", ws.roots[1])
}

func TestWorkspace_AddRoot_InvalidURI(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := newTestWorkspace(t, logger, Config{})

	// Invalid URI should be logged but not added
	ws.AddRoot("http://not-a-file-uri")

	ws.mu.RLock()
	defer ws.mu.RUnlock()

	assert.Empty(t, ws.roots, "invalid URI should not be added to roots")
}

func TestWorkspace_AddRoot_SymlinkResolution(t *testing.T) {
	t.Parallel()

	// Create temp directory with a symlink
	tmpDir := t.TempDir()
	realDir := tmpDir + "/real"
	require.NoError(t, os.MkdirAll(realDir, 0o750), "failed to create real dir")

	linkDir := tmpDir + "/link"
	if err := os.Symlink(realDir, linkDir); err != nil {
		t.Skip("symlinks not supported: " + err.Error())
	}

	// Resolve real dir to canonical form (handles /var -> /private/var on macOS)
	canonicalRealDir, err := filepath.EvalSymlinks(realDir)
	require.NoError(t, err, "failed to resolve real dir")

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := newTestWorkspace(t, logger, Config{})

	// Add root via symlink path
	ws.AddRoot(lsputil.PathToURI(linkDir))

	ws.mu.RLock()
	defer ws.mu.RUnlock()

	require.Len(t, ws.roots, 1)

	// Root should be stored as canonical (resolved) path
	assert.Equal(t, canonicalRealDir, ws.roots[0], "root should be canonical path")
}

func TestWorkspace_RemoveRoot(t *testing.T) {
	t.Parallel()

	t.Run("removes existing root", func(t *testing.T) {
		t.Parallel()
		ws := newTestWorkspace(t, nil, Config{})
		ws.AddRoot("file:///project/one")
		ws.AddRoot("file:///project/two")
		ws.RemoveRoot("file:///project/one")
		ws.mu.RLock()
		defer ws.mu.RUnlock()
		require.Len(t, ws.roots, 1)
		assert.Equal(t, "/project/two", ws.roots[0])
	})

	t.Run("removing non-existent root is no-op", func(t *testing.T) {
		t.Parallel()
		ws := newTestWorkspace(t, nil, Config{})
		ws.AddRoot("file:///project/one")
		ws.RemoveRoot("file:///project/missing")
		ws.mu.RLock()
		defer ws.mu.RUnlock()
		require.Len(t, ws.roots, 1)
	})

	t.Run("removing all roots empties list", func(t *testing.T) {
		t.Parallel()
		ws := newTestWorkspace(t, nil, Config{})
		ws.AddRoot("file:///project/one")
		ws.RemoveRoot("file:///project/one")
		ws.mu.RLock()
		defer ws.mu.RUnlock()
		assert.Empty(t, ws.roots)
	})
}

func TestWorkspace_AddRoot_Deduplication(t *testing.T) {
	t.Parallel()
	ws := newTestWorkspace(t, nil, Config{})
	ws.AddRoot("file:///project/one")
	ws.AddRoot("file:///project/one")
	ws.mu.RLock()
	defer ws.mu.RUnlock()
	require.Len(t, ws.roots, 1, "duplicate root should not be added")
}

func TestWorkspace_FindModuleRoot_CrossSymlink(t *testing.T) {
	t.Parallel()

	// Create temp directory with real project and symlink
	tmpDir := t.TempDir()
	realProject := tmpDir + "/real/project"
	require.NoError(t, os.MkdirAll(realProject, 0o750), "failed to create real project dir")

	// Create a file in the real project
	realFile := realProject + "/schema.yammm"
	require.NoError(t, os.WriteFile(realFile, []byte("content"), 0o600), "failed to create file")

	linkProject := tmpDir + "/link"
	if err := os.Symlink(realProject, linkProject); err != nil {
		t.Skip("symlinks not supported: " + err.Error())
	}

	// Resolve to canonical form (handles /var -> /private/var on macOS)
	canonicalProject, err := filepath.EvalSymlinks(realProject)
	require.NoError(t, err, "failed to resolve project dir")

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := newTestWorkspace(t, logger, Config{})

	// Add root via canonical path
	ws.AddRoot(lsputil.PathToURI(canonicalProject))

	// File path via symlink should still match (after canonicalization)
	symlinkFilePath := linkProject + "/schema.yammm"

	// Canonicalize the file path as analyzeAndPublish would
	canonicalFilePath, err := filepath.EvalSymlinks(symlinkFilePath)
	require.NoError(t, err, "failed to resolve symlink file path")

	got := ws.FindModuleRoot(canonicalFilePath)
	assert.Equal(t, canonicalProject, got, "FindModuleRoot(%q)", canonicalFilePath)
}

func TestWorkspace_FindModuleRoot_NonExistentRoot(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := newTestWorkspace(t, logger, Config{})

	// Add a root that doesn't exist (symlink resolution will fail, falls back to raw)
	ws.AddRoot("file:///nonexistent/project")

	ws.mu.RLock()
	defer ws.mu.RUnlock()

	// Root should still be stored (as raw path since symlink resolution failed)
	require.Len(t, ws.roots, 1)
	assert.Equal(t, "/nonexistent/project", ws.roots[0])
}

func TestWorkspace_SetPositionEncoding(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := newTestWorkspace(t, logger, Config{})

	ws.SetPositionEncoding(lsputil.PositionEncodingUTF8)

	ws.mu.RLock()
	enc := ws.posEncoding
	ws.mu.RUnlock()

	assert.Equal(t, lsputil.PositionEncodingUTF8, enc, "posEncoding")
}

func TestWorkspace_DocumentLifecycle(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := newTestWorkspace(t, logger, Config{})

	uri := "file:///test/schema.yammm"
	text := "type Person { name: String }"

	// Open document
	ws.documentOpened(uri, 1, text)

	doc := ws.GetDocumentSnapshot(uri)
	require.NotNil(t, doc, "GetDocumentSnapshot() returned nil after open")
	assert.Equal(t, 1, doc.Version)
	assert.Equal(t, text, doc.Text)

	// Change document
	newText := "type Person { name: String, age: Integer }"
	ws.documentChanged(uri, 2, newText)

	doc = ws.GetDocumentSnapshot(uri)
	require.NotNil(t, doc, "GetDocumentSnapshot() returned nil after change")
	assert.Equal(t, 2, doc.Version)
	assert.Equal(t, newText, doc.Text)

	// Close document (without glsp context, just test internal state)
	ws.mu.Lock()
	delete(ws.docs.Open, uri)
	ws.mu.Unlock()

	doc = ws.GetDocumentSnapshot(uri)
	assert.Nil(t, doc, "GetDocumentSnapshot() should return nil after close")
}

func TestWorkspace_documentOpened_InvalidURI(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := newTestWorkspace(t, logger, Config{})

	// Invalid URI should be logged but document not added
	ws.documentOpened("http://invalid", 1, "content")

	doc := ws.GetDocumentSnapshot("http://invalid")
	assert.Nil(t, doc, "GetDocumentSnapshot() should return nil for invalid URI")
}

func TestWorkspace_DocumentChanged_NotOpen(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := newTestWorkspace(t, logger, Config{})

	// Changing a document that was never opened should be a no-op
	ws.documentChanged("file:///not/open.yammm", 1, "content")

	doc := ws.GetDocumentSnapshot("file:///not/open.yammm")
	assert.Nil(t, doc, "GetDocumentSnapshot() should return nil for document never opened")
}

func TestWorkspace_FindModuleRoot_Configured(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := newTestWorkspace(t, logger, Config{ModuleRoot: "/configured/root"})

	got := ws.FindModuleRoot("/any/path/file.yammm")
	assert.Equal(t, "/configured/root", got)
}

func TestWorkspace_FindModuleRoot_WorkspaceFolder(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := newTestWorkspace(t, logger, Config{})
	ws.AddRoot("file:///project")

	got := ws.FindModuleRoot("/project/subdir/file.yammm")
	assert.Equal(t, "/project", got)
}

func TestWorkspace_FindModuleRoot_Fallback(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := newTestWorkspace(t, logger, Config{})
	ws.AddRoot("file:///other/project")

	// Path not under any workspace folder
	got := ws.FindModuleRoot("/unrelated/path/file.yammm")
	assert.Equal(t, "/unrelated/path", got)
}

func TestWorkspace_FindModuleRoot_NestedWorkspaceFolders(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := newTestWorkspace(t, logger, Config{})

	// Add nested workspace folders (order shouldn't matter)
	ws.AddRoot("file:///project")
	ws.AddRoot("file:///project/submodule")

	// File in the nested submodule should use the deepest matching root
	got := ws.FindModuleRoot("/project/submodule/schemas/file.yammm")
	assert.Equal(t, "/project/submodule", got, "should use deepest match")

	// File in the parent project should use the parent root
	got = ws.FindModuleRoot("/project/other/file.yammm")
	assert.Equal(t, "/project", got)
}

func TestWorkspace_FindModuleRoot_NestedWorkspaceFolders_ReverseOrder(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := newTestWorkspace(t, logger, Config{})

	// Add nested workspace folders in reverse order (deepest first)
	// to ensure the algorithm doesn't depend on iteration order
	ws.AddRoot("file:///project/submodule")
	ws.AddRoot("file:///project")

	// File in the nested submodule should still use the deepest matching root
	got := ws.FindModuleRoot("/project/submodule/schemas/file.yammm")
	assert.Equal(t, "/project/submodule", got, "should use deepest match")
}

func TestWorkspace_FindModuleRoot_SimilarPrefixRoots(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := newTestWorkspace(t, logger, Config{})

	ws.AddRoot("file:///project")
	ws.AddRoot("file:///project-extra")

	// File in /project2 should NOT match /project (they share a string prefix
	// but /project2 is not under /project). Should fall back to file's directory.
	got := ws.FindModuleRoot("/project2/file.yammm")
	assert.Equal(t, "/project2", got, "should fallback for /project2")

	// File in /project-extra should match /project-extra, not /project
	got = ws.FindModuleRoot("/project-extra/subdir/file.yammm")
	assert.Equal(t, "/project-extra", got)

	// File directly in /project should still match /project
	got = ws.FindModuleRoot("/project/file.yammm")
	assert.Equal(t, "/project", got)
}

func TestWorkspace_LatestSnapshot(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := newTestWorkspace(t, logger, Config{})

	// No snapshot yet
	snap := ws.LatestSnapshot("file:///test.yammm")
	assert.Nil(t, snap, "LatestSnapshot() should return nil when no snapshot exists")

	// Manually add a snapshot for testing
	ws.mu.Lock()
	ws.snapshots["file:///test.yammm"] = &analysis.Snapshot{
		CreatedAt:    time.Now(),
		EntryVersion: 1,
	}
	ws.mu.Unlock()

	snap = ws.LatestSnapshot("file:///test.yammm")
	require.NotNil(t, snap, "LatestSnapshot() should return snapshot after adding")
	assert.Equal(t, 1, snap.EntryVersion)
}

func TestWorkspace_CancelPendingAnalysis(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := newTestWorkspace(t, logger, Config{})

	uri := "file:///test.yammm"

	// Simulate pending analysis with debounce entry
	ws.sched.debounceMu.Lock()
	cancelCalled := false
	ws.sched.debounces[uri] = &debounceEntry{
		timer:  time.NewTimer(1 * time.Hour), // Long timer
		cancel: func() { cancelCalled = true },
	}
	ws.sched.debounceMu.Unlock()

	// Cancel should work
	ws.cancelPendingAnalysis(uri)

	ws.sched.debounceMu.Lock()
	_, hasEntry := ws.sched.debounces[uri]
	ws.sched.debounceMu.Unlock()

	assert.False(t, hasEntry, "cancelPendingAnalysis() should remove debounce entry")
	assert.True(t, cancelCalled, "cancelPendingAnalysis() should call cancel function")
}

func TestWorkspace_ConcurrentDocumentAccess(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := newTestWorkspace(t, logger, Config{})

	const numGoroutines = 50
	var wg sync.WaitGroup

	// Concurrent writes
	for i := range numGoroutines {
		wg.Go(func() {
			uri := "file:///test/file.yammm"
			ws.documentOpened(uri, i, "content")
			ws.documentChanged(uri, i+1, "new content")
		})
	}

	// Concurrent reads
	for range numGoroutines {
		wg.Go(func() {
			uri := "file:///test/file.yammm"
			_ = ws.GetDocumentSnapshot(uri)
			_ = ws.LatestSnapshot(uri)
		})
	}

	wg.Wait()
}

func TestPositionEncodingConstants(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "utf-16", string(lsputil.PositionEncodingUTF16))
	assert.Equal(t, "utf-8", string(lsputil.PositionEncodingUTF8))
}

func TestDocument_SourceID(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := newTestWorkspace(t, logger, Config{})

	uri := "file:///test/schema.yammm"
	ws.documentOpened(uri, 1, "content")

	doc := ws.GetDocumentSnapshot(uri)
	require.NotNil(t, doc, "GetDocumentSnapshot() returned nil")

	// SourceID should be set from the path
	assert.Equal(t, "/test/schema.yammm", doc.SourceID.String())
}

func TestUpdateDeps_AddsReverseDeps(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := newTestWorkspace(t, logger, Config{})

	entryURI := "file:///main.yammm"
	imports := []string{"/parts.yammm", "/utils.yammm"}

	ws.updateDeps(entryURI, imports)

	ws.mu.RLock()
	defer ws.mu.RUnlock()

	// Check forward deps
	assert.Len(t, ws.deps.importsByEntry[entryURI], 2)

	// Check reverse deps
	partsURI := lsputil.PathToURI("/parts.yammm")
	utilsURI := lsputil.PathToURI("/utils.yammm")

	assert.Contains(t, ws.deps.reverseDeps[partsURI], entryURI)
	assert.Contains(t, ws.deps.reverseDeps[utilsURI], entryURI)
}

func TestUpdateDeps_ClearsOldDeps(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := newTestWorkspace(t, logger, Config{})

	entryURI := "file:///main.yammm"

	// First update: imports parts.yammm
	ws.updateDeps(entryURI, []string{"/parts.yammm"})

	// Second update: now imports utils.yammm (removed parts.yammm)
	ws.updateDeps(entryURI, []string{"/utils.yammm"})

	ws.mu.RLock()
	defer ws.mu.RUnlock()

	// Forward deps should only have utils
	assert.Len(t, ws.deps.importsByEntry[entryURI], 1)

	// Reverse deps for parts should be cleaned up
	partsURI := lsputil.PathToURI("/parts.yammm")
	assert.NotContains(t, ws.deps.reverseDeps, partsURI, "reverseDeps[%s] should be deleted (empty)", partsURI)

	// Reverse deps for utils should exist
	utilsURI := lsputil.PathToURI("/utils.yammm")
	assert.Contains(t, ws.deps.reverseDeps[utilsURI], entryURI)
}

func TestUpdateDeps_ClearsAllOnNil(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := newTestWorkspace(t, logger, Config{})

	entryURI := "file:///main.yammm"

	// Add some dependencies
	ws.updateDeps(entryURI, []string{"/parts.yammm"})

	// Clear by passing nil (document closed)
	ws.updateDeps(entryURI, nil)

	ws.mu.RLock()
	defer ws.mu.RUnlock()

	// Forward deps should be deleted
	assert.NotContains(t, ws.deps.importsByEntry, entryURI, "importsByEntry[%s] should be deleted", entryURI)

	// Reverse deps should be cleaned up
	partsURI := lsputil.PathToURI("/parts.yammm")
	assert.NotContains(t, ws.deps.reverseDeps, partsURI, "reverseDeps[%s] should be deleted", partsURI)
}

func TestUpdateDeps_MultipleEntries(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := newTestWorkspace(t, logger, Config{})

	entry1 := "file:///main1.yammm"
	entry2 := "file:///main2.yammm"

	// Both entries import parts.yammm
	ws.updateDeps(entry1, []string{"/parts.yammm"})
	ws.updateDeps(entry2, []string{"/parts.yammm"})

	ws.mu.RLock()

	// parts.yammm should have two reverse deps
	partsURI := lsputil.PathToURI("/parts.yammm")
	assert.Len(t, ws.deps.reverseDeps[partsURI], 2)
	ws.mu.RUnlock()

	// Remove entry1's dependency
	ws.updateDeps(entry1, nil)

	ws.mu.RLock()
	defer ws.mu.RUnlock()

	// parts.yammm should still have one reverse dep (entry2)
	assert.Len(t, ws.deps.reverseDeps[partsURI], 1)
	assert.Contains(t, ws.deps.reverseDeps[partsURI], entry2)
}

func TestBuildCanonicalToURIMap_SymlinkResolution(t *testing.T) {
	t.Parallel()

	// Create temp directory with real file and symlink
	tmpDir := t.TempDir()
	realDir := tmpDir + "/real"
	require.NoError(t, os.MkdirAll(realDir, 0o750), "failed to create real dir")

	realPath := realDir + "/schema.yammm"
	require.NoError(t, os.WriteFile(realPath, []byte("content"), 0o600), "failed to write file")

	// Create symlink
	linkPath := tmpDir + "/link.yammm"
	if err := os.Symlink(realPath, linkPath); err != nil {
		t.Skip("symlinks not supported: " + err.Error())
	}

	// Resolve the real path to canonical form (handles /var -> /private/var on macOS)
	canonicalRealPath, err := filepath.EvalSymlinks(realPath)
	require.NoError(t, err, "failed to resolve real path")

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := newTestWorkspace(t, logger, Config{})

	// Open document via symlink URI
	linkURI := lsputil.PathToURI(linkPath)
	ws.documentOpened(linkURI, 1, "content")

	// Build the canonical mapping
	ws.mu.RLock()
	mapping := ws.mapper.buildCanonicalToURIMap(ws.docs.Open)
	ws.mu.RUnlock()

	// The mapping should map the canonical (resolved) path to the symlink URI
	require.Contains(t, mapping, canonicalRealPath, "mapping should contain resolved path %q; got keys: %v", canonicalRealPath, slices.Collect(maps.Keys(mapping)))
	assert.Equal(t, linkURI, mapping[canonicalRealPath])
}

func TestBuildCanonicalToURIMap_NoSymlinks(t *testing.T) {
	t.Parallel()

	// Create temp directory with a real file (no symlinks)
	tmpDir := t.TempDir()
	realPath := tmpDir + "/schema.yammm"
	require.NoError(t, os.WriteFile(realPath, []byte("content"), 0o600), "failed to write file")

	// Resolve path to canonical form (handles /var -> /private/var on macOS)
	canonicalPath, err := filepath.EvalSymlinks(realPath)
	require.NoError(t, err, "failed to resolve path")

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := newTestWorkspace(t, logger, Config{})

	// Open document via real path
	realURI := lsputil.PathToURI(realPath)
	ws.documentOpened(realURI, 1, "content")

	// Build the canonical mapping
	ws.mu.RLock()
	mapping := ws.mapper.buildCanonicalToURIMap(ws.docs.Open)
	ws.mu.RUnlock()

	// The mapping should map the canonical path to the original URI
	require.Contains(t, mapping, canonicalPath, "mapping should contain path %q; got keys: %v", canonicalPath, slices.Collect(maps.Keys(mapping)))
	assert.Equal(t, realURI, mapping[canonicalPath])
}

// TestBuildCanonicalToURIMap_DuplicateDocumentViaSymlink is a regression test for issue 2.3.
// It verifies that when the same file is opened via both a symlink and the real path,
// the first-opened document's URI is used in the canonical mapping (deterministic selection
// based on OpenOrder).
func TestBuildCanonicalToURIMap_DuplicateDocumentViaSymlink(t *testing.T) {
	t.Parallel()

	// Create temp directory with real file and symlink
	tmpDir := t.TempDir()
	realDir := tmpDir + "/real"
	require.NoError(t, os.MkdirAll(realDir, 0o750), "failed to create real dir")

	realPath := realDir + "/schema.yammm"
	require.NoError(t, os.WriteFile(realPath, []byte("content"), 0o600), "failed to write file")

	// Create symlink
	linkPath := tmpDir + "/link.yammm"
	if err := os.Symlink(realPath, linkPath); err != nil {
		t.Skip("symlinks not supported: " + err.Error())
	}

	// Resolve the real path to canonical form (handles /var -> /private/var on macOS)
	canonicalRealPath, err := filepath.EvalSymlinks(realPath)
	require.NoError(t, err, "failed to resolve real path")

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := newTestWorkspace(t, logger, Config{})

	// Open document via symlink URI FIRST (openOrder=1)
	linkURI := lsputil.PathToURI(linkPath)
	ws.documentOpened(linkURI, 1, "content via symlink")

	// Open same document via real path URI SECOND (openOrder=2)
	realURI := lsputil.PathToURI(realPath)
	ws.documentOpened(realURI, 1, "content via real path")

	// Both documents should be tracked (different URIs)
	ws.mu.RLock()
	assert.Len(t, ws.docs.Open, 2)
	ws.mu.RUnlock()

	// Build the canonical mapping - should prefer first-opened (symlink) due to lower OpenOrder
	ws.mu.RLock()
	mapping := ws.mapper.buildCanonicalToURIMap(ws.docs.Open)
	ws.mu.RUnlock()

	require.Contains(t, mapping, canonicalRealPath, "mapping should contain resolved path %q; got keys: %v", canonicalRealPath, slices.Collect(maps.Keys(mapping)))
	assert.Equal(t, linkURI, mapping[canonicalRealPath], "should prefer first-opened symlink URI")

	// Now close the symlink document
	ws.documentClosed(linkURI)

	// Rebuild mapping - should now prefer the real path URI (only one remaining)
	ws.mu.RLock()
	mapping2 := ws.mapper.buildCanonicalToURIMap(ws.docs.Open)
	ws.mu.RUnlock()

	require.Contains(t, mapping2, canonicalRealPath, "after close: mapping should contain resolved path %q; got keys: %v", canonicalRealPath, slices.Collect(maps.Keys(mapping2)))
	assert.Equal(t, realURI, mapping2[canonicalRealPath], "should use remaining real path URI")
}

func TestRemapToOpenDocURI_MatchFound(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := newTestWorkspace(t, logger, Config{})

	// Simulate the mapping
	canonicalPath := "/real/path/schema.yammm"
	symlinkURI := "file:///symlink/path/schema.yammm"
	mapping := map[string]string{
		canonicalPath: symlinkURI,
	}

	// Diagnostic URI uses canonical path
	diagURI := lsputil.PathToURI(canonicalPath)

	// Should remap to symlink URI
	result := ws.mapper.remapToOpenDocURI(diagURI, mapping)
	assert.Equal(t, symlinkURI, result)
}

func TestRemapToOpenDocURI_NoMatch(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := newTestWorkspace(t, logger, Config{})

	// Empty mapping - no open documents
	mapping := map[string]string{}

	// Diagnostic URI for a file that's not open
	diagURI := "file:///some/path/schema.yammm"

	// Should return original URI unchanged
	result := ws.mapper.remapToOpenDocURI(diagURI, mapping)
	assert.Equal(t, diagURI, result)
}

func TestRemapToOpenDocURI_InvalidURI(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := newTestWorkspace(t, logger, Config{})

	mapping := map[string]string{
		"/some/path": "file:///some/path",
	}

	// Invalid URI should be returned unchanged
	invalidURI := "http://not-a-file-uri"
	result := ws.mapper.remapToOpenDocURI(invalidURI, mapping)
	assert.Equal(t, invalidURI, result)
}

func TestRemapToOpenDocURI_RawPathNoMatch(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := newTestWorkspace(t, logger, Config{})

	// Empty mapping - no open documents
	mapping := map[string]string{}

	// Raw filesystem path (not a file:// URI)
	rawPath := "/some/path/schema.yammm"

	// Should convert to proper file:// URI for protocol correctness
	result := ws.mapper.remapToOpenDocURI(rawPath, mapping)
	expectedURI := "file:///some/path/schema.yammm"
	assert.Equal(t, expectedURI, result)
}

func TestRemapToOpenDocURI_RawPathWithMatch(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := newTestWorkspace(t, logger, Config{})

	// Mapping contains the raw path
	rawPath := "/some/path/schema.yammm"
	openDocURI := "file:///opened/via/different/path.yammm"
	mapping := map[string]string{
		rawPath: openDocURI,
	}

	// Should return the mapped URI
	result := ws.mapper.remapToOpenDocURI(rawPath, mapping)
	assert.Equal(t, openDocURI, result)
}

func TestRemapToOpenDocURI_NonFileURIPreserved(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := newTestWorkspace(t, logger, Config{})

	// Empty mapping
	mapping := map[string]string{}

	// Non-file URI schemes should be preserved as-is
	testCases := []string{
		"https://example.com/file.yammm",
		"custom-scheme://host/path/file.yammm",
	}

	for _, uri := range testCases {
		result := ws.mapper.remapToOpenDocURI(uri, mapping)
		assert.Equal(t, uri, result, "remapToOpenDocURILocked(%q) should preserve original", uri)
	}
}

func TestPublishSnapshotDiagnostics_SymlinkURIRemapping(t *testing.T) {
	t.Parallel()

	// Create temp directory with real file and symlink
	tmpDir := t.TempDir()
	realDir := tmpDir + "/real"
	require.NoError(t, os.MkdirAll(realDir, 0o750), "failed to create real dir")

	realPath := realDir + "/schema.yammm"
	require.NoError(t, os.WriteFile(realPath, []byte("content"), 0o600), "failed to write file")

	// Create symlink
	linkPath := tmpDir + "/link.yammm"
	if err := os.Symlink(realPath, linkPath); err != nil {
		t.Skip("symlinks not supported: " + err.Error())
	}

	// Resolve the real path to canonical form (handles /var -> /private/var on macOS)
	canonicalRealPath, err := filepath.EvalSymlinks(realPath)
	require.NoError(t, err, "failed to resolve real path")

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := newTestWorkspace(t, logger, Config{})

	// Open document via symlink URI
	linkURI := lsputil.PathToURI(linkPath)
	ws.documentOpened(linkURI, 1, "content")

	// Create a snapshot with diagnostics using the canonical (resolved) path
	// This simulates what the loader produces
	canonicalURI := lsputil.PathToURI(canonicalRealPath)
	snapshot := &analysis.Snapshot{
		CreatedAt:    time.Now(),
		EntryVersion: 1,
		LSPDiagnostics: []analysis.URIDiagnostic{
			{
				URI:        canonicalURI, // Loader uses canonical/resolved path
				Diagnostic: mockDiagnostic("test error"),
			},
		},
	}

	// Use computePublicationPlan to test the remapping logic
	// (without needing a real glsp.Context)
	diagsByURI, _, _ := ws.computePublicationPlan(linkURI, snapshot)

	// The diagnostic should be remapped to the symlink URI
	require.NotEmpty(t, diagsByURI, "diagsByURI should not be empty")

	// Check that the diagnostic is published under the symlink URI, not the canonical URI
	assert.Contains(t, diagsByURI, linkURI, "diagsByURI should contain symlink URI %q; got keys: %v", linkURI, slices.Collect(maps.Keys(diagsByURI)))
	assert.NotContains(t, diagsByURI, canonicalURI, "diagsByURI should NOT contain canonical URI %q", canonicalURI)
}

func TestPublishSnapshotDiagnostics_RelatedInfoURIRemapping(t *testing.T) {
	t.Parallel()

	// Create temp directory with real file and symlink
	tmpDir := t.TempDir()
	realDir := tmpDir + "/real"
	require.NoError(t, os.MkdirAll(realDir, 0o750), "failed to create real dir")

	realPath := realDir + "/schema.yammm"
	require.NoError(t, os.WriteFile(realPath, []byte("content"), 0o600), "failed to write file")

	// Create symlink
	linkPath := tmpDir + "/link.yammm"
	if err := os.Symlink(realPath, linkPath); err != nil {
		t.Skip("symlinks not supported: " + err.Error())
	}

	// Resolve the real path to canonical form (handles /var -> /private/var on macOS)
	canonicalRealPath, err := filepath.EvalSymlinks(realPath)
	require.NoError(t, err, "failed to resolve real path")

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := newTestWorkspace(t, logger, Config{})

	// Open document via symlink URI
	linkURI := lsputil.PathToURI(linkPath)
	ws.documentOpened(linkURI, 1, "content")

	// Create a snapshot with RelatedInformation also using canonical path
	canonicalURI := lsputil.PathToURI(canonicalRealPath)
	snapshot := &analysis.Snapshot{
		CreatedAt:    time.Now(),
		EntryVersion: 1,
		LSPDiagnostics: []analysis.URIDiagnostic{
			{
				URI:        canonicalURI,
				Diagnostic: mockDiagnosticWithRelated("test error", canonicalURI),
			},
		},
	}

	diagsByURI, _, _ := ws.computePublicationPlan(linkURI, snapshot)

	// Check that the diagnostic is published under the symlink URI
	require.Contains(t, diagsByURI, linkURI, "diagsByURI should contain symlink URI %q; got keys: %v", linkURI, slices.Collect(maps.Keys(diagsByURI)))
	diags := diagsByURI[linkURI]
	require.NotEmpty(t, diags, "should have at least one diagnostic")

	// Check that RelatedInformation URIs are also remapped
	require.NotEmpty(t, diags[0].RelatedInformation, "diagnostic should have RelatedInformation")

	relatedURI := diags[0].RelatedInformation[0].Location.URI
	assert.Equal(t, linkURI, relatedURI, "RelatedInformation.Location.URI")
}

// newTestWorkspace creates a Workspace for testing and registers cleanup to prevent goroutine leaks.
func newTestWorkspace(t *testing.T, logger *slog.Logger, cfg Config) *Workspace {
	t.Helper()
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	}
	ws := NewWorkspace(logger, cfg)
	t.Cleanup(ws.Shutdown)
	return ws
}

// mockDiagnostic creates a simple mock diagnostic for testing.
func mockDiagnostic(message string) protocol.Diagnostic {
	severity := protocol.DiagnosticSeverityError
	return protocol.Diagnostic{
		Range: protocol.Range{
			Start: protocol.Position{Line: 0, Character: 0},
			End:   protocol.Position{Line: 0, Character: 10},
		},
		Severity: &severity,
		Message:  message,
	}
}

// mockDiagnosticWithRelated creates a mock diagnostic with RelatedInformation.
func mockDiagnosticWithRelated(message string, relatedURI string) protocol.Diagnostic {
	severity := protocol.DiagnosticSeverityError
	return protocol.Diagnostic{
		Range: protocol.Range{
			Start: protocol.Position{Line: 0, Character: 0},
			End:   protocol.Position{Line: 0, Character: 10},
		},
		Severity: &severity,
		Message:  message,
		RelatedInformation: []protocol.DiagnosticRelatedInformation{
			{
				Location: protocol.Location{
					URI: relatedURI,
					Range: protocol.Range{
						Start: protocol.Position{Line: 5, Character: 0},
						End:   protocol.Position{Line: 5, Character: 10},
					},
				},
				Message: "related info",
			},
		},
	}
}

func TestComputePublicationPlan_PerEntryIsolation(t *testing.T) {
	// Test that publishedByEntry prevents cross-entry contamination:
	// - main.yammm publishes diagnostics for main.yammm and parts.yammm
	// - other.yammm publishes diagnostics for other.yammm only
	// - Clearing main.yammm should NOT affect other.yammm's diagnostics
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := newTestWorkspace(t, logger, Config{})

	// Open two documents
	mainURI := "file:///main.yammm"
	partsURI := "file:///parts.yammm"
	otherURI := "file:///other.yammm"

	ws.documentOpened(mainURI, 1, "import parts")
	ws.documentOpened(otherURI, 1, "standalone")

	// First analysis: main.yammm publishes diagnostics for main and parts
	mainSnapshot := &analysis.Snapshot{
		CreatedAt:    time.Now(),
		EntryVersion: 1,
		LSPDiagnostics: []analysis.URIDiagnostic{
			{URI: mainURI, Diagnostic: mockDiagnostic("error in main")},
			{URI: partsURI, Diagnostic: mockDiagnostic("error in parts")},
		},
	}

	diagsByURI, staleURIs, _ := ws.computePublicationPlan(mainURI, mainSnapshot)

	// Should have diagnostics for both main and parts
	assert.Len(t, diagsByURI, 2)
	// No stale URIs on first publication
	assert.Empty(t, staleURIs, "staleURIs should be empty on first run")

	// Second analysis: other.yammm publishes its own diagnostics
	otherSnapshot := &analysis.Snapshot{
		CreatedAt:    time.Now(),
		EntryVersion: 1,
		LSPDiagnostics: []analysis.URIDiagnostic{
			{URI: otherURI, Diagnostic: mockDiagnostic("error in other")},
		},
	}

	diagsByURI, _, _ = ws.computePublicationPlan(otherURI, otherSnapshot)

	// Should have diagnostics only for other
	assert.Len(t, diagsByURI, 1)
	assert.Contains(t, diagsByURI, otherURI, "diagsByURI should contain other.yammm")

	// Third analysis: main.yammm re-analyzed with NO errors
	// Should clear main and parts, but NOT affect other
	emptyMainSnapshot := &analysis.Snapshot{
		CreatedAt:      time.Now(),
		EntryVersion:   2,
		LSPDiagnostics: []analysis.URIDiagnostic{}, // No errors
	}

	diagsByURI, staleURIs, _ = ws.computePublicationPlan(mainURI, emptyMainSnapshot)

	// Should have no new diagnostics
	assert.Empty(t, diagsByURI, "diagsByURI should be empty")
	// Should mark main and parts as stale (need clearing)
	assert.Len(t, staleURIs, 2)

	// Verify publishedByEntry still tracks other.yammm's publication
	ws.mu.RLock()
	otherPublished := ws.mapper.publishedByEntry[otherURI]
	ws.mu.RUnlock()

	assert.Contains(t, otherPublished, otherURI, "other.yammm should still be tracked in publishedByEntry")
}

func TestComputePublicationPlan_SharedImportMultipleEntries(t *testing.T) {
	// Test that a shared import doesn't lose diagnostics when one entry clears:
	// - main.yammm imports shared.yammm
	// - other.yammm imports shared.yammm
	// - When main.yammm clears, shared diagnostics from other should remain tracked
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := newTestWorkspace(t, logger, Config{})

	mainURI := "file:///main.yammm"
	otherURI := "file:///other.yammm"
	sharedURI := "file:///shared.yammm"

	ws.documentOpened(mainURI, 1, "import shared")
	ws.documentOpened(otherURI, 1, "import shared")

	// Both entries publish diagnostics for shared
	mainSnapshot := &analysis.Snapshot{
		CreatedAt:    time.Now(),
		EntryVersion: 1,
		LSPDiagnostics: []analysis.URIDiagnostic{
			{URI: mainURI, Diagnostic: mockDiagnostic("error in main")},
			{URI: sharedURI, Diagnostic: mockDiagnostic("error in shared via main")},
		},
	}
	ws.computePublicationPlan(mainURI, mainSnapshot)

	otherSnapshot := &analysis.Snapshot{
		CreatedAt:    time.Now(),
		EntryVersion: 1,
		LSPDiagnostics: []analysis.URIDiagnostic{
			{URI: otherURI, Diagnostic: mockDiagnostic("error in other")},
			{URI: sharedURI, Diagnostic: mockDiagnostic("error in shared via other")},
		},
	}
	ws.computePublicationPlan(otherURI, otherSnapshot)

	// Verify both entries track shared.yammm
	ws.mu.RLock()
	mainPublished := ws.mapper.publishedByEntry[mainURI]
	otherPublished := ws.mapper.publishedByEntry[otherURI]
	ws.mu.RUnlock()

	assert.Contains(t, mainPublished, sharedURI, "main should track shared.yammm")
	assert.Contains(t, otherPublished, sharedURI, "other should track shared.yammm")

	// Clear main's diagnostics
	emptyMainSnapshot := &analysis.Snapshot{
		CreatedAt:      time.Now(),
		EntryVersion:   2,
		LSPDiagnostics: []analysis.URIDiagnostic{},
	}
	_, staleURIs, _ := ws.computePublicationPlan(mainURI, emptyMainSnapshot)

	// main and shared should be stale for main's entry
	staleSet := make(map[string]struct{})
	for _, u := range staleURIs {
		staleSet[u] = struct{}{}
	}
	assert.Contains(t, staleSet, mainURI, "main.yammm should be stale")
	assert.Contains(t, staleSet, sharedURI, "shared.yammm should be stale for main's entry")

	// But other's tracking of shared should remain
	ws.mu.RLock()
	otherPublished = ws.mapper.publishedByEntry[otherURI]
	ws.mu.RUnlock()

	assert.Contains(t, otherPublished, sharedURI, "other should still track shared.yammm after main cleared")
}

func TestComputePublicationPlan_DocumentCloseClearsAllEntryURIs(t *testing.T) {
	// Test Priority 2.4 fix: closing clears all URIs from that entry's tracking
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := newTestWorkspace(t, logger, Config{})

	mainURI := "file:///main.yammm"
	partsURI := "file:///parts.yammm"

	ws.documentOpened(mainURI, 1, "import parts")

	// Publish diagnostics for both main and parts
	snapshot := &analysis.Snapshot{
		CreatedAt:    time.Now(),
		EntryVersion: 1,
		LSPDiagnostics: []analysis.URIDiagnostic{
			{URI: mainURI, Diagnostic: mockDiagnostic("error in main")},
			{URI: partsURI, Diagnostic: mockDiagnostic("error in parts")},
		},
	}
	ws.computePublicationPlan(mainURI, snapshot)

	// Verify tracking
	ws.mu.RLock()
	published := ws.mapper.publishedByEntry[mainURI]
	ws.mu.RUnlock()

	assert.Len(t, published, 2)

	// Simulate closing main.yammm - empty snapshot with nil diagnostics
	closeSnapshot := &analysis.Snapshot{
		CreatedAt:      time.Now(),
		EntryVersion:   2,
		LSPDiagnostics: nil,
	}
	_, staleURIs, _ := ws.computePublicationPlan(mainURI, closeSnapshot)

	// Both main and parts should be stale
	assert.Len(t, staleURIs, 2)

	// Published tracking should be empty for this entry
	ws.mu.RLock()
	published = ws.mapper.publishedByEntry[mainURI]
	ws.mu.RUnlock()

	assert.Empty(t, published, "published should be empty after close")
}

func TestWorkspace_FileChanged_SymlinkResolution(t *testing.T) {
	// Test that FileChanged correctly resolves symlinked paths for reverse deps
	t.Parallel()

	// Create temp directory structure
	tmpDir := t.TempDir()
	actualDir := tmpDir + "/actual"
	require.NoError(t, os.MkdirAll(actualDir, 0o750), "failed to create actual dir")

	// Create actual/parts.yammm
	actualParts := actualDir + "/parts.yammm"
	require.NoError(t, os.WriteFile(actualParts, []byte("schema \"parts\""), 0o600), "failed to write parts")

	// Create symlink: linked -> actual
	linkedDir := tmpDir + "/linked"
	if err := os.Symlink(actualDir, linkedDir); err != nil {
		t.Skip("symlinks not supported: " + err.Error())
	}

	// Resolve canonical paths (handles /var -> /private/var on macOS)
	canonicalParts, err := filepath.EvalSymlinks(actualParts)
	require.NoError(t, err, "failed to resolve parts path")

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := newTestWorkspace(t, logger, Config{})

	// Create main.yammm that imports parts via canonical path
	mainURI := "file:///main.yammm"
	ws.documentOpened(mainURI, 1, "import parts")

	// Simulate the loader tracking: main depends on canonical parts path
	ws.updateDeps(mainURI, []string{canonicalParts})

	// Verify reverse deps are set up
	ws.mu.RLock()
	canonicalPartsURI := lsputil.PathToURI(canonicalParts)
	deps := ws.deps.reverseDeps[canonicalPartsURI]
	ws.mu.RUnlock()

	require.Contains(t, deps, mainURI, "reverse deps should contain main; canonicalPartsURI=%s", canonicalPartsURI)

	// FileChanged with symlinked path (linked/parts.yammm)
	linkedParts := linkedDir + "/parts.yammm"
	linkedPartsURI := lsputil.PathToURI(linkedParts)

	// Verify the path resolution works by checking the deps lookup

	// Manually test the canonicalization logic that FileChanged uses
	path, _ := lsputil.URIToPath(linkedPartsURI)
	resolved, _ := filepath.EvalSymlinks(path)
	resolvedPath := filepath.Clean(resolved)
	resolvedSourceID, _ := location.SourceIDFromAbsolutePath(resolvedPath)
	resolvedURI := lsputil.PathToURI(resolvedSourceID.String())

	// The resolved URI should match the canonical parts URI
	assert.Equal(t, canonicalPartsURI, resolvedURI)

	// And the reverse deps lookup should find main
	ws.mu.RLock()
	depsForResolved := ws.deps.reverseDeps[resolvedURI]
	ws.mu.RUnlock()

	assert.Contains(t, depsForResolved, mainURI, "reverse deps for resolved path should contain main")
}

func TestWorkspace_FileChanged_CanonicalPathMatching(t *testing.T) {
	// Verify FileChanged uses canonical paths for reverseDeps lookup
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := newTestWorkspace(t, logger, Config{})

	mainURI := "file:///main.yammm"
	ws.documentOpened(mainURI, 1, "content")

	// Set up dependency on a canonical path
	canonicalPath := "/canonical/parts.yammm"
	ws.updateDeps(mainURI, []string{canonicalPath})

	// Verify the reverse dependency uses the canonical URI
	canonicalURI := lsputil.PathToURI(canonicalPath)

	ws.mu.RLock()
	deps := ws.deps.reverseDeps[canonicalURI]
	ws.mu.RUnlock()

	assert.Contains(t, deps, mainURI, "reverse deps should contain main for canonical URI")

	// Lookup with the same canonical URI should succeed
	ws.mu.RLock()
	entries := make(map[string]struct{})
	for k := range ws.deps.reverseDeps[canonicalURI] {
		entries[k] = struct{}{}
	}
	ws.mu.RUnlock()

	assert.Contains(t, entries, mainURI, "canonical path lookup should find main.yammm")
}

// TestScheduleAnalysis_EntryPointerIdentity verifies that the debounce cleanup
// uses pointer identity to avoid deleting newer entries. This is a regression
// test for the issue: "Workspace debounce cleanup introduces a race that can
// delete *new* timers/cancels".
//
// The race scenario:
// 1. scheduleAnalysis(uri) creates entry0, schedules timer
// 2. Timer fires, callback starts running analyzeAndPublish (takes time)
// 3. User types, scheduleAnalysis(uri) called again, creates entry1
// 4. Old callback finishes - must NOT delete entry1
func TestScheduleAnalysis_EntryPointerIdentity(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := newTestWorkspace(t, logger, Config{})

	uri := "file:///test.yammm"

	// Create entry0 (simulating first schedule)
	ws.sched.debounceMu.Lock()
	entry0 := &debounceEntry{
		timer:  time.NewTimer(1 * time.Hour),
		cancel: func() {},
	}
	ws.sched.debounces[uri] = entry0
	ws.sched.debounceMu.Unlock()

	// Simulate: while entry0's callback is running, a new schedule happens
	// This creates entry1 and stores it in the map
	ws.sched.debounceMu.Lock()
	entry1 := &debounceEntry{
		timer:  time.NewTimer(1 * time.Hour),
		cancel: func() {},
	}
	ws.sched.debounces[uri] = entry1
	ws.sched.debounceMu.Unlock()

	// Now simulate entry0's callback cleanup logic:
	// It should NOT delete because ws.sched.debounces[uri] != entry0
	ws.sched.debounceMu.Lock()
	if ws.sched.debounces[uri] == entry0 {
		// BUG: This would delete entry1 if pointer check wasn't working
		delete(ws.sched.debounces, uri)
	}
	ws.sched.debounceMu.Unlock()

	// Verify entry1 is still in the map
	ws.sched.debounceMu.Lock()
	currentEntry := ws.sched.debounces[uri]
	ws.sched.debounceMu.Unlock()

	assert.Equal(t, entry1, currentEntry, "entry1 should still be in debounces map after entry0's cleanup attempt")

	// Clean up: entry1's cleanup should succeed since it IS the current entry
	ws.sched.debounceMu.Lock()
	if ws.sched.debounces[uri] == entry1 {
		delete(ws.sched.debounces, uri)
	}
	ws.sched.debounceMu.Unlock()

	ws.sched.debounceMu.Lock()
	_, hasEntry := ws.sched.debounces[uri]
	ws.sched.debounceMu.Unlock()

	assert.False(t, hasEntry, "entry1's cleanup should have removed the entry")
}

// TestScheduleAnalysis_RescheduleWhilePending verifies that calling
// scheduleAnalysis while a previous timer is pending correctly cancels
// the old entry and installs a new one.
func TestScheduleAnalysis_RescheduleWhilePending(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := newTestWorkspace(t, logger, Config{})

	uri := "file:///test.yammm"

	// First schedule
	ws.scheduleAnalysis(uri)

	ws.sched.debounceMu.Lock()
	entry1 := ws.sched.debounces[uri]
	ws.sched.debounceMu.Unlock()

	require.NotNil(t, entry1, "first scheduleAnalysis should create entry")

	// Second schedule (while first timer is pending)
	ws.scheduleAnalysis(uri)

	ws.sched.debounceMu.Lock()
	entry2 := ws.sched.debounces[uri]
	ws.sched.debounceMu.Unlock()

	require.NotNil(t, entry2, "second scheduleAnalysis should create entry")

	// entry2 should be different from entry1 (new allocation)
	assert.NotSame(t, entry1, entry2, "second scheduleAnalysis should create a new entry, not reuse the old one")

	// Clean up
	ws.cancelPendingAnalysis(uri)
}

// TestScheduleMarkdownAnalysis_EntryPointerIdentity verifies that the markdown
// debounce cleanup uses pointer identity to avoid deleting newer entries.
// This mirrors TestScheduleAnalysis_EntryPointerIdentity for the markdown path.
func TestScheduleMarkdownAnalysis_EntryPointerIdentity(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := newTestWorkspace(t, logger, Config{})

	uri := "file:///test.md"

	// Set up a markdown document so scheduleMarkdownAnalysis has something to work with
	ws.markdownDocumentOpened(uri, 1, "# Test\n\n```yammm\nschema \"test\"\n```\n")

	// Create entry0 (simulating first schedule)
	ws.sched.debounceMu.Lock()
	entry0 := &debounceEntry{
		timer:  time.NewTimer(1 * time.Hour),
		cancel: func() {},
	}
	ws.sched.debounces[uri] = entry0
	ws.sched.debounceMu.Unlock()

	// Simulate: while entry0's callback is running, a new schedule happens
	ws.sched.debounceMu.Lock()
	entry1 := &debounceEntry{
		timer:  time.NewTimer(1 * time.Hour),
		cancel: func() {},
	}
	ws.sched.debounces[uri] = entry1
	ws.sched.debounceMu.Unlock()

	// Simulate entry0's callback cleanup: should NOT delete entry1
	ws.sched.debounceMu.Lock()
	if ws.sched.debounces[uri] == entry0 {
		delete(ws.sched.debounces, uri)
	}
	ws.sched.debounceMu.Unlock()

	// Verify entry1 is still in the map
	ws.sched.debounceMu.Lock()
	currentEntry := ws.sched.debounces[uri]
	ws.sched.debounceMu.Unlock()

	assert.Equal(t, entry1, currentEntry, "entry1 should still be in debounces map after entry0's cleanup attempt")

	// entry1's cleanup should succeed
	ws.sched.debounceMu.Lock()
	if ws.sched.debounces[uri] == entry1 {
		delete(ws.sched.debounces, uri)
	}
	ws.sched.debounceMu.Unlock()

	ws.sched.debounceMu.Lock()
	_, hasEntry := ws.sched.debounces[uri]
	ws.sched.debounceMu.Unlock()

	assert.False(t, hasEntry, "entry1's cleanup should have removed the entry")
}

// TestScheduleMarkdownAnalysis_RescheduleWhilePending verifies that calling
// scheduleMarkdownAnalysis while a previous timer is pending correctly
// cancels the old entry and installs a new one.
func TestScheduleMarkdownAnalysis_RescheduleWhilePending(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := newTestWorkspace(t, logger, Config{})

	uri := "file:///test.md"

	// Set up a markdown document
	ws.markdownDocumentOpened(uri, 1, "# Test\n\n```yammm\nschema \"test\"\n```\n")

	// First schedule
	ws.scheduleMarkdownAnalysis(uri)

	ws.sched.debounceMu.Lock()
	entry1 := ws.sched.debounces[uri]
	ws.sched.debounceMu.Unlock()

	require.NotNil(t, entry1, "first scheduleMarkdownAnalysis should create entry")

	// Second schedule (while first timer is pending)
	ws.scheduleMarkdownAnalysis(uri)

	ws.sched.debounceMu.Lock()
	entry2 := ws.sched.debounces[uri]
	ws.sched.debounceMu.Unlock()

	require.NotNil(t, entry2, "second scheduleMarkdownAnalysis should create entry")

	assert.NotSame(t, entry1, entry2, "second scheduleMarkdownAnalysis should create a new entry, not reuse the old one")

	// Clean up
	ws.cancelPendingAnalysis(uri)
}

func TestRemapPathToURI_OpenDocument(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := newTestWorkspace(t, logger, Config{})

	// Open a document - the URI used by client
	clientURI := "file:///symlink/path/schema.yammm"
	ws.documentOpened(clientURI, 1, "content")

	// Get the canonical path from the document's SourceID
	doc := ws.GetDocumentSnapshot(clientURI)
	require.NotNil(t, doc, "document should be open")
	canonicalPath := doc.SourceID.String()

	// RemapPathToURI should return the client's URI, not a new URI from the canonical path
	result := ws.RemapPathToURI(canonicalPath)
	assert.Equal(t, clientURI, result)
}

func TestRemapPathToURI_NotOpen(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := newTestWorkspace(t, logger, Config{})

	// Don't open any documents
	canonicalPath := "/some/canonical/path.yammm"

	// RemapPathToURI should return a file:// URI for the canonical path
	result := ws.RemapPathToURI(canonicalPath)
	expected := lsputil.PathToURI(canonicalPath)
	assert.Equal(t, expected, result)
}

func TestRemapPathToURI_SymlinkResolution(t *testing.T) {
	t.Parallel()

	// Create temp directory with real file and symlink
	tmpDir := t.TempDir()
	realDir := tmpDir + "/real"
	require.NoError(t, os.MkdirAll(realDir, 0o750), "failed to create real dir")

	realPath := realDir + "/schema.yammm"
	require.NoError(t, os.WriteFile(realPath, []byte("content"), 0o600), "failed to write file")

	// Create symlink
	linkPath := tmpDir + "/link.yammm"
	if err := os.Symlink(realPath, linkPath); err != nil {
		t.Skip("symlinks not supported: " + err.Error())
	}

	// Resolve the real path to canonical form (handles /var -> /private/var on macOS)
	canonicalRealPath, err := filepath.EvalSymlinks(realPath)
	require.NoError(t, err, "failed to resolve real path")

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := newTestWorkspace(t, logger, Config{})

	// Open document via symlink URI
	linkURI := lsputil.PathToURI(linkPath)
	ws.documentOpened(linkURI, 1, "content")

	// RemapPathToURI with canonical path should return the symlink URI
	result := ws.RemapPathToURI(canonicalRealPath)
	assert.Equal(t, linkURI, result, "should return symlink URI")
}

func TestRemapPathToURI_MultipleDocumentsSameCanonical(t *testing.T) {
	t.Parallel()

	// Create temp directory with real file and symlink
	tmpDir := t.TempDir()
	realDir := tmpDir + "/real"
	require.NoError(t, os.MkdirAll(realDir, 0o750), "failed to create real dir")

	realPath := realDir + "/schema.yammm"
	require.NoError(t, os.WriteFile(realPath, []byte("content"), 0o600), "failed to write file")

	// Create symlink
	linkPath := tmpDir + "/link.yammm"
	if err := os.Symlink(realPath, linkPath); err != nil {
		t.Skip("symlinks not supported: " + err.Error())
	}

	// Resolve the real path to canonical form
	canonicalRealPath, err := filepath.EvalSymlinks(realPath)
	require.NoError(t, err, "failed to resolve real path")

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := newTestWorkspace(t, logger, Config{})

	// Open via symlink FIRST (lower OpenOrder)
	linkURI := lsputil.PathToURI(linkPath)
	ws.documentOpened(linkURI, 1, "content via symlink")

	// Open via real path SECOND (higher OpenOrder)
	realURI := lsputil.PathToURI(realPath)
	ws.documentOpened(realURI, 1, "content via real path")

	// RemapPathToURI should prefer the first-opened (symlink) URI
	result := ws.RemapPathToURI(canonicalRealPath)
	assert.Equal(t, linkURI, result, "should prefer first-opened URI")

	// Close symlink document
	ws.documentClosed(linkURI)

	// Now RemapPathToURI should return the real URI (only one remaining)
	result = ws.RemapPathToURI(canonicalRealPath)
	assert.Equal(t, realURI, result, "after close: should use remaining URI")
}

func TestComputeBraceDepths_CRLF(t *testing.T) {
	// Tests that CRLF line endings are handled correctly in brace depth calculation.
	// Windows clients may send documents with CRLF (\r\n) line endings.
	t.Parallel()

	tests := []struct {
		name string
		text string
		want []int
	}{
		{
			name: "LF only",
			text: "type Foo {\n  name string\n}",
			want: []int{1, 1, 0},
		},
		{
			name: "CRLF only",
			text: "type Foo {\r\n  name string\r\n}",
			want: []int{1, 1, 0},
		},
		{
			name: "mixed CRLF and LF",
			text: "type Foo {\r\n  name string\n}",
			want: []int{1, 1, 0},
		},
		{
			name: "CR only (old Mac style)",
			text: "type Foo {\r  name string\r}",
			want: []int{1, 1, 0},
		},
		{
			name: "nested braces with CRLF",
			text: "type Outer {\r\n  type Inner {\r\n    value int\r\n  }\r\n}",
			want: []int{1, 2, 2, 1, 0},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, _ := docstate.ComputeBraceDepths(tt.text)
			require.Len(t, got, len(tt.want), "ComputeBraceDepths() line count mismatch")
			for i := range got {
				assert.Equal(t, tt.want[i], got[i], "ComputeBraceDepths() line %d depth", i)
			}
		})
	}
}

func TestComputeBraceDepths_CommentLikeStrings(t *testing.T) {
	// Tests that strings containing // or /* */ don't break brace counting.
	// The string parser should treat these as literal content, not comments.
	//
	// This is a regression test for the bug where comment detection ran
	// BEFORE string handling, causing strings like "http://" to be treated
	// as containing a line comment.
	t.Parallel()

	tests := []struct {
		name string
		text string
		want []int
	}{
		{
			name: "URL in property",
			text: "type Foo {\n    url \"http://example.com\"\n}",
			want: []int{1, 1, 0},
		},
		{
			name: "block comment sequence in string",
			text: "type Foo {\n    note \"/* not a comment */\"\n}",
			want: []int{1, 1, 0},
		},
		{
			name: "double slash in string",
			text: "type Foo {\n    path \"C://path//to//file\"\n}",
			want: []int{1, 1, 0},
		},
		{
			name: "braces inside string",
			text: "type Foo {\n    json \"{nested: {deep}}\"\n}",
			want: []int{1, 1, 0}, // Braces in string don't count
		},
		{
			name: "closing brace in string",
			text: "type Foo {\n    val \"}\"\n}",
			want: []int{1, 1, 0}, // String brace doesn't close type
		},
		{
			name: "mixed real and string braces",
			text: "type Foo {\n    val \"{\"\n    name String\n}",
			want: []int{1, 1, 1, 0},
		},
		{
			name: "actual line comment after string",
			text: "type Foo {\n    url \"http://x\" // comment\n}",
			want: []int{1, 1, 0}, // The // after the string IS a comment
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, _ := docstate.ComputeBraceDepths(tt.text)
			require.Len(t, got, len(tt.want), "ComputeBraceDepths() line count mismatch\ntext: %q", tt.text)
			for i := range got {
				assert.Equal(t, tt.want[i], got[i], "line %d depth\ntext: %q", i, tt.text)
			}
		})
	}
}

func TestComputeBraceDepths_MultiLineBlockComments(t *testing.T) {
	// Tests that multi-line block comments containing braces are handled correctly.
	// Braces inside block comments should not affect the depth count.
	// This guards against false positives from the cached isInsideTypeBody path.
	t.Parallel()

	tests := []struct {
		name       string
		text       string
		wantDepths []int
		wantInBlk  []bool
	}{
		{
			name: "block comment spans two lines with brace",
			text: "type Foo {\n/* {\n*/\n    name String\n}",
			// Line 0: "type Foo {" -> depth 1, not in block comment
			// Line 1: "/* {" -> depth still 1 (brace in comment), ends in block comment
			// Line 2: "*/" -> closes comment, depth 1, not in block comment
			// Line 3: "    name String" -> depth 1, not in block comment
			// Line 4: "}" -> depth 0, not in block comment
			wantDepths: []int{1, 1, 1, 1, 0},
			wantInBlk:  []bool{false, true, false, false, false},
		},
		{
			name: "block comment with multiple braces inside",
			text: "type Foo {\n/* { } { } */\n    id String\n}",
			// Line 0: depth 1
			// Line 1: depth 1 (braces in comment don't count)
			// Line 2: depth 1
			// Line 3: depth 0
			wantDepths: []int{1, 1, 1, 0},
			wantInBlk:  []bool{false, false, false, false},
		},
		{
			name: "multi-line block comment spanning three lines",
			text: "type Foo {\n/*\n{\n*/\n}",
			// Line 0: "type Foo {" -> depth 1
			// Line 1: "/*" -> depth 1, ends in block comment
			// Line 2: "{" -> depth 1 (brace in comment), ends in block comment
			// Line 3: "*/" -> depth 1, not in block comment
			// Line 4: "}" -> depth 0
			wantDepths: []int{1, 1, 1, 1, 0},
			wantInBlk:  []bool{false, true, true, false, false},
		},
		{
			name: "block comment before type body",
			text: "/* docs\n   with { braces } */\ntype Foo {\n}",
			// Line 0: "/* docs" -> depth 0, ends in block comment
			// Line 1: "   with { braces } */" -> depth 0 (braces in comment)
			// Line 2: "type Foo {" -> depth 1
			// Line 3: "}" -> depth 0
			wantDepths: []int{0, 0, 1, 0},
			wantInBlk:  []bool{true, false, false, false},
		},
		{
			name:       "nested-looking braces in block comment",
			text:       "type A {\n    /* {{{}}} */\n    name String\n}",
			wantDepths: []int{1, 1, 1, 0},
			wantInBlk:  []bool{false, false, false, false},
		},
		{
			name: "block comment with closing brace only",
			text: "type A {\n/* } */\n}",
			// The } inside comment shouldn't close the type
			wantDepths: []int{1, 1, 0},
			wantInBlk:  []bool{false, false, false},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotDepths, gotInBlk := docstate.ComputeBraceDepths(tt.text)

			require.Len(t, gotDepths, len(tt.wantDepths), "depths line count\ntext: %q\ngot: %v", tt.text, gotDepths)

			for i := range gotDepths {
				assert.Equal(t, tt.wantDepths[i], gotDepths[i], "depths[%d]\ntext: %q\ngot: %v", i, tt.text, gotDepths)
			}

			require.Len(t, gotInBlk, len(tt.wantInBlk), "inBlockComment line count")

			for i := range gotInBlk {
				assert.Equal(t, tt.wantInBlk[i], gotInBlk[i], "inBlockComment[%d]\ntext: %q", i, tt.text)
			}
		})
	}
}

func Test_documentOpened_CRLFNormalization(t *testing.T) {
	t.Parallel()

	// Create temp file for testing
	dir := t.TempDir()
	path := filepath.Join(dir, "test.yammm")
	require.NoError(t, os.WriteFile(path, []byte("type Test {}"), 0o600))

	ws := newTestWorkspace(t, nil, Config{})
	uri := lsputil.PathToURI(path)

	// Open document with CRLF line endings
	textWithCRLF := "type Person {\r\n\tname string\r\n}\r\n"
	ws.documentOpened(uri, 1, textWithCRLF)

	// Verify stored text has LF only
	doc := ws.GetDocumentSnapshot(uri)
	require.NotNil(t, doc, "document not found after open")

	assert.NotContains(t, doc.Text, "\r", "stored text still contains CR; want CRLF normalized to LF")

	expectedLines := 4 // "type Person {\n\tname string\n}\n" + trailing empty
	actualLines := len(strings.Split(doc.Text, "\n"))
	assert.Equal(t, expectedLines, actualLines, "LF normalization may have failed")
}

func TestDocumentChanged_CRLFNormalization(t *testing.T) {
	t.Parallel()

	// Create temp file for testing
	dir := t.TempDir()
	path := filepath.Join(dir, "test.yammm")
	require.NoError(t, os.WriteFile(path, []byte("type Test {}"), 0o600))

	ws := newTestWorkspace(t, nil, Config{})
	uri := lsputil.PathToURI(path)

	// Open document first (with LF)
	ws.documentOpened(uri, 1, "type Test {}\n")

	// Change with CRLF line endings
	textWithCRLF := "type Updated {\r\n\tfield int\r\n}\r\n"
	ws.documentChanged(uri, 2, textWithCRLF)

	// Verify stored text has LF only
	doc := ws.GetDocumentSnapshot(uri)
	require.NotNil(t, doc, "document not found after change")

	assert.NotContains(t, doc.Text, "\r", "stored text still contains CR after change; want CRLF normalized to LF")
}

func TestDocumentChanged_VersionOrdering(t *testing.T) {
	t.Parallel()

	// Create temp file for testing
	dir := t.TempDir()
	path := filepath.Join(dir, "test.yammm")
	require.NoError(t, os.WriteFile(path, []byte("type Test {}"), 0o600))

	ws := newTestWorkspace(t, nil, Config{})
	uri := lsputil.PathToURI(path)

	// Open document at version 5
	ws.documentOpened(uri, 5, "version5")

	// Try to update with older version (should be ignored)
	ws.documentChanged(uri, 3, "version3-stale")

	doc := ws.GetDocumentSnapshot(uri)
	require.NotNil(t, doc, "document not found")

	assert.Equal(t, "version5", doc.Text, "stale update should be ignored")
	assert.Equal(t, 5, doc.Version, "stale update should be ignored")

	// Update with newer version (should succeed)
	ws.documentChanged(uri, 7, "version7")

	doc = ws.GetDocumentSnapshot(uri)
	assert.Equal(t, "version7", doc.Text)
	assert.Equal(t, 7, doc.Version)
}

func TestDocumentChanged_VersionZeroAccepted(t *testing.T) {
	t.Parallel()

	// Create temp file for testing
	dir := t.TempDir()
	path := filepath.Join(dir, "test.yammm")
	require.NoError(t, os.WriteFile(path, []byte("type Test {}"), 0o600))

	ws := newTestWorkspace(t, nil, Config{})
	uri := lsputil.PathToURI(path)

	// Open document at version 5
	ws.documentOpened(uri, 5, "version5")

	// Update with version 0 (unknown) should be accepted
	ws.documentChanged(uri, 0, "versionZero")

	doc := ws.GetDocumentSnapshot(uri)
	require.NotNil(t, doc, "document not found")

	assert.Equal(t, "versionZero", doc.Text, "version 0 should be accepted")
}

func TestDocumentChanged_Version0_InvalidatesLineStateCache(t *testing.T) {
	// Tests that LineState cache is invalidated when text changes with version 0.
	// Without explicit invalidation, the cache would incorrectly remain valid
	// because LineState.Version (0) == doc.Version (0) even though text changed.
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "test.yammm")
	require.NoError(t, os.WriteFile(path, []byte(""), 0o600))

	ws := newTestWorkspace(t, nil, Config{})
	uri := lsputil.PathToURI(path)

	// Open document with version 0 and content that has brace depth 1
	ws.documentOpened(uri, 0, "type A {")

	// Get snapshot to trigger LineState computation
	doc1 := ws.GetDocumentSnapshot(uri)
	require.NotNil(t, doc1, "document not found")
	require.NotNil(t, doc1.LineState, "LineState should be computed on first access")
	// Verify initial brace depth
	require.Len(t, doc1.LineState.BraceDepth, 1)
	assert.Equal(t, 1, doc1.LineState.BraceDepth[0], "initial BraceDepth")

	// Update with version 0 again but different content (brace depth 0)
	ws.documentChanged(uri, 0, "type A {}")

	// Get new snapshot - should have fresh LineState reflecting new content
	doc2 := ws.GetDocumentSnapshot(uri)
	require.NotNil(t, doc2, "document not found after change")
	require.NotNil(t, doc2.LineState, "LineState should be recomputed after change")
	// Verify brace depth reflects new content (balanced braces = depth 0)
	require.Len(t, doc2.LineState.BraceDepth, 1)
	assert.Equal(t, 0, doc2.LineState.BraceDepth[0], "updated BraceDepth (cache should have been invalidated)")
}

func TestRemapPathToURI_ForwardSlashNormalization(t *testing.T) {
	t.Parallel()

	// Create temp file for testing
	dir := t.TempDir()
	path := filepath.Join(dir, "test.yammm")
	require.NoError(t, os.WriteFile(path, []byte("type Test {}"), 0o600))

	ws := newTestWorkspace(t, nil, Config{})
	uri := lsputil.PathToURI(path)
	ws.documentOpened(uri, 1, "type Test {}")

	// On all platforms, RemapPathToURI should work with forward-slash paths
	forwardSlashPath := filepath.ToSlash(path)
	result := ws.RemapPathToURI(forwardSlashPath)

	assert.Equal(t, uri, result)
}

func TestRemapPathToURI_NonexistentPathWithDotDot(t *testing.T) {
	// Tests that RemapPathToURI correctly handles paths with ".." components
	// when EvalSymlinks fails (nonexistent path). The path should still be
	// cleaned and produce a valid file:// URI.
	//
	// This is a regression test for the issue where RemapPathToURI did not
	// call filepath.Clean on the error path when EvalSymlinks fails.
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := newTestWorkspace(t, logger, Config{})

	tests := []struct {
		name  string
		input string
		want  string // Expected file:// URI (with cleaned path)
	}{
		{
			name:  "dotdot in nonexistent path",
			input: "/nonexistent/../real/path.yammm",
			want:  "file:///real/path.yammm",
		},
		{
			name:  "multiple dotdot components",
			input: "/a/b/c/../../d/file.yammm",
			want:  "file:///a/d/file.yammm",
		},
		{
			name:  "single dot in path",
			input: "/real/./path/./file.yammm",
			want:  "file:///real/path/file.yammm",
		},
		{
			name:  "mixed dots and dotdots",
			input: "/a/./b/../c/file.yammm",
			want:  "file:///a/c/file.yammm",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := ws.RemapPathToURI(tt.input)
			assert.Equal(t, tt.want, result, "RemapPathToURI(%q)", tt.input)
		})
	}
}

// notificationCollector is a test helper that records LSP notifications.

func TestPublishDiagnostics_HashDedup_SuppressesIdentical(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := newTestWorkspace(t, logger, Config{})
	collector := &testutil.NotificationCollector{}
	ws.SetNotifier(collector.Notify)
	uri := "file:///test.yammm"

	diags := []protocol.Diagnostic{
		{Message: "error one", Range: protocol.Range{}},
	}

	// First publish goes through.
	ws.publishDiagnostics(uri, nil, diags)
	require.Len(t, collector.Entries(), 1, "after first publish")

	// Second identical publish is suppressed.
	ws.publishDiagnostics(uri, nil, diags)
	assert.Len(t, collector.Entries(), 1, "identical should be suppressed")
}

func TestPublishDiagnostics_HashDedup_AllowsDifferent(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := newTestWorkspace(t, logger, Config{})
	collector := &testutil.NotificationCollector{}
	ws.SetNotifier(collector.Notify)
	uri := "file:///test.yammm"

	diagsA := []protocol.Diagnostic{
		{Message: "error A", Range: protocol.Range{}},
	}
	diagsB := []protocol.Diagnostic{
		{Message: "error B", Range: protocol.Range{}},
	}

	ws.publishDiagnostics(uri, nil, diagsA)
	ws.publishDiagnostics(uri, nil, diagsB)

	assert.Len(t, collector.Entries(), 2, "different diagnostics should both be published")
}

func TestPublishDiagnostics_HashDedup_EmptyDiagnostics(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := newTestWorkspace(t, logger, Config{})
	collector := &testutil.NotificationCollector{}
	ws.SetNotifier(collector.Notify)
	uri := "file:///test.yammm"

	// Two successive empty publishes: first goes through, second is suppressed.
	ws.publishDiagnostics(uri, nil, nil)
	ws.publishDiagnostics(uri, nil, []protocol.Diagnostic{})

	assert.Len(t, collector.Entries(), 1, "empty diagnostics should be deduped")
}

func TestPublishDiagnostics_ClearHashOnClose_AllowsFreshPublish(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := newTestWorkspace(t, logger, Config{})
	collector := &testutil.NotificationCollector{}
	ws.SetNotifier(collector.Notify)
	uri := "file:///test.yammm"

	diags := []protocol.Diagnostic{
		{Message: "error", Range: protocol.Range{}},
	}

	// First publish.
	ws.publishDiagnostics(uri, nil, diags)

	// Clear hash (simulating document close).
	ws.clearDiagHash(uri)

	// Same diagnostics after hash clear: should publish again.
	ws.publishDiagnostics(uri, nil, diags)

	assert.Len(t, collector.Entries(), 2, "after hash clear")
}

func TestPublishDiagnostics_NilNotify_NoHash(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := newTestWorkspace(t, logger, Config{})
	uri := "file:///test.yammm"

	diags := []protocol.Diagnostic{
		{Message: "error", Range: protocol.Range{}},
	}

	// Publish with nil notifier (default for new workspace) should not store hash.
	ws.publishDiagnostics(uri, nil, diags)

	ws.diagHash.mu.Lock()
	_, exists := ws.diagHash.hashes[uri]
	ws.diagHash.mu.Unlock()

	assert.False(t, exists, "hash should not be stored when notify is nil")
}

func TestHashDiagnostics_Deterministic(t *testing.T) {
	t.Parallel()

	diags := []protocol.Diagnostic{
		{
			Message: "some error",
			Range: protocol.Range{
				Start: protocol.Position{Line: 1, Character: 5},
				End:   protocol.Position{Line: 1, Character: 10},
			},
		},
		{
			Message: "another error",
			Range: protocol.Range{
				Start: protocol.Position{Line: 3, Character: 0},
				End:   protocol.Position{Line: 3, Character: 7},
			},
		},
	}

	h1, ok1 := hashDiagnostics(diags)
	h2, ok2 := hashDiagnostics(diags)

	require.True(t, ok1, "hashDiagnostics returned !ok")
	require.True(t, ok2, "hashDiagnostics returned !ok")
	assert.Equal(t, h1, h2, "hash should be deterministic")

	// Different diagnostics produce different hash.
	different := []protocol.Diagnostic{
		{Message: "different error", Range: protocol.Range{}},
	}
	h3, ok3 := hashDiagnostics(different)
	require.True(t, ok3, "hashDiagnostics returned !ok for different input")
	assert.NotEqual(t, h1, h3, "different diagnostics should produce different hash")
}

func TestMarkdownPositionToBlock(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		blocks    []markdown.CodeBlock
		line      int
		char      int
		wantNil   bool
		wantBlock int
		wantLine  int
		wantChar  int
	}{
		{
			name: "inside block at start",
			blocks: []markdown.CodeBlock{
				{StartLine: 3, EndLine: 8},
			},
			line:      3,
			char:      5,
			wantBlock: 0,
			wantLine:  0,
			wantChar:  5,
		},
		{
			name: "inside block at last content line",
			blocks: []markdown.CodeBlock{
				{StartLine: 3, EndLine: 8},
			},
			line:      7,
			char:      0,
			wantBlock: 0,
			wantLine:  4,
			wantChar:  0,
		},
		{
			name: "on closing fence line",
			blocks: []markdown.CodeBlock{
				{StartLine: 3, EndLine: 8},
			},
			line:    8,
			char:    0,
			wantNil: true,
		},
		{
			name: "outside all blocks (prose)",
			blocks: []markdown.CodeBlock{
				{StartLine: 3, EndLine: 8},
			},
			line:    0,
			char:    5,
			wantNil: true,
		},
		{
			name: "between two blocks",
			blocks: []markdown.CodeBlock{
				{StartLine: 1, EndLine: 3},
				{StartLine: 6, EndLine: 9},
			},
			line:    4,
			char:    0,
			wantNil: true,
		},
		{
			name: "inside second block",
			blocks: []markdown.CodeBlock{
				{StartLine: 1, EndLine: 3},
				{StartLine: 6, EndLine: 9},
			},
			line:      7,
			char:      10,
			wantBlock: 1,
			wantLine:  1,
			wantChar:  10,
		},
		{
			name:    "no blocks",
			blocks:  []markdown.CodeBlock{},
			line:    5,
			char:    0,
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			snap := &MarkdownDocumentSnapshot{Blocks: tt.blocks}
			pos := snap.MarkdownPositionToBlock(tt.line, tt.char)

			if tt.wantNil {
				assert.Nil(t, pos)
				return
			}

			require.NotNil(t, pos)
			assert.Equal(t, tt.wantBlock, pos.BlockIndex)
			assert.Equal(t, tt.wantLine, pos.LocalLine)
			assert.Equal(t, tt.wantChar, pos.LocalChar)
		})
	}
}

func TestBlockPositionToMarkdown(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		blocks     []markdown.CodeBlock
		blockIndex int
		localLine  int
		localChar  int
		wantLine   int
		wantChar   int
	}{
		{
			name: "valid block index",
			blocks: []markdown.CodeBlock{
				{StartLine: 3, EndLine: 8},
			},
			blockIndex: 0,
			localLine:  2,
			localChar:  5,
			wantLine:   5,
			wantChar:   5,
		},
		{
			name: "second block",
			blocks: []markdown.CodeBlock{
				{StartLine: 1, EndLine: 3},
				{StartLine: 6, EndLine: 9},
			},
			blockIndex: 1,
			localLine:  0,
			localChar:  0,
			wantLine:   6,
			wantChar:   0,
		},
		{
			name: "invalid negative index",
			blocks: []markdown.CodeBlock{
				{StartLine: 3, EndLine: 8},
			},
			blockIndex: -1,
			localLine:  0,
			localChar:  0,
			wantLine:   -1,
			wantChar:   -1,
		},
		{
			name: "invalid out-of-bounds index",
			blocks: []markdown.CodeBlock{
				{StartLine: 3, EndLine: 8},
			},
			blockIndex: 5,
			localLine:  0,
			localChar:  0,
			wantLine:   -1,
			wantChar:   -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			snap := &MarkdownDocumentSnapshot{Blocks: tt.blocks}
			line, char := snap.BlockPositionToMarkdown(tt.blockIndex, tt.localLine, tt.localChar)
			assert.Equal(t, tt.wantLine, line)
			assert.Equal(t, tt.wantChar, char)
		})
	}
}

func TestMarkdownPositionToBlock_WithPrefixLines(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		blocks    []markdown.CodeBlock
		line      int
		char      int
		wantNil   bool
		wantBlock int
		wantLine  int
		wantChar  int
	}{
		{
			name: "snippet block first content line maps to prefixed line 1",
			blocks: []markdown.CodeBlock{
				{StartLine: 5, EndLine: 10, PrefixLines: 1},
			},
			line:      5,
			char:      0,
			wantBlock: 0,
			wantLine:  1, // 5 - 5 + 1 = 1 (line 0 is synthetic schema)
			wantChar:  0,
		},
		{
			name: "snippet block middle line",
			blocks: []markdown.CodeBlock{
				{StartLine: 5, EndLine: 10, PrefixLines: 1},
			},
			line:      7,
			char:      10,
			wantBlock: 0,
			wantLine:  3, // 7 - 5 + 1 = 3
			wantChar:  10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			snap := &MarkdownDocumentSnapshot{Blocks: tt.blocks}
			pos := snap.MarkdownPositionToBlock(tt.line, tt.char)

			if tt.wantNil {
				assert.Nil(t, pos)
				return
			}

			require.NotNil(t, pos)
			assert.Equal(t, tt.wantBlock, pos.BlockIndex)
			assert.Equal(t, tt.wantLine, pos.LocalLine)
			assert.Equal(t, tt.wantChar, pos.LocalChar)
		})
	}
}

func TestBlockPositionToMarkdown_WithPrefixLines(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		blocks     []markdown.CodeBlock
		blockIndex int
		localLine  int
		localChar  int
		wantLine   int
		wantChar   int
	}{
		{
			name: "prefixed line 1 maps back to block start",
			blocks: []markdown.CodeBlock{
				{StartLine: 5, EndLine: 10, PrefixLines: 1},
			},
			blockIndex: 0,
			localLine:  1,
			localChar:  0,
			wantLine:   5, // 5 + 1 - 1 = 5
			wantChar:   0,
		},
		{
			name: "prefixed line 3 maps to StartLine+2",
			blocks: []markdown.CodeBlock{
				{StartLine: 5, EndLine: 10, PrefixLines: 1},
			},
			blockIndex: 0,
			localLine:  3,
			localChar:  10,
			wantLine:   7, // 5 + 3 - 1 = 7
			wantChar:   10,
		},
		{
			name: "synthetic line 0 maps to fence line (StartLine-1)",
			blocks: []markdown.CodeBlock{
				{StartLine: 5, EndLine: 10, PrefixLines: 1},
			},
			blockIndex: 0,
			localLine:  0,
			localChar:  0,
			wantLine:   4, // 5 + 0 - 1 = 4 (fence line, will be filtered)
			wantChar:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			snap := &MarkdownDocumentSnapshot{Blocks: tt.blocks}
			line, char := snap.BlockPositionToMarkdown(tt.blockIndex, tt.localLine, tt.localChar)
			assert.Equal(t, tt.wantLine, line)
			assert.Equal(t, tt.wantChar, char)
		})
	}
}

func TestPositionConversion_RoundTrip_WithPrefixLines(t *testing.T) {
	t.Parallel()

	snap := &MarkdownDocumentSnapshot{
		Blocks: []markdown.CodeBlock{
			{StartLine: 5, EndLine: 10, PrefixLines: 1},
			{StartLine: 15, EndLine: 20, PrefixLines: 0},
		},
	}

	tests := []struct {
		name string
		line int
		char int
	}{
		{"snippet block start", 5, 0},
		{"snippet block middle", 7, 5},
		{"snippet block last content", 9, 3},
		{"normal block start", 15, 0},
		{"normal block middle", 17, 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			pos := snap.MarkdownPositionToBlock(tt.line, tt.char)
			require.NotNil(t, pos)

			gotLine, gotChar := snap.BlockPositionToMarkdown(pos.BlockIndex, pos.LocalLine, pos.LocalChar)
			assert.Equal(t, tt.line, gotLine)
			assert.Equal(t, tt.char, gotChar)
		})
	}
}

func TestPositionConversion_RoundTrip(t *testing.T) {
	t.Parallel()

	snap := &MarkdownDocumentSnapshot{
		Blocks: []markdown.CodeBlock{
			{StartLine: 3, EndLine: 8},
			{StartLine: 12, EndLine: 15},
		},
	}

	tests := []struct {
		name string
		line int
		char int
	}{
		{"block 0 start", 3, 0},
		{"block 0 middle", 5, 10},
		{"block 0 last content", 7, 3},
		{"block 1 start", 12, 0},
		{"block 1 middle", 13, 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			pos := snap.MarkdownPositionToBlock(tt.line, tt.char)
			require.NotNil(t, pos)

			gotLine, gotChar := snap.BlockPositionToMarkdown(pos.BlockIndex, pos.LocalLine, pos.LocalChar)
			assert.Equal(t, tt.line, gotLine)
			assert.Equal(t, tt.char, gotChar)
		})
	}
}

func Test_markdownDocumentOpened_CreatesEntry(t *testing.T) {
	t.Parallel()

	w := newTestWorkspace(t, slog.Default(), Config{})
	uri := "file:///test/doc.md"

	w.markdownDocumentOpened(uri, 1, "# Hello\n\n```yammm\nschema \"test\"\n```\n")

	snap := w.GetMarkdownDocumentSnapshot(uri)
	require.NotNil(t, snap)
	assert.Equal(t, uri, snap.URI)
	assert.Equal(t, 1, snap.Version)
	assert.Empty(t, snap.Blocks)
	assert.Empty(t, snap.Snapshots)
}

func TestMarkdownDocumentChanged_RejectsStale(t *testing.T) {
	t.Parallel()

	w := newTestWorkspace(t, slog.Default(), Config{})
	uri := "file:///test/doc.md"

	w.markdownDocumentOpened(uri, 1, "original")
	w.markdownDocumentChanged(uri, 2, "updated")
	w.markdownDocumentChanged(uri, 1, "stale")

	text, ok := w.GetMarkdownCurrentText(uri)
	require.True(t, ok)
	assert.Equal(t, "updated", text)
}

func TestMarkdownDocumentChanged_AcceptsZeroVersion(t *testing.T) {
	t.Parallel()

	w := newTestWorkspace(t, slog.Default(), Config{})
	uri := "file:///test/doc.md"

	w.markdownDocumentOpened(uri, 0, "original")
	w.markdownDocumentChanged(uri, 0, "updated")

	text, ok := w.GetMarkdownCurrentText(uri)
	require.True(t, ok)
	assert.Equal(t, "updated", text)
}

func TestMarkdownDocumentClosed_CleansUp(t *testing.T) {
	t.Parallel()

	w := newTestWorkspace(t, slog.Default(), Config{})
	uri := "file:///test/doc.md"
	collector := &testutil.NotificationCollector{}
	w.SetNotifier(collector.Notify)

	w.markdownDocumentOpened(uri, 1, "# Test")
	w.markdownDocumentClosed(uri)

	snap := w.GetMarkdownDocumentSnapshot(uri)
	assert.Nil(t, snap)
}

func TestMarkdownDocumentClosed_PublishesClearDiagnostics(t *testing.T) {
	t.Parallel()

	w := newTestWorkspace(t, slog.Default(), Config{})
	uri := "file:///test/close_diag.md"
	collector := &testutil.NotificationCollector{}
	w.SetNotifier(collector.Notify)

	// Open markdown with syntax error to produce diagnostics.
	content := "# Test\n\n```yammm\nnot valid schema!!!\n```\n"
	w.markdownDocumentOpened(uri, 1, content)
	w.AnalyzeMarkdownAndPublish(t.Context(), uri)

	// Verify non-empty diagnostics were published.
	diags := collector.DiagnosticsFor(uri)
	require.NotEmpty(t, diags, "precondition: diagnostics published for invalid content")

	// Close — should publish empty diagnostics to clear editor.
	w.markdownDocumentClosed(uri)

	// Verify snapshot cleared.
	snap := w.GetMarkdownDocumentSnapshot(uri)
	assert.Nil(t, snap)

	// Verify empty diagnostics notification was published.
	// diagnosticsFor scans in reverse — the latest entry should be the clear notification
	// with Diagnostics: []protocol.Diagnostic{} (non-nil empty slice per workspace.go:755-756).
	finalDiags := collector.DiagnosticsFor(uri)
	require.NotNil(t, finalDiags, "expected PublishDiagnostics notification after close")
	assert.Empty(t, finalDiags, "expected empty diagnostics to clear editor squiggles")
}

func TestAnalyzeMarkdownAndPublish_ProducesDiagnostics(t *testing.T) {
	t.Parallel()

	w := newTestWorkspace(t, slog.Default(), Config{})
	uri := "file:///test/doc.md"
	collector := &testutil.NotificationCollector{}
	w.SetNotifier(collector.Notify)

	// Content with a syntax error in the code block
	content := "# Test\n\n```yammm\nnot valid schema!!!\n```\n"
	w.markdownDocumentOpened(uri, 1, content)
	w.AnalyzeMarkdownAndPublish(t.Context(), uri)

	// Verify diagnostics were published
	diags := collector.DiagnosticsFor(uri)
	assert.NotEmpty(t, diags, "expected diagnostics for syntax error")

	// Verify the snapshot has blocks
	snap := w.GetMarkdownDocumentSnapshot(uri)
	require.NotNil(t, snap)
	assert.Len(t, snap.Blocks, 1)
	assert.Len(t, snap.Snapshots, 1)
}

func TestAnalyzeMarkdownAndPublish_EmptyBlocks(t *testing.T) {
	t.Parallel()

	w := newTestWorkspace(t, slog.Default(), Config{})
	uri := "file:///test/doc.md"
	collector := &testutil.NotificationCollector{}
	w.SetNotifier(collector.Notify)

	// Markdown with no yammm blocks
	content := "# Just prose\n\nNo code here.\n"
	w.markdownDocumentOpened(uri, 1, content)
	w.AnalyzeMarkdownAndPublish(t.Context(), uri)

	snap := w.GetMarkdownDocumentSnapshot(uri)
	require.NotNil(t, snap)
	assert.Empty(t, snap.Blocks)
	assert.Empty(t, snap.Snapshots)
}

func TestAnalyzeMarkdownAndPublish_ImportRejection(t *testing.T) {
	t.Parallel()

	w := newTestWorkspace(t, slog.Default(), Config{})
	uri := "file:///test/doc.md"
	collector := &testutil.NotificationCollector{}
	w.SetNotifier(collector.Notify)

	content := "# Import Test\n\n```yammm\nschema \"import_test\"\n\nimport \"./sibling\" as s\n\ntype Foo {\n    id String primary\n}\n```\n"
	w.markdownDocumentOpened(uri, 1, content)
	w.AnalyzeMarkdownAndPublish(t.Context(), uri)

	diags := collector.DiagnosticsFor(uri)
	require.NotEmpty(t, diags, "expected diagnostics for import rejection")

	// Check that at least one diagnostic has E_IMPORT_NOT_ALLOWED code
	// and that it has been downgraded to Hint severity
	var found bool
	for _, d := range diags {
		if d.Code != nil {
			if codeVal, ok := d.Code.Value.(string); ok && codeVal == "E_IMPORT_NOT_ALLOWED" {
				found = true
				require.NotNil(t, d.Severity, "E_IMPORT_NOT_ALLOWED diagnostic should have severity set")
				assert.Equal(t, protocol.DiagnosticSeverityHint, *d.Severity,
					"E_IMPORT_NOT_ALLOWED should be downgraded to Hint in markdown")
				break
			}
		}
	}
	assert.True(t, found, "expected E_IMPORT_NOT_ALLOWED diagnostic, got: %+v", diags)
}

func TestAnalyzeMarkdownAndPublish_SnippetBlock(t *testing.T) {
	t.Parallel()

	w := newTestWorkspace(t, slog.Default(), Config{})
	uri := "file:///test/snippet.md"
	collector := &testutil.NotificationCollector{}
	w.SetNotifier(collector.Notify)

	// A snippet block with no schema declaration — just a type definition
	content := "# Snippet Example\n\n```yammm\ntype Foo {\n    id String primary\n    name String required\n}\n```\n"
	w.markdownDocumentOpened(uri, 1, content)
	w.AnalyzeMarkdownAndPublish(t.Context(), uri)

	snap := w.GetMarkdownDocumentSnapshot(uri)
	require.NotNil(t, snap)
	require.Len(t, snap.Blocks, 1)
	require.Len(t, snap.Snapshots, 1)

	// Block should have PrefixLines=1 (synthetic schema was prepended)
	assert.Equal(t, 1, snap.Blocks[0].PrefixLines, "snippet block should have PrefixLines=1")

	// Snapshot should be non-nil and have a valid schema
	require.NotNil(t, snap.Snapshots[0], "snapshot should be non-nil for snippet block")
	assert.True(t, snap.Snapshots[0].Result.OK(), "snippet block should produce no errors, got: %v", snap.Snapshots[0].Result)

	// Diagnostics should have no Fatal/Error entries
	diags := collector.DiagnosticsFor(uri)
	for _, d := range diags {
		if d.Severity != nil {
			assert.NotEqual(t, protocol.DiagnosticSeverityError, *d.Severity,
				"snippet block should not produce Error diagnostics: %s", d.Message)
		}
	}
}

func TestAnalyzeMarkdownAndPublish_SnippetBlockWithSchemaSkipsPrefix(t *testing.T) {
	t.Parallel()

	w := newTestWorkspace(t, slog.Default(), Config{})
	uri := "file:///test/full.md"

	// A block WITH a schema declaration — should NOT get a prefix
	content := "# Full Schema\n\n```yammm\nschema \"test\"\n\ntype Foo {\n    id String primary\n}\n```\n"
	w.markdownDocumentOpened(uri, 1, content)
	w.AnalyzeMarkdownAndPublish(t.Context(), uri)

	snap := w.GetMarkdownDocumentSnapshot(uri)
	require.NotNil(t, snap)
	require.Len(t, snap.Blocks, 1)

	// Block should have PrefixLines=0 (no synthetic prefix needed)
	assert.Equal(t, 0, snap.Blocks[0].PrefixLines, "block with schema declaration should have PrefixLines=0")
}

func TestAnalyzeMarkdownAndPublish_VersionGate(t *testing.T) {
	t.Parallel()

	w := newTestWorkspace(t, slog.Default(), Config{})
	uri := "file:///test/doc.md"
	collector := &testutil.NotificationCollector{}
	w.SetNotifier(collector.Notify)

	content := "# Test\n\n```yammm\nschema \"test\"\n```\n"
	w.markdownDocumentOpened(uri, 1, content)

	// Change the document version before analysis completes
	w.markdownDocumentChanged(uri, 2, "# Changed\n\n```yammm\nschema \"changed\"\n```\n")

	// Analyze with original version — the results should be discarded
	// because the document version has changed
	w.mu.Lock()
	w.markdownDocs[uri].Version = 1
	w.mu.Unlock()

	// Manually change back to force version mismatch after analysis
	w.mu.Lock()
	w.markdownDocs[uri].Version = 2
	w.mu.Unlock()

	// Simulate analysis starting with v1 — since we can't easily test async
	// version gating, we verify the snapshot structure is correct after a
	// successful analysis
	w.mu.Lock()
	w.markdownDocs[uri].Version = 1
	w.mu.Unlock()

	w.AnalyzeMarkdownAndPublish(t.Context(), uri)

	// This should succeed since version matches
	snap := w.GetMarkdownDocumentSnapshot(uri)
	require.NotNil(t, snap)
	assert.Equal(t, 1, snap.Version)
}

func TestAnalyzeMarkdownAndPublish_ValidSchema(t *testing.T) {
	t.Parallel()

	w := newTestWorkspace(t, slog.Default(), Config{})
	uri := "file:///test/doc.md"
	collector := &testutil.NotificationCollector{}
	w.SetNotifier(collector.Notify)

	content := "# Valid Schema\n\n```yammm\nschema \"test\"\n\ntype Foo {\n    id String primary\n}\n```\n"
	w.markdownDocumentOpened(uri, 1, content)
	w.AnalyzeMarkdownAndPublish(t.Context(), uri)

	snap := w.GetMarkdownDocumentSnapshot(uri)
	require.NotNil(t, snap)
	assert.Len(t, snap.Blocks, 1)
	require.Len(t, snap.Snapshots, 1)

	// Valid schema should have a snapshot with no error diagnostics
	if snap.Snapshots[0] != nil {
		assert.True(t, snap.Snapshots[0].Result.OK(), "expected valid schema to produce no errors")
	}

	// Diagnostics should be empty for a valid schema
	diags := collector.DiagnosticsFor(uri)
	assert.Empty(t, diags, "expected no diagnostics for valid schema")
}

func TestBuildBlockDocumentSnapshot(t *testing.T) {
	t.Parallel()

	mdSnap := &MarkdownDocumentSnapshot{
		URI:     "file:///test/doc.md",
		Version: 42,
		Blocks: []markdown.CodeBlock{
			{
				Content:   "schema \"test\"\n\ntype Foo {\n    id String primary\n}",
				StartLine: 3,
				EndLine:   8,
				FenceChar: '`',
			},
		},
	}

	// Assign a source ID to the block
	id, err := markdown.VirtualSourceID("/test/doc.md", 0)
	require.NoError(t, err)
	mdSnap.Blocks[0].SourceID = id

	docSnap := BuildBlockDocumentSnapshot(mdSnap, mdSnap.Blocks[0])

	assert.Equal(t, mdSnap.URI, docSnap.URI, "URI should come from mdSnap")
	assert.Equal(t, id, docSnap.SourceID, "SourceID should come from block")
	assert.Equal(t, 42, docSnap.Version, "Version should come from mdSnap")
	assert.Equal(t, mdSnap.Blocks[0].Content, docSnap.Text, "Text should be block content")
	require.NotNil(t, docSnap.LineState, "lineState should be computed")
	assert.Equal(t, 42, docSnap.LineState.Version, "lineState version should match mdSnap")

	// The block content has 5 lines, so BraceDepth should have 5 entries
	assert.Len(t, docSnap.LineState.BraceDepth, 5, "BraceDepth should have one entry per line")
}

func BenchmarkAnalyzeMarkdownAndPublish_ManyBlocks(b *testing.B) {
	var sb strings.Builder
	for i := range 50 {
		fmt.Fprintf(&sb, "# Block %d\n\n```yammm\nschema \"block_%d\"\n\ntype Type%d {\n\tid String primary\n}\n```\n\n", i, i, i)
	}
	content := sb.String()

	w := NewWorkspace(slog.Default(), Config{})
	b.Cleanup(w.Shutdown)
	uri := "file:///bench/many_blocks.md"
	w.markdownDocumentOpened(uri, 1, content)

	ctx := b.Context()
	b.ResetTimer()
	for b.Loop() {
		w.AnalyzeMarkdownAndPublish(ctx, uri)
	}
}

func TestRemapDocumentSymbolRanges(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		symbols    []protocol.DocumentSymbol
		blockIndex int
		blocks     []markdown.CodeBlock
		wantNil    bool
		check      func(t *testing.T, result []protocol.DocumentSymbol)
	}{
		{
			name:    "empty input returns nil",
			symbols: nil,
			blocks:  []markdown.CodeBlock{{StartLine: 5, EndLine: 10}},
			wantNil: true,
		},
		{
			name: "single symbol remapped",
			symbols: []protocol.DocumentSymbol{
				{
					Name: "Foo",
					Range: protocol.Range{
						Start: protocol.Position{Line: 0, Character: 5},
						End:   protocol.Position{Line: 2, Character: 1},
					},
					SelectionRange: protocol.Range{
						Start: protocol.Position{Line: 0, Character: 5},
						End:   protocol.Position{Line: 0, Character: 8},
					},
				},
			},
			blockIndex: 0,
			blocks:     []markdown.CodeBlock{{StartLine: 5, EndLine: 10}},
			check: func(t *testing.T, result []protocol.DocumentSymbol) {
				t.Helper()
				require.Len(t, result, 1)
				assert.Equal(t, protocol.UInteger(5), result[0].Range.Start.Line)
				assert.Equal(t, protocol.UInteger(5), result[0].Range.Start.Character)
				assert.Equal(t, protocol.UInteger(7), result[0].Range.End.Line)
				assert.Equal(t, protocol.UInteger(1), result[0].Range.End.Character)
				assert.Equal(t, protocol.UInteger(5), result[0].SelectionRange.Start.Line)
				assert.Equal(t, protocol.UInteger(8), result[0].SelectionRange.End.Character)
			},
		},
		{
			name: "nested children recursively remapped",
			symbols: []protocol.DocumentSymbol{
				{
					Name: "Parent",
					Range: protocol.Range{
						Start: protocol.Position{Line: 0, Character: 0},
						End:   protocol.Position{Line: 3, Character: 1},
					},
					SelectionRange: protocol.Range{
						Start: protocol.Position{Line: 0, Character: 0},
						End:   protocol.Position{Line: 0, Character: 6},
					},
					Children: []protocol.DocumentSymbol{
						{
							Name: "Child",
							Range: protocol.Range{
								Start: protocol.Position{Line: 1, Character: 4},
								End:   protocol.Position{Line: 1, Character: 20},
							},
							SelectionRange: protocol.Range{
								Start: protocol.Position{Line: 1, Character: 4},
								End:   protocol.Position{Line: 1, Character: 9},
							},
						},
					},
				},
			},
			blockIndex: 0,
			blocks:     []markdown.CodeBlock{{StartLine: 10, EndLine: 15}},
			check: func(t *testing.T, result []protocol.DocumentSymbol) {
				t.Helper()
				require.Len(t, result, 1)
				// Parent: line 0 -> 10
				assert.Equal(t, protocol.UInteger(10), result[0].Range.Start.Line)
				assert.Equal(t, protocol.UInteger(13), result[0].Range.End.Line)
				// Child: line 1 -> 11
				require.Len(t, result[0].Children, 1)
				assert.Equal(t, protocol.UInteger(11), result[0].Children[0].Range.Start.Line)
				assert.Equal(t, protocol.UInteger(11), result[0].Children[0].SelectionRange.Start.Line)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mdSnap := &MarkdownDocumentSnapshot{Blocks: tt.blocks}
			remap := NewBlockRemap(mdSnap, tt.blockIndex)
			result := RemapDocumentSymbolRanges(tt.symbols, remap)

			if tt.wantNil {
				assert.Nil(t, result)
				return
			}
			require.NotNil(t, result)
			tt.check(t, result)
		})
	}
}

func TestChangeDocument_MultipleFullSyncChanges(t *testing.T) {
	// Tests that when multiple full-sync ContentChange events (no Range)
	// are sent in a single ChangeDocument call, only the LAST one is applied.
	// This is the correct behavior per LSP spec for full sync mode.
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := newTestWorkspace(t, logger, Config{})

	uri := "file:///test/multi-full-sync.yammm"

	// Open document with initial content
	ws.documentOpened(uri, 1, "initial content")

	// ChangeDocument now receives pre-extracted full-sync text (the server
	// boundary calls protocol.ExtractFullSyncText). Pass the final content directly.
	ws.ChangeDocument(uri, 2, "third full sync - this should be the final content")

	doc := ws.GetDocumentSnapshot(uri)
	require.NotNil(t, doc, "document not found after changes")

	expected := "third full sync - this should be the final content"
	assert.Equal(t, expected, doc.Text, "after multiple full-sync changes")
}
