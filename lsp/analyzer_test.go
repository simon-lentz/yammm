package lsp

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/location"
)

func TestNewAnalyzer(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	analyzer := NewAnalyzer(logger)

	if analyzer == nil {
		t.Fatal("NewAnalyzer() returned nil")
	}
	if analyzer.logger == nil {
		t.Error("analyzer.logger is nil")
	}
}

func TestSnapshot_Fields(t *testing.T) {
	t.Parallel()

	// Test that Snapshot fields are properly initialized
	snapshot := &Snapshot{
		CreatedAt:       time.Now(),
		EntrySourceID:   location.MustNewSourceID("test://file.yammm"),
		EntryVersion:    5,
		Root:            "/project",
		SymbolsBySource: make(map[location.SourceID]*SymbolIndex),
	}

	if snapshot.EntryVersion != 5 {
		t.Errorf("EntryVersion = %d; want 5", snapshot.EntryVersion)
	}
	if snapshot.Root != "/project" {
		t.Errorf("Root = %q; want /project", snapshot.Root)
	}
	if snapshot.SymbolsBySource == nil {
		t.Error("SymbolsBySource should not be nil")
	}
}

func TestURIDiagnostic(t *testing.T) {
	t.Parallel()

	// Test URIDiagnostic structure
	uriDiag := URIDiagnostic{
		URI: "file:///test/file.yammm",
	}

	if uriDiag.URI != "file:///test/file.yammm" {
		t.Errorf("URI = %q; want file:///test/file.yammm", uriDiag.URI)
	}
}

func TestToUInteger_Positive(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input int
		want  uint32
	}{
		{0, 0},
		{1, 1},
		{100, 100},
		{65535, 65535},
	}

	for _, tt := range tests {
		got := toUInteger(tt.input)
		if got != tt.want {
			t.Errorf("toUInteger(%d) = %d; want %d", tt.input, got, tt.want)
		}
	}
}

func TestToUInteger_Negative(t *testing.T) {
	t.Parallel()

	tests := []int{-1, -10, -1000}

	for _, input := range tests {
		got := toUInteger(input)
		if got != 0 {
			t.Errorf("toUInteger(%d) = %d; want 0 for negative", input, got)
		}
	}
}

func TestSymbolKind_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		kind SymbolKind
		want string
	}{
		{SymbolSchema, "Schema"},
		{SymbolImport, "Import"},
		{SymbolType, "Type"},
		{SymbolDataType, "DataType"},
		{SymbolProperty, "Property"},
		{SymbolAssociation, "Association"},
		{SymbolComposition, "Composition"},
		{SymbolInvariant, "Invariant"},
		{SymbolKind(99), "Unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			t.Parallel()
			got := tt.kind.String()
			if got != tt.want {
				t.Errorf("SymbolKind(%d).String() = %q; want %q", tt.kind, got, tt.want)
			}
		})
	}
}

func TestSymbol_Fields(t *testing.T) {
	t.Parallel()

	sourceID := location.MustNewSourceID("test://file.yammm")
	span := location.Point(sourceID, 10, 5)

	symbol := Symbol{
		Name:       "Person",
		Kind:       SymbolType,
		SourceID:   sourceID,
		Range:      span,
		Selection:  span,
		ParentName: "",
		Detail:     "type Person",
	}

	if symbol.Name != "Person" {
		t.Errorf("Name = %q; want Person", symbol.Name)
	}
	if symbol.Kind != SymbolType {
		t.Errorf("Kind = %d; want SymbolType", symbol.Kind)
	}
	if symbol.Detail != "type Person" {
		t.Errorf("Detail = %q; want 'type Person'", symbol.Detail)
	}
}

// =============================================================================
// Critical Test Gates: LSP Overlay and Registry Contracts
// These tests validate critical contracts for the LSP overlay and registry.
// =============================================================================

func TestOverlayPrecedenceOverDisk(t *testing.T) {
	// Validates overlay content wins over disk content.
	// Given: disk file contains schema "DiskVersion"
	// And: overlay provides different content for the same path
	// Then: analysis uses overlay content, not disk content
	t.Parallel()

	tmpDir := t.TempDir()

	// Create a file on disk with "DiskVersion"
	diskPath := filepath.Join(tmpDir, "main.yammm")
	diskContent := `schema "DiskVersion" type DiskType { id String }`
	err := os.WriteFile(diskPath, []byte(diskContent), 0o600)
	if err != nil {
		t.Fatalf("failed to write disk file: %v", err)
	}

	// Create overlay with different content "OverlayVersion"
	overlayContent := `schema "OverlayVersion" type OverlayType { name String }`
	overlays := map[string][]byte{
		diskPath: []byte(overlayContent),
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	analyzer := NewAnalyzer(logger)

	ctx := context.Background()
	snapshot, err := analyzer.Analyze(ctx, diskPath, overlays, tmpDir)
	if err != nil {
		t.Fatalf("Analyze() error: %v", err)
	}
	if snapshot == nil {
		t.Fatal("Analyze() returned nil snapshot")
	}

	// Verify the overlay content was used, not disk content
	if snapshot.Schema == nil {
		t.Fatal("snapshot.Schema is nil")
	}
	if snapshot.Schema.Name() != "OverlayVersion" {
		t.Errorf("Schema.Name() = %q; want OverlayVersion (overlay should win over disk)", snapshot.Schema.Name())
	}

	// Verify the type from overlay exists
	if _, ok := snapshot.Schema.Type("OverlayType"); !ok {
		t.Error("OverlayType should exist (from overlay content)")
	}
	if _, ok := snapshot.Schema.Type("DiskType"); ok {
		t.Error("DiskType should NOT exist (disk content should be ignored)")
	}
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
	if err := os.MkdirAll(realDir, 0o750); err != nil {
		t.Fatalf("failed to create real dir: %v", err)
	}

	realPath := filepath.Join(realDir, "main.yammm")
	diskContent := `schema "DiskVersion" type DiskType { id String }`
	if err := os.WriteFile(realPath, []byte(diskContent), 0o600); err != nil {
		t.Fatalf("failed to write disk file: %v", err)
	}

	// Create symlink: link -> real
	linkDir := filepath.Join(tmpDir, "link")
	if err := os.Symlink(realDir, linkDir); err != nil {
		t.Skipf("symlinks not supported: %v", err)
	}

	// Use the symlink path (non-canonical) for overlay
	symlinkPath := filepath.Join(linkDir, "main.yammm")

	// Verify symlink path differs from real path
	resolvedPath, err := filepath.EvalSymlinks(symlinkPath)
	if err != nil {
		t.Fatalf("failed to resolve symlink: %v", err)
	}
	if resolvedPath == symlinkPath {
		t.Skip("symlink path equals resolved path; test not meaningful on this filesystem")
	}

	// Create overlay with different content using the SYMLINK path (non-canonical)
	overlayContent := `schema "OverlayVersion" type OverlayType { name String }`
	overlays := map[string][]byte{
		symlinkPath: []byte(overlayContent),
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	analyzer := NewAnalyzer(logger)

	ctx := context.Background()
	// Use symlink path as entry too (matches how workspace would call this)
	snapshot, err := analyzer.Analyze(ctx, symlinkPath, overlays, linkDir)
	if err != nil {
		t.Fatalf("Analyze() error: %v", err)
	}
	if snapshot == nil {
		t.Fatal("Analyze() returned nil snapshot")
	}

	// Verify the overlay content was used, not disk content
	if snapshot.Schema == nil {
		t.Fatal("snapshot.Schema is nil")
	}
	if snapshot.Schema.Name() != "OverlayVersion" {
		t.Errorf("Schema.Name() = %q; want OverlayVersion (overlay via symlink path should win)", snapshot.Schema.Name())
	}

	// Verify the type from overlay exists
	if _, ok := snapshot.Schema.Type("OverlayType"); !ok {
		t.Error("OverlayType should exist (from overlay content)")
	}
	if _, ok := snapshot.Schema.Type("DiskType"); ok {
		t.Error("DiskType should NOT exist (overlay should override disk even via symlink)")
	}
}

func TestLoadSources_PopulatesSourceRegistry(t *testing.T) {
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
	partsContent := `schema "parts" type Wheel { diameter Integer }`
	err := os.WriteFile(partsPath, []byte(partsContent), 0o600)
	if err != nil {
		t.Fatalf("failed to write parts file: %v", err)
	}

	// Create main file on disk too (for reference)
	mainPath := filepath.Join(tmpDir, "main.yammm")
	mainContent := `schema "main" import "./parts" type Car { *-> WHEELS (many) parts.Wheel }`
	err = os.WriteFile(mainPath, []byte(mainContent), 0o600)
	if err != nil {
		t.Fatalf("failed to write main file: %v", err)
	}

	// Provide main in overlay using absolute path (parts is on disk only)
	overlays := map[string][]byte{
		mainPath: []byte(mainContent),
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	analyzer := NewAnalyzer(logger)

	ctx := context.Background()
	snapshot, err := analyzer.Analyze(ctx, mainPath, overlays, tmpDir)
	if err != nil {
		t.Fatalf("Analyze() error: %v", err)
	}
	if snapshot == nil {
		t.Fatal("Analyze() returned nil snapshot")
	}
	if snapshot.Sources == nil {
		t.Fatal("snapshot.Sources is nil")
	}

	// Canonicalize paths to match loader behavior (symlink resolution)
	mainCanonical, _ := filepath.EvalSymlinks(mainPath)
	partsCanonical, _ := filepath.EvalSymlinks(partsPath)

	// Verify registry contains the entry file
	mainSourceID, err := location.SourceIDFromAbsolutePath(mainCanonical)
	if err != nil {
		t.Fatalf("failed to create main source ID: %v", err)
	}
	if !snapshot.Sources.Has(mainSourceID) {
		t.Errorf("registry should contain main file: %s", mainCanonical)
	}

	// Verify registry contains the imported file
	partsSourceID, err := location.SourceIDFromAbsolutePath(partsCanonical)
	if err != nil {
		t.Fatalf("failed to create parts source ID: %v", err)
	}
	if !snapshot.Sources.Has(partsSourceID) {
		t.Errorf("registry should contain imported file: %s", partsCanonical)
	}

	// Verify LineStartByte works for imported file (critical for UTF-16 conversion)
	offset, ok := snapshot.Sources.LineStartByte(partsSourceID, 1)
	if !ok {
		t.Error("LineStartByte() should return true for imported file")
	}
	if offset != 0 {
		t.Errorf("LineStartByte(1) = %d; want 0 (first line starts at byte 0)", offset)
	}
}

func TestLoadSources_DiskFallback(t *testing.T) {
	// Validates that imports not in the overlay are resolved from disk
	// and participate in diagnostics.
	// Given: sources map with only the entry file
	// Then: imports not in the map are resolved from disk
	// And: types from disk imports are accessible
	t.Parallel()

	tmpDir := t.TempDir()

	// Create imported file on disk with a type
	utilsPath := filepath.Join(tmpDir, "utils.yammm")
	utilsContent := `schema "utils" type Helper { value String }`
	err := os.WriteFile(utilsPath, []byte(utilsContent), 0o600)
	if err != nil {
		t.Fatalf("failed to write utils file: %v", err)
	}

	// Create main file on disk too (for reference)
	mainPath := filepath.Join(tmpDir, "main.yammm")
	mainContent := `schema "main" import "./utils" type Service extends utils.Helper { name String }`
	err = os.WriteFile(mainPath, []byte(mainContent), 0o600)
	if err != nil {
		t.Fatalf("failed to write main file: %v", err)
	}

	// Provide only main in overlay - utils should be resolved from disk
	overlays := map[string][]byte{
		mainPath: []byte(mainContent),
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	analyzer := NewAnalyzer(logger)

	ctx := context.Background()
	snapshot, err := analyzer.Analyze(ctx, mainPath, overlays, tmpDir)
	if err != nil {
		t.Fatalf("Analyze() error: %v", err)
	}
	if snapshot == nil {
		t.Fatal("Analyze() returned nil snapshot")
	}

	// Verify schema loaded successfully (no errors means disk fallback worked)
	if snapshot.Schema == nil {
		// Print diagnostics for debugging
		for issue := range snapshot.Result.Issues() {
			t.Logf("Diagnostic: %v", issue)
		}
		t.Fatal("snapshot.Schema is nil - disk fallback may have failed")
	}
	if !snapshot.Result.OK() {
		for issue := range snapshot.Result.Issues() {
			t.Logf("Issue: %v", issue)
		}
		t.Error("Result should be OK - import resolution from disk should succeed")
	}

	// Verify the Service type exists and extends Helper (from disk)
	serviceType, ok := snapshot.Schema.Type("Service")
	if !ok {
		t.Fatal("Service type should exist")
	}

	// Verify inheritance resolved correctly (confirms disk file was loaded)
	// Use SuperTypesSlice() which returns resolved parent types
	supers := serviceType.SuperTypesSlice()
	if len(supers) == 0 {
		t.Error("Service should inherit from utils.Helper")
	} else {
		// The resolved parent should be "Helper" from the utils schema
		parentName := supers[0].Name()
		if parentName != "Helper" {
			t.Errorf("Parent type name = %q; want Helper", parentName)
		}
	}

	// Verify ImportedPaths includes the disk-based import
	// Note: paths are canonicalized (symlinks resolved), so we need to compare canonical paths
	utilsCanonical, _ := filepath.EvalSymlinks(utilsPath)
	if !slices.Contains(snapshot.ImportedPaths, utilsCanonical) {
		t.Errorf("ImportedPaths should include %s; got %v", utilsCanonical, snapshot.ImportedPaths)
	}
}

// =============================================================================
// hasURIScheme tests
// =============================================================================

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
			got := hasURIScheme(tt.input)
			if got != tt.want {
				t.Errorf("hasURIScheme(%q) = %v; want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestAnalyzer_MultiOpenDocs_CorrectEntrySelection(t *testing.T) {
	// Tests that when multiple documents are open (as overlays), the analyzer
	// uses the explicitly requested entry path rather than selecting by
	// lexicographic order.
	//
	// This validates the fix for the analyzer entry selection issue where
	// LoadSourcesWithEntry is used to specify the exact entry file.
	t.Parallel()

	tmpDir := t.TempDir()

	// Create two schema files on disk
	// File A (lexicographically first): a_types.yammm
	aPath := filepath.Join(tmpDir, "a_types.yammm")
	aContent := `schema "ATypes"
type TypeA {
    id String
}`
	err := os.WriteFile(aPath, []byte(aContent), 0o600)
	if err != nil {
		t.Fatalf("failed to write a_types.yammm: %v", err)
	}

	// File B (lexicographically second): b_main.yammm
	bPath := filepath.Join(tmpDir, "b_main.yammm")
	bContent := `schema "BMain"
import "./a_types" as types
type TypeB {
    name String
}`
	err = os.WriteFile(bPath, []byte(bContent), 0o600)
	if err != nil {
		t.Fatalf("failed to write b_main.yammm: %v", err)
	}

	// Simulate both files being open with overlays (same content as disk)
	overlays := map[string][]byte{
		aPath: []byte(aContent),
		bPath: []byte(bContent),
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	analyzer := NewAnalyzer(logger)
	ctx := context.Background()

	// Analyze requesting b_main.yammm as entry (even though a_types is lexicographically first)
	snapshot, err := analyzer.Analyze(ctx, bPath, overlays, tmpDir)
	if err != nil {
		t.Fatalf("Analyze() error: %v", err)
	}
	if snapshot == nil {
		t.Fatal("Analyze() returned nil snapshot")
	}

	// Verify the correct entry was used
	if snapshot.Schema == nil {
		t.Fatal("snapshot.Schema is nil")
	}

	// The schema name should be "BMain", not "ATypes"
	// If lexicographic selection was used, it would incorrectly be "ATypes"
	if snapshot.Schema.Name() != "BMain" {
		t.Errorf("Schema.Name() = %q; want BMain (explicit entry should be used, not lexicographic)", snapshot.Schema.Name())
	}

	// TypeB should exist in the schema
	if _, ok := snapshot.Schema.Type("TypeB"); !ok {
		t.Error("TypeB should exist (from entry file)")
	}
}

func TestConvertRelatedInfo_URIEncodingWithSpaces(t *testing.T) {
	// Tests that paths with spaces in RelatedInformation are properly
	// percent-encoded when converted to file:// URIs.
	//
	// This is a regression test for the issue where diag.Renderer
	// produced URIs with unencoded spaces (invalid per RFC 3986).
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	analyzer := NewAnalyzer(logger)

	tests := []struct {
		name     string
		inputURI string
		wantURI  string
	}{
		{
			name:     "path with spaces",
			inputURI: "/path/with spaces/file.yammm",
			wantURI:  "file:///path/with%20spaces/file.yammm",
		},
		{
			name:     "path with multiple spaces",
			inputURI: "/my projects/my file.yammm",
			wantURI:  "file:///my%20projects/my%20file.yammm",
		},
		{
			name:     "path without spaces",
			inputURI: "/normal/path/file.yammm",
			wantURI:  "file:///normal/path/file.yammm",
		},
		{
			name:     "already encoded URI",
			inputURI: "file:///already%20encoded/file.yammm",
			wantURI:  "file:///already%20encoded/file.yammm",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Create related info with the test URI
			related := []diag.LSPRelatedInfo{
				{
					Location: diag.LSPLocation{
						URI: tt.inputURI,
						Range: diag.LSPRange{
							Start: diag.LSPPosition{Line: 5, Character: 0},
							End:   diag.LSPPosition{Line: 5, Character: 10},
						},
					},
					Message: "related info message",
				},
			}

			result := analyzer.convertRelatedInfo(related)

			if len(result) != 1 {
				t.Fatalf("expected 1 related info, got %d", len(result))
			}

			gotURI := result[0].Location.URI
			if gotURI != tt.wantURI {
				t.Errorf("URI = %q; want %q", gotURI, tt.wantURI)
			}
		})
	}
}
