package schema_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/simon-lentz/yammm/internal/source"
	"github.com/simon-lentz/yammm/internal/yammmtest"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// TestSchema_Seal_PreventsMutation drives every post-seal mutation through
// one panic table: a sealed Schema must reject each setter.
func TestSchema_Seal_PreventsMutation(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(s *schema.Schema)
	}{
		{"SetTypes", func(s *schema.Schema) { schema.TestSetSchemaTypes(s, []*schema.Type{}) }},
		{"SetDataTypes", func(s *schema.Schema) { schema.TestSetSchemaDataTypes(s, []*schema.DataType{}) }},
		{"SetImports", func(s *schema.Schema) { schema.TestSetSchemaImports(s, []*schema.Import{}) }},
		{"SetSources", func(s *schema.Schema) { schema.TestSetSchemaSources(s, nil) }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := schema.TestNewSchema("test", location.SourceID{}, location.Span{}, "")
			schema.TestSealSchema(s)
			yammmtest.AssertPanics(t, func() { tt.mutate(s) })
		})
	}
}

func TestSchema_SettersWorkBeforeSeal(t *testing.T) {
	s := schema.TestNewSchema("test", location.SourceID{}, location.Span{}, "")

	// These should not panic before sealing
	schema.TestSetSchemaTypes(s, []*schema.Type{})
	schema.TestSetSchemaDataTypes(s, []*schema.DataType{})
	schema.TestSetSchemaImports(s, []*schema.Import{})
	schema.TestSetSchemaSources(s, nil)

	// Verify no panic occurred by reaching this point
}

func TestNewSchema(t *testing.T) {
	sourceID := location.MustNewSourceID("test://schema")
	span := location.Span{
		Source: sourceID,
		Start:  location.Position{Line: 1, Column: 1, Byte: 0},
		End:    location.Position{Line: 50, Column: 1, Byte: 500},
	}

	s := schema.TestNewSchema("users", sourceID, span, "User management schema")

	assert.NotNil(t, s)
	assert.Equal(t, "users", s.Name())
	assert.Equal(t, sourceID, s.SourceID())
	assert.Equal(t, span, s.Span())
	assert.Equal(t, "User management schema", s.Documentation())
}

func TestSchema_Name(t *testing.T) {
	s := schema.TestNewSchema("myschema", location.SourceID{}, location.Span{}, "")

	assert.Equal(t, "myschema", s.Name())
}

func TestSchema_SourceID(t *testing.T) {
	sourceID := location.MustNewSourceID("test://source")

	s := schema.TestNewSchema("test", sourceID, location.Span{}, "")

	assert.Equal(t, sourceID, s.SourceID())
}

func TestSchema_Span(t *testing.T) {
	span := location.Span{
		Source: location.MustNewSourceID("test://span"),
		Start:  location.Position{Line: 1, Column: 1, Byte: 0},
		End:    location.Position{Line: 100, Column: 1, Byte: 1000},
	}

	s := schema.TestNewSchema("test", location.SourceID{}, span, "")

	result := s.Span()
	assert.Equal(t, span.Start.Line, result.Start.Line)
	assert.Equal(t, span.End.Byte, result.End.Byte)
}

func TestSchema_Documentation(t *testing.T) {
	s := schema.TestNewSchema("test", location.SourceID{}, location.Span{}, "Schema documentation")

	assert.Equal(t, "Schema documentation", s.Documentation())
}

func TestSchema_Type_Found(t *testing.T) {
	s := schema.TestNewSchema("test", location.SourceID{}, location.Span{}, "")
	typ := schema.TestNewType("Person", location.SourceID{}, location.Span{}, "", false, false)
	schema.TestSetSchemaTypes(s, []*schema.Type{typ})

	result, ok := s.Type("Person")

	assert.True(t, ok)
	assert.Same(t, typ, result)
}

func TestSchema_Type_NotFound(t *testing.T) {
	s := schema.TestNewSchema("test", location.SourceID{}, location.Span{}, "")

	result, ok := s.Type("NonExistent")

	assert.False(t, ok)
	assert.Nil(t, result)
}

func TestSchema_Types_Iterator(t *testing.T) {
	s := schema.TestNewSchema("test", location.SourceID{}, location.Span{}, "")
	t1 := schema.TestNewType("Type1", location.SourceID{}, location.Span{}, "", false, false)
	t2 := schema.TestNewType("Type2", location.SourceID{}, location.Span{}, "", false, false)
	schema.TestSetSchemaTypes(s, []*schema.Type{t1, t2})

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
	s := schema.TestNewSchema("test", location.SourceID{}, location.Span{}, "")
	t1 := schema.TestNewType("Type1", location.SourceID{}, location.Span{}, "", false, false)
	schema.TestSetSchemaTypes(s, []*schema.Type{t1})

	result := s.TypesSlice()

	assert.Len(t, result, 1)
	assert.Same(t, t1, result[0])
}

func TestSchema_TypesSlice_DefensiveCopy(t *testing.T) {
	s := schema.TestNewSchema("test", location.SourceID{}, location.Span{}, "")
	t1 := schema.TestNewType("Type1", location.SourceID{}, location.Span{}, "", false, false)
	schema.TestSetSchemaTypes(s, []*schema.Type{t1})

	slice1 := s.TypesSlice()
	slice2 := s.TypesSlice()

	slice1[0] = nil
	assert.NotNil(t, slice2[0])
}

func TestSchema_ResolveType_Local(t *testing.T) {
	s := schema.TestNewSchema("test", location.SourceID{}, location.Span{}, "")
	typ := schema.TestNewType("Customer", location.SourceID{}, location.Span{}, "", false, false)
	schema.TestSetSchemaTypes(s, []*schema.Type{typ})

	ref := schema.NewTypeRef("", "Customer", location.Span{})
	result, ok := s.ResolveType(ref)

	assert.True(t, ok)
	assert.Same(t, typ, result)
}

func TestSchema_ResolveType_LocalNotFound(t *testing.T) {
	s := schema.TestNewSchema("test", location.SourceID{}, location.Span{}, "")

	ref := schema.NewTypeRef("", "NonExistent", location.Span{})
	result, ok := s.ResolveType(ref)

	assert.False(t, ok)
	assert.Nil(t, result)
}

func TestSchema_ResolveType_QualifiedNoImport(t *testing.T) {
	s := schema.TestNewSchema("test", location.SourceID{}, location.Span{}, "")

	ref := schema.NewTypeRef("users", "Person", location.Span{})
	result, ok := s.ResolveType(ref)

	assert.False(t, ok)
	assert.Nil(t, result)
}

func TestSchema_DataType_Found(t *testing.T) {
	s := schema.TestNewSchema("test", location.SourceID{}, location.Span{}, "")
	dt := schema.TestNewDataType("Email", schema.NewStringConstraint(), location.Span{}, "")
	schema.TestSetSchemaDataTypes(s, []*schema.DataType{dt})

	result, ok := s.DataType("Email")

	assert.True(t, ok)
	assert.Same(t, dt, result)
}

func TestSchema_DataType_NotFound(t *testing.T) {
	s := schema.TestNewSchema("test", location.SourceID{}, location.Span{}, "")

	result, ok := s.DataType("NonExistent")

	assert.False(t, ok)
	assert.Nil(t, result)
}

func TestSchema_DataTypes_Iterator(t *testing.T) {
	s := schema.TestNewSchema("test", location.SourceID{}, location.Span{}, "")
	dt1 := schema.TestNewDataType("Email", nil, location.Span{}, "")
	dt2 := schema.TestNewDataType("Phone", nil, location.Span{}, "")
	schema.TestSetSchemaDataTypes(s, []*schema.DataType{dt1, dt2})

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
	s := schema.TestNewSchema("test", location.SourceID{}, location.Span{}, "")
	dt := schema.TestNewDataType("Money", nil, location.Span{}, "")
	schema.TestSetSchemaDataTypes(s, []*schema.DataType{dt})

	result := s.DataTypesSlice()

	assert.Len(t, result, 1)
	assert.Same(t, dt, result[0])
}

func TestSchema_ResolveDataType_Local(t *testing.T) {
	s := schema.TestNewSchema("test", location.SourceID{}, location.Span{}, "")
	dt := schema.TestNewDataType("Currency", nil, location.Span{}, "")
	schema.TestSetSchemaDataTypes(s, []*schema.DataType{dt})

	ref := schema.NewDataTypeRef("", "Currency", location.Span{})
	result, ok := s.ResolveDataType(ref)

	assert.True(t, ok)
	assert.Same(t, dt, result)
}

func TestSchema_ResolveDataType_LocalNotFound(t *testing.T) {
	s := schema.TestNewSchema("test", location.SourceID{}, location.Span{}, "")

	ref := schema.NewDataTypeRef("", "NonExistent", location.Span{})
	result, ok := s.ResolveDataType(ref)

	assert.False(t, ok)
	assert.Nil(t, result)
}

func TestSchema_ResolveDataType_QualifiedNoImport(t *testing.T) {
	s := schema.TestNewSchema("test", location.SourceID{}, location.Span{}, "")

	ref := schema.NewDataTypeRef("types", "Email", location.Span{})
	result, ok := s.ResolveDataType(ref)

	assert.False(t, ok)
	assert.Nil(t, result)
}

func TestSchema_Imports_Iterator(t *testing.T) {
	s := schema.TestNewSchema("test", location.SourceID{}, location.Span{}, "")
	imp1 := schema.TestNewImport("./types.yammm", "types", location.SourceID{}, location.Span{})
	imp2 := schema.TestNewImport("./users.yammm", "users", location.SourceID{}, location.Span{})
	schema.TestSetSchemaImports(s, []*schema.Import{imp1, imp2})

	count := 0
	for imp := range s.Imports() {
		assert.NotNil(t, imp)
		count++
	}
	assert.Equal(t, 2, count)
}

func TestSchema_ImportsSlice(t *testing.T) {
	s := schema.TestNewSchema("test", location.SourceID{}, location.Span{}, "")
	imp := schema.TestNewImport("./types.yammm", "types", location.SourceID{}, location.Span{})
	schema.TestSetSchemaImports(s, []*schema.Import{imp})

	result := s.ImportsSlice()

	assert.Len(t, result, 1)
	assert.Same(t, imp, result[0])
}

func TestSchema_ImportsSlice_DefensiveCopy(t *testing.T) {
	s := schema.TestNewSchema("test", location.SourceID{}, location.Span{}, "")
	imp := schema.TestNewImport("./types.yammm", "types", location.SourceID{}, location.Span{})
	schema.TestSetSchemaImports(s, []*schema.Import{imp})

	slice1 := s.ImportsSlice()
	slice2 := s.ImportsSlice()

	slice1[0] = nil
	assert.NotNil(t, slice2[0])
}

func TestSchema_ImportByAlias_Found(t *testing.T) {
	s := schema.TestNewSchema("test", location.SourceID{}, location.Span{}, "")
	imp := schema.TestNewImport("./types.yammm", "types", location.SourceID{}, location.Span{})
	schema.TestSetSchemaImports(s, []*schema.Import{imp})

	result, ok := s.ImportByAlias("types")

	assert.True(t, ok)
	assert.Same(t, imp, result)
}

func TestSchema_ImportByAlias_NotFound(t *testing.T) {
	s := schema.TestNewSchema("test", location.SourceID{}, location.Span{}, "")

	result, ok := s.ImportByAlias("nonexistent")

	assert.False(t, ok)
	assert.Nil(t, result)
}

func TestSchema_FindImportAlias_Found(t *testing.T) {
	s := schema.TestNewSchema("test", location.SourceID{}, location.Span{}, "")
	resolvedID := location.MustNewSourceID("test://types")
	imp := schema.TestNewImport("./types.yammm", "types", resolvedID, location.Span{})
	schema.TestSetSchemaImports(s, []*schema.Import{imp})

	result := s.FindImportAlias(resolvedID)

	assert.Equal(t, "types", result)
}

func TestSchema_FindImportAlias_NotFound(t *testing.T) {
	s := schema.TestNewSchema("test", location.SourceID{}, location.Span{}, "")

	result := s.FindImportAlias(location.MustNewSourceID("test://unknown"))

	assert.Empty(t, result)
}

func TestSchema_FindImportAlias_OwnPath(t *testing.T) {
	sourceID := location.MustNewSourceID("test://self")
	s := schema.TestNewSchema("test", sourceID, location.Span{}, "")

	result := s.FindImportAlias(sourceID)

	assert.Empty(t, result)
}

func TestSchema_Sources_Nil(t *testing.T) {
	s := schema.TestNewSchema("test", location.SourceID{}, location.Span{}, "")

	assert.Nil(t, s.Sources())
}

func TestSchema_Sources_Set(t *testing.T) {
	s := schema.TestNewSchema("test", location.SourceID{}, location.Span{}, "")
	sources := schema.NewSources(nil) // nil registry creates nil Sources

	schema.TestSetSchemaSources(s, sources)

	// NewSources(nil) returns nil
	assert.Nil(t, s.Sources())
}

func TestSchema_IsSealed(t *testing.T) {
	s := schema.TestNewSchema("test", location.SourceID{}, location.Span{}, "")

	// New schema should not be sealed
	assert.False(t, schema.TestIsSealedSchema(s), "new schema should not be sealed")

	// After sealing, IsSealed should return true
	schema.TestSealSchema(s)
	assert.True(t, schema.TestIsSealedSchema(s), "sealed schema should report IsSealed() == true")
}

func TestSchema_HasSourceProvider_NilSources(t *testing.T) {
	s := schema.TestNewSchema("test", location.SourceID{}, location.Span{}, "")

	assert.False(t, s.HasSourceProvider())
}

func TestSchema_HasSourceProvider_WithSources(t *testing.T) {
	s := schema.TestNewSchema("test", location.SourceID{}, location.Span{}, "")
	reg := source.NewRegistry()
	sources := schema.NewSources(reg)
	schema.TestSetSchemaSources(s, sources)

	assert.True(t, s.HasSourceProvider())
}
