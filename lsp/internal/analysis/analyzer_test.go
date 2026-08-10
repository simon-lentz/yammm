package analysis

import (
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/simon-lentz/yammm/location"

	"github.com/simon-lentz/yammm/lsp/internal/lsputil"
	"github.com/simon-lentz/yammm/lsp/internal/symbols"
)

func TestNewAnalyzer(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	analyzer := NewAnalyzer(logger)

	require.NotNil(t, analyzer, "NewAnalyzer() returned nil")
}

func TestSnapshot_Fields(t *testing.T) {
	t.Parallel()

	// Test that Snapshot fields are properly initialized
	snapshot := &Snapshot{
		CreatedAt:       time.Now(),
		EntrySourceID:   location.MustNewSourceID("test://file.yammm"),
		EntryVersion:    5,
		Root:            "/project",
		SymbolsBySource: make(map[location.SourceID]*symbols.SymbolIndex),
	}

	assert.Equal(t, 5, snapshot.EntryVersion)
	assert.Equal(t, "/project", snapshot.Root)
	assert.NotNil(t, snapshot.SymbolsBySource, "SymbolsBySource should not be nil")
}

func TestURIDiagnostic(t *testing.T) {
	t.Parallel()

	// Test URIDiagnostic structure
	uriDiag := URIDiagnostic{
		URI: "file:///test/file.yammm",
	}

	assert.Equal(t, "file:///test/file.yammm", uriDiag.URI)
}

func TestSymbolKind_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		kind symbols.SymbolKind
		want string
	}{
		{symbols.SymbolSchema, "Schema"},
		{symbols.SymbolImport, "Import"},
		{symbols.SymbolType, "Type"},
		{symbols.SymbolDataType, "DataType"},
		{symbols.SymbolProperty, "Property"},
		{symbols.SymbolAssociation, "Association"},
		{symbols.SymbolComposition, "Composition"},
		{symbols.SymbolInvariant, "Invariant"},
		{symbols.SymbolKind(99), "Unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			t.Parallel()
			got := tt.kind.String()
			assert.Equal(t, tt.want, got, "SymbolKind(%d).String()", tt.kind)
		})
	}
}

func TestSymbol_Fields(t *testing.T) {
	t.Parallel()

	sourceID := location.MustNewSourceID("test://file.yammm")
	span := location.Point(sourceID, 10, 5)

	symbol := symbols.Symbol{
		Name:       "Person",
		Kind:       symbols.SymbolType,
		SourceID:   sourceID,
		Range:      span,
		Selection:  span,
		ParentName: "",
		Detail:     "type Person",
	}

	assert.Equal(t, "Person", symbol.Name)
	assert.Equal(t, symbols.SymbolType, symbol.Kind)
	assert.Equal(t, "type Person", symbol.Detail)
}

// Critical Test Gates: LSP Overlay and Registry Contracts
// These tests validate critical contracts for the LSP overlay and registry.
func TestOverlayPrecedenceOverDisk(t *testing.T) {
	// Validates overlay content wins over disk content.
	// Given: disk file contains schema "DiskVersion"
	// And: overlay provides different content for the same path
	// Then: analysis uses overlay content, not disk content
	t.Parallel()

	tmpDir := t.TempDir()

	// Create a file on disk with "DiskVersion"
	diskPath := filepath.Join(tmpDir, "main.yammm")
	diskContent := `schema "DiskVersion" type DiskType { id String primary }`
	err := os.WriteFile(diskPath, []byte(diskContent), 0o600)
	require.NoError(t, err, "failed to write disk file")

	// Create overlay with different content "OverlayVersion"
	overlayContent := `schema "OverlayVersion" type OverlayType { name String primary }`
	overlays := map[string][]byte{
		diskPath: []byte(overlayContent),
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	analyzer := NewAnalyzer(logger)

	ctx := t.Context()
	snapshot, err := analyzer.Analyze(ctx, diskPath, overlays, tmpDir, lsputil.PositionEncodingUTF16)
	require.NoError(t, err, "Analyze() error")
	require.NotNil(t, snapshot, "Analyze() returned nil snapshot")

	// Verify the overlay content was used, not disk content
	require.NotNil(t, snapshot.Schema, "snapshot.Schema is nil")
	assert.Equal(t, "OverlayVersion", snapshot.Schema.Name(), "overlay should win over disk")

	// Verify the type from overlay exists
	_, ok := snapshot.Schema.Type("OverlayType")
	assert.True(t, ok, "OverlayType should exist (from overlay content)")
	_, ok = snapshot.Schema.Type("DiskType")
	assert.False(t, ok, "DiskType should NOT exist (disk content should be ignored)")
}

func TestOverlayWithSymlinkPath_StillOverridesDisk(t *testing.T) {
	// Regression test: overlay provided under non-canonical path (via symlink)
	// should still take precedence over disk content.
	//
	// This validates that the loader's key canonicalization works correctly:
	// even if the overlay key uses a symlink path, the content should be found
	// and used instead of falling back to disk.
	//
	// Given: disk file at real/main.yammm with "DiskVersion"
	// And: symlink link -> real
	// And: overlay uses symlink path link/main.yammm with "OverlayVersion"
	// Then: analysis uses overlay content, not disk content
	t.Parallel()

	tmpDir := t.TempDir()

	// Create the real directory and file
	realDir := filepath.Join(tmpDir, "real")
	require.NoError(t, os.MkdirAll(realDir, 0o750), "failed to create real dir")

	realPath := filepath.Join(realDir, "main.yammm")
	diskContent := `schema "DiskVersion" type DiskType { id String primary }`
	require.NoError(t, os.WriteFile(realPath, []byte(diskContent), 0o600), "failed to write disk file")

	// Create symlink: link -> real
	linkDir := filepath.Join(tmpDir, "link")
	if err := os.Symlink(realDir, linkDir); err != nil {
		t.Skipf("symlinks not supported: %v", err)
	}

	// Use the symlink path (non-canonical) for overlay
	symlinkPath := filepath.Join(linkDir, "main.yammm")

	// Verify symlink path differs from real path
	resolvedPath, err := filepath.EvalSymlinks(symlinkPath)
	require.NoError(t, err, "failed to resolve symlink")
	if resolvedPath == symlinkPath {
		t.Skip("symlink path equals resolved path; test not meaningful on this filesystem")
	}

	// Create overlay with different content using the SYMLINK path (non-canonical)
	overlayContent := `schema "OverlayVersion" type OverlayType { name String primary }`
	overlays := map[string][]byte{
		symlinkPath: []byte(overlayContent),
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	analyzer := NewAnalyzer(logger)

	ctx := t.Context()
	// Use symlink path as entry too (matches how workspace would call this)
	snapshot, err := analyzer.Analyze(ctx, symlinkPath, overlays, linkDir, lsputil.PositionEncodingUTF16)
	require.NoError(t, err, "Analyze() error")
	require.NotNil(t, snapshot, "Analyze() returned nil snapshot")

	// Verify the overlay content was used, not disk content
	require.NotNil(t, snapshot.Schema, "snapshot.Schema is nil")
	assert.Equal(t, "OverlayVersion", snapshot.Schema.Name(), "overlay via symlink path should win")

	// Verify the type from overlay exists
	_, ok := snapshot.Schema.Type("OverlayType")
	assert.True(t, ok, "OverlayType should exist (from overlay content)")
	_, ok = snapshot.Schema.Type("DiskType")
	assert.False(t, ok, "DiskType should NOT exist (overlay should override disk even via symlink)")
}

func TestSources_PopulatesSourceRegistry(t *testing.T) {
	// Validates the loader populates the source registry for all files in
	// the import closure.
	// Given: entry file that imports another file
	// And: imported file exists on disk (not in sources map)
	// Then: registry contains both entry and imported file
	// And: LineStartByte works for imported file (enables UTF-16 conversion)
	t.Parallel()

	tmpDir := t.TempDir()

	// Create imported file on disk
	partsPath := filepath.Join(tmpDir, "parts.yammm")
	partsContent := `schema "parts" type Wheel { id String primary diameter Integer }`
	err := os.WriteFile(partsPath, []byte(partsContent), 0o600)
	require.NoError(t, err, "failed to write parts file")

	// Create main file on disk too (for reference)
	mainPath := filepath.Join(tmpDir, "main.yammm")
	mainContent := `schema "main" import "./parts" type Car { vin String primary --> WHEELS (many) parts.Wheel }`
	err = os.WriteFile(mainPath, []byte(mainContent), 0o600)
	require.NoError(t, err, "failed to write main file")

	// Provide main in overlay using absolute path (parts is on disk only)
	overlays := map[string][]byte{
		mainPath: []byte(mainContent),
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	analyzer := NewAnalyzer(logger)

	ctx := t.Context()
	snapshot, err := analyzer.Analyze(ctx, mainPath, overlays, tmpDir, lsputil.PositionEncodingUTF16)
	require.NoError(t, err, "Analyze() error")
	require.NotNil(t, snapshot, "Analyze() returned nil snapshot")
	require.NotNil(t, snapshot.Sources, "snapshot.Sources is nil")
	// The closure-population contract below holds for completed loads; a
	// fixture invalidated by a future schema-contract tightening must fail
	// here rather than weaken the registry assertions.
	require.False(t, snapshot.Result.HasErrors(),
		"fixture must load cleanly: %v", snapshot.Result.Err())

	// Canonicalize paths to match loader behavior (symlink resolution)
	mainCanonical, _ := filepath.EvalSymlinks(mainPath)
	partsCanonical, _ := filepath.EvalSymlinks(partsPath)

	// Verify registry contains the entry file
	mainSourceID, err := location.SourceIDFromAbsolutePath(mainCanonical)
	require.NoError(t, err, "failed to create main source ID")
	assert.True(t, snapshot.Sources.Has(mainSourceID), "registry should contain main file: %s", mainCanonical)

	// Verify registry contains the imported file
	partsSourceID, err := location.SourceIDFromAbsolutePath(partsCanonical)
	require.NoError(t, err, "failed to create parts source ID")
	assert.True(t, snapshot.Sources.Has(partsSourceID), "registry should contain imported file: %s", partsCanonical)

	// Verify LineStartByte works for imported file (critical for UTF-16 conversion)
	offset, ok := snapshot.Sources.LineStartByte(partsSourceID, 1)
	assert.True(t, ok, "LineStartByte() should return true for imported file")
	assert.Equal(t, 0, offset, "LineStartByte(1) should be 0 (first line starts at byte 0)")
}

// TestSources_FailedLoadKeepsOverlays pins the failure-path registry
// contract: when the load produces no schema, the registry holds exactly
// the pre-registered overlays — disk-read sources from the aborted load do
// not appear. Diagnostic spans carry line/column positions natively, so
// rendering still works; only sub-column UTF-16 precision for sources
// outside the overlay set is unavailable on this path.
func TestSources_FailedLoadKeepsOverlays(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// The imported file is INVALID (no primary key), so the load fails
	// after reading it from disk.
	partsPath := filepath.Join(tmpDir, "parts.yammm")
	partsContent := `schema "parts" type Wheel { diameter Integer }`
	require.NoError(t, os.WriteFile(partsPath, []byte(partsContent), 0o600))

	mainPath := filepath.Join(tmpDir, "main.yammm")
	mainContent := `schema "main" import "./parts" type Car { vin String primary --> WHEELS (many) parts.Wheel }`
	require.NoError(t, os.WriteFile(mainPath, []byte(mainContent), 0o600))

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	analyzer := NewAnalyzer(logger)

	snapshot, err := analyzer.Analyze(t.Context(), mainPath, map[string][]byte{
		mainPath: []byte(mainContent),
	}, tmpDir, lsputil.PositionEncodingUTF16)
	require.NoError(t, err, "a semantic load failure is not an Analyze error")
	require.NotNil(t, snapshot)
	require.Nil(t, snapshot.Schema, "the load must fail for this contract to apply")
	require.True(t, snapshot.Result.HasErrors())

	// The registry keys overlays canonically (symlinks resolved), matching
	// the loader's identity, so the expected ID is minted the same way.
	mainCanonical, err := filepath.EvalSymlinks(mainPath)
	require.NoError(t, err)
	mainSourceID, err := location.SourceIDFromAbsolutePath(mainCanonical)
	require.NoError(t, err)
	assert.True(t, snapshot.Sources.Has(mainSourceID), "overlay content stays available")
	assert.Equal(t, 1, snapshot.Sources.Len(), "no partially-loaded sources beyond the overlays")
}

func TestSources_DiskFallback(t *testing.T) {
	// Validates that imports not in the overlay are resolved from disk
	// and participate in diagnostics.
	// Given: sources map with only the entry file
	// Then: imports not in the map are resolved from disk
	// And: types from disk imports are accessible
	t.Parallel()

	tmpDir := t.TempDir()

	// Create imported file on disk with a type
	utilsPath := filepath.Join(tmpDir, "utils.yammm")
	utilsContent := `schema "utils" type Helper { value String primary }`
	err := os.WriteFile(utilsPath, []byte(utilsContent), 0o600)
	require.NoError(t, err, "failed to write utils file")

	// Create main file on disk too (for reference)
	mainPath := filepath.Join(tmpDir, "main.yammm")
	mainContent := `schema "main" import "./utils" type Service extends utils.Helper { name String }`
	err = os.WriteFile(mainPath, []byte(mainContent), 0o600)
	require.NoError(t, err, "failed to write main file")

	// Provide only main in overlay - utils should be resolved from disk
	overlays := map[string][]byte{
		mainPath: []byte(mainContent),
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	analyzer := NewAnalyzer(logger)

	ctx := t.Context()
	snapshot, err := analyzer.Analyze(ctx, mainPath, overlays, tmpDir, lsputil.PositionEncodingUTF16)
	require.NoError(t, err, "Analyze() error")
	require.NotNil(t, snapshot, "Analyze() returned nil snapshot")

	// Verify schema loaded successfully (no errors means disk fallback worked)
	if snapshot.Schema == nil {
		// Print diagnostics for debugging
		for issue := range snapshot.Result.Issues() {
			t.Logf("Diagnostic: %v", issue)
		}
		require.NotNil(t, snapshot.Schema, "snapshot.Schema is nil - disk fallback may have failed")
	}
	if !snapshot.Result.OK() {
		for issue := range snapshot.Result.Issues() {
			t.Logf("Issue: %v", issue)
		}
		assert.True(t, snapshot.Result.OK(), "Result should be OK - import resolution from disk should succeed")
	}

	// Verify the Service type exists and extends Helper (from disk)
	serviceType, ok := snapshot.Schema.Type("Service")
	require.True(t, ok, "Service type should exist")

	// Verify inheritance resolved correctly (confirms disk file was loaded)
	// Use SuperTypesSlice() which returns resolved parent types
	supers := serviceType.SuperTypesSlice()
	require.NotEmpty(t, supers, "Service should inherit from utils.Helper")
	// The resolved parent should be "Helper" from the utils schema
	assert.Equal(t, "Helper", supers[0].Name(), "Parent type name")

	// Verify ImportedPaths includes the disk-based import
	// Note: paths are canonicalized (symlinks resolved), so we need to compare canonical paths
	utilsCanonical, _ := filepath.EvalSymlinks(utilsPath)
	assert.True(t, slices.Contains(snapshot.ImportedPaths, utilsCanonical),
		"ImportedPaths should include %s; got %v", utilsCanonical, snapshot.ImportedPaths)
}

func TestHasURIScheme_FileURI(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  bool
	}{
		// URIs with schemes - should return true
		{"file:///path/to/file.yammm", true},
		{"file:///C:/path/file.yammm", true},
		{"http://example.com/path", true},
		{"https://example.com/path", true},

		// Long schemes - RFC3986 compliant, should return true
		{"custom-scheme://host/path", true},
		{"verylongscheme://host", true},

		// Filesystem paths - should return false
		{"/path/to/file.yammm", false},
		{"/Users/test/project/schema.yammm", false},
		{"C:\\path\\to\\file.yammm", false},
		{"./relative/path.yammm", false},
		{"../parent/path.yammm", false},
		{"file.yammm", false},

		// Edge cases
		{"", false},
		{"://noscheme", false},               // No scheme before ://
		{"/path/with/colon://inside", false}, // Contains :// but scheme has invalid chars
		{"a]://short", false},                // Invalid char ']' in scheme per RFC3986
		{"x://h", true},                      // Minimal valid URI
		{"ab://host", true},                  // Short scheme
		{"a+b://host", true},                 // RFC3986 allows + in scheme
		{"a-b://host", true},                 // RFC3986 allows - in scheme
		{"a.b://host", true},                 // RFC3986 allows . in scheme
		{"1st://host", false},                // RFC3986 requires scheme to start with ALPHA
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			got := lsputil.HasURIScheme(tt.input)
			assert.Equal(t, tt.want, got, "lsputil.HasURIScheme(%q)", tt.input)
		})
	}
}

func TestAnalyzer_MultiOpenDocs_CorrectEntrySelection(t *testing.T) {
	// Tests that when multiple documents are open (as overlays), the analyzer
	// uses the explicitly requested entry path rather than selecting by
	// lexicographic order.
	//
	// This validates the fix for the analyzer entry selection issue where
	// SourcesWithEntry is used to specify the exact entry file.
	t.Parallel()

	tmpDir := t.TempDir()

	// Create two schema files on disk
	// File A (lexicographically first): a_types.yammm
	aPath := filepath.Join(tmpDir, "a_types.yammm")
	aContent := `schema "ATypes"
type TypeA {
    id String primary
}`
	err := os.WriteFile(aPath, []byte(aContent), 0o600)
	require.NoError(t, err, "failed to write a_types.yammm")

	// File B (lexicographically second): b_main.yammm
	bPath := filepath.Join(tmpDir, "b_main.yammm")
	bContent := `schema "BMain"
import "./a_types" as types
type TypeB {
    name String primary
}`
	err = os.WriteFile(bPath, []byte(bContent), 0o600)
	require.NoError(t, err, "failed to write b_main.yammm")

	// Simulate both files being open with overlays (same content as disk)
	overlays := map[string][]byte{
		aPath: []byte(aContent),
		bPath: []byte(bContent),
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	analyzer := NewAnalyzer(logger)
	ctx := t.Context()

	// Analyze requesting b_main.yammm as entry (even though a_types is lexicographically first)
	snapshot, err := analyzer.Analyze(ctx, bPath, overlays, tmpDir, lsputil.PositionEncodingUTF16)
	require.NoError(t, err, "Analyze() error")
	require.NotNil(t, snapshot, "Analyze() returned nil snapshot")

	// Verify the correct entry was used
	require.NotNil(t, snapshot.Schema, "snapshot.Schema is nil")

	// The schema name should be "BMain", not "ATypes"
	// If lexicographic selection was used, it would incorrectly be "ATypes"
	assert.Equal(t, "BMain", snapshot.Schema.Name(), "explicit entry should be used, not lexicographic")

	// TypeB should exist in the schema
	_, ok := snapshot.Schema.Type("TypeB")
	assert.True(t, ok, "TypeB should exist (from entry file)")
}

func TestAnalyze_PartialSnapshotOnLoadError(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	analyzer := NewAnalyzer(logger)

	// Schema that imports a non-existent file — triggers load error but partial result
	overlays := map[string][]byte{
		"/test/entry.yammm": []byte("schema \"test\"\nimport \"missing.yammm\"\ntype Foo {\n}\n"),
	}

	snapshot, err := analyzer.Analyze(t.Context(), "/test/entry.yammm", overlays, "", lsputil.PositionEncodingUTF16)
	// Should get both a snapshot (with diagnostics) and an error
	assert.NotNil(t, snapshot, "partial snapshot should be returned on load error")
	if snapshot != nil {
		assert.NotEmpty(t, snapshot.LSPDiagnostics, "partial snapshot should contain diagnostics")
	}
	// Note: err may or may not be non-nil depending on yammm's load behavior.
	// The important thing is that we get a usable snapshot either way.
	_ = err
}

func TestConvertRelatedInfo_URIEncodingWithSpaces(t *testing.T) {
	// Tests that paths with spaces in RelatedInformation are properly
	// percent-encoded when converted to file:// URIs.
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	analyzer := NewAnalyzer(logger)

	tests := []struct {
		name    string
		path    string
		wantURI string
	}{
		{
			name:    "path with spaces",
			path:    "/path/with spaces/file.yammm",
			wantURI: "file:///path/with%20spaces/file.yammm",
		},
		{
			name:    "path with multiple spaces",
			path:    "/my projects/my file.yammm",
			wantURI: "file:///my%20projects/my%20file.yammm",
		},
		{
			name:    "path without spaces",
			path:    "/normal/path/file.yammm",
			wantURI: "file:///normal/path/file.yammm",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sourceID, err := location.SourceIDFromAbsolutePath(tt.path)
			if err != nil {
				t.Skipf("skipping: %v", err)
			}

			related := []location.RelatedInfo{
				{
					Span:    location.Point(sourceID, 6, 1),
					Message: "related info message",
				},
			}

			result := analyzer.ConvertRelatedInfo(related, nil, lsputil.PositionEncodingUTF16)

			require.Len(t, result, 1, "expected 1 related info")

			gotURI := result[0].Location.URI
			assert.Equal(t, tt.wantURI, gotURI)
		})
	}
}
