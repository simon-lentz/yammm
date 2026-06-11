package lsp

import (
	"slices"
	"testing"

	"github.com/simon-lentz/yammm/lsp/internal/protocol"
	"github.com/simon-lentz/yammm/lsp/internal/testutil"
	"github.com/simon-lentz/yammm/lsp/internal/workspace"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleCompletion_NilUnit(t *testing.T) {
	t.Parallel()
	h := handleCompletion(&fakeResolver{unit: nil}, testutil.DiscardLogger())
	var result any
	err := callHandler(t, h, &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: "file:///test.yammm"},
			Position:     protocol.Position{Line: 0, Character: 0},
		},
	}, &result)
	require.NoError(t, err)
	assert.Nil(t, result)
}

func TestHandleCompletion_WithUnit(t *testing.T) {
	t.Parallel()

	snapshot, doc := buildTestSnapshot(t)

	// Position cursor inside the type body (line 3 = "	name String")
	// where completion should offer type-body items.
	h := handleCompletion(&fakeResolver{
		unit: &workspace.Unit{
			Snapshot:  snapshot,
			Doc:       doc,
			LocalLine: 3,
			LocalChar: 0,
		},
	}, testutil.DiscardLogger())

	var result []protocol.CompletionItem
	err := callHandler(t, h, &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: "file:///test.yammm"},
			Position:     protocol.Position{Line: 3, Character: 0},
		},
	}, &result)

	require.NoError(t, err)
	require.NotNil(t, result, "expected non-nil completion items")
	assert.NotEmpty(t, result, "should return completion items inside type body")
}

// TestHandleCompletion_CrossSchemaImports verifies that completion at an extends
// position offers qualified type names from imported schemas (e.g., region.Region).
func TestHandleCompletion_CrossSchemaImports(t *testing.T) {
	t.Parallel()

	snapshot, doc, _ := buildCrossSchemaSnapshot(t)

	// Entry line 4: "type Listing extends region.Region {"
	// Character 21 is right after "extends " where qualified type completions
	// should be offered. DetectContext sees "type Listing extends" → Extends context.
	h := handleCompletion(&fakeResolver{
		unit: &workspace.Unit{
			Snapshot:  snapshot,
			Doc:       doc,
			LocalLine: 4,
			LocalChar: 21,
		},
	}, testutil.DiscardLogger())

	var result []protocol.CompletionItem
	err := callHandler(t, h, &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: doc.URI},
			Position:     protocol.Position{Line: 4, Character: 21},
		},
	}, &result)

	require.NoError(t, err)
	require.NotNil(t, result, "expected completion items at extends position")

	// Search for a qualified type name from the import.
	hasQualifiedType := slices.ContainsFunc(result, func(item protocol.CompletionItem) bool {
		return item.Label == "region.Region"
	})
	assert.True(t, hasQualifiedType,
		"completion should offer 'region.Region' from imported schema; got labels: %v",
		completionLabels(result))
}
