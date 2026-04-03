package e2e_test

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/simon-lentz/yammm/lsp/internal/analysis"
	"github.com/simon-lentz/yammm/lsp/internal/markdown"
	"github.com/simon-lentz/yammm/lsp/internal/protocol"

	"github.com/simon-lentz/yammm/lsp/internal/docstate"
	"github.com/simon-lentz/yammm/lsp/internal/testutil"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// notificationCollector captures LSP notifications for testing.
type notificationCollector struct {
	mu      sync.Mutex
	entries []notificationEntry
}

type notificationEntry struct {
	Method string
	Params any
}

func (c *notificationCollector) notify(method string, params any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = append(c.entries, notificationEntry{Method: method, Params: params})
}

func (c *notificationCollector) diagnosticsFor(uri string) []protocol.Diagnostic {
	c.mu.Lock()
	defer c.mu.Unlock()

	for i := len(c.entries) - 1; i >= 0; i-- {
		e := c.entries[i]
		if e.Method != protocol.ServerTextDocumentPublishDiagnostics {
			continue
		}
		p, ok := e.Params.(protocol.PublishDiagnosticsParams)
		if ok && p.URI == uri {
			return p.Diagnostics
		}
	}
	return nil
}

func TestMarkdownIntegration_DiagnosticsInCodeBlock(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	h, server := newTestHarnessWithServer(t, tmpDir)
	defer h.Close()

	// Write a markdown file with syntax errors
	content := "# Test\n\n```yammm\nnot valid schema!!!\n```\n"
	mdPath := filepath.Join(tmpDir, "test.md")
	require.NoError(t, os.WriteFile(mdPath, []byte(content), 0o600))

	// Open the markdown document directly in the workspace (bypassing jrpc2)
	// so we can call analyzeMarkdownAndPublish synchronously without a race.
	uri := testutil.PathToURI(mdPath)
	server.Workspace().MarkdownDocumentOpenedForTest(uri, 1, content)

	// Re-analyze with a notificationCollector so we can inspect published diagnostics.
	collector := &notificationCollector{}
	server.Workspace().SetNotifier(collector.notify)
	server.Workspace().AnalyzeMarkdownAndPublish(t.Context(), uri)

	diags := collector.diagnosticsFor(uri)
	assert.NotEmpty(t, diags, "expected diagnostics for syntax error in code block")

	// At least one diagnostic should have a range in markdown coordinates (line >= 3,
	// since the code block content starts at line 3).
	var hasMarkdownCoords bool
	for _, d := range diags {
		if int(d.Range.Start.Line) >= 3 {
			hasMarkdownCoords = true
			break
		}
	}
	assert.True(t, hasMarkdownCoords,
		"expected at least one diagnostic with markdown-coordinate line >= 3, got: %+v", diags)
}

func TestMarkdownIntegration_HoverInCodeBlock(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	h := newMarkdownTestHarness(t, tmpDir)
	defer h.Close()

	// Line 0: "# Test"
	// Line 1: ""
	// Line 2: "```yammm"
	// Line 3: schema "test"       <- block local line 0
	// Line 4:                      <- block local line 1
	// Line 5: type Foo {           <- block local line 2
	// Line 6:     id String primary <- block local line 3
	// Line 7: }                    <- block local line 4
	// Line 8: "```"
	content := "# Test\n\n```yammm\nschema \"test\"\n\ntype Foo {\n    id String primary\n}\n```\n"
	mdPath := filepath.Join(tmpDir, "hover.md")
	require.NoError(t, os.WriteFile(mdPath, []byte(content), 0o600))

	err := h.OpenMarkdownDocument(mdPath, content)
	require.NoError(t, err)

	// Hover over "Foo" at line 5, character 5 (markdown coords)
	hover, err := h.Hover(mdPath, 5, 5)
	require.NoError(t, err)
	require.NotNil(t, hover, "expected hover result for type name in code block")

	// Verify the range is in markdown coordinates (not block-local)
	if hover.Range != nil {
		assert.GreaterOrEqual(t, int(hover.Range.Start.Line), 3,
			"hover range should be in markdown coordinates")
	}
}

func TestMarkdownIntegration_OutsideCodeBlock(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	h := newMarkdownTestHarness(t, tmpDir)
	defer h.Close()

	content := "# Test\n\nSome prose here.\n\n```yammm\nschema \"test\"\n```\n"
	mdPath := filepath.Join(tmpDir, "outside.md")
	require.NoError(t, os.WriteFile(mdPath, []byte(content), 0o600))

	err := h.OpenMarkdownDocument(mdPath, content)
	require.NoError(t, err)

	// Hover on prose (line 2) should return nil
	hover, err := h.Hover(mdPath, 2, 0)
	require.NoError(t, err)
	assert.Nil(t, hover, "expected nil hover for prose position outside code block")
}

func TestMarkdownIntegration_CompletionOutsideCodeBlock(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	h := newMarkdownTestHarness(t, tmpDir)
	defer h.Close()

	content := "# Test\n\nSome prose here.\n\n```yammm\nschema \"test\"\n```\n"
	mdPath := filepath.Join(tmpDir, "comp_outside.md")
	require.NoError(t, os.WriteFile(mdPath, []byte(content), 0o600))

	err := h.OpenMarkdownDocument(mdPath, content)
	require.NoError(t, err)

	// Completion on prose (line 2) should return nil
	result, err := h.Completion(mdPath, 2, 0)
	require.NoError(t, err)
	assert.Nil(t, result, "expected nil completion for prose position outside code block")
}

func TestMarkdownIntegration_DefinitionOutsideCodeBlock(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	h := newMarkdownTestHarness(t, tmpDir)
	defer h.Close()

	content := "# Test\n\nSome prose here.\n\n```yammm\nschema \"test\"\n```\n"
	mdPath := filepath.Join(tmpDir, "def_outside.md")
	require.NoError(t, os.WriteFile(mdPath, []byte(content), 0o600))

	err := h.OpenMarkdownDocument(mdPath, content)
	require.NoError(t, err)

	// Definition on prose (line 2) should return nil
	result, err := h.Definition(mdPath, 2, 0)
	require.NoError(t, err)
	assert.Nil(t, result, "expected nil definition for prose position outside code block")
}

func TestMarkdownIntegration_SymbolsNoCodeBlocks(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	h := newMarkdownTestHarness(t, tmpDir)
	defer h.Close()

	// Markdown with only prose — no yammm fenced blocks
	content := "# Just Prose\n\nNo code blocks here.\n\nMore text.\n"
	mdPath := filepath.Join(tmpDir, "no_blocks.md")
	require.NoError(t, os.WriteFile(mdPath, []byte(content), 0o600))

	err := h.OpenMarkdownDocument(mdPath, content)
	require.NoError(t, err)

	// Symbols should return nil when there are no code blocks
	symbols, err := h.DocumentSymbols(mdPath)
	require.NoError(t, err)
	assert.Nil(t, symbols, "expected nil symbols for markdown with no code blocks")
}

func TestMarkdownIntegration_MultipleBlocks(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	h := newMarkdownTestHarness(t, tmpDir)
	defer h.Close()

	// Two independent code blocks
	content := "# Block One\n\n```yammm\nschema \"block_one\"\n\ntype Alpha {\n    id String primary\n}\n```\n\n# Block Two\n\n```yammm\nschema \"block_two\"\n\ntype Beta {\n    name String primary\n}\n```\n"
	mdPath := filepath.Join(tmpDir, "multi.md")
	require.NoError(t, os.WriteFile(mdPath, []byte(content), 0o600))

	err := h.OpenMarkdownDocument(mdPath, content)
	require.NoError(t, err)

	// Hover in first block (line 5 = "type Alpha {")
	hover1, err := h.Hover(mdPath, 5, 5)
	require.NoError(t, err)
	require.NotNil(t, hover1, "expected hover in first block")

	// Hover in second block (line 15 = "type Beta {")
	hover2, err := h.Hover(mdPath, 15, 5)
	require.NoError(t, err)
	require.NotNil(t, hover2, "expected hover in second block")

	// Document symbols should include types from both blocks
	symbols, err := h.DocumentSymbols(mdPath)
	require.NoError(t, err)
	require.NotNil(t, symbols, "expected symbols from both blocks")

	syms, ok := symbols.([]protocol.DocumentSymbol)
	require.True(t, ok, "expected []protocol.DocumentSymbol")
	assert.GreaterOrEqual(t, len(syms), 2, "expected symbols from both blocks")
}

func TestMarkdownIntegration_CompletionInBlock(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	h := newMarkdownTestHarness(t, tmpDir)
	defer h.Close()

	// Line 0: "# Test"
	// Line 1: ""
	// Line 2: "```yammm"
	// Line 3: schema "test"
	// Line 4:
	// Line 5: type Foo {
	// Line 6:     <- cursor here for property-type completion
	// Line 7: }
	// Line 8: "```"
	content := "# Test\n\n```yammm\nschema \"test\"\n\ntype Foo {\n    \n}\n```\n"
	mdPath := filepath.Join(tmpDir, "completion.md")
	require.NoError(t, os.WriteFile(mdPath, []byte(content), 0o600))

	err := h.OpenMarkdownDocument(mdPath, content)
	require.NoError(t, err)

	// Request completion inside type body (line 6, char 4)
	result, err := h.Completion(mdPath, 6, 4)
	require.NoError(t, err)
	require.NotNil(t, result, "expected completion items inside code block")

	// Should have keyword/type completions
	if items, ok := result.([]protocol.CompletionItem); ok {
		assert.NotEmpty(t, items, "expected completion items")
	}
}

func TestMarkdownIntegration_ImportRejection(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	h, server := newTestHarnessWithServer(t, tmpDir)
	defer h.Close()

	content := "# Import Test\n\n```yammm\nschema \"import_test\"\n\nimport \"./sibling\" as s\n\ntype Foo {\n    id String primary\n}\n```\n"
	mdPath := filepath.Join(tmpDir, "imports.md")
	require.NoError(t, os.WriteFile(mdPath, []byte(content), 0o600))

	// Open the markdown document directly in the workspace (bypassing jrpc2)
	// so we can call analyzeMarkdownAndPublish synchronously without a race.
	uri := testutil.PathToURI(mdPath)
	server.Workspace().MarkdownDocumentOpenedForTest(uri, 1, content)

	// Re-analyze with a notificationCollector so we can inspect published diagnostics.
	collector := &notificationCollector{}
	server.Workspace().SetNotifier(collector.notify)
	server.Workspace().AnalyzeMarkdownAndPublish(t.Context(), uri)

	diags := collector.diagnosticsFor(uri)
	require.NotEmpty(t, diags, "expected diagnostics for import rejection")

	// Assert E_IMPORT_NOT_ALLOWED code exists with Hint severity.
	var found bool
	for _, d := range diags {
		if d.Code == nil {
			continue
		}
		codeVal, ok := d.Code.Value.(string)
		if !ok || codeVal != "E_IMPORT_NOT_ALLOWED" {
			continue
		}
		found = true
		require.NotNil(t, d.Severity, "E_IMPORT_NOT_ALLOWED diagnostic should have severity")
		assert.Equal(t, protocol.DiagnosticSeverityHint, *d.Severity,
			"E_IMPORT_NOT_ALLOWED should be downgraded to Hint in markdown")
		// The import is on line 5 in markdown coordinates (line 0: heading, 1: empty,
		// 2: fence, 3: schema, 4: empty, 5: import).
		assert.GreaterOrEqual(t, int(d.Range.Start.Line), 3,
			"diagnostic should be in markdown coordinates")
		break
	}
	assert.True(t, found, "expected E_IMPORT_NOT_ALLOWED diagnostic, got: %+v", diags)

	// Features should still work despite the import error (no crash).
	// Open via harness for feature requests; sync to ensure processing.
	err := h.OpenMarkdownDocument(mdPath, content)
	require.NoError(t, err)
	h.Sync()
	_, err = h.DocumentSymbols(mdPath)
	require.NoError(t, err, "document symbols should not error despite import rejection")
}

func TestMarkdownIntegration_CloseCleansDiagnostics(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	h := newMarkdownTestHarness(t, tmpDir)
	defer h.Close()

	content := "# Test\n\n```yammm\nschema \"test\"\n\ntype Foo {\n    id String primary\n}\n```\n"
	mdPath := filepath.Join(tmpDir, "close.md")
	require.NoError(t, os.WriteFile(mdPath, []byte(content), 0o600))

	// Open
	err := h.OpenMarkdownDocument(mdPath, content)
	require.NoError(t, err)

	// Verify document is tracked (hover works)
	hover, err := h.Hover(mdPath, 5, 5)
	require.NoError(t, err)
	require.NotNil(t, hover, "expected hover before close")

	// Close
	err = h.CloseDocument(mdPath)
	require.NoError(t, err)

	// After close, hover should return nil (document no longer tracked)
	hover, err = h.Hover(mdPath, 5, 5)
	require.NoError(t, err)
	assert.Nil(t, hover, "expected nil hover after close")
}

func TestMarkdownIntegration_StaleVersionRejection(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	h, server := newTestHarnessWithServer(t, tmpDir)
	defer h.Close()

	content := "# Test\n\n```yammm\nschema \"test\"\n\ntype Foo {\n    id String primary\n}\n```\n"
	mdPath := filepath.Join(tmpDir, "stale.md")
	require.NoError(t, os.WriteFile(mdPath, []byte(content), 0o600))

	// Open at version 1
	err := h.OpenMarkdownDocument(mdPath, content)
	require.NoError(t, err)

	// Update to version 2
	updatedContent := "# Updated\n\n```yammm\nschema \"updated\"\n\ntype Bar {\n    name String primary\n}\n```\n"
	err = h.ChangeDocument(mdPath, updatedContent, 2)
	require.NoError(t, err)

	// Send stale version 1 change — should be ignored by the workspace
	err = h.ChangeDocument(mdPath, content, 1)
	require.NoError(t, err)

	// Ensure all notifications have been processed before inspecting workspace state.
	h.Sync()

	// Directly verify the stored text retained v2 content (stale v1 was rejected).
	uri := testutil.PathToURI(mdPath)
	text, ok := server.Workspace().GetMarkdownCurrentText(uri)
	require.True(t, ok, "markdown document should still be tracked")
	assert.Equal(t, updatedContent, text, "stale v1 change should not overwrite v2 content")
}

func TestMarkdownIntegration_FormattingReturnsEmpty(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	h := newMarkdownTestHarness(t, tmpDir)
	defer h.Close()

	content := "# Test\n\n```yammm\nschema \"test\"\n\ntype Foo {\n    id String primary\n}\n```\n"
	mdPath := filepath.Join(tmpDir, "format.md")
	require.NoError(t, os.WriteFile(mdPath, []byte(content), 0o600))

	err := h.OpenMarkdownDocument(mdPath, content)
	require.NoError(t, err)

	// Formatting on markdown file should return empty edits
	edits, err := h.Formatting(mdPath)
	require.NoError(t, err)
	assert.Empty(t, edits, "formatting should return empty edits for markdown files")
}

func TestMarkdownIntegration_IgnoreNonMarkdownExtension(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	h := newMarkdownTestHarness(t, tmpDir)
	defer h.Close()

	// Content has yammm blocks but file is .txt — should be ignored
	content := "# Test\n\n```yammm\nschema \"test\"\n\ntype Foo {\n    id String primary\n}\n```\n"
	txtPath := filepath.Join(tmpDir, "test.txt")
	require.NoError(t, os.WriteFile(txtPath, []byte(content), 0o600))

	// Open with languageID "plaintext" and .txt extension
	uri := testutil.PathToURI(txtPath)
	err := h.OpenDocumentRaw(uri, "plaintext", content)
	require.NoError(t, err)

	// Hover should return nil — .txt files are not handled
	hover, err := h.Hover(txtPath, 5, 5)
	require.NoError(t, err)
	assert.Nil(t, hover, "expected nil hover for .txt file")
}

func TestMarkdownIntegration_SnippetBlockNoSchema(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	h := newMarkdownTestHarness(t, tmpDir)
	defer h.Close()

	// Snippet block without schema declaration — just type definitions
	// Line 0: "# Snippet Example"
	// Line 1: ""
	// Line 2: "```yammm"
	// Line 3: type Foo {           <- content starts here
	// Line 4:     id String primary
	// Line 5:     name String required
	// Line 6: }
	// Line 7: "```"
	content := "# Snippet Example\n\n```yammm\ntype Foo {\n    id String primary\n    name String required\n}\n```\n"
	mdPath := filepath.Join(tmpDir, "snippet.md")
	require.NoError(t, os.WriteFile(mdPath, []byte(content), 0o600))

	err := h.OpenMarkdownDocument(mdPath, content)
	require.NoError(t, err)

	// Hover over "Foo" at line 3, char 5 — should work despite no schema declaration
	hover, err := h.Hover(mdPath, 3, 5)
	require.NoError(t, err)
	require.NotNil(t, hover, "expected hover result for type name in snippet block")

	// Verify hover range is in markdown coordinates (not prefixed-content)
	if hover.Range != nil {
		assert.GreaterOrEqual(t, int(hover.Range.Start.Line), 3,
			"hover range should be in markdown coordinates")
	}

	// Symbols should include Foo
	symbols, err := h.DocumentSymbols(mdPath)
	require.NoError(t, err)
	require.NotNil(t, symbols, "expected document symbols for snippet block")

	// Verify Foo symbol has correct markdown-coordinate range
	if syms, ok := symbols.([]protocol.DocumentSymbol); ok {
		var foundFoo bool
		for _, sym := range syms {
			if sym.Name == "Foo" {
				foundFoo = true
				assert.GreaterOrEqual(t, int(sym.Range.Start.Line), 3,
					"Foo symbol range should be in markdown coordinates")
				break
			}
			for _, child := range sym.Children {
				if child.Name == "Foo" {
					foundFoo = true
					break
				}
			}
		}
		assert.True(t, foundFoo, "expected Foo symbol in document symbols")
	}
}

// TestMarkdownIntegration_FeaturesWithMarkdownOnly verifies that all LSP features
// (hover, completion, symbols, formatting) work when only markdown files are open
// and no standalone .yammm files have been opened in the session.
func TestMarkdownIntegration_FeaturesWithMarkdownOnly(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	h := newMarkdownTestHarness(t, tmpDir)
	defer h.Close()

	// Open only a markdown file (no .yammm files opened)
	content := "# Only Markdown\n\n```yammm\nschema \"md_only\"\n\ntype Solo {\n    id String primary\n}\n```\n"
	mdPath := filepath.Join(tmpDir, "only.md")
	require.NoError(t, os.WriteFile(mdPath, []byte(content), 0o600))

	err := h.OpenMarkdownDocument(mdPath, content)
	require.NoError(t, err)

	// All features should work even when only .md files are open

	// Hover
	hover, err := h.Hover(mdPath, 5, 5)
	require.NoError(t, err)
	require.NotNil(t, hover, "hover should work with only .md files open")

	// Completion
	result, err := h.Completion(mdPath, 6, 4)
	require.NoError(t, err)
	require.NotNil(t, result, "completion should work with only .md files open")

	// Symbols
	symbols, err := h.DocumentSymbols(mdPath)
	require.NoError(t, err)
	require.NotNil(t, symbols, "symbols should work with only .md files open")

	// Formatting (returns empty for markdown)
	edits, err := h.Formatting(mdPath)
	require.NoError(t, err)
	assert.Empty(t, edits, "formatting returns empty for markdown")
}

// TestMarkdownIntegration_Fixtures exercises the full LSP handler stack against
// on-disk fixture files from testdata/lsp/markdown/. Each fixture is opened,
// and basic features (symbols, hover) are exercised to verify non-crash behavior.
func TestMarkdownIntegration_Fixtures(t *testing.T) {
	t.Parallel()

	fixtureDir := filepath.Join("..", "testdata", "lsp", "markdown")

	tests := []struct {
		name string
		file string
		// hoverLine is a line inside a code block (markdown coords) to probe hover.
		// Use -1 to skip hover probing (e.g., malformed fixtures).
		hoverLine int
	}{
		{name: "simple", file: "simple.md", hoverLine: 7},
		{name: "errors", file: "errors.md", hoverLine: -1},
		{name: "imports", file: "imports.md", hoverLine: 7},
		{name: "indented", file: "indented.md", hoverLine: 19},
		{name: "multiple", file: "multiple.md", hoverLine: 7},
		{name: "nested", file: "nested.md", hoverLine: -1},
		{name: "empty", file: "empty.md", hoverLine: -1},
		{name: "malformed", file: "malformed.md", hoverLine: -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tmpDir := t.TempDir()
			h := newMarkdownTestHarness(t, tmpDir)
			defer h.Close()

			data, err := os.ReadFile(filepath.Join(fixtureDir, tt.file))
			require.NoError(t, err)
			content := docstate.NormalizeLineEndings(string(data))

			mdPath := filepath.Join(tmpDir, tt.file)
			require.NoError(t, os.WriteFile(mdPath, []byte(content), 0o600))

			err = h.OpenMarkdownDocument(mdPath, content)
			require.NoError(t, err, "opening fixture %s should not error", tt.file)

			// Symbols should not crash regardless of content validity.
			_, err = h.DocumentSymbols(mdPath)
			require.NoError(t, err, "symbols for fixture %s should not error", tt.file)

			if tt.hoverLine >= 0 {
				_, err = h.Hover(mdPath, tt.hoverLine, 5)
				require.NoError(t, err, "hover for fixture %s should not error", tt.file)
			}
		})
	}
}

func TestMarkdownIntegration_CompletionNilSnapshot(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	mdContent := "```yammm\nschema \"test\"\n```\n"
	mdPath := filepath.Join(tmpDir, "completion.md")
	require.NoError(t, os.WriteFile(mdPath, []byte(mdContent), 0o600))

	h, server := newTestHarnessWithServer(t, tmpDir)
	defer h.Close()

	// Open the markdown document in the workspace and inject a nil snapshot.
	uri := testutil.PathToURI(mdPath)
	server.Workspace().MarkdownDocumentOpenedForTest(uri, 1, mdContent)

	id, err := markdown.VirtualSourceID(mdPath, 0)
	require.NoError(t, err)

	server.Workspace().SetMarkdownBlocksForTest(uri, []markdown.CodeBlock{
		{
			Content:   "schema \"test\"\n",
			SourceID:  id,
			StartLine: 1,
			EndLine:   2,
			FenceChar: '`',
		},
	}, []*analysis.Snapshot{nil})

	// Request completion inside the block (line 1 = block start).
	result, err := h.Completion(mdPath, 1, 0)
	require.NoError(t, err)
	require.NotNil(t, result, "completion should return items even with nil snapshot (graceful degradation)")

	items, ok := result.([]protocol.CompletionItem)
	require.True(t, ok, "expected []CompletionItem")
	assert.NotEmpty(t, items, "expected keyword/snippet completions")
}

func TestMarkdownIntegration_DefinitionRemapsURI(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Content with a type definition and an association referencing it.
	// Line 0: "# Test"
	// Line 1: ""
	// Line 2: "```yammm"
	// Line 3: schema "test"       <- block line 0
	// Line 4:                      <- block line 1
	// Line 5: type Foo {           <- block line 2
	// Line 6:     id String primary <- block line 3
	// Line 7: }                    <- block line 4
	// Line 8:                      <- block line 5
	// Line 9: type Bar {           <- block line 6
	// Line 10:     --> OWNS (one) Foo <- block line 7
	// Line 11: }                   <- block line 8
	// Line 12: "```"
	content := "# Test\n\n```yammm\nschema \"test\"\n\ntype Foo {\n    id String primary\n}\n\ntype Bar {\n    --> OWNS (one) Foo\n}\n```\n"
	mdPath := filepath.Join(tmpDir, "definition.md")
	require.NoError(t, os.WriteFile(mdPath, []byte(content), 0o600))

	h, server := newTestHarnessWithServer(t, tmpDir)
	defer h.Close()

	// Open and analyze so definition resolution has a snapshot.
	uri := testutil.PathToURI(mdPath)
	server.Workspace().MarkdownDocumentOpenedForTest(uri, 1, content)
	server.Workspace().AnalyzeMarkdownAndPublish(t.Context(), uri)

	// Also open via harness so the jrpc2 handler finds the document.
	require.NoError(t, h.OpenMarkdownDocument(mdPath, content))
	h.Sync()

	// Go-to-definition on "Foo" in "--> OWNS (one) Foo" at line 10, char 19.
	result, err := h.Definition(mdPath, 10, 19)
	require.NoError(t, err)

	if result != nil {
		loc, ok := result.(*protocol.Location)
		if ok && loc != nil {
			// The definition URI should be the markdown URI, not the virtual path.
			assert.Contains(t, loc.URI, "definition.md",
				"definition URI should reference the markdown file")
			// The range should be in markdown coordinates (line >= 3).
			assert.GreaterOrEqual(t, int(loc.Range.Start.Line), 3,
				"definition range should be in markdown coordinates")
		}
	}
}

func TestMarkdownIntegration_DocumentSymbolsNilSnapshots(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	mdContent := "```yammm\ninvalid\n```\n\n```yammm\nalso invalid\n```\n"
	mdPath := filepath.Join(tmpDir, "nil_snap.md")
	require.NoError(t, os.WriteFile(mdPath, []byte(mdContent), 0o600))

	h, server := newTestHarnessWithServer(t, tmpDir)
	defer h.Close()

	// Open and inject blocks with nil snapshots.
	uri := testutil.PathToURI(mdPath)
	server.Workspace().MarkdownDocumentOpenedForTest(uri, 1, mdContent)
	server.Workspace().SetMarkdownBlocksForTest(uri, []markdown.CodeBlock{
		{Content: "invalid", StartLine: 1, EndLine: 2},
		{Content: "also invalid", StartLine: 5, EndLine: 6},
	}, []*analysis.Snapshot{nil, nil})

	// Also open via harness so the jrpc2 handler finds the document.
	require.NoError(t, h.OpenMarkdownDocument(mdPath, mdContent))
	h.Sync()

	// Document symbols should return nil (graceful degradation for nil snapshots).
	symbols, err := h.DocumentSymbols(mdPath)
	require.NoError(t, err)
	assert.Nil(t, symbols, "expected nil when all snapshots are nil")
}
