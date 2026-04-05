package definition_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/simon-lentz/yammm/internal/source"
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

	src := "schema \"test\"\n\ntype Person {\n    id String primary\n    name String\n}"
	reg := source.NewRegistry()
	s, result, err := schema.LoadString(t.Context(), src, "test.yammm", schema.WithSourceRegistry(reg))
	require.NoError(t, err)
	require.True(t, result.OK(), "schema should parse without errors: %v", result)

	typ, ok := s.Type("Person")
	require.True(t, ok, "schema should contain type Person")

	nameSpan := typ.NameSpan()
	fullSpan := typ.Span()
	sourceID := s.SourceID()

	sym := &symbols.Symbol{
		Name:      "Person",
		Kind:      symbols.SymbolType,
		SourceID:  sourceID,
		Range:     fullSpan,
		Selection: nameSpan,
		Data:      typ,
	}

	snapshot := &analysis.Snapshot{
		Root:    "/test",
		Sources: reg,
	}

	loc := definition.ResolveSymbol(snapshot, sym, lsputil.PositionEncodingUTF16, identityURI)
	require.NotNil(t, loc)
	assert.NotEmpty(t, loc.URI)

	// The selection span comes from the parser. Verify that LSP range is
	// 0-indexed and matches the name span position.
	assert.Equal(t, lsputil.ToUInteger(nameSpan.Start.Line-1), loc.Range.Start.Line)
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

	src := "schema \"test\"\n\ntype Customer {\n    id String primary\n}"
	reg := source.NewRegistry()
	s, result, err := schema.LoadString(context.Background(), src, "types.yammm", schema.WithSourceRegistry(reg))
	require.NoError(t, err)
	require.True(t, result.OK(), "schema should parse without errors: %v", result)

	typ, ok := s.Type("Customer")
	require.True(t, ok, "schema should contain type Customer")

	nameSpan := typ.NameSpan()
	fullSpan := typ.Span()
	sourceID := s.SourceID()

	sym := &symbols.Symbol{
		Name:      "Customer",
		Kind:      symbols.SymbolType,
		SourceID:  sourceID,
		Range:     fullSpan,
		Selection: nameSpan,
	}

	loc := definition.SymbolToLocation(reg, sym, lsputil.PositionEncodingUTF16, identityURI)
	require.NotNil(t, loc)
	assert.NotEmpty(t, loc.URI)

	// Should use selection span converted to 0-indexed LSP coordinates.
	assert.Equal(t, lsputil.ToUInteger(nameSpan.Start.Line-1), loc.Range.Start.Line)
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
