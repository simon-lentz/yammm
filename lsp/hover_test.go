package lsp

import (
	"testing"

	"github.com/simon-lentz/yammm/lsp/internal/protocol"
	"github.com/simon-lentz/yammm/lsp/internal/workspace"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleHover_NilUnit(t *testing.T) {
	t.Parallel()
	h := handleHover(&fakeResolver{unit: nil}, testLogger())
	var result *protocol.Hover
	err := callHandler(t, h, &protocol.HoverParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: "file:///test.yammm"},
			Position:     protocol.Position{Line: 0, Character: 0},
		},
	}, &result)
	require.NoError(t, err)
	assert.Nil(t, result)
}

func TestHandleHover_WithUnit(t *testing.T) {
	t.Parallel()

	snapshot, doc := buildTestSnapshot(t)

	h := handleHover(&fakeResolver{
		unit: &workspace.Unit{
			Snapshot:  snapshot,
			Doc:       doc,
			LocalLine: 2, // LSP 0-indexed line for "type Person {"
			LocalChar: 7, // cursor on "P" of "Person"
		},
	}, testLogger())

	var result *protocol.Hover
	err := callHandler(t, h, &protocol.HoverParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: "file:///test.yammm"},
			Position:     protocol.Position{Line: 2, Character: 7},
		},
	}, &result)

	require.NoError(t, err)
	require.NotNil(t, result, "expected non-nil hover result")
	assert.Contains(t, result.Contents.Value, "Person", "hover content should mention the type name")
	assert.NotNil(t, result.Range, "hover should include a range")
}

// TestHandleHover_CrossSchemaQualifiedRef verifies that hovering on a qualified
// type reference (region.Region) resolves through the import to the imported
// type and renders its hover documentation.
func TestHandleHover_CrossSchemaQualifiedRef(t *testing.T) {
	t.Parallel()

	snapshot, doc, _ := buildCrossSchemaSnapshot(t)

	// Entry line 4: "type Listing extends region.Region {"
	// Character 28 is on 'R' of 'Region' in the qualified reference.
	h := handleHover(&fakeResolver{
		unit: &workspace.Unit{
			Snapshot:  snapshot,
			Doc:       doc,
			LocalLine: 4,
			LocalChar: 28,
		},
	}, testLogger())

	var result *protocol.Hover
	err := callHandler(t, h, &protocol.HoverParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: doc.URI},
			Position:     protocol.Position{Line: 4, Character: 28},
		},
	}, &result)

	require.NoError(t, err)
	require.NotNil(t, result, "hover should resolve cross-schema qualified type reference")
	assert.Contains(t, result.Contents.Value, "Region",
		"hover content should mention the imported type name")
}
