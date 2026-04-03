package lsp

import (
	"testing"

	"github.com/simon-lentz/yammm/lsp/internal/protocol"
	"github.com/simon-lentz/yammm/lsp/internal/workspace"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleDocumentSymbol_NilUnits(t *testing.T) {
	t.Parallel()
	h := handleDocumentSymbol(&fakeResolver{units: nil}, testLogger())
	var result any
	err := callHandler(t, h, &protocol.DocumentSymbolParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: "file:///test.yammm"},
	}, &result)
	require.NoError(t, err)
	assert.Nil(t, result)
}

func TestHandleDocumentSymbol_WithUnits(t *testing.T) {
	t.Parallel()

	snapshot, doc := buildTestSnapshot(t)

	h := handleDocumentSymbol(&fakeResolver{
		units: []workspace.Unit{
			{
				Snapshot: snapshot,
				Doc:      doc,
			},
		},
	}, testLogger())

	var result []protocol.DocumentSymbol
	err := callHandler(t, h, &protocol.DocumentSymbolParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: "file:///test.yammm"},
	}, &result)

	require.NoError(t, err)
	require.NotEmpty(t, result, "expected document symbols")

	// Person is nested under the schema symbol as a child.
	// Search recursively.
	var findSymbol func(syms []protocol.DocumentSymbol, name string) bool
	findSymbol = func(syms []protocol.DocumentSymbol, name string) bool {
		for _, sym := range syms {
			if sym.Name == name {
				return true
			}
			if findSymbol(sym.Children, name) {
				return true
			}
		}
		return false
	}
	assert.True(t, findSymbol(result, "test"), "should contain schema symbol 'test'")
	assert.True(t, findSymbol(result, "Person"), "should contain type symbol 'Person'")
}
