package definition_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"

	"github.com/simon-lentz/yammm/lsp/internal/analysis"
	"github.com/simon-lentz/yammm/lsp/internal/definition"
	"github.com/simon-lentz/yammm/lsp/internal/lsputil"
	"github.com/simon-lentz/yammm/lsp/internal/symbols"
)

// identityURI is a trivial mapper that returns the input as a file:// URI.
func identityURI(path string) string {
	return lsputil.PathToURI(path)
}

func TestResolveSymbol_LocalType(t *testing.T) {
	t.Parallel()

	sourceID := location.MustNewSourceID("test://schema.yammm")
	span := location.Range(sourceID, 5, 1, 10, 1)
	selectionSpan := location.Range(sourceID, 5, 6, 5, 12)

	typ := schema.InternalNewType("Person", sourceID, span, "A person type", false, false)

	sym := &symbols.Symbol{
		Name:      "Person",
		Kind:      symbols.SymbolType,
		SourceID:  sourceID,
		Range:     span,
		Selection: selectionSpan,
		Data:      typ,
	}

	// Snapshot with nil Sources triggers the fallback path in SymbolToLocation
	snapshot := &analysis.Snapshot{
		Root:    "/test",
		Sources: nil, // Will use fallback path
	}

	loc := definition.ResolveSymbol(snapshot, sym, lsputil.PositionEncodingUTF16, identityURI)
	require.NotNil(t, loc)
	assert.NotEmpty(t, loc.URI)

	// Range should be from the selection span (0-indexed: line 5 -> 4)
	assert.Equal(t, uint32(4), loc.Range.Start.Line)
	assert.Equal(t, uint32(5), loc.Range.Start.Character)
}

func TestResolveSymbol_ImportWithoutSchema(t *testing.T) {
	t.Parallel()

	mainSourceID := location.MustNewSourceID("test://main.yammm")
	importedSourceID := location.MustNewSourceID("test://missing.yammm")
	importSpan := location.Range(mainSourceID, 1, 1, 1, 30)

	// Create an import WITHOUT a resolved schema (unresolved import)
	imp := schema.InternalNewImport("./missing", "missing", importedSourceID, importSpan)
	// Don't call imp.SetSchema() - simulates unresolved import

	sym := &symbols.Symbol{
		Name:     "missing",
		Kind:     symbols.SymbolImport,
		SourceID: mainSourceID,
		Range:    importSpan,
		Data:     imp,
	}

	snapshot := &analysis.Snapshot{
		Root: "/test",
	}

	loc := definition.ResolveSymbol(snapshot, sym, lsputil.PositionEncodingUTF16, identityURI)
	assert.Nil(t, loc, "expected nil result for unresolved import")
}

func TestResolveReference_NotFound(t *testing.T) {
	t.Parallel()

	sourceID := location.MustNewSourceID("test://schema.yammm")

	// Create empty snapshot - ResolveTypeReference will return nil
	snapshot := &analysis.Snapshot{
		Root:            "/test",
		SymbolsBySource: map[location.SourceID]*symbols.SymbolIndex{},
	}

	// Reference to a non-existent type
	ref := &symbols.ReferenceSymbol{
		TargetName: "NonExistent",
		Qualifier:  "",
		Span:       location.Range(sourceID, 5, 10, 5, 20),
	}

	loc := definition.ResolveReference(snapshot, ref, sourceID, lsputil.PositionEncodingUTF16, identityURI)
	assert.Nil(t, loc, "expected nil result for unresolved reference")
}

func TestSymbolToLocation(t *testing.T) {
	t.Parallel()

	sourceID := location.MustNewSourceID("test://types.yammm")
	selectionSpan := location.Range(sourceID, 3, 6, 3, 12)
	fullSpan := location.Range(sourceID, 3, 1, 8, 1)

	sym := &symbols.Symbol{
		Name:      "Customer",
		Kind:      symbols.SymbolType,
		SourceID:  sourceID,
		Range:     fullSpan,
		Selection: selectionSpan,
	}

	// Snapshot with nil Sources to test fallback path
	snapshot := &analysis.Snapshot{
		Root:    "/test",
		Sources: nil,
	}

	loc := definition.SymbolToLocation(snapshot.Sources, sym, lsputil.PositionEncodingUTF16, identityURI)
	require.NotNil(t, loc)
	assert.NotEmpty(t, loc.URI)

	// Should use selection span (0-indexed from 1-indexed)
	assert.Equal(t, uint32(2), loc.Range.Start.Line)
	assert.Equal(t, uint32(5), loc.Range.Start.Character)
}

func TestSymbolToLocation_NilSymbol(t *testing.T) {
	t.Parallel()

	loc := definition.SymbolToLocation(nil, nil, lsputil.PositionEncodingUTF16, identityURI)
	assert.Nil(t, loc, "expected nil location for nil symbol")
}

func TestSymbolToLocation_ZeroRange(t *testing.T) {
	t.Parallel()

	sourceID := location.MustNewSourceID("test://types.yammm")

	sym := &symbols.Symbol{
		Name:     "NoRange",
		Kind:     symbols.SymbolType,
		SourceID: sourceID,
		Range:    location.Span{}, // zero span
	}

	loc := definition.SymbolToLocation(nil, sym, lsputil.PositionEncodingUTF16, identityURI)
	assert.Nil(t, loc, "expected nil location for symbol with zero range")
}
