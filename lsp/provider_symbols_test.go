package lsp

import (
	"testing"

	protocol "github.com/tliron/glsp/protocol_3_16"

	"github.com/simon-lentz/yammm/internal/source"
	"github.com/simon-lentz/yammm/location"
)

// emptySnapshot creates a minimal snapshot for testing symbol structure.
// UTF-16 conversion will fall back to naive rune column conversion.
func emptySnapshot() *Snapshot {
	return &Snapshot{
		Sources: source.NewRegistry(),
	}
}

// testServer creates a Server with a minimal workspace for testing.
func testServer() *Server {
	return &Server{
		workspace: NewWorkspace(nil, Config{}),
	}
}

func TestBuildDocumentSymbols_Empty(t *testing.T) {
	t.Parallel()

	s := testServer()
	snap := emptySnapshot()

	// Nil index
	result := s.buildDocumentSymbols(nil, snap)
	if result != nil {
		t.Error("expected nil for nil index")
	}

	// Empty index
	idx := &SymbolIndex{Symbols: []Symbol{}}
	result = s.buildDocumentSymbols(idx, snap)
	if result != nil {
		t.Error("expected nil for empty index")
	}
}

func TestBuildDocumentSymbols_SchemaOnly(t *testing.T) {
	t.Parallel()

	s := testServer()
	snap := emptySnapshot()

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

	result := s.buildDocumentSymbols(idx, snap)

	if len(result) != 1 {
		t.Fatalf("expected 1 symbol, got %d", len(result))
	}

	if result[0].Name != "MySchema" {
		t.Errorf("Name = %q; want 'MySchema'", result[0].Name)
	}

	if result[0].Kind != protocol.SymbolKindModule {
		t.Errorf("Kind = %v; want Module", result[0].Kind)
	}
}

func TestBuildDocumentSymbols_TypeWithMembers(t *testing.T) {
	t.Parallel()

	s := testServer()
	snap := emptySnapshot()

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

	result := s.buildDocumentSymbols(idx, snap)

	if len(result) != 1 {
		t.Fatalf("expected 1 top-level symbol, got %d", len(result))
	}

	typeSym := result[0]
	if typeSym.Name != "Person" {
		t.Errorf("Name = %q; want 'Person'", typeSym.Name)
	}

	if typeSym.Kind != protocol.SymbolKindClass {
		t.Errorf("Kind = %v; want Class", typeSym.Kind)
	}

	if len(typeSym.Children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(typeSym.Children))
	}

	propSym := typeSym.Children[0]
	if propSym.Name != "name" {
		t.Errorf("Child Name = %q; want 'name'", propSym.Name)
	}

	if propSym.Kind != protocol.SymbolKindField {
		t.Errorf("Child Kind = %v; want Field", propSym.Kind)
	}
}

func TestBuildDocumentSymbols_SchemaWithImportsAndTypes(t *testing.T) {
	t.Parallel()

	s := testServer()
	snap := emptySnapshot()

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

	result := s.buildDocumentSymbols(idx, snap)

	// Schema should be top-level with imports and types as children
	if len(result) != 1 {
		t.Fatalf("expected 1 top-level symbol (schema), got %d", len(result))
	}

	schemaSym := result[0]
	if schemaSym.Name != "Main" {
		t.Errorf("Schema Name = %q; want 'Main'", schemaSym.Name)
	}

	// Schema should have 2 children: import and type
	if len(schemaSym.Children) != 2 {
		t.Fatalf("expected 2 children, got %d", len(schemaSym.Children))
	}

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

	if !importFound {
		t.Error("import 'parts' not found in children")
	}
	if !typeFound {
		t.Error("type 'Car' not found in children")
	}
}

func TestBuildDocumentSymbols_MultipleTypes(t *testing.T) {
	t.Parallel()

	s := testServer()
	snap := emptySnapshot()

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

	result := s.buildDocumentSymbols(idx, snap)

	// Should have 2 top-level types
	if len(result) != 2 {
		t.Fatalf("expected 2 top-level symbols, got %d", len(result))
	}

	// Each type should have its own child
	for _, sym := range result {
		if len(sym.Children) != 1 {
			t.Errorf("type %q should have 1 child, got %d", sym.Name, len(sym.Children))
		}
	}
}

func TestBuildDocumentSymbols_Relations(t *testing.T) {
	t.Parallel()

	s := testServer()
	snap := emptySnapshot()

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

	result := s.buildDocumentSymbols(idx, snap)

	if len(result) != 1 {
		t.Fatalf("expected 1 top-level symbol, got %d", len(result))
	}

	typeSym := result[0]
	if len(typeSym.Children) != 2 {
		t.Fatalf("expected 2 children (relations), got %d", len(typeSym.Children))
	}

	// Both should be Property kind
	for _, child := range typeSym.Children {
		if child.Kind != protocol.SymbolKindProperty {
			t.Errorf("relation %q should have Property kind, got %v", child.Name, child.Kind)
		}
	}
}

func TestBuildDocumentSymbols_Invariants(t *testing.T) {
	t.Parallel()

	s := testServer()
	snap := emptySnapshot()

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

	result := s.buildDocumentSymbols(idx, snap)

	if len(result) != 1 {
		t.Fatalf("expected 1 top-level symbol, got %d", len(result))
	}

	typeSym := result[0]
	if len(typeSym.Children) != 1 {
		t.Fatalf("expected 1 child (invariant), got %d", len(typeSym.Children))
	}

	invSym := typeSym.Children[0]
	if invSym.Kind != protocol.SymbolKindEvent {
		t.Errorf("invariant should have Event kind, got %v", invSym.Kind)
	}
}

func TestBuildDocumentSymbols_DataType(t *testing.T) {
	t.Parallel()

	s := testServer()
	snap := emptySnapshot()

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

	result := s.buildDocumentSymbols(idx, snap)

	if len(result) != 1 {
		t.Fatalf("expected 1 symbol, got %d", len(result))
	}

	dtSym := result[0]
	if dtSym.Kind != protocol.SymbolKindTypeParameter {
		t.Errorf("datatype should have TypeParameter kind, got %v", dtSym.Kind)
	}
}

func TestSymbolKindToLSP(t *testing.T) {
	t.Parallel()

	s := testServer()

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
			result := s.symbolKindToLSP(tt.kind)
			if result != tt.expected {
				t.Errorf("symbolKindToLSP(%v) = %v; want %v", tt.kind, result, tt.expected)
			}
		})
	}
}

func TestSymbolToDocumentSymbol(t *testing.T) {
	t.Parallel()

	s := testServer()
	snap := emptySnapshot()

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

	result := s.symbolToDocumentSymbol(sym, snap)

	if result.Name != "Person" {
		t.Errorf("Name = %q; want 'Person'", result.Name)
	}

	if result.Kind != protocol.SymbolKindClass {
		t.Errorf("Kind = %v; want Class", result.Kind)
	}

	if result.Detail == nil || *result.Detail != "type Person" {
		t.Error("Detail mismatch")
	}

	// Check ranges are converted correctly (1-based to 0-based)
	if result.Range.Start.Line != 0 || result.Range.Start.Character != 0 {
		t.Errorf("Range.Start = (%d, %d); want (0, 0)",
			result.Range.Start.Line, result.Range.Start.Character)
	}

	if result.SelectionRange.Start.Line != 0 || result.SelectionRange.Start.Character != 5 {
		t.Errorf("SelectionRange.Start = (%d, %d); want (0, 5)",
			result.SelectionRange.Start.Line, result.SelectionRange.Start.Character)
	}
}

func TestSymbolToDocumentSymbol_NoDetail(t *testing.T) {
	t.Parallel()

	s := testServer()
	snap := emptySnapshot()

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

	result := s.symbolToDocumentSymbol(sym, snap)

	// Should fall back to kind string
	if result.Detail == nil || *result.Detail != "Type" {
		t.Errorf("Detail = %v; want 'Type'", result.Detail)
	}
}

func TestBuildDocumentSymbols_OrphanImports_SyntheticSchema(t *testing.T) {
	t.Parallel()

	s := testServer()
	snap := emptySnapshot()

	sourceID := location.MustNewSourceID("test://orphan.yammm")

	// Simulate a broken file with imports but no schema declaration
	// (e.g., severe parse failure that only extracted the import)
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

	result := s.buildDocumentSymbols(idx, snap)

	// Should have a synthetic schema root containing the import
	if len(result) != 1 {
		t.Fatalf("expected 1 top-level symbol (synthetic schema), got %d", len(result))
	}

	schemaSym := result[0]
	if schemaSym.Name != "(schema)" {
		t.Errorf("Name = %q; want '(schema)'", schemaSym.Name)
	}

	if schemaSym.Kind != protocol.SymbolKindModule {
		t.Errorf("Kind = %v; want Module", schemaSym.Kind)
	}

	if schemaSym.Detail == nil || *schemaSym.Detail != "parse error" {
		t.Errorf("Detail = %v; want 'parse error'", schemaSym.Detail)
	}

	// Import should be nested under the synthetic schema
	if len(schemaSym.Children) != 1 {
		t.Fatalf("expected 1 child (import), got %d", len(schemaSym.Children))
	}

	importSym := schemaSym.Children[0]
	if importSym.Name != "parts" {
		t.Errorf("Child Name = %q; want 'parts'", importSym.Name)
	}

	if importSym.Kind != protocol.SymbolKindPackage {
		t.Errorf("Child Kind = %v; want Package", importSym.Kind)
	}
}

func TestBuildDocumentSymbols_OrphanImportsAndTypes_SyntheticSchema(t *testing.T) {
	t.Parallel()

	s := testServer()
	snap := emptySnapshot()

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

	result := s.buildDocumentSymbols(idx, snap)

	// Should have a synthetic schema root containing both import and type
	if len(result) != 1 {
		t.Fatalf("expected 1 top-level symbol (synthetic schema), got %d", len(result))
	}

	schemaSym := result[0]
	if schemaSym.Name != "(schema)" {
		t.Errorf("Name = %q; want '(schema)'", schemaSym.Name)
	}

	// Both import and type should be children of synthetic schema
	if len(schemaSym.Children) != 2 {
		t.Fatalf("expected 2 children (import + type), got %d", len(schemaSym.Children))
	}

	// Verify both children exist (order may vary)
	childNames := make(map[string]bool)
	for _, child := range schemaSym.Children {
		childNames[child.Name] = true
	}

	if !childNames["parts"] {
		t.Error("expected 'parts' import as child of synthetic schema")
	}
	if !childNames["Car"] {
		t.Error("expected 'Car' type as child of synthetic schema")
	}
}

// TestBuildDocumentSymbols_SchemaNameEqualsTypeName is a regression test for issue 2.8.
// It verifies that when a schema and type share the same name, both symbols appear
// in the output with correct kinds and proper parent/child relationships.
// This test guards against infinite recursion and outline corruption from name collisions.
func TestBuildDocumentSymbols_SchemaNameEqualsTypeName(t *testing.T) {
	t.Parallel()

	s := testServer()
	snap := emptySnapshot()

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

	result := s.buildDocumentSymbols(idx, snap)

	// Should complete without infinite recursion
	// Schema should be top-level
	if len(result) != 1 {
		t.Fatalf("expected 1 top-level symbol (schema), got %d", len(result))
	}

	schemaSym := result[0]
	if schemaSym.Name != "Person" {
		t.Errorf("Schema Name = %q; want 'Person'", schemaSym.Name)
	}
	if schemaSym.Kind != protocol.SymbolKindModule {
		t.Errorf("Schema Kind = %v; want Module", schemaSym.Kind)
	}

	// Schema should have the type Person as child
	if len(schemaSym.Children) != 1 {
		t.Fatalf("expected 1 schema child (type), got %d", len(schemaSym.Children))
	}

	typeSym := schemaSym.Children[0]
	if typeSym.Name != "Person" {
		t.Errorf("Type Name = %q; want 'Person'", typeSym.Name)
	}
	if typeSym.Kind != protocol.SymbolKindClass {
		t.Errorf("Type Kind = %v; want Class", typeSym.Kind)
	}

	// Type should have the property as child
	if len(typeSym.Children) != 1 {
		t.Fatalf("expected 1 type child (property), got %d", len(typeSym.Children))
	}

	propSym := typeSym.Children[0]
	if propSym.Name != "name" {
		t.Errorf("Property Name = %q; want 'name'", propSym.Name)
	}
	if propSym.Kind != protocol.SymbolKindField {
		t.Errorf("Property Kind = %v; want Field", propSym.Kind)
	}
}
