package lsp

import (
	"testing"

	"github.com/simon-lentz/yammm/internal/source"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

func TestExtractSymbols_NilSchema(t *testing.T) {
	t.Parallel()

	symbols := ExtractSymbols(nil, nil)
	if symbols != nil {
		t.Errorf("ExtractSymbols(nil) = %v; want nil", symbols)
	}
}

func TestExtractSymbols_EmptySchema(t *testing.T) {
	t.Parallel()

	sourceID := location.MustNewSourceID("test://empty.yammm")
	span := location.Range(sourceID, 1, 1, 1, 20)

	s := schema.NewSchema("empty", sourceID, span, "")

	symbols := ExtractSymbols(s, nil)
	if len(symbols) != 1 {
		t.Errorf("len(symbols) = %d; want 1 (schema itself)", len(symbols))
	}
	if len(symbols) > 0 && symbols[0].Kind != SymbolSchema {
		t.Errorf("symbols[0].Kind = %v; want SymbolSchema", symbols[0].Kind)
	}
}

func TestExtractSymbols_WithType(t *testing.T) {
	t.Parallel()

	sourceID := location.MustNewSourceID("test://person.yammm")
	schemaSpan := location.Range(sourceID, 1, 1, 10, 1)
	typeSpan := location.Range(sourceID, 3, 1, 7, 1)

	s := schema.NewSchema("People", sourceID, schemaSpan, "")

	typ := schema.NewType("Person", sourceID, typeSpan, "", false, false)
	s.SetTypes([]*schema.Type{typ})

	symbols := ExtractSymbols(s, nil)

	// Should have schema + type
	if len(symbols) < 2 {
		t.Fatalf("len(symbols) = %d; want >= 2", len(symbols))
	}

	// Verify we have both schema and type symbols
	var hasSchema, hasType bool
	for _, sym := range symbols {
		if sym.Kind == SymbolSchema && sym.Name == "People" {
			hasSchema = true
		}
		if sym.Kind == SymbolType && sym.Name == "Person" {
			hasType = true
		}
	}

	if !hasSchema {
		t.Error("missing schema symbol")
	}
	if !hasType {
		t.Error("missing type symbol")
	}
}

func TestExtractReferences_NilSchema(t *testing.T) {
	t.Parallel()

	refs := ExtractReferences(nil)
	if refs != nil {
		t.Errorf("ExtractReferences(nil) = %v; want nil", refs)
	}
}

func TestBuildSymbolIndex(t *testing.T) {
	t.Parallel()

	sourceID := location.MustNewSourceID("test://index.yammm")
	schemaSpan := location.Range(sourceID, 1, 1, 10, 1)

	s := schema.NewSchema("Index", sourceID, schemaSpan, "")

	idx := BuildSymbolIndex(s, nil)
	if idx == nil {
		t.Fatal("BuildSymbolIndex() returned nil")
	}
	// Symbols should not be nil (contains at least schema symbol)
	if idx.Symbols == nil {
		t.Error("idx.Symbols is nil")
	}
	// References may be nil for schemas without type references
	// (that's acceptable - we're just testing the index was created)
}

func TestSymbolAtPosition_Empty(t *testing.T) {
	t.Parallel()

	var idx *SymbolIndex
	sym := idx.SymbolAtPosition(location.Position{Line: 1, Column: 1})
	if sym != nil {
		t.Errorf("SymbolAtPosition(nil index) = %v; want nil", sym)
	}
}

func TestSymbolAtPosition_FindsSymbol(t *testing.T) {
	t.Parallel()

	sourceID := location.MustNewSourceID("test://find.yammm")
	schemaSpan := location.Range(sourceID, 1, 1, 20, 1)
	typeSpan := location.Range(sourceID, 5, 1, 15, 1)

	s := schema.NewSchema("Find", sourceID, schemaSpan, "")
	typ := schema.NewType("Target", sourceID, typeSpan, "", false, false)
	s.SetTypes([]*schema.Type{typ})

	idx := BuildSymbolIndex(s, nil)

	// Position inside the type
	sym := idx.SymbolAtPosition(location.Position{Line: 7, Column: 5})
	if sym == nil {
		t.Fatal("SymbolAtPosition() returned nil")
	}
	if sym.Name != "Target" {
		t.Errorf("sym.Name = %q; want Target", sym.Name)
	}
}

func TestReferenceAtPosition_Empty(t *testing.T) {
	t.Parallel()

	var idx *SymbolIndex
	ref := idx.ReferenceAtPosition(location.Position{Line: 1, Column: 1})
	if ref != nil {
		t.Errorf("ReferenceAtPosition(nil index) = %v; want nil", ref)
	}
}

func TestSnapshot_SymbolIndexAt(t *testing.T) {
	t.Parallel()

	sourceID := location.MustNewSourceID("test://snap.yammm")
	idx := &SymbolIndex{
		Symbols:    []Symbol{{Name: "Test", Kind: SymbolType}},
		References: []ReferenceSymbol{},
	}

	snapshot := &Snapshot{
		SymbolsBySource: map[location.SourceID]*SymbolIndex{
			sourceID: idx,
		},
	}

	// Should find the index
	got := snapshot.SymbolIndexAt(sourceID)
	if got != idx {
		t.Error("SymbolIndexAt() returned wrong index")
	}

	// Should return nil for unknown source
	unknownID := location.MustNewSourceID("test://unknown.yammm")
	got = snapshot.SymbolIndexAt(unknownID)
	if got != nil {
		t.Error("SymbolIndexAt(unknown) should return nil")
	}

	// Should handle nil snapshot
	var nilSnap *Snapshot
	got = nilSnap.SymbolIndexAt(sourceID)
	if got != nil {
		t.Error("nil snapshot SymbolIndexAt() should return nil")
	}
}

func TestSnapshot_FindSymbolByName(t *testing.T) {
	t.Parallel()

	sourceID := location.MustNewSourceID("test://find.yammm")
	idx := &SymbolIndex{
		Symbols: []Symbol{
			{Name: "Person", Kind: SymbolType},
			{Name: "name", Kind: SymbolProperty},
			{Name: "Employee", Kind: SymbolType},
		},
		References: []ReferenceSymbol{},
	}

	snapshot := &Snapshot{
		SymbolsBySource: map[location.SourceID]*SymbolIndex{
			sourceID: idx,
		},
	}

	// Find existing type
	sym := snapshot.FindSymbolByName(sourceID, "Person", SymbolType)
	if sym == nil {
		t.Fatal("FindSymbolByName(Person, SymbolType) returned nil")
	}
	if sym.Name != "Person" {
		t.Errorf("sym.Name = %q; want Person", sym.Name)
	}

	// Find property
	sym = snapshot.FindSymbolByName(sourceID, "name", SymbolProperty)
	if sym == nil {
		t.Fatal("FindSymbolByName(name, SymbolProperty) returned nil")
	}

	// Should not find with wrong kind
	sym = snapshot.FindSymbolByName(sourceID, "name", SymbolType)
	if sym != nil {
		t.Error("FindSymbolByName with wrong kind should return nil")
	}

	// Should not find non-existent
	sym = snapshot.FindSymbolByName(sourceID, "Unknown", SymbolType)
	if sym != nil {
		t.Error("FindSymbolByName for unknown should return nil")
	}
}

func TestSnapshot_ResolveTypeReference_Nil(t *testing.T) {
	t.Parallel()

	var snapshot *Snapshot
	sourceID := location.MustNewSourceID("test://ref.yammm")

	sym := snapshot.ResolveTypeReference(nil, sourceID)
	if sym != nil {
		t.Error("ResolveTypeReference(nil) should return nil")
	}
}

func TestSnapshot_ResolveTypeReference_Local(t *testing.T) {
	t.Parallel()

	sourceID := location.MustNewSourceID("test://local.yammm")
	typeSpan := location.Range(sourceID, 5, 1, 10, 1)

	idx := &SymbolIndex{
		Symbols: []Symbol{
			{Name: "Person", Kind: SymbolType, SourceID: sourceID, Range: typeSpan},
		},
		References: []ReferenceSymbol{},
	}

	snapshot := &Snapshot{
		SymbolsBySource: map[location.SourceID]*SymbolIndex{
			sourceID: idx,
		},
	}

	ref := &ReferenceSymbol{
		Kind:       RefExtends,
		TargetName: "Person",
		Qualifier:  "", // Local reference
	}

	sym := snapshot.ResolveTypeReference(ref, sourceID)
	if sym == nil {
		t.Fatal("ResolveTypeReference(local) returned nil")
	}
	if sym.Name != "Person" {
		t.Errorf("resolved sym.Name = %q; want Person", sym.Name)
	}
}

func TestIsSmaller(t *testing.T) {
	t.Parallel()

	sourceID := location.MustNewSourceID("test://smaller.yammm")

	tests := []struct {
		name   string
		a, b   location.Span
		expect bool
	}{
		{
			name:   "fewer lines is smaller",
			a:      location.Range(sourceID, 5, 1, 6, 1),  // 1 line
			b:      location.Range(sourceID, 1, 1, 10, 1), // 9 lines
			expect: true,
		},
		{
			name:   "same lines, narrower columns is smaller",
			a:      location.Range(sourceID, 5, 5, 5, 15),  // 10 cols
			b:      location.Range(sourceID, 5, 1, 5, 100), // 99 cols
			expect: true,
		},
		{
			name:   "more lines is not smaller",
			a:      location.Range(sourceID, 1, 1, 20, 1),
			b:      location.Range(sourceID, 5, 1, 6, 1),
			expect: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := isSmaller(tt.a, tt.b)
			if got != tt.expect {
				t.Errorf("isSmaller() = %v; want %v", got, tt.expect)
			}
		})
	}
}

func TestPositionBefore(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		a, b   location.Position
		expect bool
	}{
		{
			name:   "earlier line is before",
			a:      location.Position{Line: 1, Column: 10},
			b:      location.Position{Line: 2, Column: 1},
			expect: true,
		},
		{
			name:   "same line, earlier column is before",
			a:      location.Position{Line: 5, Column: 5},
			b:      location.Position{Line: 5, Column: 10},
			expect: true,
		},
		{
			name:   "later line is not before",
			a:      location.Position{Line: 10, Column: 1},
			b:      location.Position{Line: 5, Column: 1},
			expect: false,
		},
		{
			name:   "same position is not before",
			a:      location.Position{Line: 5, Column: 5},
			b:      location.Position{Line: 5, Column: 5},
			expect: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := positionBefore(tt.a, tt.b)
			if got != tt.expect {
				t.Errorf("positionBefore() = %v; want %v", got, tt.expect)
			}
		})
	}
}

// TestExtractTypeSymbols_AbstractAndPart tests extraction of abstract and part type symbols.
func TestExtractTypeSymbols_AbstractAndPart(t *testing.T) {
	t.Parallel()

	sourceID := location.MustNewSourceID("test://abstract.yammm")
	schemaSpan := location.Range(sourceID, 1, 1, 20, 1)
	abstractSpan := location.Range(sourceID, 3, 1, 5, 1)
	partSpan := location.Range(sourceID, 7, 1, 9, 1)

	s := schema.NewSchema("Types", sourceID, schemaSpan, "")

	abstract := schema.NewType("Entity", sourceID, abstractSpan, "", true, false)
	part := schema.NewType("Wheel", sourceID, partSpan, "", false, true)
	s.SetTypes([]*schema.Type{abstract, part})

	symbols := ExtractSymbols(s, nil)

	var entitySym, wheelSym *Symbol
	for i := range symbols {
		switch symbols[i].Name {
		case "Entity":
			entitySym = &symbols[i]
		case "Wheel":
			wheelSym = &symbols[i]
		}
	}

	if entitySym == nil {
		t.Error("missing abstract Entity symbol")
	} else if entitySym.Detail != "abstract type Entity" {
		t.Errorf("Entity.Detail = %q; want 'abstract type Entity'", entitySym.Detail)
	}

	if wheelSym == nil {
		t.Error("missing part Wheel symbol")
	} else if wheelSym.Detail != "part type Wheel" {
		t.Errorf("Wheel.Detail = %q; want 'part type Wheel'", wheelSym.Detail)
	}
}

func TestSymbolKind_StringValues(t *testing.T) {
	t.Parallel()

	// Verify all kinds have string representations
	kinds := []struct {
		kind SymbolKind
		want string
	}{
		{SymbolSchema, "Schema"},
		{SymbolImport, "Import"},
		{SymbolType, "Type"},
		{SymbolDataType, "DataType"},
		{SymbolProperty, "Property"},
		{SymbolAssociation, "Association"},
		{SymbolComposition, "Composition"},
		{SymbolInvariant, "Invariant"},
	}

	for _, tt := range kinds {
		got := tt.kind.String()
		if got != tt.want {
			t.Errorf("SymbolKind(%d).String() = %q; want %q", tt.kind, got, tt.want)
		}
	}
}

func TestSpanToLSPRange_Conversion(t *testing.T) {
	t.Parallel()

	sources := source.NewRegistry()
	sourceID := location.MustNewSourceID("test://range.yammm")
	span := location.Range(sourceID, 5, 10, 7, 20) // 1-based

	// SpanToLSPRange falls back to rune column conversion when source is not registered
	start, end, ok := SpanToLSPRange(sources, span, PositionEncodingUTF16)
	if !ok {
		t.Fatal("SpanToLSPRange returned !ok")
	}

	// Extract values to satisfy linter (gosec false positive on array access after t.Fatal)
	startLine, startChar := start[0], start[1]
	endLine, endChar := end[0], end[1]

	// LSP uses 0-based positions
	if startLine != 4 {
		t.Errorf("Start.Line = %d; want 4 (0-based)", startLine)
	}
	if startChar != 9 {
		t.Errorf("Start.Character = %d; want 9 (0-based)", startChar)
	}
	if endLine != 6 {
		t.Errorf("End.Line = %d; want 6 (0-based)", endLine)
	}
	if endChar != 19 {
		t.Errorf("End.Character = %d; want 19 (0-based)", endChar)
	}
}

// =============================================================================
// Datatype Symbol Tests (Priority 5: Test Coverage Gaps)
// =============================================================================

func TestExtractSymbols_WithDataType(t *testing.T) {
	t.Parallel()

	sourceID := location.MustNewSourceID("test://datatype.yammm")
	schemaSpan := location.Range(sourceID, 1, 1, 10, 1)
	dtSpan := location.Range(sourceID, 3, 1, 3, 30)

	s := schema.NewSchema("DataTypes", sourceID, schemaSpan, "")

	// Create a datatype alias
	dt := schema.NewDataType("ShortString", schema.NewStringConstraint(), dtSpan, "")
	s.SetDataTypes([]*schema.DataType{dt})

	symbols := ExtractSymbols(s, nil)

	// Should have schema + datatype
	if len(symbols) < 2 {
		t.Fatalf("len(symbols) = %d; want >= 2", len(symbols))
	}

	// Verify we have both schema and datatype symbols
	var hasSchema, hasDataType bool
	var dtSymbol *Symbol
	for i := range symbols {
		sym := &symbols[i]
		if sym.Kind == SymbolSchema && sym.Name == "DataTypes" {
			hasSchema = true
		}
		if sym.Kind == SymbolDataType && sym.Name == "ShortString" {
			hasDataType = true
			dtSymbol = sym
		}
	}

	if !hasSchema {
		t.Error("missing schema symbol")
	}
	if !hasDataType {
		t.Error("missing datatype symbol")
	}
	if dtSymbol != nil && dtSymbol.ParentName != "DataTypes" {
		t.Errorf("datatype ParentName = %q; want DataTypes", dtSymbol.ParentName)
	}
}

func TestExtractReferences_DataTypeRef(t *testing.T) {
	t.Parallel()

	sourceID := location.MustNewSourceID("test://dtref.yammm")
	schemaSpan := location.Range(sourceID, 1, 1, 20, 1)
	typeSpan := location.Range(sourceID, 3, 1, 10, 1)
	propSpan := location.Range(sourceID, 5, 5, 5, 30)
	dtRefSpan := location.Range(sourceID, 5, 15, 5, 25)

	s := schema.NewSchema("RefTest", sourceID, schemaSpan, "")

	// Create a type with a property that has a datatype reference
	typ := schema.NewType("Person", sourceID, typeSpan, "", false, false)

	// Create a property with a DataTypeRef
	dtRef := schema.LocalDataTypeRef("ShortString", dtRefSpan)
	prop := schema.NewProperty(
		"name",
		propSpan,
		"",
		schema.NewStringConstraint(),
		dtRef,
		false,
		false,
		schema.DeclaringScope{},
	)
	typ.SetProperties([]*schema.Property{prop})
	s.SetTypes([]*schema.Type{typ})

	refs := ExtractReferences(s)

	// Should have at least one RefDataType reference
	var foundDataTypeRef bool
	for _, ref := range refs {
		if ref.Kind == RefDataType && ref.TargetName == "ShortString" {
			foundDataTypeRef = true
			if ref.Qualifier != "" {
				t.Errorf("local datatype ref should have empty qualifier; got %q", ref.Qualifier)
			}
			break
		}
	}

	if !foundDataTypeRef {
		t.Error("missing RefDataType reference for ShortString")
	}
}

func TestExtractReferences_QualifiedDataTypeRef(t *testing.T) {
	t.Parallel()

	sourceID := location.MustNewSourceID("test://qualified.yammm")
	schemaSpan := location.Range(sourceID, 1, 1, 20, 1)
	typeSpan := location.Range(sourceID, 5, 1, 15, 1)
	propSpan := location.Range(sourceID, 7, 5, 7, 40)
	dtRefSpan := location.Range(sourceID, 7, 15, 7, 35)

	s := schema.NewSchema("QualifiedTest", sourceID, schemaSpan, "")

	// Create a type with a property that has a qualified datatype reference
	typ := schema.NewType("User", sourceID, typeSpan, "", false, false)

	// Create a property with a qualified DataTypeRef (e.g., types.Email)
	dtRef := schema.NewDataTypeRef("types", "Email", dtRefSpan)
	prop := schema.NewProperty(
		"email",
		propSpan,
		"",
		schema.NewStringConstraint(),
		dtRef,
		false,
		false,
		schema.DeclaringScope{},
	)
	typ.SetProperties([]*schema.Property{prop})
	s.SetTypes([]*schema.Type{typ})

	refs := ExtractReferences(s)

	// Should have RefDataType reference with qualifier
	var foundQualifiedRef bool
	for _, ref := range refs {
		if ref.Kind == RefDataType && ref.TargetName == "Email" {
			foundQualifiedRef = true
			if ref.Qualifier != "types" {
				t.Errorf("qualified datatype ref should have qualifier 'types'; got %q", ref.Qualifier)
			}
			break
		}
	}

	if !foundQualifiedRef {
		t.Error("missing qualified RefDataType reference for types.Email")
	}
}

func TestSnapshot_ResolveTypeReference_DataType(t *testing.T) {
	t.Parallel()

	sourceID := location.MustNewSourceID("test://resolve.yammm")
	dtSpan := location.Range(sourceID, 3, 1, 3, 30)

	idx := &SymbolIndex{
		Symbols: []Symbol{
			{Name: "ShortString", Kind: SymbolDataType, SourceID: sourceID, Range: dtSpan},
			{Name: "Person", Kind: SymbolType, SourceID: sourceID},
		},
		References: []ReferenceSymbol{},
	}

	snapshot := &Snapshot{
		SymbolsBySource: map[location.SourceID]*SymbolIndex{
			sourceID: idx,
		},
	}

	// Reference to a datatype (RefDataType kind)
	ref := &ReferenceSymbol{
		Kind:       RefDataType,
		TargetName: "ShortString",
		Qualifier:  "", // Local reference
	}

	sym := snapshot.ResolveTypeReference(ref, sourceID)
	if sym == nil {
		t.Fatal("ResolveTypeReference(datatype ref) returned nil")
	}
	if sym.Name != "ShortString" {
		t.Errorf("resolved sym.Name = %q; want ShortString", sym.Name)
	}
	if sym.Kind != SymbolDataType {
		t.Errorf("resolved sym.Kind = %v; want SymbolDataType", sym.Kind)
	}
}

func TestSnapshot_ResolveTypeReference_DataType_NotType(t *testing.T) {
	// Verify that RefDataType does NOT resolve to SymbolType
	t.Parallel()

	sourceID := location.MustNewSourceID("test://nottype.yammm")

	idx := &SymbolIndex{
		Symbols: []Symbol{
			// Only a Type symbol exists, no DataType with same name
			{Name: "ShortString", Kind: SymbolType, SourceID: sourceID},
		},
		References: []ReferenceSymbol{},
	}

	snapshot := &Snapshot{
		SymbolsBySource: map[location.SourceID]*SymbolIndex{
			sourceID: idx,
		},
	}

	// RefDataType should NOT resolve to SymbolType
	ref := &ReferenceSymbol{
		Kind:       RefDataType,
		TargetName: "ShortString",
		Qualifier:  "",
	}

	sym := snapshot.ResolveTypeReference(ref, sourceID)
	if sym != nil {
		t.Errorf("RefDataType should not resolve to SymbolType; got %v", sym)
	}
}

func TestFormatPropertyDetail_NilConstraint(t *testing.T) {
	// Tests that formatPropertyDetail handles nil constraints without panicking.
	// Properties can have nil constraints during partial parses or in early builder stages.
	t.Parallel()

	// Create a property with nil constraint (optional so not required)
	prop := schema.NewProperty(
		"testProp",
		location.Span{},
		"",
		nil, // nil constraint
		schema.DataTypeRef{},
		true,  // optional (so not required)
		false, // not primary key
		schema.DeclaringScope{},
	)

	// This should not panic and should return a valid detail string
	detail := formatPropertyDetail(prop)

	// Should include property name and placeholder for unknown constraint
	expected := "testProp <unknown>"
	if detail != expected {
		t.Errorf("formatPropertyDetail(nil constraint) = %q; want %q", detail, expected)
	}
}

func TestFormatPropertyDetail_NilConstraintWithModifiers(t *testing.T) {
	// Tests that formatPropertyDetail correctly appends modifiers even with nil constraint.
	t.Parallel()

	// Create a property with nil constraint but with primary key and required flags
	prop := schema.NewProperty(
		"id",
		location.Span{},
		"",
		nil, // nil constraint
		schema.DataTypeRef{},
		false, // not optional (= required)
		true,  // is primary key
		schema.DeclaringScope{},
	)

	detail := formatPropertyDetail(prop)

	// Should include all modifiers
	expected := "id <unknown> primary required"
	if detail != expected {
		t.Errorf("formatPropertyDetail(nil constraint + modifiers) = %q; want %q", detail, expected)
	}
}

func TestFormatPropertyDetail_WithConstraint(t *testing.T) {
	// Tests that formatPropertyDetail correctly formats a property with a constraint.
	t.Parallel()

	constraint := schema.NewStringConstraint()
	prop := schema.NewProperty(
		"name",
		location.Span{},
		"",
		constraint,
		schema.DataTypeRef{},
		true,  // optional
		false, // not primary key
		schema.DeclaringScope{},
	)

	detail := formatPropertyDetail(prop)

	// Should include constraint type (String with capital S) but not "required" since it's optional
	expected := "name String"
	if detail != expected {
		t.Errorf("formatPropertyDetail(with constraint) = %q; want %q", detail, expected)
	}
}
