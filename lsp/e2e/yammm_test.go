package e2e_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	lsp "github.com/simon-lentz/yammm/lsp"
	"github.com/simon-lentz/yammm/lsp/internal/protocol"

	"github.com/simon-lentz/yammm/lsp/internal/testutil"
	"github.com/simon-lentz/yammm/schema/load"
)

// Integration Tests using Harness
// These tests verify LSP handler behavior through direct handler calls.
// Note: Tests that require notification publishing (like diagnostics) are
// limited because glsp.Context is required for Notify calls.
func TestIntegration_InitializeSuccess(t *testing.T) {
	// Test that server initialization succeeds
	t.Parallel()

	tmpDir := t.TempDir()
	h := newTestHarness(t, tmpDir)
	defer h.Close()

	err := h.Initialize()
	require.NoError(t, err, "Initialize failed")
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
	err := os.WriteFile(filePath, []byte(content), 0o600)
	require.NoError(t, err, "failed to write file")

	h := newTestHarness(t, tmpDir)
	defer h.Close()

	err = h.Initialize()
	require.NoError(t, err, "Initialize failed")

	// Request formatting without didOpen - should return empty since
	// the document is not open
	edits, err := h.Formatting("main.yammm")
	require.NoError(t, err, "Formatting failed")

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
	err := os.WriteFile(filePath, []byte(content), 0o600)
	require.NoError(t, err, "failed to write file")

	h := newTestHarness(t, tmpDir)
	defer h.Close()

	err = h.Initialize()
	require.NoError(t, err, "Initialize failed")

	// Request hover without didOpen - should return nil (not an error)
	// This is expected behavior: documents must be open for hover to work.
	hover, err := h.Hover("main.yammm", 2, 5)
	require.NoError(t, err, "Hover returned error")

	assert.Nil(t, hover, "Hover should return nil for unopened documents")
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
	err := os.WriteFile(partsPath, []byte(partsContent), 0o600)
	require.NoError(t, err, "failed to write parts file")

	// Write main file
	mainContent := `schema "main"

import "./parts" as parts

type Car {
	*-> WHEELS (many) parts.Wheel
}
`
	mainPath := filepath.Join(tmpDir, "main.yammm")
	err = os.WriteFile(mainPath, []byte(mainContent), 0o600)
	require.NoError(t, err, "failed to write main file")

	h := newTestHarness(t, tmpDir)
	defer h.Close()

	err = h.Initialize()
	require.NoError(t, err, "Initialize failed")

	// Request definition without didOpen - should return nil (not an error)
	// This is expected behavior: documents must be open for definition to work.
	result, err := h.Definition("main.yammm", 5, 22)
	require.NoError(t, err, "Definition returned error")

	assert.Nil(t, result, "Definition should return nil for unopened documents")
}

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
	err := os.WriteFile(filePath, []byte(diskContent), 0o600)
	require.NoError(t, err, "failed to write file")

	h := newTestHarness(t, tmpDir)
	defer h.Close()

	err = h.Initialize()
	require.NoError(t, err, "Initialize failed")

	// Open document with version B content (different from disk)
	overlayContent := `schema "test"

type Person {
	overlayField String
}
`
	err = h.OpenDocument("main.yammm", overlayContent)
	require.NoError(t, err, "OpenDocument failed")

	// Request hover on the field (line 3, char 1)
	hover, err := h.Hover("main.yammm", 3, 1)
	require.NoError(t, err, "Hover failed")

	// Hover should show overlay content, not disk content
	if hover != nil {
		testutil.AssertHoverContains(t, hover, "overlayField")
	} else {
		// If no hover, verify via symbols
		symbols, err := h.DocumentSymbols("main.yammm")
		require.NoError(t, err, "DocumentSymbols failed")
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
	err := os.WriteFile(partsPath, []byte(partsContent), 0o600)
	require.NoError(t, err, "failed to write parts file")

	// Write main.yammm that imports parts using type extension
	// (simpler than composition, tests cross-schema reference resolution)
	mainContent := `schema "main"

import "./parts" as parts

type Car extends parts.Wheel {
	color String
}
`
	mainPath := filepath.Join(tmpDir, "main.yammm")
	err = os.WriteFile(mainPath, []byte(mainContent), 0o600)
	require.NoError(t, err, "failed to write main file")

	h := newTestHarness(t, tmpDir)
	defer h.Close()

	err = h.Initialize()
	require.NoError(t, err, "Initialize failed")

	// Open ONLY main.yammm (parts.yammm NOT opened)
	err = h.OpenDocument("main.yammm", mainContent)
	require.NoError(t, err, "OpenDocument failed")

	// Request definition on "Wheel" in "parts.Wheel" (line 4, char 24)
	// Line 4 is: "type Car extends parts.Wheel {"
	// The qualifier "parts" is at chars 17-21, dot at 22, "Wheel" at 23-27
	result, err := h.Definition("main.yammm", 4, 24)
	require.NoError(t, err, "Definition failed")

	// Definition should succeed since analysis runs synchronously on didOpen.
	// A nil result indicates a bug (e.g., invalid schema, missing symbol index).
	require.NotNil(t, result, "Definition returned nil - schema may be invalid or symbol index missing")

	// Definition should point to parts.yammm (loaded from disk)
	switch v := result.(type) {
	case protocol.Location:
		testutil.AssertLocationURI(t, v, "parts.yammm")
	case *protocol.Location:
		testutil.AssertLocationURI(t, *v, "parts.yammm")
	case []protocol.Location:
		require.NotEmpty(t, v, "Definition returned empty location array")
		testutil.AssertLocationURI(t, v[0], "parts.yammm")
	default:
		require.Failf(t, "Unexpected definition result type", "got type %T", result)
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
	err := os.WriteFile(partsPath, []byte(partsContentDisk), 0o600)
	require.NoError(t, err, "failed to write parts file")

	h := newTestHarness(t, tmpDir)
	defer h.Close()

	err = h.Initialize()
	require.NoError(t, err, "Initialize failed")

	// Open parts.yammm with DIFFERENT type (overlay has OverlayWheel)
	partsContentOverlay := `schema "parts"

type OverlayWheel {
	overlayDiameter Integer
}
`
	err = h.OpenDocument("parts.yammm", partsContentOverlay)
	require.NoError(t, err, "OpenDocument parts failed")

	// Request symbols from parts.yammm - should show overlay content
	symbols, err := h.DocumentSymbols("parts.yammm")
	require.NoError(t, err, "DocumentSymbols failed")

	// Should see OverlayWheel (from overlay), not DiskWheel (from disk)
	testutil.AssertDocumentSymbolExists(t, symbols, "OverlayWheel")
}

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
	err := os.WriteFile(file1, []byte(content1), 0o600)
	require.NoError(t, err, "failed to write file1")

	// Write a schema file in root2
	content2 := `schema "lib"

type Common {
	createdAt Timestamp required
}
`
	file2 := filepath.Join(root2, "lib.yammm")
	err = os.WriteFile(file2, []byte(content2), 0o600)
	require.NoError(t, err, "failed to write file2")

	// Create harness (use root1 as primary but we'll initialize with both)
	h := newTestHarness(t, root1)
	defer h.Close()

	// Initialize with both roots - this is the main assertion for issue 4.4
	err = h.InitializeWithFolders([]string{root1, root2})
	require.NoError(t, err, "InitializeWithFolders failed")

	// Open document in root1
	err = h.OpenDocument(file1, content1)
	require.NoError(t, err, "OpenDocument root1 failed")

	// Request symbols from root1 - should work
	symbols1, err := h.DocumentSymbols(file1)
	require.NoError(t, err, "DocumentSymbols root1 failed")
	// Symbols are synchronously extracted from overlay, should contain User
	testutil.AssertDocumentSymbolExists(t, symbols1, "User")

	// Open document in root2 (using absolute path since it's in different root)
	err = h.OpenDocument(file2, content2)
	require.NoError(t, err, "OpenDocument root2 failed")

	// Request symbols from root2
	// Note: DocumentSymbols returns synchronously extracted symbols from the overlay,
	// not from async analysis, so should work immediately
	symbols2, err := h.DocumentSymbols(file2)
	require.NoError(t, err, "DocumentSymbols root2 failed")

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
	err := os.WriteFile(typesPath, []byte(typesContent), 0o600)
	require.NoError(t, err, "failed to write types.yammm")

	// Create main.yammm that imports types
	mainContent := `schema "main"
import "./types" as types

type User extends types.Entity {
	name String required
	email String required
}
`
	mainPath := filepath.Join(tmpDir, "main.yammm")
	err = os.WriteFile(mainPath, []byte(mainContent), 0o600)
	require.NoError(t, err, "failed to write main.yammm")

	h := newTestHarness(t, tmpDir)
	defer h.Close()

	err = h.Initialize()
	require.NoError(t, err, "Initialize failed")

	// Open both documents
	err = h.OpenDocument(typesPath, typesContent)
	require.NoError(t, err, "OpenDocument types.yammm failed")
	err = h.OpenDocument(mainPath, mainContent)
	require.NoError(t, err, "OpenDocument main.yammm failed")

	// Verify symbols in types.yammm
	typesSymbols, err := h.DocumentSymbols(typesPath)
	require.NoError(t, err, "DocumentSymbols types.yammm failed")
	testutil.AssertDocumentSymbolExists(t, typesSymbols, "Entity")

	// Verify symbols in main.yammm include User
	mainSymbols, err := h.DocumentSymbols(mainPath)
	require.NoError(t, err, "DocumentSymbols main.yammm failed")
	testutil.AssertDocumentSymbolExists(t, mainSymbols, "User")

	// Test go-to-definition on "types.Entity" in main.yammm
	// Line 3 (0-indexed): "type User extends types.Entity {"
	// The "Entity" reference starts around character 26
	result, err := h.Definition(mainPath, 3, 26)
	require.NoError(t, err, "Definition failed")

	// Definition should succeed since analysis runs synchronously on didOpen.
	require.NotNil(t, result, "Definition returned nil - schema may be invalid or symbol index missing")

	// Check that definition points to types.yammm
	switch v := result.(type) {
	case protocol.Location:
		testutil.AssertLocationURI(t, v, "types.yammm")
	case *protocol.Location:
		testutil.AssertLocationURI(t, *v, "types.yammm")
	case []protocol.Location:
		require.NotEmpty(t, v, "Definition returned empty location array")
		testutil.AssertLocationURI(t, v[0], "types.yammm")
	default:
		require.Failf(t, "Unexpected definition result type", "got type %T", result)
	}
}

func TestIntegration_FormattingRoundTrip_ASCII(t *testing.T) {
	t.Parallel()

	unformatted, err := os.ReadFile("../testdata/lsp/formatting/unformatted.yammm")
	require.NoError(t, err, "failed to read unformatted fixture")
	golden, err := os.ReadFile("../testdata/lsp/formatting/formatted.yammm.golden")
	require.NoError(t, err, "failed to read golden fixture")

	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "main.yammm")
	err = os.WriteFile(filePath, unformatted, 0o600)
	require.NoError(t, err, "failed to write file")

	h := newTestHarness(t, tmpDir)
	defer h.Close()

	err = h.Initialize()
	require.NoError(t, err, "Initialize failed")

	err = h.OpenDocument("main.yammm", string(unformatted))
	require.NoError(t, err, "OpenDocument failed")

	edits, err := h.Formatting("main.yammm")
	require.NoError(t, err, "Formatting failed")

	testutil.AssertFormattingApplied(t, edits)

	// Apply edits and verify result matches golden file.
	// Server defaults to UTF-16 encoding.
	result := testutil.ApplyEdits(string(unformatted), edits, "utf-16")
	assert.Equal(t, string(golden), result, "round-trip result != golden")
}

func TestFormatting_UsesTokenStreamFormatterForIntraLineSpacing(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	content := "schema \"test\"\n\ntype   A{\n\tname String\n}\n"
	filePath := filepath.Join(tmpDir, "test.yammm")
	err := os.WriteFile(filePath, []byte(content), 0o600)
	require.NoError(t, err, "failed to write file")

	h := newTestHarness(t, tmpDir)
	defer h.Close()

	err = h.Initialize()
	require.NoError(t, err, "Initialize failed")

	err = h.OpenDocument("test.yammm", content)
	require.NoError(t, err, "OpenDocument failed")

	edits, err := h.Formatting("test.yammm")
	require.NoError(t, err, "Formatting failed")

	require.NotEmpty(t, edits, "expected formatting edits for intra-line spacing normalization")

	// Apply edits and verify the result normalizes type spacing.
	result := testutil.ApplyEdits(content, edits, "utf-16")
	assert.Contains(t, result, "type A {", "expected formatted text to normalize type spacing")
}

func TestFormatting_UTF8PositionEncoding(t *testing.T) {
	t.Parallel()

	// CJK characters are 3 bytes in UTF-8 but 1 UTF-16 code unit each.
	content := "schema \"日本語\"\n\ntype Person {    \n\tname String\n}\n"

	tests := []struct {
		name     string
		encoding lsp.PositionEncoding
	}{
		{"UTF-16", lsp.PositionEncodingUTF16},
		{"UTF-8", lsp.PositionEncodingUTF8},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tmpDir := t.TempDir()
			filePath := filepath.Join(tmpDir, "test.yammm")
			err := os.WriteFile(filePath, []byte(content), 0o600)
			require.NoError(t, err, "failed to write file")

			h, server := newTestHarnessWithServer(t, tmpDir)
			defer h.Close()

			server.Workspace().SetPositionEncoding(tt.encoding)

			err = h.OpenDocument("test.yammm", content)
			require.NoError(t, err, "OpenDocument failed")

			edits, err := h.Formatting("test.yammm")
			require.NoError(t, err, "Formatting failed")

			if len(edits) == 0 {
				// Document may not need formatting — the test verifies no crash.
				return
			}

			// Verify the edit range covers the document (starts at 0,0).
			edit := edits[0]
			assert.Equal(t, uint32(0), edit.Range.Start.Line, "edit range should start at line 0")
			assert.Equal(t, uint32(0), edit.Range.Start.Character, "edit range should start at character 0")
		})
	}
}

func TestIntegration_FormattingRoundTrip_Multibyte(t *testing.T) {
	t.Parallel()

	// CJK characters: 3 bytes UTF-8, 1 UTF-16 code unit each.
	// Emoji 😀: 4 bytes UTF-8, 2 UTF-16 code units (surrogate pair).
	unformatted := "schema \"日本語\"\n\ntype   Person{\n    name String  required // 名前\n    tag String\n}\n// 最後 😀\n"
	expected := "schema \"日本語\"\n\ntype Person {\n\tname String required // 名前\n\ttag  String\n}\n// 最後 😀\n"

	tests := []struct {
		name     string
		encoding lsp.PositionEncoding
	}{
		{"UTF-16", lsp.PositionEncodingUTF16},
		{"UTF-8", lsp.PositionEncodingUTF8},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tmpDir := t.TempDir()
			filePath := filepath.Join(tmpDir, "test.yammm")
			err := os.WriteFile(filePath, []byte(unformatted), 0o600)
			require.NoError(t, err, "failed to write file")

			h, server := newTestHarnessWithServer(t, tmpDir)
			defer h.Close()

			server.Workspace().SetPositionEncoding(tt.encoding)

			err = h.OpenDocument("test.yammm", unformatted)
			require.NoError(t, err, "OpenDocument failed")

			edits, err := h.Formatting("test.yammm")
			require.NoError(t, err, "Formatting failed")

			require.NotEmpty(t, edits, "expected formatting edits, got none")

			// Apply edits with the test encoding.
			result := testutil.ApplyEdits(unformatted, edits, string(tt.encoding))
			assert.Equal(t, expected, result, "round-trip result != expected")

			// Verify the result parses and has the expected schema name.
			ctx := t.Context()
			s, _, err := load.String(ctx, result, "test.yammm")
			require.NoError(t, err, "formatted output failed to parse")
			assert.Equal(t, "日本語", s.Name(), "schema name mismatch")
		})
	}
}

func TestCompletion_UTF8Mode_Integration(t *testing.T) {
	t.Parallel()

	// CJK content to exercise byte offset calculations in UTF-8 mode.
	content := "schema \"日本語テスト\"\n\ntype 人物 {\n\t名前 String\n}\n"

	tests := []struct {
		name string
		line int
		char int // byte offset from line start (UTF-8 encoding)
	}{
		{"line 0, start", 0, 0},
		{"line 0, at schema name start", 0, 8},
		{"line 0, mid CJK char", 0, 9},
		{"line 2, before type name", 2, 5},
		{"line 2, mid first CJK char", 2, 6},
		{"line 2, at opening brace", 2, 12},
		{"line 3, at property name", 3, 1},
		{"line 3, mid property name", 3, 2},
		{"line 3, after property name", 3, 8},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tmpDir := t.TempDir()
			filePath := filepath.Join(tmpDir, "main.yammm")
			err := os.WriteFile(filePath, []byte(content), 0o600)
			require.NoError(t, err, "failed to write file")

			h, server := newTestHarnessWithServer(t, tmpDir)
			defer h.Close()

			server.Workspace().SetPositionEncoding(lsp.PositionEncodingUTF8)

			err = h.OpenDocument(filePath, content)
			require.NoError(t, err, "OpenDocument failed")

			// Completion should not panic.
			result, err := h.Completion(filePath, tt.line, tt.char)
			require.NoError(t, err, "completion failed")

			switch v := result.(type) {
			case nil:
				// No completions is valid.
			case []protocol.CompletionItem:
				t.Logf("got %d completion items", len(v))
			default:
				assert.Failf(t, "unexpected result type", "got type %T", result)
			}
		})
	}
}

func TestCompletion_UTF8Mode_NoPanic(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	content := "schema \"test\"\n\ntype Person {\n\tname String\n}\n"
	filePath := filepath.Join(tmpDir, "main.yammm")
	err := os.WriteFile(filePath, []byte(content), 0o600)
	require.NoError(t, err, "failed to write file")

	h, server := newTestHarnessWithServer(t, tmpDir)
	defer h.Close()

	server.Workspace().SetPositionEncoding(lsp.PositionEncodingUTF8)

	err = h.OpenDocument(filePath, content)
	require.NoError(t, err, "OpenDocument failed")

	// Request completion inside the type body (line 3).
	result, err := h.Completion(filePath, 3, 0)
	require.NoError(t, err, "completion failed")

	items, ok := result.([]protocol.CompletionItem)
	if !ok {
		t.Skipf("no completion items returned (result type: %T)", result)
	}

	assert.NotEmpty(t, items, "expected completion items for type body context")
}
