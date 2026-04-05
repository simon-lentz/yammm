package symbols_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/simon-lentz/yammm/internal/source"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"

	"github.com/simon-lentz/yammm/lsp/internal/analysis"
	"github.com/simon-lentz/yammm/lsp/internal/lsputil"
	"github.com/simon-lentz/yammm/lsp/internal/symbols"
)

func TestExtractSymbols_NilSchema(t *testing.T) {
	t.Parallel()

	syms := symbols.ExtractSymbols(nil, nil)
	assert.Nil(t, syms, "ExtractSymbols(nil) should return nil")
}

func TestExtractSymbols_EmptySchema(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s, result, err := schema.LoadString(ctx, `schema "empty"`, "empty.yammm")
	require.NoError(t, err)
	require.False(t, result.HasErrors(), "load errors: %v", result.Messages())

	syms := symbols.ExtractSymbols(s, nil)
	assert.Len(t, syms, 1, "should contain only the schema itself")
	if len(syms) > 0 {
		assert.Equal(t, symbols.SymbolSchema, syms[0].Kind)
	}
}

func TestExtractSymbols_WithType(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	src := `schema "People"

type Person {
    id String primary
}
`
	s, result, err := schema.LoadString(ctx, src, "person.yammm")
	require.NoError(t, err)
	require.False(t, result.HasErrors(), "load errors: %v", result.Messages())

	syms := symbols.ExtractSymbols(s, nil)

	// Should have schema + type
	require.GreaterOrEqual(t, len(syms), 2, "should have at least schema + type symbols")

	// Verify we have both schema and type symbols
	var hasSchema, hasType bool
	for _, sym := range syms {
		if sym.Kind == symbols.SymbolSchema && sym.Name == "People" {
			hasSchema = true
		}
		if sym.Kind == symbols.SymbolType && sym.Name == "Person" {
			hasType = true
		}
	}

	assert.True(t, hasSchema, "missing schema symbol")
	assert.True(t, hasType, "missing type symbol")
}

func TestExtractReferences_NilSchema(t *testing.T) {
	t.Parallel()

	refs := symbols.ExtractReferences(nil)
	assert.Nil(t, refs, "symbols.ExtractReferences(nil) should return nil")
}

func TestBuildSymbolIndex(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s, result, err := schema.LoadString(ctx, `schema "Index"`, "index.yammm")
	require.NoError(t, err)
	require.False(t, result.HasErrors(), "load errors: %v", result.Messages())

	idx := symbols.BuildSymbolIndex(s, nil)
	require.NotNil(t, idx, "symbols.BuildSymbolIndex() returned nil")
	// Symbols should not be nil (contains at least schema symbol)
	assert.NotNil(t, idx.Symbols, "idx.Symbols is nil")
	// References may be nil for schemas without type references
	// (that's acceptable - we're just testing the index was created)
}

func TestSymbolAtPosition_Empty(t *testing.T) {
	t.Parallel()

	var idx *symbols.SymbolIndex
	sym := idx.SymbolAtPosition(location.Position{Line: 1, Column: 1})
	assert.Nil(t, sym, "SymbolAtPosition(nil index) should return nil")
}

func TestSymbolAtPosition_FindsSymbol(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	src := `schema "Find"

type Target {
    id String primary
}
`
	s, result, err := schema.LoadString(ctx, src, "find.yammm")
	require.NoError(t, err)
	require.False(t, result.HasErrors(), "load errors: %v", result.Messages())

	idx := symbols.BuildSymbolIndex(s, nil)

	// Position on the type declaration line (line 3: "type Target {").
	// Byte must be -1 (unknown) to force line/column comparison;
	// a zero Byte would be interpreted as byte offset 0.
	sym := idx.SymbolAtPosition(location.Position{Line: 3, Column: 5, Byte: -1})
	require.NotNil(t, sym, "SymbolAtPosition() returned nil")
	assert.Equal(t, "Target", sym.Name)
}

func TestReferenceAtPosition_Empty(t *testing.T) {
	t.Parallel()

	var idx *symbols.SymbolIndex
	ref := idx.ReferenceAtPosition(location.Position{Line: 1, Column: 1})
	assert.Nil(t, ref, "ReferenceAtPosition(nil index) should return nil")
}

func TestSnapshot_SymbolIndexAt(t *testing.T) {
	t.Parallel()

	sourceID := location.MustNewSourceID("test://snap.yammm")
	idx := &symbols.SymbolIndex{
		Symbols:    []symbols.Symbol{{Name: "Test", Kind: symbols.SymbolType}},
		References: []symbols.ReferenceSymbol{},
	}

	snapshot := &analysis.Snapshot{
		SymbolsBySource: map[location.SourceID]*symbols.SymbolIndex{
			sourceID: idx,
		},
	}

	// Should find the index
	got := snapshot.SymbolIndexAt(sourceID)
	assert.Equal(t, idx, got, "SymbolIndexAt() returned wrong index")

	// Should return nil for unknown source
	unknownID := location.MustNewSourceID("test://unknown.yammm")
	got = snapshot.SymbolIndexAt(unknownID)
	assert.Nil(t, got, "SymbolIndexAt(unknown) should return nil")

	// Should handle nil snapshot
	var nilSnap *analysis.Snapshot
	got = nilSnap.SymbolIndexAt(sourceID)
	assert.Nil(t, got, "nil snapshot SymbolIndexAt() should return nil")
}

func TestSnapshot_FindSymbolByName(t *testing.T) {
	t.Parallel()

	sourceID := location.MustNewSourceID("test://find.yammm")
	idx := &symbols.SymbolIndex{
		Symbols: []symbols.Symbol{
			{Name: "Person", Kind: symbols.SymbolType},
			{Name: "name", Kind: symbols.SymbolProperty},
			{Name: "Employee", Kind: symbols.SymbolType},
		},
		References: []symbols.ReferenceSymbol{},
	}

	snapshot := &analysis.Snapshot{
		SymbolsBySource: map[location.SourceID]*symbols.SymbolIndex{
			sourceID: idx,
		},
	}

	// Find existing type
	sym := snapshot.FindSymbolByName(sourceID, "Person", symbols.SymbolType)
	require.NotNil(t, sym, "FindSymbolByName(Person, symbols.SymbolType) returned nil")
	assert.Equal(t, "Person", sym.Name)

	// Find property
	sym = snapshot.FindSymbolByName(sourceID, "name", symbols.SymbolProperty)
	require.NotNil(t, sym, "FindSymbolByName(name, symbols.SymbolProperty) returned nil")

	// Should not find with wrong kind
	sym = snapshot.FindSymbolByName(sourceID, "name", symbols.SymbolType)
	assert.Nil(t, sym, "FindSymbolByName with wrong kind should return nil")

	// Should not find non-existent
	sym = snapshot.FindSymbolByName(sourceID, "Unknown", symbols.SymbolType)
	assert.Nil(t, sym, "FindSymbolByName for unknown should return nil")
}

func TestSnapshot_ResolveTypeReference_Nil(t *testing.T) {
	t.Parallel()

	var snapshot *analysis.Snapshot
	sourceID := location.MustNewSourceID("test://ref.yammm")

	sym := snapshot.ResolveTypeReference(nil, sourceID)
	assert.Nil(t, sym, "ResolveTypeReference(nil) should return nil")
}

func TestSnapshot_ResolveTypeReference_Local(t *testing.T) {
	t.Parallel()

	sourceID := location.MustNewSourceID("test://local.yammm")
	typeSpan := location.Range(sourceID, 5, 1, 10, 1)

	idx := &symbols.SymbolIndex{
		Symbols: []symbols.Symbol{
			{Name: "Person", Kind: symbols.SymbolType, SourceID: sourceID, Range: typeSpan},
		},
		References: []symbols.ReferenceSymbol{},
	}

	snapshot := &analysis.Snapshot{
		SymbolsBySource: map[location.SourceID]*symbols.SymbolIndex{
			sourceID: idx,
		},
	}

	ref := &symbols.ReferenceSymbol{
		Kind:       symbols.RefExtends,
		TargetName: "Person",
		Qualifier:  "", // Local reference
	}

	sym := snapshot.ResolveTypeReference(ref, sourceID)
	require.NotNil(t, sym, "ResolveTypeReference(local) returned nil")
	assert.Equal(t, "Person", sym.Name)
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
			got := symbols.IsSmaller(tt.a, tt.b)
			assert.Equal(t, tt.expect, got, "isSmaller() result mismatch")
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
			got := symbols.PositionBefore(tt.a, tt.b)
			assert.Equal(t, tt.expect, got, "positionBefore() result mismatch")
		})
	}
}

// TestExtractTypeSymbols_AbstractAndPart tests extraction of abstract and part type symbols.
func TestExtractTypeSymbols_AbstractAndPart(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	src := `schema "Types"

abstract type Entity {
    id String primary
}

part type Wheel {
    size Integer required
}
`
	s, result, err := schema.LoadString(ctx, src, "abstract.yammm")
	require.NoError(t, err)
	require.False(t, result.HasErrors(), "load errors: %v", result.Messages())

	syms := symbols.ExtractSymbols(s, nil)

	var entitySym, wheelSym *symbols.Symbol
	for i := range syms {
		switch syms[i].Name {
		case "Entity":
			entitySym = &syms[i]
		case "Wheel":
			wheelSym = &syms[i]
		}
	}

	if assert.NotNil(t, entitySym, "missing abstract Entity symbol") {
		assert.Equal(t, "abstract type Entity", entitySym.Detail)
	}

	if assert.NotNil(t, wheelSym, "missing part Wheel symbol") {
		assert.Equal(t, "part type Wheel", wheelSym.Detail)
	}
}

func TestSymbolKind_StringValues(t *testing.T) {
	t.Parallel()

	// Verify all kinds have string representations
	kinds := []struct {
		kind symbols.SymbolKind
		want string
	}{
		{symbols.SymbolSchema, "Schema"},
		{symbols.SymbolImport, "Import"},
		{symbols.SymbolType, "Type"},
		{symbols.SymbolDataType, "DataType"},
		{symbols.SymbolProperty, "Property"},
		{symbols.SymbolAssociation, "Association"},
		{symbols.SymbolComposition, "Composition"},
		{symbols.SymbolInvariant, "Invariant"},
	}

	for _, tt := range kinds {
		got := tt.kind.String()
		assert.Equal(t, tt.want, got, "SymbolKind(%d).String()", tt.kind)
	}
}

func TestSpanToLSPRange_Conversion(t *testing.T) {
	t.Parallel()

	sources := source.NewRegistry()
	sourceID := location.MustNewSourceID("test://range.yammm")
	span := location.Range(sourceID, 5, 10, 7, 20) // 1-based

	// SpanToLSPRange falls back to rune column conversion when source is not registered
	start, end, ok := lsputil.SpanToLSPRange(sources, span, lsputil.PositionEncodingUTF16)
	require.True(t, ok, "SpanToLSPRange returned !ok")

	// Extract values to satisfy linter (gosec false positive on array access after t.Fatal)
	startLine, startChar := start[0], start[1]
	endLine, endChar := end[0], end[1]

	// LSP uses 0-based positions
	assert.Equal(t, 4, startLine, "Start.Line (0-based)")
	assert.Equal(t, 9, startChar, "Start.Character (0-based)")
	assert.Equal(t, 6, endLine, "End.Line (0-based)")
	assert.Equal(t, 19, endChar, "End.Character (0-based)")
}

func TestExtractSymbols_WithDataType(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	src := `schema "DataTypes"

type ShortString = String[1, 50]
`
	s, result, err := schema.LoadString(ctx, src, "datatype.yammm")
	require.NoError(t, err)
	require.False(t, result.HasErrors(), "load errors: %v", result.Messages())

	syms := symbols.ExtractSymbols(s, nil)

	// Should have schema + datatype
	require.GreaterOrEqual(t, len(syms), 2, "should have at least schema + datatype symbols")

	// Verify we have both schema and datatype symbols
	var hasSchema, hasDataType bool
	var dtSymbol *symbols.Symbol
	for i := range syms {
		sym := &syms[i]
		if sym.Kind == symbols.SymbolSchema && sym.Name == "DataTypes" {
			hasSchema = true
		}
		if sym.Kind == symbols.SymbolDataType && sym.Name == "ShortString" {
			hasDataType = true
			dtSymbol = sym
		}
	}

	assert.True(t, hasSchema, "missing schema symbol")
	assert.True(t, hasDataType, "missing datatype symbol")
	if dtSymbol != nil {
		assert.Equal(t, "DataTypes", dtSymbol.ParentName)
	}
}

func TestExtractReferences_DataTypeRef(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	src := `schema "RefTest"

type ShortString = String[1, 50]

type Person {
    id String primary
    name ShortString
}
`
	s, result, err := schema.LoadString(ctx, src, "dtref.yammm")
	require.NoError(t, err)
	require.False(t, result.HasErrors(), "load errors: %v", result.Messages())

	refs := symbols.ExtractReferences(s)

	// Should have at least one RefDataType reference
	var foundDataTypeRef bool
	for _, ref := range refs {
		if ref.Kind == symbols.RefDataType && ref.TargetName == "ShortString" {
			foundDataTypeRef = true
			assert.Empty(t, ref.Qualifier, "local datatype ref should have empty qualifier")
			break
		}
	}

	assert.True(t, foundDataTypeRef, "missing RefDataType reference for ShortString")
}

func TestExtractReferences_QualifiedDataTypeRef(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tmpDir := t.TempDir()
	sources := map[string][]byte{
		"types.yammm": []byte(`schema "types"
type Email = String[1, 254]
`),
		"main.yammm": []byte(`schema "QualifiedTest"
import "./types" as types

type User {
    id String primary
    email types.Email
}
`),
	}

	s, result, err := schema.LoadSourcesWithEntry(ctx, sources, "main.yammm", tmpDir)
	require.NoError(t, err)
	require.False(t, result.HasErrors(), "load errors: %v", result.Messages())

	refs := symbols.ExtractReferences(s)

	// Should have RefDataType reference with qualifier
	var foundQualifiedRef bool
	for _, ref := range refs {
		if ref.Kind == symbols.RefDataType && ref.TargetName == "Email" {
			foundQualifiedRef = true
			assert.Equal(t, "types", ref.Qualifier, "qualified datatype ref should have qualifier 'types'")
			break
		}
	}

	assert.True(t, foundQualifiedRef, "missing qualified RefDataType reference for types.Email")
}

func TestSnapshot_ResolveTypeReference_DataType(t *testing.T) {
	t.Parallel()

	sourceID := location.MustNewSourceID("test://resolve.yammm")
	dtSpan := location.Range(sourceID, 3, 1, 3, 30)

	idx := &symbols.SymbolIndex{
		Symbols: []symbols.Symbol{
			{Name: "ShortString", Kind: symbols.SymbolDataType, SourceID: sourceID, Range: dtSpan},
			{Name: "Person", Kind: symbols.SymbolType, SourceID: sourceID},
		},
		References: []symbols.ReferenceSymbol{},
	}

	snapshot := &analysis.Snapshot{
		SymbolsBySource: map[location.SourceID]*symbols.SymbolIndex{
			sourceID: idx,
		},
	}

	// Reference to a datatype (RefDataType kind)
	ref := &symbols.ReferenceSymbol{
		Kind:       symbols.RefDataType,
		TargetName: "ShortString",
		Qualifier:  "", // Local reference
	}

	sym := snapshot.ResolveTypeReference(ref, sourceID)
	require.NotNil(t, sym, "ResolveTypeReference(datatype ref) returned nil")
	assert.Equal(t, "ShortString", sym.Name)
	assert.Equal(t, symbols.SymbolDataType, sym.Kind)
}

func TestSnapshot_ResolveTypeReference_DataType_NotType(t *testing.T) {
	// Verify that RefDataType does NOT resolve to SymbolType
	t.Parallel()

	sourceID := location.MustNewSourceID("test://nottype.yammm")

	idx := &symbols.SymbolIndex{
		Symbols: []symbols.Symbol{
			// Only a Type symbol exists, no DataType with same name
			{Name: "ShortString", Kind: symbols.SymbolType, SourceID: sourceID},
		},
		References: []symbols.ReferenceSymbol{},
	}

	snapshot := &analysis.Snapshot{
		SymbolsBySource: map[location.SourceID]*symbols.SymbolIndex{
			sourceID: idx,
		},
	}

	// RefDataType should NOT resolve to SymbolType
	ref := &symbols.ReferenceSymbol{
		Kind:       symbols.RefDataType,
		TargetName: "ShortString",
		Qualifier:  "",
	}

	sym := snapshot.ResolveTypeReference(ref, sourceID)
	assert.Nil(t, sym, "RefDataType should not resolve to SymbolType")
}

func TestFormatPropertyDetail_WithConstraint(t *testing.T) {
	// Tests that formatPropertyDetail correctly formats a property with a constraint.
	t.Parallel()

	s, result := schema.NewBuilder().
		WithName("test").
		WithSourceID(location.MustNewSourceID("test://test.yammm")).
		AddType("Person").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithOptionalProperty("name", schema.NewStringConstraint()).
		Done().
		Build()
	require.False(t, result.HasErrors(), "builder errors: %v", result)

	typ, ok := s.Type("Person")
	require.True(t, ok)
	prop, ok := typ.Property("name")
	require.True(t, ok)

	detail := symbols.FormatPropertyDetail(prop)

	// Should include constraint type (String with capital S) but not "required" since it's optional
	assert.Equal(t, "name String", detail)
}
