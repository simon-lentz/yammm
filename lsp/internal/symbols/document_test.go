package symbols

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/simon-lentz/yammm/lsp/internal/protocol"

	"github.com/simon-lentz/yammm/internal/source"
	"github.com/simon-lentz/yammm/location"

	"github.com/simon-lentz/yammm/lsp/internal/lsputil"
)

// testSources creates a minimal source registry for testing symbol structure.
// UTF-16 conversion will fall back to naive rune column conversion.
func testSources() *source.Registry {
	return source.NewRegistry()
}

// testEncoding returns the default position encoding used in tests.
func testEncoding() lsputil.PositionEncoding {
	return lsputil.PositionEncodingUTF16
}

func TestBuildDocumentSymbols_Empty(t *testing.T) {
	t.Parallel()

	sources := testSources()
	enc := testEncoding()

	// Nil index
	result := BuildDocumentSymbols(nil, sources, enc)
	assert.Nil(t, result, "expected nil for nil index")

	// Empty index
	idx := &SymbolIndex{Symbols: []Symbol{}}
	result = BuildDocumentSymbols(idx, sources, enc)
	assert.Nil(t, result, "expected nil for empty index")
}

func TestBuildDocumentSymbols_SchemaOnly(t *testing.T) {
	t.Parallel()

	sources := testSources()
	enc := testEncoding()

	sourceID := location.MustNewSourceID("test://schema.yammm")
	span := location.Range(sourceID, 1, 1, 1, 20)

	idx := &SymbolIndex{
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

	result := BuildDocumentSymbols(idx, sources, enc)

	require.Len(t, result, 1, "expected 1 symbol")

	assert.Equal(t, "MySchema", result[0].Name)

	assert.Equal(t, protocol.SymbolKindModule, result[0].Kind)
}

func TestBuildDocumentSymbols_TypeWithMembers(t *testing.T) {
	t.Parallel()

	sources := testSources()
	enc := testEncoding()

	sourceID := location.MustNewSourceID("test://types.yammm")
	typeSpan := location.Range(sourceID, 1, 1, 5, 1)
	propSpan := location.Range(sourceID, 2, 5, 2, 20)

	idx := &SymbolIndex{
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

	result := BuildDocumentSymbols(idx, sources, enc)

	require.Len(t, result, 1, "expected 1 top-level symbol")

	typeSym := result[0]
	assert.Equal(t, "Person", typeSym.Name)

	assert.Equal(t, protocol.SymbolKindClass, typeSym.Kind)

	require.Len(t, typeSym.Children, 1, "expected 1 child")

	propSym := typeSym.Children[0]
	assert.Equal(t, "name", propSym.Name)

	assert.Equal(t, protocol.SymbolKindField, propSym.Kind)
}

func TestBuildDocumentSymbols_SchemaWithImportsAndTypes(t *testing.T) {
	t.Parallel()

	sources := testSources()
	enc := testEncoding()

	sourceID := location.MustNewSourceID("test://full.yammm")
	schemaSpan := location.Range(sourceID, 1, 1, 1, 20)
	importSpan := location.Range(sourceID, 2, 1, 2, 30)
	typeSpan := location.Range(sourceID, 4, 1, 10, 1)

	idx := &SymbolIndex{
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

	result := BuildDocumentSymbols(idx, sources, enc)

	// Schema should be top-level with imports and types as children
	require.Len(t, result, 1, "expected 1 top-level symbol (schema)")

	schemaSym := result[0]
	assert.Equal(t, "Main", schemaSym.Name)

	// Schema should have 2 children: import and type
	require.Len(t, schemaSym.Children, 2, "expected 2 children")

	// Check import
	importFound := false
	typeFound := false
	for _, child := range schemaSym.Children {
		if child.Name == "parts" && child.Kind == protocol.SymbolKindPackage {
			importFound = true
		}
		if child.Name == "Car" && child.Kind == protocol.SymbolKindClass {
			typeFound = true
		}
	}

	assert.True(t, importFound, "import 'parts' not found in children")
	assert.True(t, typeFound, "type 'Car' not found in children")
}

func TestBuildDocumentSymbols_MultipleTypes(t *testing.T) {
	t.Parallel()

	sources := testSources()
	enc := testEncoding()

	sourceID := location.MustNewSourceID("test://multi.yammm")

	idx := &SymbolIndex{
		Symbols: []Symbol{
			{
				Name:      "Person",
				Kind:      SymbolType,
				SourceID:  sourceID,
				Range:     location.Range(sourceID, 1, 1, 5, 1),
				Selection: location.Range(sourceID, 1, 6, 1, 12),
			},
			{
				Name:       "name",
				Kind:       SymbolProperty,
				SourceID:   sourceID,
				Range:      location.Range(sourceID, 2, 5, 2, 20),
				Selection:  location.Range(sourceID, 2, 5, 2, 9),
				ParentName: "Person",
			},
			{
				Name:      "Company",
				Kind:      SymbolType,
				SourceID:  sourceID,
				Range:     location.Range(sourceID, 7, 1, 11, 1),
				Selection: location.Range(sourceID, 7, 6, 7, 13),
			},
			{
				Name:       "title",
				Kind:       SymbolProperty,
				SourceID:   sourceID,
				Range:      location.Range(sourceID, 8, 5, 8, 20),
				Selection:  location.Range(sourceID, 8, 5, 8, 10),
				ParentName: "Company",
			},
		},
	}

	result := BuildDocumentSymbols(idx, sources, enc)

	// Should have 2 top-level types
	require.Len(t, result, 2, "expected 2 top-level symbols")

	// Each type should have its own child
	for _, sym := range result {
		assert.Len(t, sym.Children, 1, "type %q should have 1 child", sym.Name)
	}
}

func TestBuildDocumentSymbols_Relations(t *testing.T) {
	t.Parallel()

	sources := testSources()
	enc := testEncoding()

	sourceID := location.MustNewSourceID("test://relations.yammm")

	idx := &SymbolIndex{
		Symbols: []Symbol{
			{
				Name:      "Person",
				Kind:      SymbolType,
				SourceID:  sourceID,
				Range:     location.Range(sourceID, 1, 1, 10, 1),
				Selection: location.Range(sourceID, 1, 6, 1, 12),
			},
			{
				Name:       "EMPLOYER",
				Kind:       SymbolAssociation,
				SourceID:   sourceID,
				Range:      location.Range(sourceID, 2, 5, 2, 30),
				Selection:  location.Range(sourceID, 2, 9, 2, 17),
				ParentName: "Person",
				Detail:     "--> EMPLOYER (one) Company",
			},
			{
				Name:       "DOCUMENTS",
				Kind:       SymbolComposition,
				SourceID:   sourceID,
				Range:      location.Range(sourceID, 3, 5, 3, 30),
				Selection:  location.Range(sourceID, 3, 9, 3, 18),
				ParentName: "Person",
				Detail:     "*-> DOCUMENTS (many) Document",
			},
		},
	}

	result := BuildDocumentSymbols(idx, sources, enc)

	require.Len(t, result, 1, "expected 1 top-level symbol")

	typeSym := result[0]
	require.Len(t, typeSym.Children, 2, "expected 2 children (relations)")

	// Both should be Property kind
	for _, child := range typeSym.Children {
		assert.Equal(t, protocol.SymbolKindProperty, child.Kind, "relation %q should have Property kind", child.Name)
	}
}

func TestBuildDocumentSymbols_Invariants(t *testing.T) {
	t.Parallel()

	sources := testSources()
	enc := testEncoding()

	sourceID := location.MustNewSourceID("test://invariants.yammm")

	idx := &SymbolIndex{
		Symbols: []Symbol{
			{
				Name:      "Person",
				Kind:      SymbolType,
				SourceID:  sourceID,
				Range:     location.Range(sourceID, 1, 1, 5, 1),
				Selection: location.Range(sourceID, 1, 6, 1, 12),
			},
			{
				Name:       "age must be positive",
				Kind:       SymbolInvariant,
				SourceID:   sourceID,
				Range:      location.Range(sourceID, 3, 5, 3, 40),
				Selection:  location.Range(sourceID, 3, 7, 3, 27),
				ParentName: "Person",
			},
		},
	}

	result := BuildDocumentSymbols(idx, sources, enc)

	require.Len(t, result, 1, "expected 1 top-level symbol")

	typeSym := result[0]
	require.Len(t, typeSym.Children, 1, "expected 1 child (invariant)")

	invSym := typeSym.Children[0]
	assert.Equal(t, protocol.SymbolKindEvent, invSym.Kind, "invariant should have Event kind")
}

func TestBuildDocumentSymbols_DataType(t *testing.T) {
	t.Parallel()

	sources := testSources()
	enc := testEncoding()

	sourceID := location.MustNewSourceID("test://datatypes.yammm")

	idx := &SymbolIndex{
		Symbols: []Symbol{
			{
				Name:      "ShortName",
				Kind:      SymbolDataType,
				SourceID:  sourceID,
				Range:     location.Range(sourceID, 1, 1, 1, 30),
				Selection: location.Range(sourceID, 1, 6, 1, 15),
				Detail:    "type ShortName = String[1, 50]",
			},
		},
	}

	result := BuildDocumentSymbols(idx, sources, enc)

	require.Len(t, result, 1, "expected 1 symbol")

	dtSym := result[0]
	assert.Equal(t, protocol.SymbolKindTypeParameter, dtSym.Kind, "datatype should have TypeParameter kind")
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
	enc := testEncoding()

	sourceID := location.MustNewSourceID("test://sym.yammm")
	fullSpan := location.Range(sourceID, 1, 1, 5, 1)
	nameSpan := location.Range(sourceID, 1, 6, 1, 12)

	sym := &Symbol{
		Name:      "Person",
		Kind:      SymbolType,
		SourceID:  sourceID,
		Range:     fullSpan,
		Selection: nameSpan,
		Detail:    "type Person",
	}

	result := SymbolToDocumentSymbol(sym, sources, enc)

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
	enc := testEncoding()

	sourceID := location.MustNewSourceID("test://sym.yammm")
	span := location.Range(sourceID, 1, 1, 1, 10)

	sym := &Symbol{
		Name:      "Test",
		Kind:      SymbolType,
		SourceID:  sourceID,
		Range:     span,
		Selection: span,
		Detail:    "", // Empty detail
	}

	result := SymbolToDocumentSymbol(sym, sources, enc)

	// Should fall back to kind string
	require.NotNil(t, result.Detail)
	assert.Equal(t, "Type", *result.Detail)
}

func TestBuildDocumentSymbols_OrphanImports_SyntheticSchema(t *testing.T) {
	t.Parallel()

	sources := testSources()
	enc := testEncoding()

	sourceID := location.MustNewSourceID("test://orphan.yammm")

	// Simulate a broken file with imports but no schema declaration
	idx := &SymbolIndex{
		Symbols: []Symbol{
			{
				Name:      "parts",
				Kind:      SymbolImport,
				SourceID:  sourceID,
				Range:     location.Range(sourceID, 1, 1, 1, 30),
				Selection: location.Range(sourceID, 1, 20, 1, 25),
				Detail:    "import \"./parts\" as parts",
			},
		},
	}

	result := BuildDocumentSymbols(idx, sources, enc)

	// Should have a synthetic schema root containing the import
	require.Len(t, result, 1, "expected 1 top-level symbol (synthetic schema)")

	schemaSym := result[0]
	assert.Equal(t, "(schema)", schemaSym.Name)

	assert.Equal(t, protocol.SymbolKindModule, schemaSym.Kind)

	require.NotNil(t, schemaSym.Detail)
	assert.Equal(t, "parse error", *schemaSym.Detail)

	// Import should be nested under the synthetic schema
	require.Len(t, schemaSym.Children, 1, "expected 1 child (import)")

	importSym := schemaSym.Children[0]
	assert.Equal(t, "parts", importSym.Name)

	assert.Equal(t, protocol.SymbolKindPackage, importSym.Kind)
}

func TestBuildDocumentSymbols_OrphanImportsAndTypes_SyntheticSchema(t *testing.T) {
	t.Parallel()

	sources := testSources()
	enc := testEncoding()

	sourceID := location.MustNewSourceID("test://orphan-mixed.yammm")

	// Simulate a broken file with both imports and types but no schema declaration
	idx := &SymbolIndex{
		Symbols: []Symbol{
			{
				Name:      "parts",
				Kind:      SymbolImport,
				SourceID:  sourceID,
				Range:     location.Range(sourceID, 1, 1, 1, 30),
				Selection: location.Range(sourceID, 1, 20, 1, 25),
				Detail:    "import \"./parts\" as parts",
			},
			{
				Name:      "Car",
				Kind:      SymbolType,
				SourceID:  sourceID,
				Range:     location.Range(sourceID, 3, 1, 6, 2),
				Selection: location.Range(sourceID, 3, 6, 3, 9),
				Detail:    "type Car",
			},
		},
	}

	result := BuildDocumentSymbols(idx, sources, enc)

	// Should have a synthetic schema root containing both import and type
	require.Len(t, result, 1, "expected 1 top-level symbol (synthetic schema)")

	schemaSym := result[0]
	assert.Equal(t, "(schema)", schemaSym.Name)

	// Both import and type should be children of synthetic schema
	require.Len(t, schemaSym.Children, 2, "expected 2 children (import + type)")

	// Verify both children exist (order may vary)
	childNames := make(map[string]bool)
	for _, child := range schemaSym.Children {
		childNames[child.Name] = true
	}

	assert.True(t, childNames["parts"], "expected 'parts' import as child of synthetic schema")
	assert.True(t, childNames["Car"], "expected 'Car' type as child of synthetic schema")
}

// TestBuildDocumentSymbols_SchemaNameEqualsTypeName is a regression test for issue 2.8.
// It verifies that when a schema and type share the same name, both symbols appear
// in the output with correct kinds and proper parent/child relationships.
// This test guards against infinite recursion and outline corruption from name collisions.
func TestBuildDocumentSymbols_SchemaNameEqualsTypeName(t *testing.T) {
	t.Parallel()

	sources := testSources()
	enc := testEncoding()

	sourceID := location.MustNewSourceID("test://collision.yammm")

	// Schema and Type both named "Person"
	idx := &SymbolIndex{
		Symbols: []Symbol{
			{
				Name:      "Person",
				Kind:      SymbolSchema,
				SourceID:  sourceID,
				Range:     location.Range(sourceID, 1, 1, 6, 1),
				Selection: location.Range(sourceID, 1, 8, 1, 14),
				Detail:    "schema \"Person\"",
			},
			{
				Name:       "Person",
				Kind:       SymbolType,
				SourceID:   sourceID,
				Range:      location.Range(sourceID, 2, 1, 5, 1),
				Selection:  location.Range(sourceID, 2, 6, 2, 12),
				ParentName: "Person", // Parent is the schema
				Detail:     "type Person",
			},
			{
				Name:       "name",
				Kind:       SymbolProperty,
				SourceID:   sourceID,
				Range:      location.Range(sourceID, 3, 5, 3, 20),
				Selection:  location.Range(sourceID, 3, 5, 3, 9),
				ParentName: "Person", // Parent is the type Person
				Detail:     "name String required",
			},
		},
	}

	result := BuildDocumentSymbols(idx, sources, enc)

	// Should complete without infinite recursion
	// Schema should be top-level
	require.Len(t, result, 1, "expected 1 top-level symbol (schema)")

	schemaSym := result[0]
	assert.Equal(t, "Person", schemaSym.Name)
	assert.Equal(t, protocol.SymbolKindModule, schemaSym.Kind)

	// Schema should have the type Person as child
	require.Len(t, schemaSym.Children, 1, "expected 1 schema child (type)")

	typeSym := schemaSym.Children[0]
	assert.Equal(t, "Person", typeSym.Name)
	assert.Equal(t, protocol.SymbolKindClass, typeSym.Kind)

	// Type should have the property as child
	require.Len(t, typeSym.Children, 1, "expected 1 type child (property)")

	propSym := typeSym.Children[0]
	assert.Equal(t, "name", propSym.Name)
	assert.Equal(t, protocol.SymbolKindField, propSym.Kind)
}
