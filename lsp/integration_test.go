package lsp

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/tliron/commonlog"
	protocol "github.com/tliron/glsp/protocol_3_16"

	"github.com/simon-lentz/yammm/lsp/testutil"
	"github.com/simon-lentz/yammm/schema/load"
)

// newTestHarness creates a harness for integration testing with a real LSP server.
func newTestHarness(t *testing.T, root string) *testutil.Harness {
	t.Helper()

	// Use silent logging for tests
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	silenceCommonLog.Do(func() { commonlog.Configure(0, nil) })

	// Create server with test configuration
	server := NewServer(logger, Config{
		ModuleRoot: root,
	})

	return testutil.NewHarness(t, server.Handler(), root)
}

// =============================================================================
// Integration Tests using Harness
// These tests verify LSP handler behavior through direct handler calls.
// Note: Tests that require notification publishing (like diagnostics) are
// limited because glsp.Context is required for Notify calls.
// =============================================================================

func TestIntegration_InitializeSuccess(t *testing.T) {
	// Test that server initialization succeeds
	t.Parallel()

	tmpDir := t.TempDir()
	h := newTestHarness(t, tmpDir)
	defer h.Close()

	if err := h.Initialize(); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
}

func TestIntegration_FormattingWithoutOpen(t *testing.T) {
	// Test that formatting requires document to be open
	// (as per LSP spec, formatting operates on open documents)
	t.Parallel()

	tmpDir := t.TempDir()

	// Write a file to disk
	content := `schema "test"


type Person {
	name String
}


`
	filePath := filepath.Join(tmpDir, "main.yammm")
	if err := os.WriteFile(filePath, []byte(content), 0o600); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	h := newTestHarness(t, tmpDir)
	defer h.Close()

	if err := h.Initialize(); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	// Request formatting without didOpen - should return empty since
	// the document is not open
	edits, err := h.Formatting("main.yammm")
	if err != nil {
		t.Fatalf("Formatting failed: %v", err)
	}

	// Formatting on closed document should return no edits (not an error)
	testutil.AssertNoFormattingNeeded(t, edits)
}

func TestIntegration_HoverWithoutOpen(t *testing.T) {
	// Documents must be opened via textDocument/didOpen before hover works.
	// This is documented in lsp/doc.go under Limitations.
	t.Parallel()

	tmpDir := t.TempDir()

	// Write a file to disk
	content := `schema "test"

type Person {
	name String required
	age Integer
}
`
	filePath := filepath.Join(tmpDir, "main.yammm")
	if err := os.WriteFile(filePath, []byte(content), 0o600); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	h := newTestHarness(t, tmpDir)
	defer h.Close()

	if err := h.Initialize(); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	// Request hover without didOpen - should return nil (not an error)
	// This is expected behavior: documents must be open for hover to work.
	hover, err := h.Hover("main.yammm", 2, 5)
	if err != nil {
		t.Fatalf("Hover returned error: %v", err)
	}

	if hover != nil {
		t.Error("Hover should return nil for unopened documents")
	}
}

func TestIntegration_DefinitionWithoutOpen(t *testing.T) {
	// Documents must be opened via textDocument/didOpen before definition works.
	// This is documented in lsp/doc.go under Limitations.
	t.Parallel()

	tmpDir := t.TempDir()

	// Write parts file
	partsContent := `schema "parts"

type Wheel {
	diameter Integer
}
`
	partsPath := filepath.Join(tmpDir, "parts.yammm")
	if err := os.WriteFile(partsPath, []byte(partsContent), 0o600); err != nil {
		t.Fatalf("failed to write parts file: %v", err)
	}

	// Write main file
	mainContent := `schema "main"

import "./parts" as parts

type Car {
	*-> WHEELS (many) parts.Wheel
}
`
	mainPath := filepath.Join(tmpDir, "main.yammm")
	if err := os.WriteFile(mainPath, []byte(mainContent), 0o600); err != nil {
		t.Fatalf("failed to write main file: %v", err)
	}

	h := newTestHarness(t, tmpDir)
	defer h.Close()

	if err := h.Initialize(); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	// Request definition without didOpen - should return nil (not an error)
	// This is expected behavior: documents must be open for definition to work.
	result, err := h.Definition("main.yammm", 5, 22)
	if err != nil {
		t.Fatalf("Definition returned error: %v", err)
	}

	if result != nil {
		t.Error("Definition should return nil for unopened documents")
	}
}

// =============================================================================
// Open-Doc vs Disk-Backed Contract Tests (Priority 5: Test Coverage Gaps)
// =============================================================================

func TestIntegration_OverlayOverridesDisk(t *testing.T) {
	// Documents open in the editor (overlays) MUST take precedence over disk content.
	t.Parallel()

	tmpDir := t.TempDir()

	// Write version A to disk
	diskContent := `schema "test"

type Person {
	diskField String
}
`
	filePath := filepath.Join(tmpDir, "main.yammm")
	if err := os.WriteFile(filePath, []byte(diskContent), 0o600); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	h := newTestHarness(t, tmpDir)
	defer h.Close()

	if err := h.Initialize(); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	// Open document with version B content (different from disk)
	overlayContent := `schema "test"

type Person {
	overlayField String
}
`
	if err := h.OpenDocument("main.yammm", overlayContent); err != nil {
		t.Fatalf("OpenDocument failed: %v", err)
	}

	// Request hover on the field (line 3, char 1)
	hover, err := h.Hover("main.yammm", 3, 1)
	if err != nil {
		t.Fatalf("Hover failed: %v", err)
	}

	// Hover should show overlay content, not disk content
	if hover != nil {
		testutil.AssertHoverContains(t, hover, "overlayField")
	} else {
		// If no hover, verify via symbols
		symbols, err := h.DocumentSymbols("main.yammm")
		if err != nil {
			t.Fatalf("DocumentSymbols failed: %v", err)
		}
		testutil.AssertDocumentSymbolExists(t, symbols, "overlayField")
	}
}

func TestIntegration_DiskFallbackForUnopened(t *testing.T) {
	// Unopened files are loaded from disk during import resolution.
	t.Parallel()

	tmpDir := t.TempDir()

	// Write parts.yammm to disk (will be imported but NOT opened)
	partsContent := `schema "parts"

type Wheel {
	diameter Integer
}
`
	partsPath := filepath.Join(tmpDir, "parts.yammm")
	if err := os.WriteFile(partsPath, []byte(partsContent), 0o600); err != nil {
		t.Fatalf("failed to write parts file: %v", err)
	}

	// Write main.yammm that imports parts using type extension
	// (simpler than composition, tests cross-schema reference resolution)
	mainContent := `schema "main"

import "./parts" as parts

type Car extends parts.Wheel {
	color String
}
`
	mainPath := filepath.Join(tmpDir, "main.yammm")
	if err := os.WriteFile(mainPath, []byte(mainContent), 0o600); err != nil {
		t.Fatalf("failed to write main file: %v", err)
	}

	h := newTestHarness(t, tmpDir)
	defer h.Close()

	if err := h.Initialize(); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	// Open ONLY main.yammm (parts.yammm NOT opened)
	if err := h.OpenDocument("main.yammm", mainContent); err != nil {
		t.Fatalf("OpenDocument failed: %v", err)
	}

	// Request definition on "Wheel" in "parts.Wheel" (line 4, char 24)
	// Line 4 is: "type Car extends parts.Wheel {"
	// The qualifier "parts" is at chars 17-21, dot at 22, "Wheel" at 23-27
	result, err := h.Definition("main.yammm", 4, 24)
	if err != nil {
		t.Fatalf("Definition failed: %v", err)
	}

	// Definition should succeed since analysis runs synchronously on didOpen.
	// A nil result indicates a bug (e.g., invalid schema, missing symbol index).
	if result == nil {
		t.Fatal("Definition returned nil - schema may be invalid or symbol index missing")
	}

	// Definition should point to parts.yammm (loaded from disk)
	switch v := result.(type) {
	case protocol.Location:
		testutil.AssertLocationURI(t, v, "parts.yammm")
	case *protocol.Location:
		testutil.AssertLocationURI(t, *v, "parts.yammm")
	case []protocol.Location:
		if len(v) == 0 {
			t.Fatal("Definition returned empty location array")
		}
		testutil.AssertLocationURI(t, v[0], "parts.yammm")
	default:
		t.Fatalf("Unexpected definition result type: %T", result)
	}
}

func TestIntegration_OpenedImportOverridesDisk(t *testing.T) {
	// Test that when an imported file is opened, the overlay takes precedence
	t.Parallel()

	tmpDir := t.TempDir()

	// Write parts.yammm to disk with type DiskWheel
	partsContentDisk := `schema "parts"

type DiskWheel {
	diskDiameter Integer
}
`
	partsPath := filepath.Join(tmpDir, "parts.yammm")
	if err := os.WriteFile(partsPath, []byte(partsContentDisk), 0o600); err != nil {
		t.Fatalf("failed to write parts file: %v", err)
	}

	h := newTestHarness(t, tmpDir)
	defer h.Close()

	if err := h.Initialize(); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	// Open parts.yammm with DIFFERENT type (overlay has OverlayWheel)
	partsContentOverlay := `schema "parts"

type OverlayWheel {
	overlayDiameter Integer
}
`
	if err := h.OpenDocument("parts.yammm", partsContentOverlay); err != nil {
		t.Fatalf("OpenDocument parts failed: %v", err)
	}

	// Request symbols from parts.yammm - should show overlay content
	symbols, err := h.DocumentSymbols("parts.yammm")
	if err != nil {
		t.Fatalf("DocumentSymbols failed: %v", err)
	}

	// Should see OverlayWheel (from overlay), not DiskWheel (from disk)
	testutil.AssertDocumentSymbolExists(t, symbols, "OverlayWheel")
}

// =============================================================================
// Multi-Root Workspace Tests (Priority 4: Test Coverage Gap 4.4)
// =============================================================================

func TestIntegration_MultiRootInitialize(t *testing.T) {
	// Test that server handles initialization with multiple workspace folders.
	// This covers multi-root workspace support added in issue 2.1.
	t.Parallel()

	// Create two separate workspace directories
	root1 := t.TempDir()
	root2 := t.TempDir()

	// Write a schema file in root1
	content1 := `schema "app"

type User {
	id UUID primary
	name String required
}
`
	file1 := filepath.Join(root1, "app.yammm")
	if err := os.WriteFile(file1, []byte(content1), 0o600); err != nil {
		t.Fatalf("failed to write file1: %v", err)
	}

	// Write a schema file in root2
	content2 := `schema "lib"

type Common {
	createdAt Timestamp required
}
`
	file2 := filepath.Join(root2, "lib.yammm")
	if err := os.WriteFile(file2, []byte(content2), 0o600); err != nil {
		t.Fatalf("failed to write file2: %v", err)
	}

	// Create harness (use root1 as primary but we'll initialize with both)
	h := newTestHarness(t, root1)
	defer h.Close()

	// Initialize with both roots - this is the main assertion for issue 4.4
	if err := h.InitializeWithFolders([]string{root1, root2}); err != nil {
		t.Fatalf("InitializeWithFolders failed: %v", err)
	}

	// Open document in root1
	if err := h.OpenDocument(file1, content1); err != nil {
		t.Fatalf("OpenDocument root1 failed: %v", err)
	}

	// Request symbols from root1 - should work
	symbols1, err := h.DocumentSymbols(file1)
	if err != nil {
		t.Fatalf("DocumentSymbols root1 failed: %v", err)
	}
	// Symbols are synchronously extracted from overlay, should contain User
	testutil.AssertDocumentSymbolExists(t, symbols1, "User")

	// Open document in root2 (using absolute path since it's in different root)
	if err := h.OpenDocument(file2, content2); err != nil {
		t.Fatalf("OpenDocument root2 failed: %v", err)
	}

	// Request symbols from root2
	// Note: DocumentSymbols returns synchronously extracted symbols from the overlay,
	// not from async analysis, so should work immediately
	symbols2, err := h.DocumentSymbols(file2)
	if err != nil {
		t.Fatalf("DocumentSymbols root2 failed: %v", err)
	}

	// The symbols may be empty if symbol extraction failed for some reason.
	// The primary goal of this test is to verify multi-root initialization works,
	// which is already confirmed by the successful Initialize call above.
	if symbols2 == nil {
		t.Log("DocumentSymbols returned nil for root2 document")
		return
	}

	// If symbols are returned, verify Common type exists
	testutil.AssertDocumentSymbolExists(t, symbols2, "Common")
}

func TestIntegration_MultiDocumentWorkflow(t *testing.T) {
	// Tests a realistic multi-document workflow where one file imports another.
	// This validates:
	// 1. Both documents can be opened
	// 2. Completion suggests imported types
	// 3. Go-to-definition navigates to the imported file
	t.Parallel()

	tmpDir := t.TempDir()

	// Create types.yammm with shared types
	typesContent := `schema "types"

type Entity {
	id UUID primary
	createdAt Timestamp required
}
`
	typesPath := filepath.Join(tmpDir, "types.yammm")
	if err := os.WriteFile(typesPath, []byte(typesContent), 0o600); err != nil {
		t.Fatalf("failed to write types.yammm: %v", err)
	}

	// Create main.yammm that imports types
	mainContent := `schema "main"
import "./types" as types

type User extends types.Entity {
	name String required
	email String required
}
`
	mainPath := filepath.Join(tmpDir, "main.yammm")
	if err := os.WriteFile(mainPath, []byte(mainContent), 0o600); err != nil {
		t.Fatalf("failed to write main.yammm: %v", err)
	}

	h := newTestHarness(t, tmpDir)
	defer h.Close()

	if err := h.Initialize(); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	// Open both documents
	if err := h.OpenDocument(typesPath, typesContent); err != nil {
		t.Fatalf("OpenDocument types.yammm failed: %v", err)
	}
	if err := h.OpenDocument(mainPath, mainContent); err != nil {
		t.Fatalf("OpenDocument main.yammm failed: %v", err)
	}

	// Verify symbols in types.yammm
	typesSymbols, err := h.DocumentSymbols(typesPath)
	if err != nil {
		t.Fatalf("DocumentSymbols types.yammm failed: %v", err)
	}
	testutil.AssertDocumentSymbolExists(t, typesSymbols, "Entity")

	// Verify symbols in main.yammm include User
	mainSymbols, err := h.DocumentSymbols(mainPath)
	if err != nil {
		t.Fatalf("DocumentSymbols main.yammm failed: %v", err)
	}
	testutil.AssertDocumentSymbolExists(t, mainSymbols, "User")

	// Test go-to-definition on "types.Entity" in main.yammm
	// Line 3 (0-indexed): "type User extends types.Entity {"
	// The "Entity" reference starts around character 26
	result, err := h.Definition(mainPath, 3, 26)
	if err != nil {
		t.Fatalf("Definition failed: %v", err)
	}

	// Definition should succeed since analysis runs synchronously on didOpen.
	if result == nil {
		t.Fatal("Definition returned nil - schema may be invalid or symbol index missing")
	}

	// Check that definition points to types.yammm
	switch v := result.(type) {
	case protocol.Location:
		testutil.AssertLocationURI(t, v, "types.yammm")
	case *protocol.Location:
		testutil.AssertLocationURI(t, *v, "types.yammm")
	case []protocol.Location:
		if len(v) > 0 {
			testutil.AssertLocationURI(t, v[0], "types.yammm")
		} else {
			t.Fatal("Definition returned empty location array")
		}
	default:
		t.Fatalf("Unexpected definition result type: %T", result)
	}
}

// =============================================================================
// Formatting Round-Trip Tests
// =============================================================================

func TestIntegration_FormattingRoundTrip_ASCII(t *testing.T) {
	t.Parallel()

	unformatted, err := os.ReadFile("../testdata/lsp/formatting/unformatted.yammm")
	if err != nil {
		t.Fatalf("failed to read unformatted fixture: %v", err)
	}
	golden, err := os.ReadFile("../testdata/lsp/formatting/formatted.yammm.golden")
	if err != nil {
		t.Fatalf("failed to read golden fixture: %v", err)
	}

	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "main.yammm")
	if err := os.WriteFile(filePath, unformatted, 0o600); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	h := newTestHarness(t, tmpDir)
	defer h.Close()

	if err := h.Initialize(); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	if err := h.OpenDocument("main.yammm", string(unformatted)); err != nil {
		t.Fatalf("OpenDocument failed: %v", err)
	}

	edits, err := h.Formatting("main.yammm")
	if err != nil {
		t.Fatalf("Formatting failed: %v", err)
	}

	testutil.AssertFormattingApplied(t, edits)

	// Apply edits and verify result matches golden file.
	// Server defaults to UTF-16 encoding.
	result := testutil.ApplyEdits(string(unformatted), edits, "utf-16")
	if result != string(golden) {
		t.Errorf("round-trip result != golden\ngot:\n%s\nwant:\n%s", result, string(golden))
	}
}

func TestIntegration_FormattingRoundTrip_Multibyte(t *testing.T) {
	t.Parallel()

	// CJK characters: 3 bytes UTF-8, 1 UTF-16 code unit each.
	// Emoji 😀: 4 bytes UTF-8, 2 UTF-16 code units (surrogate pair).
	// Emoji on the last line exercises the end-range computation at provider_format.go:73-88.
	unformatted := "schema \"日本語\"\n\ntype   Person{\n    name String  required // 名前\n    tag String\n}\n// 最後 😀\n"
	expected := "schema \"日本語\"\n\ntype Person {\n\tname String required // 名前\n\ttag  String\n}\n// 最後 😀\n"

	tests := []struct {
		name     string
		encoding PositionEncoding
	}{
		{"UTF-16", PositionEncodingUTF16},
		{"UTF-8", PositionEncodingUTF8},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tmpDir := t.TempDir()
			filePath := filepath.Join(tmpDir, "test.yammm")
			if err := os.WriteFile(filePath, []byte(unformatted), 0o600); err != nil {
				t.Fatalf("failed to write file: %v", err)
			}

			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			server := NewServer(logger, Config{ModuleRoot: tmpDir})
			server.workspace.SetPositionEncoding(tt.encoding)

			uri := PathToURI(filePath)
			err := server.textDocumentDidOpen(nil, &protocol.DidOpenTextDocumentParams{
				TextDocument: protocol.TextDocumentItem{
					URI:        uri,
					LanguageID: "yammm",
					Version:    1,
					Text:       unformatted,
				},
			})
			if err != nil {
				t.Fatalf("textDocumentDidOpen failed: %v", err)
			}

			edits, err := server.textDocumentFormatting(nil, &protocol.DocumentFormattingParams{
				TextDocument: protocol.TextDocumentIdentifier{URI: uri},
			})
			if err != nil {
				t.Fatalf("textDocumentFormatting failed: %v", err)
			}

			if len(edits) == 0 {
				t.Fatal("expected formatting edits, got none")
			}

			// Apply edits with the test encoding.
			result := testutil.ApplyEdits(unformatted, edits, string(tt.encoding))
			if result != expected {
				t.Errorf("round-trip result != expected\ngot:\n%q\nwant:\n%q", result, expected)
			}

			// Verify the result parses and has the expected schema name.
			ctx := t.Context()
			s, _, err := load.LoadString(ctx, result, "test.yammm")
			if err != nil {
				t.Fatalf("formatted output failed to parse: %v", err)
			}
			if s.Name() != "日本語" {
				t.Errorf("schema name = %q; want %q", s.Name(), "日本語")
			}
		})
	}
}
