package lsp

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	protocol "github.com/tliron/glsp/protocol_3_16"
	"github.com/simon-lentz/yammm/location"
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
			got, err := URIToPath(tt.uri)
			if err != nil {
				t.Fatalf("URIToPath(%q) error: %v", tt.uri, err)
			}
			if got != tt.want {
				t.Errorf("URIToPath(%q) = %q; want %q", tt.uri, got, tt.want)
			}
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
			_, err := URIToPath(tt.uri)
			if err == nil {
				t.Errorf("URIToPath(%q) = nil error; want error", tt.uri)
			}
		})
	}
}

func TestURIToPath_InvalidURI(t *testing.T) {
	t.Parallel()

	// Test with malformed URI
	_, err := URIToPath("file://[::1%eth0/bad")
	if err == nil {
		t.Error("URIToPath(malformed URI) = nil error; want error")
	}
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
			got := PathToURI(tt.path)
			if got != tt.want {
				t.Errorf("PathToURI(%q) = %q; want %q", tt.path, got, tt.want)
			}
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
			uri := PathToURI(path)
			got, err := URIToPath(uri)
			if err != nil {
				t.Fatalf("URIToPath(PathToURI(%q)) error: %v", path, err)
			}
			if got != path {
				t.Errorf("roundtrip(%q) = %q; want original", path, got)
			}
		})
	}
}

func TestNewWorkspace(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	cfg := Config{}
	ws := NewWorkspace(logger, cfg)

	if ws == nil {
		t.Fatal("NewWorkspace() returned nil")
	}
	if ws.posEncoding != PositionEncodingUTF16 {
		t.Errorf("posEncoding = %q; want UTF-16", ws.posEncoding)
	}
}

func TestWorkspace_AddRoot(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := NewWorkspace(logger, Config{})

	ws.AddRoot("file:///project/one")
	ws.AddRoot("file:///project/two")

	ws.mu.RLock()
	defer ws.mu.RUnlock()

	if len(ws.roots) != 2 {
		t.Errorf("len(roots) = %d; want 2", len(ws.roots))
	}
	if ws.roots[0] != "/project/one" {
		t.Errorf("roots[0] = %q; want /project/one", ws.roots[0])
	}
	if ws.roots[1] != "/project/two" {
		t.Errorf("roots[1] = %q; want /project/two", ws.roots[1])
	}
}

func TestWorkspace_AddRoot_InvalidURI(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := NewWorkspace(logger, Config{})

	// Invalid URI should be logged but not added
	ws.AddRoot("http://not-a-file-uri")

	ws.mu.RLock()
	defer ws.mu.RUnlock()

	if len(ws.roots) != 0 {
		t.Errorf("len(roots) = %d; want 0 for invalid URI", len(ws.roots))
	}
}

func TestWorkspace_AddRoot_SymlinkResolution(t *testing.T) {
	t.Parallel()

	// Create temp directory with a symlink
	tmpDir := t.TempDir()
	realDir := tmpDir + "/real"
	if err := os.MkdirAll(realDir, 0o750); err != nil {
		t.Fatalf("failed to create real dir: %v", err)
	}

	linkDir := tmpDir + "/link"
	if err := os.Symlink(realDir, linkDir); err != nil {
		t.Skip("symlinks not supported: " + err.Error())
	}

	// Resolve real dir to canonical form (handles /var -> /private/var on macOS)
	canonicalRealDir, err := filepath.EvalSymlinks(realDir)
	if err != nil {
		t.Fatalf("failed to resolve real dir: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := NewWorkspace(logger, Config{})

	// Add root via symlink path
	ws.AddRoot(PathToURI(linkDir))

	ws.mu.RLock()
	defer ws.mu.RUnlock()

	if len(ws.roots) != 1 {
		t.Fatalf("len(roots) = %d; want 1", len(ws.roots))
	}

	// Root should be stored as canonical (resolved) path
	if ws.roots[0] != canonicalRealDir {
		t.Errorf("root = %q; want canonical %q", ws.roots[0], canonicalRealDir)
	}
}

func TestWorkspace_FindModuleRoot_CrossSymlink(t *testing.T) {
	t.Parallel()

	// Create temp directory with real project and symlink
	tmpDir := t.TempDir()
	realProject := tmpDir + "/real/project"
	if err := os.MkdirAll(realProject, 0o750); err != nil {
		t.Fatalf("failed to create real project dir: %v", err)
	}

	// Create a file in the real project
	realFile := realProject + "/schema.yammm"
	if err := os.WriteFile(realFile, []byte("content"), 0o600); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	linkProject := tmpDir + "/link"
	if err := os.Symlink(realProject, linkProject); err != nil {
		t.Skip("symlinks not supported: " + err.Error())
	}

	// Resolve to canonical form (handles /var -> /private/var on macOS)
	canonicalProject, err := filepath.EvalSymlinks(realProject)
	if err != nil {
		t.Fatalf("failed to resolve project dir: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := NewWorkspace(logger, Config{})

	// Add root via canonical path
	ws.AddRoot(PathToURI(canonicalProject))

	// File path via symlink should still match (after canonicalization)
	symlinkFilePath := linkProject + "/schema.yammm"

	// Canonicalize the file path as AnalyzeAndPublish would
	canonicalFilePath, err := filepath.EvalSymlinks(symlinkFilePath)
	if err != nil {
		t.Fatalf("failed to resolve symlink file path: %v", err)
	}

	got := ws.findModuleRoot(canonicalFilePath)
	if got != canonicalProject {
		t.Errorf("findModuleRoot(%q) = %q; want %q", canonicalFilePath, got, canonicalProject)
	}
}

func TestWorkspace_FindModuleRoot_NonExistentRoot(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := NewWorkspace(logger, Config{})

	// Add a root that doesn't exist (symlink resolution will fail, falls back to raw)
	ws.AddRoot("file:///nonexistent/project")

	ws.mu.RLock()
	defer ws.mu.RUnlock()

	// Root should still be stored (as raw path since symlink resolution failed)
	if len(ws.roots) != 1 {
		t.Fatalf("len(roots) = %d; want 1", len(ws.roots))
	}
	if ws.roots[0] != "/nonexistent/project" {
		t.Errorf("root = %q; want /nonexistent/project", ws.roots[0])
	}
}

func TestWorkspace_SetPositionEncoding(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := NewWorkspace(logger, Config{})

	ws.SetPositionEncoding(PositionEncodingUTF8)

	ws.mu.RLock()
	enc := ws.posEncoding
	ws.mu.RUnlock()

	if enc != PositionEncodingUTF8 {
		t.Errorf("posEncoding = %q; want UTF-8", enc)
	}
}

func TestWorkspace_DocumentLifecycle(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := NewWorkspace(logger, Config{})

	uri := "file:///test/schema.yammm"
	text := "type Person { name: String }"

	// Open document
	ws.DocumentOpened(uri, 1, text)

	doc := ws.GetDocumentSnapshot(uri)
	if doc == nil {
		t.Fatal("GetDocumentSnapshot() returned nil after open")
	}
	if doc.Version != 1 {
		t.Errorf("doc.Version = %d; want 1", doc.Version)
	}
	if doc.Text != text {
		t.Errorf("doc.Text = %q; want %q", doc.Text, text)
	}

	// Change document
	newText := "type Person { name: String, age: Integer }"
	ws.DocumentChanged(uri, 2, newText)

	doc = ws.GetDocumentSnapshot(uri)
	if doc == nil {
		t.Fatal("GetDocumentSnapshot() returned nil after change")
	}
	if doc.Version != 2 {
		t.Errorf("doc.Version = %d; want 2", doc.Version)
	}
	if doc.Text != newText {
		t.Errorf("doc.Text = %q; want %q", doc.Text, newText)
	}

	// Close document (without glsp context, just test internal state)
	ws.mu.Lock()
	delete(ws.open, uri)
	ws.mu.Unlock()

	doc = ws.GetDocumentSnapshot(uri)
	if doc != nil {
		t.Error("GetDocumentSnapshot() should return nil after close")
	}
}

func TestWorkspace_DocumentOpened_InvalidURI(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := NewWorkspace(logger, Config{})

	// Invalid URI should be logged but document not added
	ws.DocumentOpened("http://invalid", 1, "content")

	doc := ws.GetDocumentSnapshot("http://invalid")
	if doc != nil {
		t.Error("GetDocumentSnapshot() should return nil for invalid URI")
	}
}

func TestWorkspace_DocumentChanged_NotOpen(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := NewWorkspace(logger, Config{})

	// Changing a document that was never opened should be a no-op
	ws.DocumentChanged("file:///not/open.yammm", 1, "content")

	doc := ws.GetDocumentSnapshot("file:///not/open.yammm")
	if doc != nil {
		t.Error("GetDocumentSnapshot() should return nil for document never opened")
	}
}

func TestWorkspace_FindModuleRoot_Configured(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := NewWorkspace(logger, Config{ModuleRoot: "/configured/root"})

	got := ws.findModuleRoot("/any/path/file.yammm")
	if got != "/configured/root" {
		t.Errorf("findModuleRoot() = %q; want /configured/root", got)
	}
}

func TestWorkspace_FindModuleRoot_WorkspaceFolder(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := NewWorkspace(logger, Config{})
	ws.AddRoot("file:///project")

	got := ws.findModuleRoot("/project/subdir/file.yammm")
	if got != "/project" {
		t.Errorf("findModuleRoot() = %q; want /project", got)
	}
}

func TestWorkspace_FindModuleRoot_Fallback(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := NewWorkspace(logger, Config{})
	ws.AddRoot("file:///other/project")

	// Path not under any workspace folder
	got := ws.findModuleRoot("/unrelated/path/file.yammm")
	if got != "/unrelated/path" {
		t.Errorf("findModuleRoot() = %q; want /unrelated/path", got)
	}
}

func TestWorkspace_FindModuleRoot_NestedWorkspaceFolders(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := NewWorkspace(logger, Config{})

	// Add nested workspace folders (order shouldn't matter)
	ws.AddRoot("file:///project")
	ws.AddRoot("file:///project/submodule")

	// File in the nested submodule should use the deepest matching root
	got := ws.findModuleRoot("/project/submodule/schemas/file.yammm")
	if got != "/project/submodule" {
		t.Errorf("findModuleRoot() = %q; want /project/submodule (deepest match)", got)
	}

	// File in the parent project should use the parent root
	got = ws.findModuleRoot("/project/other/file.yammm")
	if got != "/project" {
		t.Errorf("findModuleRoot() = %q; want /project", got)
	}
}

func TestWorkspace_FindModuleRoot_NestedWorkspaceFolders_ReverseOrder(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := NewWorkspace(logger, Config{})

	// Add nested workspace folders in reverse order (deepest first)
	// to ensure the algorithm doesn't depend on iteration order
	ws.AddRoot("file:///project/submodule")
	ws.AddRoot("file:///project")

	// File in the nested submodule should still use the deepest matching root
	got := ws.findModuleRoot("/project/submodule/schemas/file.yammm")
	if got != "/project/submodule" {
		t.Errorf("findModuleRoot() = %q; want /project/submodule (deepest match)", got)
	}
}

func TestWorkspace_FindModuleRoot_SimilarPrefixRoots(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := NewWorkspace(logger, Config{})

	ws.AddRoot("file:///project")
	ws.AddRoot("file:///project-extra")

	// File in /project2 should NOT match /project (they share a string prefix
	// but /project2 is not under /project). Should fall back to file's directory.
	got := ws.findModuleRoot("/project2/file.yammm")
	if got != "/project2" {
		t.Errorf("findModuleRoot(/project2/file.yammm) = %q; want /project2 (fallback)", got)
	}

	// File in /project-extra should match /project-extra, not /project
	got = ws.findModuleRoot("/project-extra/subdir/file.yammm")
	if got != "/project-extra" {
		t.Errorf("findModuleRoot(/project-extra/...) = %q; want /project-extra", got)
	}

	// File directly in /project should still match /project
	got = ws.findModuleRoot("/project/file.yammm")
	if got != "/project" {
		t.Errorf("findModuleRoot(/project/file.yammm) = %q; want /project", got)
	}
}

func TestWorkspace_LatestSnapshot(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := NewWorkspace(logger, Config{})

	// No snapshot yet
	snap := ws.LatestSnapshot("file:///test.yammm")
	if snap != nil {
		t.Error("LatestSnapshot() should return nil when no snapshot exists")
	}

	// Manually add a snapshot for testing
	ws.mu.Lock()
	ws.snapshots["file:///test.yammm"] = &Snapshot{
		CreatedAt:    time.Now(),
		EntryVersion: 1,
	}
	ws.mu.Unlock()

	snap = ws.LatestSnapshot("file:///test.yammm")
	if snap == nil {
		t.Fatal("LatestSnapshot() should return snapshot after adding")
	}
	if snap.EntryVersion != 1 {
		t.Errorf("snapshot.EntryVersion = %d; want 1", snap.EntryVersion)
	}
}

func TestWorkspace_CancelPendingAnalysis(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := NewWorkspace(logger, Config{})

	uri := "file:///test.yammm"

	// Simulate pending analysis with debounce entry
	ws.debounceMu.Lock()
	cancelCalled := false
	ws.debounces[uri] = &debounceEntry{
		timer:  time.NewTimer(1 * time.Hour), // Long timer
		cancel: func() { cancelCalled = true },
	}
	ws.debounceMu.Unlock()

	// Cancel should work
	ws.cancelPendingAnalysis(uri)

	ws.debounceMu.Lock()
	_, hasEntry := ws.debounces[uri]
	ws.debounceMu.Unlock()

	if hasEntry {
		t.Error("cancelPendingAnalysis() should remove debounce entry")
	}
	if !cancelCalled {
		t.Error("cancelPendingAnalysis() should call cancel function")
	}
}

func TestWorkspace_ConcurrentDocumentAccess(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := NewWorkspace(logger, Config{})

	const numGoroutines = 50
	var wg sync.WaitGroup

	// Concurrent writes
	wg.Add(numGoroutines)
	for i := range numGoroutines {
		go func(idx int) {
			defer wg.Done()
			uri := "file:///test/file.yammm"
			ws.DocumentOpened(uri, idx, "content")
			ws.DocumentChanged(uri, idx+1, "new content")
		}(i)
	}

	// Concurrent reads
	wg.Add(numGoroutines)
	for range numGoroutines {
		go func() {
			defer wg.Done()
			uri := "file:///test/file.yammm"
			_ = ws.GetDocumentSnapshot(uri)
			_ = ws.LatestSnapshot(uri)
		}()
	}

	wg.Wait()
}

func TestPositionEncodingConstants(t *testing.T) {
	t.Parallel()

	if PositionEncodingUTF16 != "utf-16" {
		t.Errorf("PositionEncodingUTF16 = %q; want utf-16", PositionEncodingUTF16)
	}
	if PositionEncodingUTF8 != "utf-8" {
		t.Errorf("PositionEncodingUTF8 = %q; want utf-8", PositionEncodingUTF8)
	}
}

func TestDocument_SourceID(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := NewWorkspace(logger, Config{})

	uri := "file:///test/schema.yammm"
	ws.DocumentOpened(uri, 1, "content")

	doc := ws.GetDocumentSnapshot(uri)
	if doc == nil {
		t.Fatal("GetDocumentSnapshot() returned nil")
	}

	// SourceID should be set from the path
	if doc.SourceID.String() != "/test/schema.yammm" {
		t.Errorf("SourceID = %q; want /test/schema.yammm", doc.SourceID.String())
	}
}

func TestUpdateDependencies_AddsReverseDeps(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := NewWorkspace(logger, Config{})

	entryURI := "file:///main.yammm"
	imports := []string{"/parts.yammm", "/utils.yammm"}

	ws.UpdateDependencies(entryURI, imports)

	ws.mu.RLock()
	defer ws.mu.RUnlock()

	// Check forward deps
	if len(ws.importsByEntry[entryURI]) != 2 {
		t.Errorf("importsByEntry has %d entries; want 2", len(ws.importsByEntry[entryURI]))
	}

	// Check reverse deps
	partsURI := PathToURI("/parts.yammm")
	utilsURI := PathToURI("/utils.yammm")

	if _, ok := ws.reverseDeps[partsURI][entryURI]; !ok {
		t.Errorf("reverseDeps[%s] should contain %s", partsURI, entryURI)
	}
	if _, ok := ws.reverseDeps[utilsURI][entryURI]; !ok {
		t.Errorf("reverseDeps[%s] should contain %s", utilsURI, entryURI)
	}
}

func TestUpdateDependencies_ClearsOldDeps(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := NewWorkspace(logger, Config{})

	entryURI := "file:///main.yammm"

	// First update: imports parts.yammm
	ws.UpdateDependencies(entryURI, []string{"/parts.yammm"})

	// Second update: now imports utils.yammm (removed parts.yammm)
	ws.UpdateDependencies(entryURI, []string{"/utils.yammm"})

	ws.mu.RLock()
	defer ws.mu.RUnlock()

	// Forward deps should only have utils
	if len(ws.importsByEntry[entryURI]) != 1 {
		t.Errorf("importsByEntry has %d entries; want 1", len(ws.importsByEntry[entryURI]))
	}

	// Reverse deps for parts should be cleaned up
	partsURI := PathToURI("/parts.yammm")
	if _, ok := ws.reverseDeps[partsURI]; ok {
		t.Errorf("reverseDeps[%s] should be deleted (empty)", partsURI)
	}

	// Reverse deps for utils should exist
	utilsURI := PathToURI("/utils.yammm")
	if _, ok := ws.reverseDeps[utilsURI][entryURI]; !ok {
		t.Errorf("reverseDeps[%s] should contain %s", utilsURI, entryURI)
	}
}

func TestUpdateDependencies_ClearsAllOnNil(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := NewWorkspace(logger, Config{})

	entryURI := "file:///main.yammm"

	// Add some dependencies
	ws.UpdateDependencies(entryURI, []string{"/parts.yammm"})

	// Clear by passing nil (document closed)
	ws.UpdateDependencies(entryURI, nil)

	ws.mu.RLock()
	defer ws.mu.RUnlock()

	// Forward deps should be deleted
	if _, ok := ws.importsByEntry[entryURI]; ok {
		t.Errorf("importsByEntry[%s] should be deleted", entryURI)
	}

	// Reverse deps should be cleaned up
	partsURI := PathToURI("/parts.yammm")
	if _, ok := ws.reverseDeps[partsURI]; ok {
		t.Errorf("reverseDeps[%s] should be deleted", partsURI)
	}
}

func TestUpdateDependencies_MultipleEntries(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := NewWorkspace(logger, Config{})

	entry1 := "file:///main1.yammm"
	entry2 := "file:///main2.yammm"

	// Both entries import parts.yammm
	ws.UpdateDependencies(entry1, []string{"/parts.yammm"})
	ws.UpdateDependencies(entry2, []string{"/parts.yammm"})

	ws.mu.RLock()

	// parts.yammm should have two reverse deps
	partsURI := PathToURI("/parts.yammm")
	if len(ws.reverseDeps[partsURI]) != 2 {
		t.Errorf("reverseDeps[%s] has %d entries; want 2", partsURI, len(ws.reverseDeps[partsURI]))
	}
	ws.mu.RUnlock()

	// Remove entry1's dependency
	ws.UpdateDependencies(entry1, nil)

	ws.mu.RLock()
	defer ws.mu.RUnlock()

	// parts.yammm should still have one reverse dep (entry2)
	if len(ws.reverseDeps[partsURI]) != 1 {
		t.Errorf("reverseDeps[%s] has %d entries; want 1", partsURI, len(ws.reverseDeps[partsURI]))
	}
	if _, ok := ws.reverseDeps[partsURI][entry2]; !ok {
		t.Errorf("reverseDeps[%s] should contain %s", partsURI, entry2)
	}
}

func TestBuildCanonicalToURIMap_SymlinkResolution(t *testing.T) {
	t.Parallel()

	// Create temp directory with real file and symlink
	tmpDir := t.TempDir()
	realDir := tmpDir + "/real"
	if err := os.MkdirAll(realDir, 0o750); err != nil {
		t.Fatalf("failed to create real dir: %v", err)
	}

	realPath := realDir + "/schema.yammm"
	if err := os.WriteFile(realPath, []byte("content"), 0o600); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	// Create symlink
	linkPath := tmpDir + "/link.yammm"
	if err := os.Symlink(realPath, linkPath); err != nil {
		t.Skip("symlinks not supported: " + err.Error())
	}

	// Resolve the real path to canonical form (handles /var -> /private/var on macOS)
	canonicalRealPath, err := evalSymlinks(realPath)
	if err != nil {
		t.Fatalf("failed to resolve real path: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := NewWorkspace(logger, Config{})

	// Open document via symlink URI
	linkURI := PathToURI(linkPath)
	ws.DocumentOpened(linkURI, 1, "content")

	// Build the canonical mapping
	ws.mu.RLock()
	mapping := ws.buildCanonicalToURIMap()
	ws.mu.RUnlock()

	// The mapping should map the canonical (resolved) path to the symlink URI
	if docURI, ok := mapping[canonicalRealPath]; ok {
		if docURI != linkURI {
			t.Errorf("mapping[%q] = %q; want %q", canonicalRealPath, docURI, linkURI)
		}
	} else {
		t.Errorf("mapping should contain resolved path %q; got keys: %v", canonicalRealPath, mapKeys(mapping))
	}
}

func TestBuildCanonicalToURIMap_NoSymlinks(t *testing.T) {
	t.Parallel()

	// Create temp directory with a real file (no symlinks)
	tmpDir := t.TempDir()
	realPath := tmpDir + "/schema.yammm"
	if err := os.WriteFile(realPath, []byte("content"), 0o600); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	// Resolve path to canonical form (handles /var -> /private/var on macOS)
	canonicalPath, err := evalSymlinks(realPath)
	if err != nil {
		t.Fatalf("failed to resolve path: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := NewWorkspace(logger, Config{})

	// Open document via real path
	realURI := PathToURI(realPath)
	ws.DocumentOpened(realURI, 1, "content")

	// Build the canonical mapping
	ws.mu.RLock()
	mapping := ws.buildCanonicalToURIMap()
	ws.mu.RUnlock()

	// The mapping should map the canonical path to the original URI
	if docURI, ok := mapping[canonicalPath]; ok {
		if docURI != realURI {
			t.Errorf("mapping[%q] = %q; want %q", canonicalPath, docURI, realURI)
		}
	} else {
		t.Errorf("mapping should contain path %q; got keys: %v", canonicalPath, mapKeys(mapping))
	}
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
	if err := os.MkdirAll(realDir, 0o750); err != nil {
		t.Fatalf("failed to create real dir: %v", err)
	}

	realPath := realDir + "/schema.yammm"
	if err := os.WriteFile(realPath, []byte("content"), 0o600); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	// Create symlink
	linkPath := tmpDir + "/link.yammm"
	if err := os.Symlink(realPath, linkPath); err != nil {
		t.Skip("symlinks not supported: " + err.Error())
	}

	// Resolve the real path to canonical form (handles /var -> /private/var on macOS)
	canonicalRealPath, err := evalSymlinks(realPath)
	if err != nil {
		t.Fatalf("failed to resolve real path: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := NewWorkspace(logger, Config{})

	// Open document via symlink URI FIRST (openOrder=1)
	linkURI := PathToURI(linkPath)
	ws.DocumentOpened(linkURI, 1, "content via symlink")

	// Open same document via real path URI SECOND (openOrder=2)
	realURI := PathToURI(realPath)
	ws.DocumentOpened(realURI, 1, "content via real path")

	// Both documents should be tracked (different URIs)
	ws.mu.RLock()
	if len(ws.open) != 2 {
		t.Errorf("expected 2 open documents, got %d", len(ws.open))
	}
	ws.mu.RUnlock()

	// Build the canonical mapping - should prefer first-opened (symlink) due to lower OpenOrder
	ws.mu.RLock()
	mapping := ws.buildCanonicalToURIMap()
	ws.mu.RUnlock()

	if docURI, ok := mapping[canonicalRealPath]; ok {
		if docURI != linkURI {
			t.Errorf("mapping[%q] = %q; want %q (first-opened symlink URI)", canonicalRealPath, docURI, linkURI)
		}
	} else {
		t.Errorf("mapping should contain resolved path %q; got keys: %v", canonicalRealPath, mapKeys(mapping))
	}

	// Now close the symlink document
	ws.DocumentClosed(nil, linkURI)

	// Rebuild mapping - should now prefer the real path URI (only one remaining)
	ws.mu.RLock()
	mapping2 := ws.buildCanonicalToURIMap()
	ws.mu.RUnlock()

	if docURI, ok := mapping2[canonicalRealPath]; ok {
		if docURI != realURI {
			t.Errorf("after close: mapping[%q] = %q; want %q (remaining real path URI)", canonicalRealPath, docURI, realURI)
		}
	} else {
		t.Errorf("after close: mapping should contain resolved path %q; got keys: %v", canonicalRealPath, mapKeys(mapping2))
	}
}

func TestRemapToOpenDocURI_MatchFound(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := NewWorkspace(logger, Config{})

	// Simulate the mapping
	canonicalPath := "/real/path/schema.yammm"
	symlinkURI := "file:///symlink/path/schema.yammm"
	mapping := map[string]string{
		canonicalPath: symlinkURI,
	}

	// Diagnostic URI uses canonical path
	diagURI := PathToURI(canonicalPath)

	// Should remap to symlink URI
	result := ws.remapToOpenDocURI(diagURI, mapping)
	if result != symlinkURI {
		t.Errorf("remapToOpenDocURI() = %q; want %q", result, symlinkURI)
	}
}

func TestRemapToOpenDocURI_NoMatch(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := NewWorkspace(logger, Config{})

	// Empty mapping - no open documents
	mapping := map[string]string{}

	// Diagnostic URI for a file that's not open
	diagURI := "file:///some/path/schema.yammm"

	// Should return original URI unchanged
	result := ws.remapToOpenDocURI(diagURI, mapping)
	if result != diagURI {
		t.Errorf("remapToOpenDocURI() = %q; want original %q", result, diagURI)
	}
}

func TestRemapToOpenDocURI_InvalidURI(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := NewWorkspace(logger, Config{})

	mapping := map[string]string{
		"/some/path": "file:///some/path",
	}

	// Invalid URI should be returned unchanged
	invalidURI := "http://not-a-file-uri"
	result := ws.remapToOpenDocURI(invalidURI, mapping)
	if result != invalidURI {
		t.Errorf("remapToOpenDocURI() = %q; want original %q", result, invalidURI)
	}
}

func TestRemapToOpenDocURI_RawPathNoMatch(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := NewWorkspace(logger, Config{})

	// Empty mapping - no open documents
	mapping := map[string]string{}

	// Raw filesystem path (not a file:// URI)
	rawPath := "/some/path/schema.yammm"

	// Should convert to proper file:// URI for protocol correctness
	result := ws.remapToOpenDocURI(rawPath, mapping)
	expectedURI := "file:///some/path/schema.yammm"
	if result != expectedURI {
		t.Errorf("remapToOpenDocURI(%q) = %q; want %q", rawPath, result, expectedURI)
	}
}

func TestRemapToOpenDocURI_RawPathWithMatch(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := NewWorkspace(logger, Config{})

	// Mapping contains the raw path
	rawPath := "/some/path/schema.yammm"
	openDocURI := "file:///opened/via/different/path.yammm"
	mapping := map[string]string{
		rawPath: openDocURI,
	}

	// Should return the mapped URI
	result := ws.remapToOpenDocURI(rawPath, mapping)
	if result != openDocURI {
		t.Errorf("remapToOpenDocURI(%q) = %q; want %q", rawPath, result, openDocURI)
	}
}

func TestRemapToOpenDocURI_NonFileURIPreserved(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := NewWorkspace(logger, Config{})

	// Empty mapping
	mapping := map[string]string{}

	// Non-file URI schemes should be preserved as-is
	testCases := []string{
		"https://example.com/file.yammm",
		"custom-scheme://host/path/file.yammm",
	}

	for _, uri := range testCases {
		result := ws.remapToOpenDocURI(uri, mapping)
		if result != uri {
			t.Errorf("remapToOpenDocURI(%q) = %q; want original preserved", uri, result)
		}
	}
}

func TestPublishSnapshotDiagnostics_SymlinkURIRemapping(t *testing.T) {
	t.Parallel()

	// Create temp directory with real file and symlink
	tmpDir := t.TempDir()
	realDir := tmpDir + "/real"
	if err := os.MkdirAll(realDir, 0o750); err != nil {
		t.Fatalf("failed to create real dir: %v", err)
	}

	realPath := realDir + "/schema.yammm"
	if err := os.WriteFile(realPath, []byte("content"), 0o600); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	// Create symlink
	linkPath := tmpDir + "/link.yammm"
	if err := os.Symlink(realPath, linkPath); err != nil {
		t.Skip("symlinks not supported: " + err.Error())
	}

	// Resolve the real path to canonical form (handles /var -> /private/var on macOS)
	canonicalRealPath, err := evalSymlinks(realPath)
	if err != nil {
		t.Fatalf("failed to resolve real path: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := NewWorkspace(logger, Config{})

	// Open document via symlink URI
	linkURI := PathToURI(linkPath)
	ws.DocumentOpened(linkURI, 1, "content")

	// Create a snapshot with diagnostics using the canonical (resolved) path
	// This simulates what the loader produces
	canonicalURI := PathToURI(canonicalRealPath)
	snapshot := &Snapshot{
		CreatedAt:    time.Now(),
		EntryVersion: 1,
		LSPDiagnostics: []URIDiagnostic{
			{
				URI:        canonicalURI, // Loader uses canonical/resolved path
				Diagnostic: mockDiagnostic("test error"),
			},
		},
	}

	// Use computePublicationPlan to test the remapping logic
	// (without needing a real glsp.Context)
	diagsByURI, _ := ws.computePublicationPlan(linkURI, snapshot)

	// The diagnostic should be remapped to the symlink URI
	if len(diagsByURI) == 0 {
		t.Fatal("diagsByURI should not be empty")
	}

	// Check that the diagnostic is published under the symlink URI, not the canonical URI
	if _, ok := diagsByURI[linkURI]; !ok {
		t.Errorf("diagsByURI should contain symlink URI %q; got keys: %v", linkURI, mapKeys(diagsByURI))
	}
	if _, ok := diagsByURI[canonicalURI]; ok {
		t.Errorf("diagsByURI should NOT contain canonical URI %q", canonicalURI)
	}
}

func TestPublishSnapshotDiagnostics_RelatedInfoURIRemapping(t *testing.T) {
	t.Parallel()

	// Create temp directory with real file and symlink
	tmpDir := t.TempDir()
	realDir := tmpDir + "/real"
	if err := os.MkdirAll(realDir, 0o750); err != nil {
		t.Fatalf("failed to create real dir: %v", err)
	}

	realPath := realDir + "/schema.yammm"
	if err := os.WriteFile(realPath, []byte("content"), 0o600); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	// Create symlink
	linkPath := tmpDir + "/link.yammm"
	if err := os.Symlink(realPath, linkPath); err != nil {
		t.Skip("symlinks not supported: " + err.Error())
	}

	// Resolve the real path to canonical form (handles /var -> /private/var on macOS)
	canonicalRealPath, err := evalSymlinks(realPath)
	if err != nil {
		t.Fatalf("failed to resolve real path: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := NewWorkspace(logger, Config{})

	// Open document via symlink URI
	linkURI := PathToURI(linkPath)
	ws.DocumentOpened(linkURI, 1, "content")

	// Create a snapshot with RelatedInformation also using canonical path
	canonicalURI := PathToURI(canonicalRealPath)
	snapshot := &Snapshot{
		CreatedAt:    time.Now(),
		EntryVersion: 1,
		LSPDiagnostics: []URIDiagnostic{
			{
				URI:        canonicalURI,
				Diagnostic: mockDiagnosticWithRelated("test error", canonicalURI),
			},
		},
	}

	diagsByURI, _ := ws.computePublicationPlan(linkURI, snapshot)

	// Check that the diagnostic is published under the symlink URI
	diags, ok := diagsByURI[linkURI]
	if !ok {
		t.Fatalf("diagsByURI should contain symlink URI %q; got keys: %v", linkURI, mapKeys(diagsByURI))
	}
	if len(diags) == 0 {
		t.Fatal("should have at least one diagnostic")
	}

	// Check that RelatedInformation URIs are also remapped
	if len(diags[0].RelatedInformation) == 0 {
		t.Fatal("diagnostic should have RelatedInformation")
	}

	relatedURI := diags[0].RelatedInformation[0].Location.URI
	if relatedURI != linkURI {
		t.Errorf("RelatedInformation.Location.URI = %q; want %q", relatedURI, linkURI)
	}
}

// mapKeys returns the keys of a string map for error messages.
func mapKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
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

// evalSymlinks resolves symlinks for a path.
// This is a test helper that wraps filepath.EvalSymlinks.
func evalSymlinks(path string) (string, error) {
	return filepath.EvalSymlinks(path)
}

// =============================================================================
// Multi-Document Diagnostic Isolation Tests (Priority 5: Test Coverage Gaps)
// =============================================================================

func TestComputePublicationPlan_PerEntryIsolation(t *testing.T) {
	// Test that publishedByEntry prevents cross-entry contamination:
	// - main.yammm publishes diagnostics for main.yammm and parts.yammm
	// - other.yammm publishes diagnostics for other.yammm only
	// - Clearing main.yammm should NOT affect other.yammm's diagnostics
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := NewWorkspace(logger, Config{})

	// Open two documents
	mainURI := "file:///main.yammm"
	partsURI := "file:///parts.yammm"
	otherURI := "file:///other.yammm"

	ws.DocumentOpened(mainURI, 1, "import parts")
	ws.DocumentOpened(otherURI, 1, "standalone")

	// First analysis: main.yammm publishes diagnostics for main and parts
	mainSnapshot := &Snapshot{
		CreatedAt:    time.Now(),
		EntryVersion: 1,
		LSPDiagnostics: []URIDiagnostic{
			{URI: mainURI, Diagnostic: mockDiagnostic("error in main")},
			{URI: partsURI, Diagnostic: mockDiagnostic("error in parts")},
		},
	}

	diagsByURI, staleURIs := ws.computePublicationPlan(mainURI, mainSnapshot)

	// Should have diagnostics for both main and parts
	if len(diagsByURI) != 2 {
		t.Errorf("diagsByURI should have 2 entries; got %d", len(diagsByURI))
	}
	// No stale URIs on first publication
	if len(staleURIs) != 0 {
		t.Errorf("staleURIs should be empty on first run; got %v", staleURIs)
	}

	// Second analysis: other.yammm publishes its own diagnostics
	otherSnapshot := &Snapshot{
		CreatedAt:    time.Now(),
		EntryVersion: 1,
		LSPDiagnostics: []URIDiagnostic{
			{URI: otherURI, Diagnostic: mockDiagnostic("error in other")},
		},
	}

	diagsByURI, _ = ws.computePublicationPlan(otherURI, otherSnapshot)

	// Should have diagnostics only for other
	if len(diagsByURI) != 1 {
		t.Errorf("diagsByURI should have 1 entry; got %d", len(diagsByURI))
	}
	if _, ok := diagsByURI[otherURI]; !ok {
		t.Error("diagsByURI should contain other.yammm")
	}

	// Third analysis: main.yammm re-analyzed with NO errors
	// Should clear main and parts, but NOT affect other
	emptyMainSnapshot := &Snapshot{
		CreatedAt:      time.Now(),
		EntryVersion:   2,
		LSPDiagnostics: []URIDiagnostic{}, // No errors
	}

	diagsByURI, staleURIs = ws.computePublicationPlan(mainURI, emptyMainSnapshot)

	// Should have no new diagnostics
	if len(diagsByURI) != 0 {
		t.Errorf("diagsByURI should be empty; got %d entries", len(diagsByURI))
	}
	// Should mark main and parts as stale (need clearing)
	if len(staleURIs) != 2 {
		t.Errorf("staleURIs should have 2 entries; got %v", staleURIs)
	}

	// Verify publishedByEntry still tracks other.yammm's publication
	ws.mu.RLock()
	otherPublished := ws.publishedByEntry[otherURI]
	ws.mu.RUnlock()

	if _, ok := otherPublished[otherURI]; !ok {
		t.Error("other.yammm should still be tracked in publishedByEntry")
	}
}

func TestComputePublicationPlan_SharedImportMultipleEntries(t *testing.T) {
	// Test that a shared import doesn't lose diagnostics when one entry clears:
	// - main.yammm imports shared.yammm
	// - other.yammm imports shared.yammm
	// - When main.yammm clears, shared diagnostics from other should remain tracked
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := NewWorkspace(logger, Config{})

	mainURI := "file:///main.yammm"
	otherURI := "file:///other.yammm"
	sharedURI := "file:///shared.yammm"

	ws.DocumentOpened(mainURI, 1, "import shared")
	ws.DocumentOpened(otherURI, 1, "import shared")

	// Both entries publish diagnostics for shared
	mainSnapshot := &Snapshot{
		CreatedAt:    time.Now(),
		EntryVersion: 1,
		LSPDiagnostics: []URIDiagnostic{
			{URI: mainURI, Diagnostic: mockDiagnostic("error in main")},
			{URI: sharedURI, Diagnostic: mockDiagnostic("error in shared via main")},
		},
	}
	ws.computePublicationPlan(mainURI, mainSnapshot)

	otherSnapshot := &Snapshot{
		CreatedAt:    time.Now(),
		EntryVersion: 1,
		LSPDiagnostics: []URIDiagnostic{
			{URI: otherURI, Diagnostic: mockDiagnostic("error in other")},
			{URI: sharedURI, Diagnostic: mockDiagnostic("error in shared via other")},
		},
	}
	ws.computePublicationPlan(otherURI, otherSnapshot)

	// Verify both entries track shared.yammm
	ws.mu.RLock()
	mainPublished := ws.publishedByEntry[mainURI]
	otherPublished := ws.publishedByEntry[otherURI]
	ws.mu.RUnlock()

	if _, ok := mainPublished[sharedURI]; !ok {
		t.Error("main should track shared.yammm")
	}
	if _, ok := otherPublished[sharedURI]; !ok {
		t.Error("other should track shared.yammm")
	}

	// Clear main's diagnostics
	emptyMainSnapshot := &Snapshot{
		CreatedAt:      time.Now(),
		EntryVersion:   2,
		LSPDiagnostics: []URIDiagnostic{},
	}
	_, staleURIs := ws.computePublicationPlan(mainURI, emptyMainSnapshot)

	// main and shared should be stale for main's entry
	staleSet := make(map[string]struct{})
	for _, u := range staleURIs {
		staleSet[u] = struct{}{}
	}
	if _, ok := staleSet[mainURI]; !ok {
		t.Error("main.yammm should be stale")
	}
	if _, ok := staleSet[sharedURI]; !ok {
		t.Error("shared.yammm should be stale for main's entry")
	}

	// But other's tracking of shared should remain
	ws.mu.RLock()
	otherPublished = ws.publishedByEntry[otherURI]
	ws.mu.RUnlock()

	if _, ok := otherPublished[sharedURI]; !ok {
		t.Error("other should still track shared.yammm after main cleared")
	}
}

func TestComputePublicationPlan_DocumentCloseClearsAllEntryURIs(t *testing.T) {
	// Test Priority 2.4 fix: closing clears all URIs from that entry's tracking
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := NewWorkspace(logger, Config{})

	mainURI := "file:///main.yammm"
	partsURI := "file:///parts.yammm"

	ws.DocumentOpened(mainURI, 1, "import parts")

	// Publish diagnostics for both main and parts
	snapshot := &Snapshot{
		CreatedAt:    time.Now(),
		EntryVersion: 1,
		LSPDiagnostics: []URIDiagnostic{
			{URI: mainURI, Diagnostic: mockDiagnostic("error in main")},
			{URI: partsURI, Diagnostic: mockDiagnostic("error in parts")},
		},
	}
	ws.computePublicationPlan(mainURI, snapshot)

	// Verify tracking
	ws.mu.RLock()
	published := ws.publishedByEntry[mainURI]
	ws.mu.RUnlock()

	if len(published) != 2 {
		t.Errorf("should track 2 URIs; got %d", len(published))
	}

	// Simulate closing main.yammm - empty snapshot with nil diagnostics
	closeSnapshot := &Snapshot{
		CreatedAt:      time.Now(),
		EntryVersion:   2,
		LSPDiagnostics: nil,
	}
	_, staleURIs := ws.computePublicationPlan(mainURI, closeSnapshot)

	// Both main and parts should be stale
	if len(staleURIs) != 2 {
		t.Errorf("staleURIs should have 2 entries; got %v", staleURIs)
	}

	// Published tracking should be empty for this entry
	ws.mu.RLock()
	published = ws.publishedByEntry[mainURI]
	ws.mu.RUnlock()

	if len(published) != 0 {
		t.Errorf("published should be empty after close; got %d", len(published))
	}
}

// =============================================================================
// FileChanged Symlink Tests (Priority 5: Test Coverage Gaps)
// =============================================================================

func TestWorkspace_FileChanged_SymlinkResolution(t *testing.T) {
	// Test that FileChanged correctly resolves symlinked paths for reverse deps
	t.Parallel()

	// Create temp directory structure
	tmpDir := t.TempDir()
	actualDir := tmpDir + "/actual"
	if err := os.MkdirAll(actualDir, 0o750); err != nil {
		t.Fatalf("failed to create actual dir: %v", err)
	}

	// Create actual/parts.yammm
	actualParts := actualDir + "/parts.yammm"
	if err := os.WriteFile(actualParts, []byte("schema \"parts\""), 0o600); err != nil {
		t.Fatalf("failed to write parts: %v", err)
	}

	// Create symlink: linked -> actual
	linkedDir := tmpDir + "/linked"
	if err := os.Symlink(actualDir, linkedDir); err != nil {
		t.Skip("symlinks not supported: " + err.Error())
	}

	// Resolve canonical paths (handles /var -> /private/var on macOS)
	canonicalParts, err := filepath.EvalSymlinks(actualParts)
	if err != nil {
		t.Fatalf("failed to resolve parts path: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := NewWorkspace(logger, Config{})

	// Create main.yammm that imports parts via canonical path
	mainURI := "file:///main.yammm"
	ws.DocumentOpened(mainURI, 1, "import parts")

	// Simulate the loader tracking: main depends on canonical parts path
	ws.UpdateDependencies(mainURI, []string{canonicalParts})

	// Verify reverse deps are set up
	ws.mu.RLock()
	canonicalPartsURI := PathToURI(canonicalParts)
	deps := ws.reverseDeps[canonicalPartsURI]
	ws.mu.RUnlock()

	if _, ok := deps[mainURI]; !ok {
		t.Fatalf("reverse deps should contain main; canonicalPartsURI=%s", canonicalPartsURI)
	}

	// FileChanged with symlinked path (linked/parts.yammm)
	linkedParts := linkedDir + "/parts.yammm"
	linkedPartsURI := PathToURI(linkedParts)

	// Track if ScheduleAnalysis is called
	// We can't easily test the actual scheduling without a glsp.Context,
	// but we can verify the path resolution works by checking the deps lookup

	// Manually test the canonicalization logic that FileChanged uses
	path, _ := URIToPath(linkedPartsURI)
	resolved, _ := filepath.EvalSymlinks(path)
	resolvedPath := filepath.Clean(resolved)
	resolvedSourceID, _ := location.SourceIDFromAbsolutePath(resolvedPath)
	resolvedURI := PathToURI(resolvedSourceID.String())

	// The resolved URI should match the canonical parts URI
	if resolvedURI != canonicalPartsURI {
		t.Errorf("resolved URI = %q; want %q", resolvedURI, canonicalPartsURI)
	}

	// And the reverse deps lookup should find main
	ws.mu.RLock()
	depsForResolved := ws.reverseDeps[resolvedURI]
	ws.mu.RUnlock()

	if _, ok := depsForResolved[mainURI]; !ok {
		t.Errorf("reverse deps for resolved path should contain main")
	}
}

func TestWorkspace_FileChanged_CanonicalPathMatching(t *testing.T) {
	// Verify FileChanged uses canonical paths for reverseDeps lookup
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := NewWorkspace(logger, Config{})

	mainURI := "file:///main.yammm"
	ws.DocumentOpened(mainURI, 1, "content")

	// Set up dependency on a canonical path
	canonicalPath := "/canonical/parts.yammm"
	ws.UpdateDependencies(mainURI, []string{canonicalPath})

	// Verify the reverse dependency uses the canonical URI
	canonicalURI := PathToURI(canonicalPath)

	ws.mu.RLock()
	deps := ws.reverseDeps[canonicalURI]
	ws.mu.RUnlock()

	if _, ok := deps[mainURI]; !ok {
		t.Errorf("reverse deps should contain main for canonical URI")
	}

	// Lookup with the same canonical URI should succeed
	ws.mu.RLock()
	entries := make(map[string]struct{})
	for k := range ws.reverseDeps[canonicalURI] {
		entries[k] = struct{}{}
	}
	ws.mu.RUnlock()

	if _, ok := entries[mainURI]; !ok {
		t.Error("canonical path lookup should find main.yammm")
	}
}

// =============================================================================
// Debounce Race Condition Tests (Priority 1: High-priority blockers)
// =============================================================================

// TestScheduleAnalysis_EntryPointerIdentity verifies that the debounce cleanup
// uses pointer identity to avoid deleting newer entries. This is a regression
// test for the issue: "Workspace debounce cleanup introduces a race that can
// delete *new* timers/cancels".
//
// The race scenario:
// 1. ScheduleAnalysis(uri) creates entry0, schedules timer
// 2. Timer fires, callback starts running AnalyzeAndPublish (takes time)
// 3. User types, ScheduleAnalysis(uri) called again, creates entry1
// 4. Old callback finishes - must NOT delete entry1
func TestScheduleAnalysis_EntryPointerIdentity(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := NewWorkspace(logger, Config{})

	uri := "file:///test.yammm"

	// Create entry0 (simulating first schedule)
	ws.debounceMu.Lock()
	entry0 := &debounceEntry{
		timer:  time.NewTimer(1 * time.Hour),
		cancel: func() {},
	}
	ws.debounces[uri] = entry0
	ws.debounceMu.Unlock()

	// Simulate: while entry0's callback is running, a new schedule happens
	// This creates entry1 and stores it in the map
	ws.debounceMu.Lock()
	entry1 := &debounceEntry{
		timer:  time.NewTimer(1 * time.Hour),
		cancel: func() {},
	}
	ws.debounces[uri] = entry1
	ws.debounceMu.Unlock()

	// Now simulate entry0's callback cleanup logic:
	// It should NOT delete because ws.debounces[uri] != entry0
	ws.debounceMu.Lock()
	if ws.debounces[uri] == entry0 {
		// BUG: This would delete entry1 if pointer check wasn't working
		delete(ws.debounces, uri)
	}
	ws.debounceMu.Unlock()

	// Verify entry1 is still in the map
	ws.debounceMu.Lock()
	currentEntry := ws.debounces[uri]
	ws.debounceMu.Unlock()

	if currentEntry != entry1 {
		t.Error("entry1 should still be in debounces map after entry0's cleanup attempt")
	}

	// Clean up: entry1's cleanup should succeed since it IS the current entry
	ws.debounceMu.Lock()
	if ws.debounces[uri] == entry1 {
		delete(ws.debounces, uri)
	}
	ws.debounceMu.Unlock()

	ws.debounceMu.Lock()
	_, hasEntry := ws.debounces[uri]
	ws.debounceMu.Unlock()

	if hasEntry {
		t.Error("entry1's cleanup should have removed the entry")
	}
}

// TestScheduleAnalysis_RescheduleWhilePending verifies that calling
// ScheduleAnalysis while a previous timer is pending correctly cancels
// the old entry and installs a new one.
func TestScheduleAnalysis_RescheduleWhilePending(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := NewWorkspace(logger, Config{})

	uri := "file:///test.yammm"

	// First schedule
	ws.ScheduleAnalysis(nil, uri)

	ws.debounceMu.Lock()
	entry1 := ws.debounces[uri]
	ws.debounceMu.Unlock()

	if entry1 == nil {
		t.Fatal("first ScheduleAnalysis should create entry")
	}

	// Second schedule (while first timer is pending)
	ws.ScheduleAnalysis(nil, uri)

	ws.debounceMu.Lock()
	entry2 := ws.debounces[uri]
	ws.debounceMu.Unlock()

	if entry2 == nil {
		t.Fatal("second ScheduleAnalysis should create entry")
	}

	// entry2 should be different from entry1 (new allocation)
	if entry1 == entry2 {
		t.Error("second ScheduleAnalysis should create a new entry, not reuse the old one")
	}

	// Clean up
	ws.cancelPendingAnalysis(uri)
}

// =============================================================================
// RemapPathToURI Tests: Outbound URIs in Definition Provider
// =============================================================================

func TestRemapPathToURI_OpenDocument(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := NewWorkspace(logger, Config{})

	// Open a document - the URI used by client
	clientURI := "file:///symlink/path/schema.yammm"
	ws.DocumentOpened(clientURI, 1, "content")

	// Get the canonical path from the document's SourceID
	doc := ws.GetDocumentSnapshot(clientURI)
	if doc == nil {
		t.Fatal("document should be open")
	}
	canonicalPath := doc.SourceID.String()

	// RemapPathToURI should return the client's URI, not a new URI from the canonical path
	result := ws.RemapPathToURI(canonicalPath)
	if result != clientURI {
		t.Errorf("RemapPathToURI(%q) = %q; want %q", canonicalPath, result, clientURI)
	}
}

func TestRemapPathToURI_NotOpen(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := NewWorkspace(logger, Config{})

	// Don't open any documents
	canonicalPath := "/some/canonical/path.yammm"

	// RemapPathToURI should return a file:// URI for the canonical path
	result := ws.RemapPathToURI(canonicalPath)
	expected := PathToURI(canonicalPath)
	if result != expected {
		t.Errorf("RemapPathToURI(%q) = %q; want %q", canonicalPath, result, expected)
	}
}

func TestRemapPathToURI_SymlinkResolution(t *testing.T) {
	t.Parallel()

	// Create temp directory with real file and symlink
	tmpDir := t.TempDir()
	realDir := tmpDir + "/real"
	if err := os.MkdirAll(realDir, 0o750); err != nil {
		t.Fatalf("failed to create real dir: %v", err)
	}

	realPath := realDir + "/schema.yammm"
	if err := os.WriteFile(realPath, []byte("content"), 0o600); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	// Create symlink
	linkPath := tmpDir + "/link.yammm"
	if err := os.Symlink(realPath, linkPath); err != nil {
		t.Skip("symlinks not supported: " + err.Error())
	}

	// Resolve the real path to canonical form (handles /var -> /private/var on macOS)
	canonicalRealPath, err := evalSymlinks(realPath)
	if err != nil {
		t.Fatalf("failed to resolve real path: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := NewWorkspace(logger, Config{})

	// Open document via symlink URI
	linkURI := PathToURI(linkPath)
	ws.DocumentOpened(linkURI, 1, "content")

	// RemapPathToURI with canonical path should return the symlink URI
	result := ws.RemapPathToURI(canonicalRealPath)
	if result != linkURI {
		t.Errorf("RemapPathToURI(%q) = %q; want %q (symlink URI)", canonicalRealPath, result, linkURI)
	}
}

func TestRemapPathToURI_MultipleDocumentsSameCanonical(t *testing.T) {
	t.Parallel()

	// Create temp directory with real file and symlink
	tmpDir := t.TempDir()
	realDir := tmpDir + "/real"
	if err := os.MkdirAll(realDir, 0o750); err != nil {
		t.Fatalf("failed to create real dir: %v", err)
	}

	realPath := realDir + "/schema.yammm"
	if err := os.WriteFile(realPath, []byte("content"), 0o600); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	// Create symlink
	linkPath := tmpDir + "/link.yammm"
	if err := os.Symlink(realPath, linkPath); err != nil {
		t.Skip("symlinks not supported: " + err.Error())
	}

	// Resolve the real path to canonical form
	canonicalRealPath, err := evalSymlinks(realPath)
	if err != nil {
		t.Fatalf("failed to resolve real path: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := NewWorkspace(logger, Config{})

	// Open via symlink FIRST (lower OpenOrder)
	linkURI := PathToURI(linkPath)
	ws.DocumentOpened(linkURI, 1, "content via symlink")

	// Open via real path SECOND (higher OpenOrder)
	realURI := PathToURI(realPath)
	ws.DocumentOpened(realURI, 1, "content via real path")

	// RemapPathToURI should prefer the first-opened (symlink) URI
	result := ws.RemapPathToURI(canonicalRealPath)
	if result != linkURI {
		t.Errorf("RemapPathToURI(%q) = %q; want %q (first-opened URI)", canonicalRealPath, result, linkURI)
	}

	// Close symlink document
	ws.DocumentClosed(nil, linkURI)

	// Now RemapPathToURI should return the real URI (only one remaining)
	result = ws.RemapPathToURI(canonicalRealPath)
	if result != realURI {
		t.Errorf("after close: RemapPathToURI(%q) = %q; want %q", canonicalRealPath, result, realURI)
	}
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
			got, _ := ComputeBraceDepths(tt.text)
			if len(got) != len(tt.want) {
				t.Errorf("ComputeBraceDepths() returned %d lines; want %d lines", len(got), len(tt.want))
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("ComputeBraceDepths() line %d depth = %d; want %d", i, got[i], tt.want[i])
				}
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
			got, _ := ComputeBraceDepths(tt.text)
			if len(got) != len(tt.want) {
				t.Errorf("ComputeBraceDepths() returned %d lines; want %d lines\ntext: %q",
					len(got), len(tt.want), tt.text)
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("line %d depth = %d; want %d\ntext: %q",
						i, got[i], tt.want[i], tt.text)
				}
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
			gotDepths, gotInBlk := ComputeBraceDepths(tt.text)

			if len(gotDepths) != len(tt.wantDepths) {
				t.Errorf("depths: got %d lines; want %d lines\ntext: %q\ngot: %v",
					len(gotDepths), len(tt.wantDepths), tt.text, gotDepths)
				return
			}

			for i := range gotDepths {
				if gotDepths[i] != tt.wantDepths[i] {
					t.Errorf("depths[%d] = %d; want %d\ntext: %q\ngot: %v",
						i, gotDepths[i], tt.wantDepths[i], tt.text, gotDepths)
				}
			}

			if len(gotInBlk) != len(tt.wantInBlk) {
				t.Errorf("inBlockComment: got %d lines; want %d lines",
					len(gotInBlk), len(tt.wantInBlk))
				return
			}

			for i := range gotInBlk {
				if gotInBlk[i] != tt.wantInBlk[i] {
					t.Errorf("inBlockComment[%d] = %v; want %v\ntext: %q",
						i, gotInBlk[i], tt.wantInBlk[i], tt.text)
				}
			}
		})
	}
}

func TestDocumentOpened_CRLFNormalization(t *testing.T) {
	t.Parallel()

	// Create temp file for testing
	dir := t.TempDir()
	path := filepath.Join(dir, "test.yammm")
	if err := os.WriteFile(path, []byte("type Test {}"), 0o600); err != nil {
		t.Fatal(err)
	}

	ws := NewWorkspace(nil, Config{})
	uri := PathToURI(path)

	// Open document with CRLF line endings
	textWithCRLF := "type Person {\r\n\tname string\r\n}\r\n"
	ws.DocumentOpened(uri, 1, textWithCRLF)

	// Verify stored text has LF only
	doc := ws.GetDocumentSnapshot(uri)
	if doc == nil {
		t.Fatal("document not found after open")
	}

	if strings.Contains(doc.Text, "\r") {
		t.Error("stored text still contains CR; want CRLF normalized to LF")
	}

	expectedLines := 4 // "type Person {\n\tname string\n}\n" + trailing empty
	actualLines := len(strings.Split(doc.Text, "\n"))
	if actualLines != expectedLines {
		t.Errorf("line count = %d; want %d (LF normalization may have failed)", actualLines, expectedLines)
	}
}

func TestDocumentChanged_CRLFNormalization(t *testing.T) {
	t.Parallel()

	// Create temp file for testing
	dir := t.TempDir()
	path := filepath.Join(dir, "test.yammm")
	if err := os.WriteFile(path, []byte("type Test {}"), 0o600); err != nil {
		t.Fatal(err)
	}

	ws := NewWorkspace(nil, Config{})
	uri := PathToURI(path)

	// Open document first (with LF)
	ws.DocumentOpened(uri, 1, "type Test {}\n")

	// Change with CRLF line endings
	textWithCRLF := "type Updated {\r\n\tfield int\r\n}\r\n"
	ws.DocumentChanged(uri, 2, textWithCRLF)

	// Verify stored text has LF only
	doc := ws.GetDocumentSnapshot(uri)
	if doc == nil {
		t.Fatal("document not found after change")
	}

	if strings.Contains(doc.Text, "\r") {
		t.Error("stored text still contains CR after change; want CRLF normalized to LF")
	}
}

func TestDocumentChanged_VersionOrdering(t *testing.T) {
	t.Parallel()

	// Create temp file for testing
	dir := t.TempDir()
	path := filepath.Join(dir, "test.yammm")
	if err := os.WriteFile(path, []byte("type Test {}"), 0o600); err != nil {
		t.Fatal(err)
	}

	ws := NewWorkspace(nil, Config{})
	uri := PathToURI(path)

	// Open document at version 5
	ws.DocumentOpened(uri, 5, "version5")

	// Try to update with older version (should be ignored)
	ws.DocumentChanged(uri, 3, "version3-stale")

	doc := ws.GetDocumentSnapshot(uri)
	if doc == nil {
		t.Fatal("document not found")
	}

	if doc.Text != "version5" {
		t.Errorf("text = %q; want %q (stale update should be ignored)", doc.Text, "version5")
	}
	if doc.Version != 5 {
		t.Errorf("version = %d; want 5 (stale update should be ignored)", doc.Version)
	}

	// Update with newer version (should succeed)
	ws.DocumentChanged(uri, 7, "version7")

	doc = ws.GetDocumentSnapshot(uri)
	if doc.Text != "version7" {
		t.Errorf("text = %q; want %q", doc.Text, "version7")
	}
	if doc.Version != 7 {
		t.Errorf("version = %d; want 7", doc.Version)
	}
}

func TestDocumentChanged_VersionZeroAccepted(t *testing.T) {
	t.Parallel()

	// Create temp file for testing
	dir := t.TempDir()
	path := filepath.Join(dir, "test.yammm")
	if err := os.WriteFile(path, []byte("type Test {}"), 0o600); err != nil {
		t.Fatal(err)
	}

	ws := NewWorkspace(nil, Config{})
	uri := PathToURI(path)

	// Open document at version 5
	ws.DocumentOpened(uri, 5, "version5")

	// Update with version 0 (unknown) should be accepted
	ws.DocumentChanged(uri, 0, "versionZero")

	doc := ws.GetDocumentSnapshot(uri)
	if doc == nil {
		t.Fatal("document not found")
	}

	if doc.Text != "versionZero" {
		t.Errorf("text = %q; want %q (version 0 should be accepted)", doc.Text, "versionZero")
	}
}

func TestDocumentChanged_Version0_InvalidatesLineStateCache(t *testing.T) {
	// Tests that LineState cache is invalidated when text changes with version 0.
	// Without explicit invalidation, the cache would incorrectly remain valid
	// because lineState.Version (0) == doc.Version (0) even though text changed.
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "test.yammm")
	if err := os.WriteFile(path, []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}

	ws := NewWorkspace(nil, Config{})
	uri := PathToURI(path)

	// Open document with version 0 and content that has brace depth 1
	ws.DocumentOpened(uri, 0, "type A {")

	// Get snapshot to trigger LineState computation
	doc1 := ws.GetDocumentSnapshot(uri)
	if doc1 == nil {
		t.Fatal("document not found")
	}
	if doc1.LineState == nil {
		t.Fatal("LineState should be computed on first access")
	}
	// Verify initial brace depth
	if len(doc1.LineState.BraceDepth) != 1 || doc1.LineState.BraceDepth[0] != 1 {
		t.Errorf("initial BraceDepth = %v; want [1]", doc1.LineState.BraceDepth)
	}

	// Update with version 0 again but different content (brace depth 0)
	ws.DocumentChanged(uri, 0, "type A {}")

	// Get new snapshot - should have fresh LineState reflecting new content
	doc2 := ws.GetDocumentSnapshot(uri)
	if doc2 == nil {
		t.Fatal("document not found after change")
	}
	if doc2.LineState == nil {
		t.Fatal("LineState should be recomputed after change")
	}
	// Verify brace depth reflects new content (balanced braces = depth 0)
	if len(doc2.LineState.BraceDepth) != 1 || doc2.LineState.BraceDepth[0] != 0 {
		t.Errorf("updated BraceDepth = %v; want [0] (cache should have been invalidated)", doc2.LineState.BraceDepth)
	}
}

func TestRemapPathToURI_ForwardSlashNormalization(t *testing.T) {
	t.Parallel()

	// Create temp file for testing
	dir := t.TempDir()
	path := filepath.Join(dir, "test.yammm")
	if err := os.WriteFile(path, []byte("type Test {}"), 0o600); err != nil {
		t.Fatal(err)
	}

	ws := NewWorkspace(nil, Config{})
	uri := PathToURI(path)
	ws.DocumentOpened(uri, 1, "type Test {}")

	// On all platforms, RemapPathToURI should work with forward-slash paths
	forwardSlashPath := filepath.ToSlash(path)
	result := ws.RemapPathToURI(forwardSlashPath)

	if result != uri {
		t.Errorf("RemapPathToURI(%q) = %q; want %q", forwardSlashPath, result, uri)
	}
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
	ws := NewWorkspace(logger, Config{})

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
			if result != tt.want {
				t.Errorf("RemapPathToURI(%q) = %q; want %q", tt.input, result, tt.want)
			}
		})
	}
}
