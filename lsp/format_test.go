package lsp

import (
	"io"
	"log/slog"
	"testing"

	"github.com/simon-lentz/yammm/lsp/internal/docstate"
	"github.com/simon-lentz/yammm/lsp/internal/lsputil"
	"github.com/simon-lentz/yammm/lsp/internal/protocol"
	"github.com/simon-lentz/yammm/lsp/internal/testutil"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleFormatting_NilDoc(t *testing.T) {
	t.Parallel()
	h := handleFormatting(&fakeResolver{docSnap: nil}, testutil.DiscardLogger())
	var result []protocol.TextEdit
	err := callHandler(t, h, &protocol.DocumentFormattingParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: "file:///test.yammm"},
	}, &result)
	require.NoError(t, err)
	assert.Nil(t, result)
}

func TestHandleFormatting_WithDoc(t *testing.T) {
	t.Parallel()

	// Use text with incorrect indentation that the formatter will fix.
	unformatted := "schema \"test\"\n\ntype Person {\n  name String\n}\n"

	h := handleFormatting(&fakeResolver{
		docSnap: &docstate.Snapshot{
			URI:     "file:///test.yammm",
			Version: 1,
			Text:    unformatted,
		},
	}, testutil.DiscardLogger())

	var result []protocol.TextEdit
	err := callHandler(t, h, &protocol.DocumentFormattingParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: "file:///test.yammm"},
	}, &result)

	require.NoError(t, err)
	require.NotEmpty(t, result, "expected at least one text edit for unformatted document")
	assert.NotEqual(t, unformatted, result[0].NewText, "formatted text should differ from input")
	assert.Contains(t, result[0].NewText, "\tname String", "formatted text should use tab indentation")
}

// TestHandleFormatting_MarkdownURI verifies that formatting returns empty edits for markdown URIs.
func TestHandleFormatting_MarkdownURI(t *testing.T) {
	t.Parallel()

	resolver := &fakeResolver{encoding: lsputil.PositionEncodingUTF16}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := handleFormatting(resolver, logger)

	params := protocol.DocumentFormattingParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: "file:///doc.md"},
	}

	var edits []protocol.TextEdit
	err := callHandler(t, h, params, &edits)
	require.NoError(t, err)
	assert.Empty(t, edits, "formatting a markdown URI should return empty edits")
}
