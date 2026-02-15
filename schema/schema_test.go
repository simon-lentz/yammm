package schema_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/simon-lentz/yammm/internal/source"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

func TestSchema_Seal_PreventsSetTypes(t *testing.T) {
	s := schema.NewSchema("test", location.SourceID{}, location.Span{}, "")
	s.SetTypes([]*schema.Type{})
	s.Seal()

	defer func() {
		if r := recover(); r == nil {
			t.Errorf("expected panic on SetTypes after Seal, but no panic occurred")
		}
	}()

	s.SetTypes([]*schema.Type{})
}

func TestSchema_Seal_PreventsSetDataTypes(t *testing.T) {
	s := schema.NewSchema("test", location.SourceID{}, location.Span{}, "")
	s.Seal()

	defer func() {
		if r := recover(); r == nil {
			t.Errorf("expected panic on SetDataTypes after Seal, but no panic occurred")
		}
	}()

	s.SetDataTypes([]*schema.DataType{})
}

func TestSchema_Seal_PreventsSetImports(t *testing.T) {
	s := schema.NewSchema("test", location.SourceID{}, location.Span{}, "")
	s.Seal()

	defer func() {
		if r := recover(); r == nil {
			t.Errorf("expected panic on SetImports after Seal, but no panic occurred")
		}
	}()

	s.SetImports([]*schema.Import{})
}

func TestSchema_Seal_PreventsSetSources(t *testing.T) {
	s := schema.NewSchema("test", location.SourceID{}, location.Span{}, "")
	s.Seal()

	defer func() {
		if r := recover(); r == nil {
			t.Errorf("expected panic on SetSources after Seal, but no panic occurred")
		}
	}()

	s.SetSources(nil)
}

func TestSchema_SettersWorkBeforeSeal(t *testing.T) {
	s := schema.NewSchema("test", location.SourceID{}, location.Span{}, "")

	// These should not panic before sealing
	s.SetTypes([]*schema.Type{})
	s.SetDataTypes([]*schema.DataType{})
	s.SetImports([]*schema.Import{})
	s.SetSources(nil)

	// Verify no panic occurred by reaching this point
}

// --- Constructor and Accessor Tests ---

func TestNewSchema(t *testing.T) {
	sourceID := location.MustNewSourceID("test://schema")
	span := location.Span{
		Source: sourceID,
		Start:  location.Position{Line: 1, Column: 1, Byte: 0},
		End:    location.Position{Line: 50, Column: 1, Byte: 500},
	}

	s := schema.NewSchema("users", sourceID, span, "User management schema")

	assert.NotNil(t, s)
	assert.Equal(t, "users", s.Name())
	assert.Equal(t, sourceID, s.SourceID())
	assert.Equal(t, span, s.Span())
	assert.Equal(t, "User management schema", s.Documentation())
}

func TestSchema_Name(t *testing.T) {
	s := schema.NewSchema("myschema", location.SourceID{}, location.Span{}, "")

	assert.Equal(t, "myschema", s.Name())
}

func TestSchema_SourceID(t *testing.T) {
	sourceID := location.MustNewSourceID("test://source")

	s := schema.NewSchema("test", sourceID, location.Span{}, "")

	assert.Equal(t, sourceID, s.SourceID())
}

func TestSchema_Span(t *testing.T) {
	span := location.Span{
		Source: location.MustNewSourceID("test://span"),
		Start:  location.Position{Line: 1, Column: 1, Byte: 0},
		End:    location.Position{Line: 100, Column: 1, Byte: 1000},
	}

	s := schema.NewSchema("test", location.SourceID{}, span, "")

	result := s.Span()
	assert.Equal(t, span.Start.Line, result.Start.Line)
	assert.Equal(t, span.End.Byte, result.End.Byte)
}

func TestSchema_Documentation(t *testing.T) {
	s := schema.NewSchema("test", location.SourceID{}, location.Span{}, "Schema documentation")

	assert.Equal(t, "Schema documentation", s.Documentation())
}

func TestSchema_Type_Found(t *testing.T) {
	s := schema.NewSchema("test", location.SourceID{}, location.Span{}, "")
	typ := schema.NewType("Person", location.SourceID{}, location.Span{}, "", false, false)
	s.SetTypes([]*schema.Type{typ})

	result, ok := s.Type("Person")

	assert.True(t, ok)
	assert.Same(t, typ, result)
}

func TestSchema_Type_NotFound(t *testing.T) {
	s := schema.NewSchema("test", location.SourceID{}, location.Span{}, "")

	result, ok := s.Type("NonExistent")

	assert.False(t, ok)
	assert.Nil(t, result)
}

func TestSchema_Types_Iterator(t *testing.T) {
	s := schema.NewSchema("test", location.SourceID{}, location.Span{}, "")
	t1 := schema.NewType("Type1", location.SourceID{}, location.Span{}, "", false, false)
	t2 := schema.NewType("Type2", location.SourceID{}, location.Span{}, "", false, false)
	s.SetTypes([]*schema.Type{t1, t2})

	count := 0
	for name, typ := range s.Types() {
		assert.NotEmpty(t, name)
		assert.NotNil(t, typ)
		assert.Equal(t, name, typ.Name())
		count++
	}
	assert.Equal(t, 2, count)
}

func TestSchema_TypesSlice(t *testing.T) {
	s := schema.NewSchema("test", location.SourceID{}, location.Span{}, "")
	t1 := schema.NewType("Type1", location.SourceID{}, location.Span{}, "", false, false)
	s.SetTypes([]*schema.Type{t1})

	result := s.TypesSlice()

	assert.Len(t, result, 1)
	assert.Same(t, t1, result[0])
}

func TestSchema_TypesSlice_DefensiveCopy(t *testing.T) {
	s := schema.NewSchema("test", location.SourceID{}, location.Span{}, "")
	t1 := schema.NewType("Type1", location.SourceID{}, location.Span{}, "", false, false)
	s.SetTypes([]*schema.Type{t1})

	slice1 := s.TypesSlice()
	slice2 := s.TypesSlice()

	slice1[0] = nil
	assert.NotNil(t, slice2[0])
}

func TestSchema_TypeNames(t *testing.T) {
	s := schema.NewSchema("test", location.SourceID{}, location.Span{}, "")
	t1 := schema.NewType("Beta", location.SourceID{}, location.Span{}, "", false, false)
	t2 := schema.NewType("Alpha", location.SourceID{}, location.Span{}, "", false, false)
	s.SetTypes([]*schema.Type{t1, t2})

	names := s.TypeNames()

	assert.Len(t, names, 2)
	// Verify lexicographic order
	assert.Equal(t, "Alpha", names[0])
	assert.Equal(t, "Beta", names[1])
}

func TestSchema_TypeCount(t *testing.T) {
	s := schema.NewSchema("test", location.SourceID{}, location.Span{}, "")
	assert.Equal(t, 0, s.TypeCount())

	t1 := schema.NewType("Type1", location.SourceID{}, location.Span{}, "", false, false)
	t2 := schema.NewType("Type2", location.SourceID{}, location.Span{}, "", false, false)
	s.SetTypes([]*schema.Type{t1, t2})

	assert.Equal(t, 2, s.TypeCount())
}

func TestSchema_DataTypeNames(t *testing.T) {
	s := schema.NewSchema("test", location.SourceID{}, location.Span{}, "")
	dt1 := schema.NewDataType("Zebra", nil, location.Span{}, "")
	dt2 := schema.NewDataType("Apple", nil, location.Span{}, "")
	s.SetDataTypes([]*schema.DataType{dt1, dt2})

	names := s.DataTypeNames()

	assert.Len(t, names, 2)
	// Verify lexicographic order
	assert.Equal(t, "Apple", names[0])
	assert.Equal(t, "Zebra", names[1])
}

func TestSchema_ImportCount(t *testing.T) {
	s := schema.NewSchema("test", location.SourceID{}, location.Span{}, "")
	assert.Equal(t, 0, s.ImportCount())

	imp1 := schema.NewImport("./a.yammm", "a", location.SourceID{}, location.Span{})
	imp2 := schema.NewImport("./b.yammm", "b", location.SourceID{}, location.Span{})
	s.SetImports([]*schema.Import{imp1, imp2})

	assert.Equal(t, 2, s.ImportCount())
}

func TestSchema_ResolveType_Local(t *testing.T) {
	s := schema.NewSchema("test", location.SourceID{}, location.Span{}, "")
	typ := schema.NewType("Customer", location.SourceID{}, location.Span{}, "", false, false)
	s.SetTypes([]*schema.Type{typ})

	ref := schema.NewTypeRef("", "Customer", location.Span{})
	result, ok := s.ResolveType(ref)

	assert.True(t, ok)
	assert.Same(t, typ, result)
}

func TestSchema_ResolveType_LocalNotFound(t *testing.T) {
	s := schema.NewSchema("test", location.SourceID{}, location.Span{}, "")

	ref := schema.NewTypeRef("", "NonExistent", location.Span{})
	result, ok := s.ResolveType(ref)

	assert.False(t, ok)
	assert.Nil(t, result)
}

func TestSchema_ResolveType_QualifiedNoImport(t *testing.T) {
	s := schema.NewSchema("test", location.SourceID{}, location.Span{}, "")

	ref := schema.NewTypeRef("users", "Person", location.Span{})
	result, ok := s.ResolveType(ref)

	assert.False(t, ok)
	assert.Nil(t, result)
}

func TestSchema_DataType_Found(t *testing.T) {
	s := schema.NewSchema("test", location.SourceID{}, location.Span{}, "")
	dt := schema.NewDataType("Email", schema.NewStringConstraint(), location.Span{}, "")
	s.SetDataTypes([]*schema.DataType{dt})

	result, ok := s.DataType("Email")

	assert.True(t, ok)
	assert.Same(t, dt, result)
}

func TestSchema_DataType_NotFound(t *testing.T) {
	s := schema.NewSchema("test", location.SourceID{}, location.Span{}, "")

	result, ok := s.DataType("NonExistent")

	assert.False(t, ok)
	assert.Nil(t, result)
}

func TestSchema_DataTypes_Iterator(t *testing.T) {
	s := schema.NewSchema("test", location.SourceID{}, location.Span{}, "")
	dt1 := schema.NewDataType("Email", nil, location.Span{}, "")
	dt2 := schema.NewDataType("Phone", nil, location.Span{}, "")
	s.SetDataTypes([]*schema.DataType{dt1, dt2})

	count := 0
	for name, dt := range s.DataTypes() {
		assert.NotEmpty(t, name)
		assert.NotNil(t, dt)
		assert.Equal(t, name, dt.Name())
		count++
	}
	assert.Equal(t, 2, count)
}

func TestSchema_DataTypesSlice(t *testing.T) {
	s := schema.NewSchema("test", location.SourceID{}, location.Span{}, "")
	dt := schema.NewDataType("Money", nil, location.Span{}, "")
	s.SetDataTypes([]*schema.DataType{dt})

	result := s.DataTypesSlice()

	assert.Len(t, result, 1)
	assert.Same(t, dt, result[0])
}

func TestSchema_ResolveDataType_Local(t *testing.T) {
	s := schema.NewSchema("test", location.SourceID{}, location.Span{}, "")
	dt := schema.NewDataType("Currency", nil, location.Span{}, "")
	s.SetDataTypes([]*schema.DataType{dt})

	ref := schema.NewDataTypeRef("", "Currency", location.Span{})
	result, ok := s.ResolveDataType(ref)

	assert.True(t, ok)
	assert.Same(t, dt, result)
}

func TestSchema_ResolveDataType_LocalNotFound(t *testing.T) {
	s := schema.NewSchema("test", location.SourceID{}, location.Span{}, "")

	ref := schema.NewDataTypeRef("", "NonExistent", location.Span{})
	result, ok := s.ResolveDataType(ref)

	assert.False(t, ok)
	assert.Nil(t, result)
}

func TestSchema_ResolveDataType_QualifiedNoImport(t *testing.T) {
	s := schema.NewSchema("test", location.SourceID{}, location.Span{}, "")

	ref := schema.NewDataTypeRef("types", "Email", location.Span{})
	result, ok := s.ResolveDataType(ref)

	assert.False(t, ok)
	assert.Nil(t, result)
}

func TestSchema_Imports_Iterator(t *testing.T) {
	s := schema.NewSchema("test", location.SourceID{}, location.Span{}, "")
	imp1 := schema.NewImport("./types.yammm", "types", location.SourceID{}, location.Span{})
	imp2 := schema.NewImport("./users.yammm", "users", location.SourceID{}, location.Span{})
	s.SetImports([]*schema.Import{imp1, imp2})

	count := 0
	for imp := range s.Imports() {
		assert.NotNil(t, imp)
		count++
	}
	assert.Equal(t, 2, count)
}

func TestSchema_ImportsSlice(t *testing.T) {
	s := schema.NewSchema("test", location.SourceID{}, location.Span{}, "")
	imp := schema.NewImport("./types.yammm", "types", location.SourceID{}, location.Span{})
	s.SetImports([]*schema.Import{imp})

	result := s.ImportsSlice()

	assert.Len(t, result, 1)
	assert.Same(t, imp, result[0])
}

func TestSchema_ImportsSlice_DefensiveCopy(t *testing.T) {
	s := schema.NewSchema("test", location.SourceID{}, location.Span{}, "")
	imp := schema.NewImport("./types.yammm", "types", location.SourceID{}, location.Span{})
	s.SetImports([]*schema.Import{imp})

	slice1 := s.ImportsSlice()
	slice2 := s.ImportsSlice()

	slice1[0] = nil
	assert.NotNil(t, slice2[0])
}

func TestSchema_ImportByAlias_Found(t *testing.T) {
	s := schema.NewSchema("test", location.SourceID{}, location.Span{}, "")
	imp := schema.NewImport("./types.yammm", "types", location.SourceID{}, location.Span{})
	s.SetImports([]*schema.Import{imp})

	result, ok := s.ImportByAlias("types")

	assert.True(t, ok)
	assert.Same(t, imp, result)
}

func TestSchema_ImportByAlias_NotFound(t *testing.T) {
	s := schema.NewSchema("test", location.SourceID{}, location.Span{}, "")

	result, ok := s.ImportByAlias("nonexistent")

	assert.False(t, ok)
	assert.Nil(t, result)
}

func TestSchema_FindImportAlias_Found(t *testing.T) {
	s := schema.NewSchema("test", location.SourceID{}, location.Span{}, "")
	resolvedID := location.MustNewSourceID("test://types")
	imp := schema.NewImport("./types.yammm", "types", resolvedID, location.Span{})
	s.SetImports([]*schema.Import{imp})

	result := s.FindImportAlias(resolvedID)

	assert.Equal(t, "types", result)
}

func TestSchema_FindImportAlias_NotFound(t *testing.T) {
	s := schema.NewSchema("test", location.SourceID{}, location.Span{}, "")

	result := s.FindImportAlias(location.MustNewSourceID("test://unknown"))

	assert.Equal(t, "", result)
}

func TestSchema_FindImportAlias_OwnPath(t *testing.T) {
	sourceID := location.MustNewSourceID("test://self")
	s := schema.NewSchema("test", sourceID, location.Span{}, "")

	result := s.FindImportAlias(sourceID)

	assert.Equal(t, "", result)
}

func TestSchema_Sources_Nil(t *testing.T) {
	s := schema.NewSchema("test", location.SourceID{}, location.Span{}, "")

	assert.Nil(t, s.Sources())
}

func TestSchema_Sources_Set(t *testing.T) {
	s := schema.NewSchema("test", location.SourceID{}, location.Span{}, "")
	sources := schema.NewSources(nil) // nil registry creates nil Sources

	s.SetSources(sources)

	// NewSources(nil) returns nil
	assert.Nil(t, s.Sources())
}

func TestSchema_IsSealed(t *testing.T) {
	s := schema.NewSchema("test", location.SourceID{}, location.Span{}, "")

	// New schema should not be sealed
	assert.False(t, s.IsSealed(), "new schema should not be sealed")

	// After sealing, IsSealed should return true
	s.Seal()
	assert.True(t, s.IsSealed(), "sealed schema should report IsSealed() == true")
}

func TestSchema_HasSourceProvider_NilSources(t *testing.T) {
	s := schema.NewSchema("test", location.SourceID{}, location.Span{}, "")

	assert.False(t, s.HasSourceProvider())
}

func TestSchema_HasSourceProvider_WithSources(t *testing.T) {
	s := schema.NewSchema("test", location.SourceID{}, location.Span{}, "")
	reg := source.NewRegistry()
	sources := schema.NewSources(reg)
	s.SetSources(sources)

	assert.True(t, s.HasSourceProvider())
}
