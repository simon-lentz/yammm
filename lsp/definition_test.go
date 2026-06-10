package lsp

import (
	"path/filepath"
	"testing"

	"github.com/simon-lentz/yammm/location"

	"github.com/simon-lentz/yammm/lsp/internal/lsputil"
	"github.com/simon-lentz/yammm/lsp/internal/protocol"
	"github.com/simon-lentz/yammm/lsp/internal/testutil"
	"github.com/simon-lentz/yammm/lsp/internal/workspace"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleDefinition_NilUnit(t *testing.T) {
	t.Parallel()
	h := handleDefinition(&fakeResolver{unit: nil}, testutil.DiscardLogger())
	var result any
	err := callHandler(t, h, &protocol.DefinitionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: "file:///test.yammm"},
			Position:     protocol.Position{Line: 0, Character: 0},
		},
	}, &result)
	require.NoError(t, err)
	assert.Nil(t, result)
}

func TestHandleDefinition_WithUnit(t *testing.T) {
	t.Parallel()

	snapshot, doc := buildTestSnapshot(t)

	// LoadString produces a synthetic sourceID "string://test.yammm".
	// Map it to a client URI so the remap assertion works.
	sourceKey := snapshot.EntrySourceID.String()

	h := handleDefinition(&fakeResolver{
		unit: &workspace.Unit{
			Snapshot:  snapshot,
			Doc:       doc,
			LocalLine: 2, // "type Person {"
			LocalChar: 7, // cursor on "Person"
		},
		uriMap: map[string]string{
			sourceKey: "file:///client/test.yammm",
		},
	}, testutil.DiscardLogger())

	var result *protocol.Location
	err := callHandler(t, h, &protocol.DefinitionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: "file:///test.yammm"},
			Position:     protocol.Position{Line: 2, Character: 7},
		},
	}, &result)

	require.NoError(t, err)
	require.NotNil(t, result, "expected non-nil location")
	assert.Equal(t, "file:///client/test.yammm", result.URI, "URI should be remapped via uriMap")
}

// TestHandleDefinition_CrossSchemaImport verifies that go-to-definition on a
// qualified type reference (region.Region) navigates to the type definition
// in the imported schema file.
func TestHandleDefinition_CrossSchemaImport(t *testing.T) {
	t.Parallel()

	snapshot, doc, tmpDir := buildCrossSchemaSnapshot(t)

	foundationPath := filepath.Join(tmpDir, "foundation.yammm")
	foundationSourceID, err := location.SourceIDFromAbsolutePath(foundationPath)
	require.NoError(t, err)
	foundationURI := lsputil.PathToURI(foundationPath)

	// Entry line 4: "type Listing extends region.Region {"
	// Character 28 is on 'R' of 'Region'.
	h := handleDefinition(&fakeResolver{
		unit: &workspace.Unit{
			Snapshot:  snapshot,
			Doc:       doc,
			LocalLine: 4,
			LocalChar: 28,
		},
		uriMap: map[string]string{
			foundationSourceID.String(): foundationURI,
		},
	}, testutil.DiscardLogger())

	var result *protocol.Location
	err = callHandler(t, h, &protocol.DefinitionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: doc.URI},
			Position:     protocol.Position{Line: 4, Character: 28},
		},
	}, &result)

	require.NoError(t, err)
	require.NotNil(t, result, "definition should resolve to the imported type")
	assert.Equal(t, foundationURI, result.URI,
		"definition should point to the imported schema file")
}
