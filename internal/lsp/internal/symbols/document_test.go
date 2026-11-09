package symbols

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/simon-lentz/yammm/internal/lsp/internal/protocol"

	"github.com/simon-lentz/yammm/internal/yammmtest"
	"github.com/simon-lentz/yammm/location"

	"github.com/simon-lentz/yammm/internal/lsp/internal/lsputil"
)

// testSources creates a minimal source registry for testing symbol structure.
// UTF-16 conversion will fall back to naive rune column conversion.
func testSources() *lsputil.Texts {
	return lsputil.NewTexts()
}

func TestBuildDocumentSymbols_Empty(t *testing.T) {
	t.Parallel()

	sources := testSources()

	// Nil index
	result := BuildDocumentSymbols(nil, sources)
	assert.Nil(t, result, "expected nil for nil index")

	// Empty index
	idx := &SymbolIndex{Symbols: []Symbol{}}
	result = BuildDocumentSymbols(idx, sources)
	assert.Nil(t, result, "expected nil for empty index")
}

// TestBuildDocumentSymbols_Golden drives every tree-shaping scenario —
// nesting under schema/type parents, relation and invariant kinds, the
// synthetic "(schema)" root for parse-broken files, and the name-collision
// regression where a schema and type share a name — and pins each complete
// DocumentSymbol tree (names, kinds, details, converted ranges, children)
// as a JSON golden.
func TestBuildDocumentSymbols_Golden(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		idx  func() *SymbolIndex
	}{
		{
			name: "schema only",
			idx: func() *SymbolIndex {
				sourceID := location.NewSourceID("test://schema.yammm")
				span := location.RangeWithBytes(sourceID, 1, 1, -1, 1, 20, -1)
				return &SymbolIndex{
					Symbols: []Symbol{
						{
							Name:      "MySchema",
							Kind:      SymbolSchema,
							SourceID:  sourceID,
							Range:     span,
							Selection: span,
							Detail:    "schema \"MySchema\"",
						},
					},
				}
			},
		},
		{
			name: "type with members",
			idx: func() *SymbolIndex {
				sourceID := location.NewSourceID("test://types.yammm")
				typeSpan := location.RangeWithBytes(sourceID, 1, 1, -1, 5, 1, -1)
				propSpan := location.RangeWithBytes(sourceID, 2, 5, -1, 2, 20, -1)
				return &SymbolIndex{
					Symbols: []Symbol{
						{
							Name:      "Person",
							Kind:      SymbolType,
							SourceID:  sourceID,
							Range:     typeSpan,
							Selection: typeSpan,
							Detail:    "type Person",
						},
						{
							Name:       "name",
							Kind:       SymbolProperty,
							SourceID:   sourceID,
							Range:      propSpan,
							Selection:  propSpan,
							ParentName: "Person",
							Detail:     "name String",
						},
					},
				}
			},
		},
		{
			name: "schema with imports and types",
			idx: func() *SymbolIndex {
				sourceID := location.NewSourceID("test://full.yammm")
				schemaSpan := location.RangeWithBytes(sourceID, 1, 1, -1, 1, 20, -1)
				importSpan := location.RangeWithBytes(sourceID, 2, 1, -1, 2, 30, -1)
				typeSpan := location.RangeWithBytes(sourceID, 4, 1, -1, 10, 1, -1)
				return &SymbolIndex{
					Symbols: []Symbol{
						{
							Name:      "Main",
							Kind:      SymbolSchema,
							SourceID:  sourceID,
							Range:     schemaSpan,
							Selection: schemaSpan,
						},
						{
							Name:       "parts",
							Kind:       SymbolImport,
							SourceID:   sourceID,
							Range:      importSpan,
							Selection:  importSpan,
							ParentName: "Main",
							Detail:     "import \"./parts\" as parts",
						},
						{
							Name:       "Car",
							Kind:       SymbolType,
							SourceID:   sourceID,
							Range:      typeSpan,
							Selection:  typeSpan,
							ParentName: "Main",
							Detail:     "type Car",
						},
					},
				}
			},
		},
		{
			name: "multiple types",
			idx: func() *SymbolIndex {
				sourceID := location.NewSourceID("test://multi.yammm")
				return &SymbolIndex{
					Symbols: []Symbol{
						{
							Name:      "Person",
							Kind:      SymbolType,
							SourceID:  sourceID,
							Range:     location.RangeWithBytes(sourceID, 1, 1, -1, 5, 1, -1),
							Selection: location.RangeWithBytes(sourceID, 1, 6, -1, 1, 12, -1),
						},
						{
							Name:       "name",
							Kind:       SymbolProperty,
							SourceID:   sourceID,
							Range:      location.RangeWithBytes(sourceID, 2, 5, -1, 2, 20, -1),
							Selection:  location.RangeWithBytes(sourceID, 2, 5, -1, 2, 9, -1),
							ParentName: "Person",
						},
						{
							Name:      "Company",
							Kind:      SymbolType,
							SourceID:  sourceID,
							Range:     location.RangeWithBytes(sourceID, 7, 1, -1, 11, 1, -1),
							Selection: location.RangeWithBytes(sourceID, 7, 6, -1, 7, 13, -1),
						},
						{
							Name:       "title",
							Kind:       SymbolProperty,
							SourceID:   sourceID,
							Range:      location.RangeWithBytes(sourceID, 8, 5, -1, 8, 20, -1),
							Selection:  location.RangeWithBytes(sourceID, 8, 5, -1, 8, 10, -1),
							ParentName: "Company",
						},
					},
				}
			},
		},
		{
			name: "relations",
			idx: func() *SymbolIndex {
				sourceID := location.NewSourceID("test://relations.yammm")
				return &SymbolIndex{
					Symbols: []Symbol{
						{
							Name:      "Person",
							Kind:      SymbolType,
							SourceID:  sourceID,
							Range:     location.RangeWithBytes(sourceID, 1, 1, -1, 10, 1, -1),
							Selection: location.RangeWithBytes(sourceID, 1, 6, -1, 1, 12, -1),
						},
						{
							Name:       "EMPLOYER",
							Kind:       SymbolAssociation,
							SourceID:   sourceID,
							Range:      location.RangeWithBytes(sourceID, 2, 5, -1, 2, 30, -1),
							Selection:  location.RangeWithBytes(sourceID, 2, 9, -1, 2, 17, -1),
							ParentName: "Person",
							Detail:     "--> EMPLOYER (one) Company",
						},
						{
							Name:       "DOCUMENTS",
							Kind:       SymbolComposition,
							SourceID:   sourceID,
							Range:      location.RangeWithBytes(sourceID, 3, 5, -1, 3, 30, -1),
							Selection:  location.RangeWithBytes(sourceID, 3, 9, -1, 3, 18, -1),
							ParentName: "Person",
							Detail:     "*-> DOCUMENTS (many) Document",
						},
					},
				}
			},
		},
		{
			name: "invariants",
			idx: func() *SymbolIndex {
				sourceID := location.NewSourceID("test://invariants.yammm")
				return &SymbolIndex{
					Symbols: []Symbol{
						{
							Name:      "Person",
							Kind:      SymbolType,
							SourceID:  sourceID,
							Range:     location.RangeWithBytes(sourceID, 1, 1, -1, 5, 1, -1),
							Selection: location.RangeWithBytes(sourceID, 1, 6, -1, 1, 12, -1),
						},
						{
							Name:       "age must be positive",
							Kind:       SymbolInvariant,
							SourceID:   sourceID,
							Range:      location.RangeWithBytes(sourceID, 3, 5, -1, 3, 40, -1),
							Selection:  location.RangeWithBytes(sourceID, 3, 7, -1, 3, 27, -1),
							ParentName: "Person",
						},
					},
				}
			},
		},
		{
			name: "datatype",
			idx: func() *SymbolIndex {
				sourceID := location.NewSourceID("test://datatypes.yammm")
				return &SymbolIndex{
					Symbols: []Symbol{
						{
							Name:      "ShortName",
							Kind:      SymbolDataType,
							SourceID:  sourceID,
							Range:     location.RangeWithBytes(sourceID, 1, 1, -1, 1, 30, -1),
							Selection: location.RangeWithBytes(sourceID, 1, 6, -1, 1, 15, -1),
							Detail:    "type ShortName = String[1, 50]",
						},
					},
				}
			},
		},
		{
			name: "orphan imports synthetic schema",
			idx: func() *SymbolIndex {
				sourceID := location.NewSourceID("test://orphan.yammm")
				return &SymbolIndex{
					Symbols: []Symbol{
						{
							Name:      "parts",
							Kind:      SymbolImport,
							SourceID:  sourceID,
							Range:     location.RangeWithBytes(sourceID, 1, 1, -1, 1, 30, -1),
							Selection: location.RangeWithBytes(sourceID, 1, 20, -1, 1, 25, -1),
							Detail:    "import \"./parts\" as parts",
						},
					},
				}
			},
		},
		{
			name: "orphan imports and types synthetic schema",
			idx: func() *SymbolIndex {
				sourceID := location.NewSourceID("test://orphan-mixed.yammm")
				return &SymbolIndex{
					Symbols: []Symbol{
						{
							Name:      "parts",
							Kind:      SymbolImport,
							SourceID:  sourceID,
							Range:     location.RangeWithBytes(sourceID, 1, 1, -1, 1, 30, -1),
							Selection: location.RangeWithBytes(sourceID, 1, 20, -1, 1, 25, -1),
							Detail:    "import \"./parts\" as parts",
						},
						{
							Name:      "Car",
							Kind:      SymbolType,
							SourceID:  sourceID,
							Range:     location.RangeWithBytes(sourceID, 3, 1, -1, 6, 2, -1),
							Selection: location.RangeWithBytes(sourceID, 3, 6, -1, 3, 9, -1),
							Detail:    "type Car",
						},
					},
				}
			},
		},
		{
			name: "schema name equals type name",
			idx: func() *SymbolIndex {
				sourceID := location.NewSourceID("test://collision.yammm")
				return &SymbolIndex{
					Symbols: []Symbol{
						{
							Name:      "Person",
							Kind:      SymbolSchema,
							SourceID:  sourceID,
							Range:     location.RangeWithBytes(sourceID, 1, 1, -1, 6, 1, -1),
							Selection: location.RangeWithBytes(sourceID, 1, 8, -1, 1, 14, -1),
							Detail:    "schema \"Person\"",
						},
						{
							Name:       "Person",
							Kind:       SymbolType,
							SourceID:   sourceID,
							Range:      location.RangeWithBytes(sourceID, 2, 1, -1, 5, 1, -1),
							Selection:  location.RangeWithBytes(sourceID, 2, 6, -1, 2, 12, -1),
							ParentName: "Person", // Parent is the schema
							Detail:     "type Person",
						},
						{
							Name:       "name",
							Kind:       SymbolProperty,
							SourceID:   sourceID,
							Range:      location.RangeWithBytes(sourceID, 3, 5, -1, 3, 20, -1),
							Selection:  location.RangeWithBytes(sourceID, 3, 5, -1, 3, 9, -1),
							ParentName: "Person", // Parent is the type Person
							Detail:     "name String required",
						},
					},
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := BuildDocumentSymbols(tt.idx(), testSources())
			yammmtest.GoldenJSON(t, "document_symbols_"+strings.ReplaceAll(tt.name, " ", "_"), result)
		})
	}
}

func TestSymbolKindToLSP(t *testing.T) {
	t.Parallel()

	tests := []struct {
		kind     SymbolKind
		expected protocol.SymbolKind
	}{
		{SymbolSchema, protocol.SymbolKindModule},
		{SymbolImport, protocol.SymbolKindPackage},
		{SymbolType, protocol.SymbolKindClass},
		{SymbolDataType, protocol.SymbolKindTypeParameter},
		{SymbolProperty, protocol.SymbolKindField},
		{SymbolAssociation, protocol.SymbolKindProperty},
		{SymbolComposition, protocol.SymbolKindProperty},
		{SymbolInvariant, protocol.SymbolKindEvent},
		{SymbolKind(99), protocol.SymbolKindVariable}, // Unknown
	}

	for _, tt := range tests {
		t.Run(tt.kind.String(), func(t *testing.T) {
			t.Parallel()
			result := SymbolKindToLSP(tt.kind)
			assert.Equal(t, tt.expected, result, "SymbolKindToLSP(%v)", tt.kind)
		})
	}
}

func TestSymbolToDocumentSymbol(t *testing.T) {
	t.Parallel()

	sources := testSources()

	sourceID := location.NewSourceID("test://sym.yammm")
	fullSpan := location.RangeWithBytes(sourceID, 1, 1, -1, 5, 1, -1)
	nameSpan := location.RangeWithBytes(sourceID, 1, 6, -1, 1, 12, -1)

	sym := &Symbol{
		Name:      "Person",
		Kind:      SymbolType,
		SourceID:  sourceID,
		Range:     fullSpan,
		Selection: nameSpan,
		Detail:    "type Person",
	}

	result := SymbolToDocumentSymbol(sym, sources)

	assert.Equal(t, "Person", result.Name)

	assert.Equal(t, protocol.SymbolKindClass, result.Kind)

	require.NotNil(t, result.Detail)
	assert.Equal(t, "type Person", *result.Detail)

	// Check ranges are converted correctly (1-based to 0-based)
	assert.Equal(t, uint32(0), result.Range.Start.Line, "Range.Start.Line")
	assert.Equal(t, uint32(0), result.Range.Start.Character, "Range.Start.Character")

	assert.Equal(t, uint32(0), result.SelectionRange.Start.Line, "SelectionRange.Start.Line")
	assert.Equal(t, uint32(5), result.SelectionRange.Start.Character, "SelectionRange.Start.Character")
}

func TestSymbolToDocumentSymbol_NoDetail(t *testing.T) {
	t.Parallel()

	sources := testSources()

	sourceID := location.NewSourceID("test://sym.yammm")
	span := location.RangeWithBytes(sourceID, 1, 1, -1, 1, 10, -1)

	sym := &Symbol{
		Name:      "Test",
		Kind:      SymbolType,
		SourceID:  sourceID,
		Range:     span,
		Selection: span,
		Detail:    "", // Empty detail
	}

	result := SymbolToDocumentSymbol(sym, sources)

	// Should fall back to kind string
	require.NotNil(t, result.Detail)
	assert.Equal(t, "Type", *result.Detail)
}
