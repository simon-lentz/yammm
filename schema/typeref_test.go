package schema_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// --- TypeRef Tests ---

func TestNewTypeRef(t *testing.T) {
	span := location.Span{
		Source: location.MustNewSourceID("test://typeref"),
		Start:  location.Position{Line: 5, Column: 10, Byte: 50},
		End:    location.Position{Line: 5, Column: 20, Byte: 60},
	}

	ref := schema.NewTypeRef("users", "Person", span)

	assert.Equal(t, "users", ref.Qualifier())
	assert.Equal(t, "Person", ref.Name())
	assert.Equal(t, span, ref.Span())
	assert.True(t, ref.IsQualified())
}

func TestLocalTypeRef(t *testing.T) {
	span := location.Span{
		Source: location.MustNewSourceID("test://local"),
		Start:  location.Position{Line: 1, Column: 1, Byte: 0},
		End:    location.Position{Line: 1, Column: 10, Byte: 10},
	}

	ref := schema.LocalTypeRef("Customer", span)

	assert.Equal(t, "", ref.Qualifier())
	assert.Equal(t, "Customer", ref.Name())
	assert.Equal(t, span, ref.Span())
	assert.False(t, ref.IsQualified())
}

func TestTypeRef_Qualifier(t *testing.T) {
	tests := []struct {
		name      string
		qualifier string
		expected  string
	}{
		{"with qualifier", "pkg", "pkg"},
		{"empty qualifier", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ref := schema.NewTypeRef(tt.qualifier, "Type", location.Span{})
			assert.Equal(t, tt.expected, ref.Qualifier())
		})
	}
}

func TestTypeRef_Name(t *testing.T) {
	ref := schema.NewTypeRef("", "Employee", location.Span{})

	assert.Equal(t, "Employee", ref.Name())
}

func TestTypeRef_Span(t *testing.T) {
	span := location.Span{
		Source: location.MustNewSourceID("test://span"),
		Start:  location.Position{Line: 10, Column: 5, Byte: 100},
		End:    location.Position{Line: 10, Column: 15, Byte: 110},
	}

	ref := schema.NewTypeRef("", "Type", span)

	result := ref.Span()
	assert.Equal(t, span.Source, result.Source)
	assert.Equal(t, 10, result.Start.Line)
}

func TestTypeRef_IsQualified(t *testing.T) {
	qualified := schema.NewTypeRef("alias", "Type", location.Span{})
	unqualified := schema.NewTypeRef("", "Type", location.Span{})

	assert.True(t, qualified.IsQualified())
	assert.False(t, unqualified.IsQualified())
}

func TestTypeRef_IsZero(t *testing.T) {
	zero := schema.TypeRef{}
	nonZero := schema.NewTypeRef("", "Type", location.Span{})

	assert.True(t, zero.IsZero())
	assert.False(t, nonZero.IsZero())
}

func TestTypeRef_IsZero_WithSpan(t *testing.T) {
	// Not zero if span is non-zero
	span := location.Span{Start: location.Position{Line: 1}}
	ref := schema.NewTypeRef("", "", span)

	assert.False(t, ref.IsZero())
}

func TestTypeRef_String_Qualified(t *testing.T) {
	ref := schema.NewTypeRef("common", "Money", location.Span{})

	assert.Equal(t, "common.Money", ref.String())
}

func TestTypeRef_String_Local(t *testing.T) {
	ref := schema.NewTypeRef("", "Customer", location.Span{})

	assert.Equal(t, "Customer", ref.String())
}

// --- DataTypeRef Tests ---

func TestNewDataTypeRef(t *testing.T) {
	span := location.Span{
		Source: location.MustNewSourceID("test://datatype"),
		Start:  location.Position{Line: 3, Column: 5, Byte: 30},
		End:    location.Position{Line: 3, Column: 15, Byte: 40},
	}

	ref := schema.NewDataTypeRef("types", "Email", span)

	assert.Equal(t, "types", ref.Qualifier())
	assert.Equal(t, "Email", ref.Name())
	assert.Equal(t, span, ref.Span())
	assert.True(t, ref.IsQualified())
}

func TestLocalDataTypeRef(t *testing.T) {
	span := location.Span{
		Source: location.MustNewSourceID("test://local-dt"),
		Start:  location.Position{Line: 2, Column: 1, Byte: 10},
		End:    location.Position{Line: 2, Column: 10, Byte: 20},
	}

	ref := schema.LocalDataTypeRef("PhoneNumber", span)

	assert.Equal(t, "", ref.Qualifier())
	assert.Equal(t, "PhoneNumber", ref.Name())
	assert.Equal(t, span, ref.Span())
	assert.False(t, ref.IsQualified())
}

func TestDataTypeRef_Accessors(t *testing.T) {
	span := location.Span{
		Source: location.MustNewSourceID("test://dt-accessors"),
		Start:  location.Position{Line: 5, Column: 1, Byte: 50},
		End:    location.Position{Line: 5, Column: 20, Byte: 70},
	}

	ref := schema.NewDataTypeRef("common", "Currency", span)

	assert.Equal(t, "common", ref.Qualifier())
	assert.Equal(t, "Currency", ref.Name())
	assert.Equal(t, span, ref.Span())
}

func TestDataTypeRef_IsQualified(t *testing.T) {
	qualified := schema.NewDataTypeRef("pkg", "DataType", location.Span{})
	unqualified := schema.NewDataTypeRef("", "DataType", location.Span{})

	assert.True(t, qualified.IsQualified())
	assert.False(t, unqualified.IsQualified())
}

func TestDataTypeRef_IsZero(t *testing.T) {
	zero := schema.DataTypeRef{}
	nonZero := schema.NewDataTypeRef("", "Type", location.Span{})

	assert.True(t, zero.IsZero())
	assert.False(t, nonZero.IsZero())
}

func TestDataTypeRef_String_Qualified(t *testing.T) {
	ref := schema.NewDataTypeRef("common", "Money", location.Span{})

	assert.Equal(t, "common.Money", ref.String())
}

func TestDataTypeRef_String_Local(t *testing.T) {
	ref := schema.NewDataTypeRef("", "Email", location.Span{})

	assert.Equal(t, "Email", ref.String())
}

// --- ResolvedTypeRef Tests ---

func TestNewResolvedTypeRef(t *testing.T) {
	sourceID := location.MustNewSourceID("test://schema")
	ref := schema.NewTypeRef("pkg", "Type", location.Span{})
	id := schema.NewTypeID(sourceID, "Type")

	resolved := schema.NewResolvedTypeRef(ref, id)

	assert.Equal(t, ref, resolved.Ref())
	assert.Equal(t, id, resolved.ID())
}

func TestResolvedTypeRef_Ref(t *testing.T) {
	ref := schema.NewTypeRef("users", "Person", location.Span{})
	id := schema.NewTypeID(location.MustNewSourceID("test://users"), "Person")

	resolved := schema.NewResolvedTypeRef(ref, id)

	result := resolved.Ref()
	assert.Equal(t, "users", result.Qualifier())
	assert.Equal(t, "Person", result.Name())
}

func TestResolvedTypeRef_ID(t *testing.T) {
	sourceID := location.MustNewSourceID("test://schema")
	ref := schema.NewTypeRef("", "Customer", location.Span{})
	id := schema.NewTypeID(sourceID, "Customer")

	resolved := schema.NewResolvedTypeRef(ref, id)

	result := resolved.ID()
	assert.Equal(t, sourceID, result.SchemaPath())
	assert.Equal(t, "Customer", result.Name())
}

func TestResolvedTypeRef_Name(t *testing.T) {
	ref := schema.NewTypeRef("alias", "Employee", location.Span{})
	id := schema.TypeID{}

	resolved := schema.NewResolvedTypeRef(ref, id)

	assert.Equal(t, "Employee", resolved.Name())
}

func TestResolvedTypeRef_Qualifier(t *testing.T) {
	ref := schema.NewTypeRef("org", "Department", location.Span{})
	id := schema.TypeID{}

	resolved := schema.NewResolvedTypeRef(ref, id)

	assert.Equal(t, "org", resolved.Qualifier())
}

func TestResolvedTypeRef_Span(t *testing.T) {
	span := location.Span{
		Source: location.MustNewSourceID("test://span"),
		Start:  location.Position{Line: 5, Column: 1, Byte: 50},
		End:    location.Position{Line: 5, Column: 20, Byte: 70},
	}
	ref := schema.NewTypeRef("", "Type", span)
	id := schema.TypeID{}

	resolved := schema.NewResolvedTypeRef(ref, id)

	result := resolved.Span()
	assert.Equal(t, span.Source, result.Source)
	assert.Equal(t, 5, result.Start.Line)
}

func TestResolvedTypeRef_String(t *testing.T) {
	ref := schema.NewTypeRef("common", "Entity", location.Span{})
	id := schema.TypeID{}

	resolved := schema.NewResolvedTypeRef(ref, id)

	assert.Equal(t, "common.Entity", resolved.String())
}

func TestResolvedTypeRef_String_Local(t *testing.T) {
	ref := schema.NewTypeRef("", "LocalType", location.Span{})
	id := schema.TypeID{}

	resolved := schema.NewResolvedTypeRef(ref, id)

	assert.Equal(t, "LocalType", resolved.String())
}

func TestResolvedTypeRef_IsZero(t *testing.T) {
	zero := schema.ResolvedTypeRef{}

	assert.True(t, zero.IsZero())
}

func TestResolvedTypeRef_IsZero_NonZeroRef(t *testing.T) {
	ref := schema.NewTypeRef("", "Type", location.Span{})
	id := schema.TypeID{}

	resolved := schema.NewResolvedTypeRef(ref, id)

	assert.False(t, resolved.IsZero())
}

func TestResolvedTypeRef_IsZero_NonZeroID(t *testing.T) {
	ref := schema.TypeRef{}
	id := schema.NewTypeID(location.MustNewSourceID("test://schema"), "Type")

	resolved := schema.NewResolvedTypeRef(ref, id)

	assert.False(t, resolved.IsZero())
}

func TestResolvedTypeRef_IsLocal(t *testing.T) {
	localRef := schema.NewTypeRef("", "LocalType", location.Span{})
	qualifiedRef := schema.NewTypeRef("pkg", "ImportedType", location.Span{})
	id := schema.TypeID{}

	localResolved := schema.NewResolvedTypeRef(localRef, id)
	qualifiedResolved := schema.NewResolvedTypeRef(qualifiedRef, id)

	assert.True(t, localResolved.IsLocal())
	assert.False(t, qualifiedResolved.IsLocal())
}

func TestResolvedTypeRef_Equal_SameTypeID(t *testing.T) {
	sourceID := location.MustNewSourceID("test://schema")
	id := schema.NewTypeID(sourceID, "Person")

	// Same type resolved via different syntactic paths
	ref1 := schema.NewTypeRef("", "Person", location.Span{})
	ref2 := schema.NewTypeRef("users", "Person", location.Span{})

	resolved1 := schema.NewResolvedTypeRef(ref1, id)
	resolved2 := schema.NewResolvedTypeRef(ref2, id)

	assert.True(t, resolved1.Equal(resolved2))
	assert.True(t, resolved2.Equal(resolved1)) // symmetric
}

func TestResolvedTypeRef_Equal_DifferentTypeID(t *testing.T) {
	sourceID1 := location.MustNewSourceID("test://schema1")
	sourceID2 := location.MustNewSourceID("test://schema2")
	id1 := schema.NewTypeID(sourceID1, "Person")
	id2 := schema.NewTypeID(sourceID2, "Person")

	ref1 := schema.NewTypeRef("", "Person", location.Span{})
	ref2 := schema.NewTypeRef("", "Person", location.Span{})

	resolved1 := schema.NewResolvedTypeRef(ref1, id1)
	resolved2 := schema.NewResolvedTypeRef(ref2, id2)

	// Same name but different schema sources = not equal
	assert.False(t, resolved1.Equal(resolved2))
}

func TestResolvedTypeRef_Equal_DifferentNames(t *testing.T) {
	sourceID := location.MustNewSourceID("test://schema")
	id1 := schema.NewTypeID(sourceID, "Person")
	id2 := schema.NewTypeID(sourceID, "Employee")

	ref1 := schema.NewTypeRef("", "Person", location.Span{})
	ref2 := schema.NewTypeRef("", "Employee", location.Span{})

	resolved1 := schema.NewResolvedTypeRef(ref1, id1)
	resolved2 := schema.NewResolvedTypeRef(ref2, id2)

	// Same source but different type names = not equal
	assert.False(t, resolved1.Equal(resolved2))
}

func TestResolvedTypeRef_Equal_ZeroValues(t *testing.T) {
	zero1 := schema.ResolvedTypeRef{}
	zero2 := schema.ResolvedTypeRef{}

	// Two zero values are equal
	assert.True(t, zero1.Equal(zero2))
}

func TestResolvedTypeRef_Equal_Reflexive(t *testing.T) {
	sourceID := location.MustNewSourceID("test://schema")
	id := schema.NewTypeID(sourceID, "Type")
	ref := schema.NewTypeRef("", "Type", location.Span{})

	resolved1 := schema.NewResolvedTypeRef(ref, id)
	resolved2 := schema.NewResolvedTypeRef(ref, id)

	// Reflexivity: same construction produces equal values
	assert.True(t, resolved1.Equal(resolved2))
}

func TestResolvedTypeRef_Equal_IgnoresSyntacticDifferences(t *testing.T) {
	sourceID := location.MustNewSourceID("test://common")
	id := schema.NewTypeID(sourceID, "Entity")

	// Different spans (different source locations)
	span1 := location.Span{
		Source: location.MustNewSourceID("test://file1"),
		Start:  location.Position{Line: 1, Column: 1, Byte: 0},
		End:    location.Position{Line: 1, Column: 10, Byte: 10},
	}
	span2 := location.Span{
		Source: location.MustNewSourceID("test://file2"),
		Start:  location.Position{Line: 5, Column: 5, Byte: 50},
		End:    location.Position{Line: 5, Column: 15, Byte: 60},
	}

	// Different qualifiers (one local, one via import alias)
	ref1 := schema.NewTypeRef("", "Entity", span1)
	ref2 := schema.NewTypeRef("common", "Entity", span2)

	resolved1 := schema.NewResolvedTypeRef(ref1, id)
	resolved2 := schema.NewResolvedTypeRef(ref2, id)

	// Same TypeID means equal, regardless of syntactic differences
	assert.True(t, resolved1.Equal(resolved2))
}

// --- ResolvedTypeRefFromType Tests ---

func TestResolvedTypeRefFromType_SameSchema(t *testing.T) {
	// Type and viewing perspective are in the same schema → no qualifier
	sourceID := location.MustNewSourceID("test://app")
	span := location.Span{
		Source: sourceID,
		Start:  location.Position{Line: 1, Column: 1, Byte: 0},
		End:    location.Position{Line: 1, Column: 10, Byte: 10},
	}

	typ := schema.NewType("User", sourceID, span, "", false, false)
	typ.SetSchemaName("app")

	resolved := schema.ResolvedTypeRefFromType(typ, "test://app")

	assert.Equal(t, "", resolved.Qualifier())
	assert.Equal(t, "User", resolved.Name())
	assert.Equal(t, "User", resolved.String())
	assert.True(t, resolved.IsLocal())
}

func TestResolvedTypeRefFromType_DifferentSchema(t *testing.T) {
	// Type is in app schema but viewing from common schema → uses schema name as qualifier
	appSourceID := location.MustNewSourceID("test://app")
	span := location.Span{
		Source: appSourceID,
		Start:  location.Position{Line: 1, Column: 1, Byte: 0},
		End:    location.Position{Line: 1, Column: 10, Byte: 10},
	}

	typ := schema.NewType("User", appSourceID, span, "", false, false)
	typ.SetSchemaName("app")

	resolved := schema.ResolvedTypeRefFromType(typ, "test://common")

	assert.Equal(t, "app", resolved.Qualifier())
	assert.Equal(t, "User", resolved.Name())
	assert.Equal(t, "app.User", resolved.String())
	assert.False(t, resolved.IsLocal())
}

func TestResolvedTypeRefFromType_PreservesTypeID(t *testing.T) {
	sourceID := location.MustNewSourceID("test://schema")
	span := location.Span{
		Source: sourceID,
		Start:  location.Position{Line: 5, Column: 1, Byte: 40},
		End:    location.Position{Line: 5, Column: 20, Byte: 60},
	}

	typ := schema.NewType("Entity", sourceID, span, "", false, false)
	typ.SetSchemaName("schema")

	resolved := schema.ResolvedTypeRefFromType(typ, "test://other")

	// Verify TypeID is correctly set
	assert.Equal(t, sourceID, resolved.ID().SchemaPath())
	assert.Equal(t, "Entity", resolved.ID().Name())
}

func TestResolvedTypeRefFromType_PreservesSpan(t *testing.T) {
	sourceID := location.MustNewSourceID("test://schema")
	span := location.Span{
		Source: sourceID,
		Start:  location.Position{Line: 10, Column: 5, Byte: 100},
		End:    location.Position{Line: 10, Column: 25, Byte: 120},
	}

	typ := schema.NewType("Person", sourceID, span, "", false, false)
	typ.SetSchemaName("schema")

	resolved := schema.ResolvedTypeRefFromType(typ, "test://schema")

	// Verify span is preserved
	assert.Equal(t, span.Source, resolved.Span().Source)
	assert.Equal(t, 10, resolved.Span().Start.Line)
	assert.Equal(t, 5, resolved.Span().Start.Column)
}
