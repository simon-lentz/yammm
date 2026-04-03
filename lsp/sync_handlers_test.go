package lsp

import (
	"io"
	"log/slog"
	"testing"

	"github.com/simon-lentz/yammm/lsp/internal/protocol"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleDidOpen_CallsOpenDocument(t *testing.T) {
	t.Parallel()
	dm := &fakeDocManager{}
	h := handleDidOpen(dm, testLogger())
	err := callHandler(t, h, &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{
			URI:     "file:///test.yammm",
			Version: 1,
			Text:    "schema \"test\"",
		},
	}, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"file:///test.yammm"}, dm.openCalls)
}

func TestHandleDidChange_ExtractsFullSyncText(t *testing.T) {
	t.Parallel()
	dm := &fakeDocManager{}
	h := handleDidChange(dm, testLogger())
	err := callHandler(t, h, &protocol.DidChangeTextDocumentParams{
		TextDocument: protocol.VersionedTextDocumentIdentifier{
			TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: "file:///test.yammm"},
			Version:                2,
		},
		ContentChanges: []protocol.ContentChange{{Text: "new content"}},
	}, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"file:///test.yammm"}, dm.changeCalls)
}

func TestHandleDidClose_CallsCloseDocument(t *testing.T) {
	t.Parallel()
	dm := &fakeDocManager{}
	h := handleDidClose(dm, testLogger())
	err := callHandler(t, h, &protocol.DidCloseTextDocumentParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: "file:///test.yammm"},
	}, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"file:///test.yammm"}, dm.closeCalls)
}

// TestHandleDidChange_IncrementalIgnored verifies that non-full-sync (incremental)
// changes are ignored and do not call ChangeDocument.
func TestHandleDidChange_IncrementalIgnored(t *testing.T) {
	t.Parallel()

	dm := &fakeDocManager{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := handleDidChange(dm, logger)

	// Incremental change (has Range set) should be ignored
	changeRange := protocol.Range{
		Start: protocol.Position{Line: 0, Character: 0},
		End:   protocol.Position{Line: 0, Character: 5},
	}
	params := protocol.DidChangeTextDocumentParams{
		TextDocument: protocol.VersionedTextDocumentIdentifier{
			TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: "file:///test.yammm"},
			Version:                2,
		},
		ContentChanges: []protocol.ContentChange{
			{Range: &changeRange, Text: "hello"},
		},
	}

	err := callHandler(t, h, params, nil)
	require.NoError(t, err)
	assert.Empty(t, dm.changeCalls, "ChangeDocument should not be called for incremental changes")
}
