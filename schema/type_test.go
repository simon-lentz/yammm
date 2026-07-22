package schema_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/simon-lentz/yammm/internal/yammmtest"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// TestType_Seal_PreventsMutation drives every post-seal mutation through one panic table:
// a sealed Type must reject each setter.
func TestType_Seal_PreventsMutation(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(s *schema.Type)
	}{
		{"SetProperties", func(s *schema.Type) { schema.TestSetTypeProperties(s, []*schema.Property{}) }},
		{"SetAssociations", func(s *schema.Type) { schema.TestSetTypeAssociations(s, []*schema.Relation{}) }},
		{"SetCompositions", func(s *schema.Type) { schema.TestSetTypeCompositions(s, []*schema.Relation{}) }},
		{"SetInvariants", func(s *schema.Type) { schema.TestSetTypeInvariants(s, []*schema.Invariant{}) }},
		{"SetAnnotations", func(s *schema.Type) { schema.TestSetTypeAnnotations(s, []*schema.Annotation{}) }},
		{"SetAllAnnotations", func(s *schema.Type) { schema.TestSetTypeAllAnnotations(s, []*schema.Annotation{}) }},
		{"SetInherits", func(s *schema.Type) { schema.TestSetTypeInherits(s, []schema.TypeRef{}) }},
		{"SetAllProperties", func(s *schema.Type) { schema.TestSetTypeAllProperties(s, []*schema.Property{}) }},
		{"SetPrimaryKeys", func(s *schema.Type) { schema.TestSetTypePrimaryKeys(s, []*schema.Property{}) }},
		{"SetAllAssociations", func(s *schema.Type) { schema.TestSetTypeAllAssociations(s, []*schema.Relation{}) }},
		{"SetAllCompositions", func(s *schema.Type) { schema.TestSetTypeAllCompositions(s, []*schema.Relation{}) }},
		{"SetSuperTypes", func(s *schema.Type) { schema.TestSetTypeSuperTypes(s, []schema.ResolvedTypeRef{}) }},
		{"SetSubTypes", func(s *schema.Type) { schema.TestSetTypeSubTypes(s, []schema.ResolvedTypeRef{}) }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := schema.TestNewType("Test", location.SourceID{}, location.Span{}, "", false, false)
			schema.TestSealType(s)
			yammmtest.AssertPanics(t, func() { tt.mutate(s) })
		})
	}
}

func TestType_SettersWorkBeforeSeal(t *testing.T) {
	typ := schema.TestNewType("Test", location.SourceID{}, location.Span{}, "", false, false)

	// These should not panic before sealing
	schema.TestSetTypeProperties(typ, []*schema.Property{})
	schema.TestSetTypeAssociations(typ, []*schema.Relation{})
	schema.TestSetTypeCompositions(typ, []*schema.Relation{})
	schema.TestSetTypeInvariants(typ, []*schema.Invariant{})
	schema.TestSetTypeInherits(typ, []schema.TypeRef{})
	schema.TestSetTypeAllProperties(typ, []*schema.Property{})
	schema.TestSetTypePrimaryKeys(typ, []*schema.Property{})
	schema.TestSetTypeAllAssociations(typ, []*schema.Relation{})
	schema.TestSetTypeAllCompositions(typ, []*schema.Relation{})
	schema.TestSetTypeSuperTypes(typ, []schema.ResolvedTypeRef{})
	schema.TestSetTypeSubTypes(typ, []schema.ResolvedTypeRef{})

	// Verify no panic occurred by reaching this point
}

func TestNewType(t *testing.T) {
	sourceID := location.MustNewSourceID("test://type")
	span := location.Span{
		Source: sourceID,
		Start:  location.Position{Line: 5, Column: 1, Byte: 50},
		End:    location.Position{Line: 20, Column: 1, Byte: 200},
	}

	typ := schema.TestNewType("Person", sourceID, span, "A person type", true, false)

	assert.NotNil(t, typ)
	assert.Equal(t, "Person", typ.Name())
	assert.Equal(t, sourceID, typ.SourceID())
	assert.Equal(t, span, typ.Span())
	assert.Equal(t, "A person type", typ.Documentation())
	assert.True(t, typ.IsAbstract())
	assert.False(t, typ.IsPart())
}

func TestType_SourceID(t *testing.T) {
	sourceID := location.MustNewSourceID("test://source")
	typ := schema.TestNewType("Test", sourceID, location.Span{}, "", false, false)

	assert.Equal(t, sourceID, typ.SourceID())
}

func TestType_SchemaName(t *testing.T) {
	typ := schema.TestNewType("Test", location.SourceID{}, location.Span{}, "", false, false)
	assert.Empty(t, typ.SchemaName()) // Initially empty

	schema.TestSetTypeSchemaName(typ, "myschema")
	assert.Equal(t, "myschema", typ.SchemaName())
}

func TestType_SetSchemaName_PanicsWhenSealed(t *testing.T) {
	typ := schema.TestNewType("Test", location.SourceID{}, location.Span{}, "", false, false)
	schema.TestSealType(typ)

	assert.Panics(t, func() {
		schema.TestSetTypeSchemaName(typ, "test")
	})
}

func TestType_Span(t *testing.T) {
	span := location.Span{
		Source: location.MustNewSourceID("test://span"),
		Start:  location.Position{Line: 10, Column: 1, Byte: 100},
		End:    location.Position{Line: 15, Column: 1, Byte: 150},
	}
	typ := schema.TestNewType("Test", location.SourceID{}, span, "", false, false)

	result := typ.Span()
	assert.Equal(t, 10, result.Start.Line)
	assert.Equal(t, 150, result.End.Byte)
}

func TestType_Documentation(t *testing.T) {
	typ := schema.TestNewType("Test", location.SourceID{}, location.Span{}, "Type documentation", false, false)

	assert.Equal(t, "Type documentation", typ.Documentation())
}

func TestType_IsAbstract(t *testing.T) {
	abstractType := schema.TestNewType("Abstract", location.SourceID{}, location.Span{}, "", true, false)
	concreteType := schema.TestNewType("Concrete", location.SourceID{}, location.Span{}, "", false, false)

	assert.True(t, abstractType.IsAbstract())
	assert.False(t, concreteType.IsAbstract())
}

func TestType_IsPart(t *testing.T) {
	partType := schema.TestNewType("Part", location.SourceID{}, location.Span{}, "", false, true)
	normalType := schema.TestNewType("Normal", location.SourceID{}, location.Span{}, "", false, false)

	assert.True(t, partType.IsPart())
	assert.False(t, normalType.IsPart())
}

func TestType_ID(t *testing.T) {
	sourceID := location.MustNewSourceID("test://schema")
	typ := schema.TestNewType("Person", sourceID, location.Span{}, "", false, false)

	id := typ.ID()
	assert.Equal(t, sourceID, id.SchemaPath())
	assert.Equal(t, "Person", id.Name())
}

func TestType_Property_Found(t *testing.T) {
	typ := schema.TestNewType("Person", location.SourceID{}, location.Span{}, "", false, false)
	prop := schema.TestNewProperty("name", location.Span{}, "", schema.NewStringConstraint(), schema.DataTypeRef{}, false, false, schema.DeclaringScope{}, nil)
	schema.TestSetTypeProperties(typ, []*schema.Property{prop})

	result, ok := typ.Property("name")

	assert.True(t, ok)
	assert.Same(t, prop, result)
}

func TestType_Property_NotFound(t *testing.T) {
	typ := schema.TestNewType("Person", location.SourceID{}, location.Span{}, "", false, false)

	result, ok := typ.Property("nonexistent")

	assert.False(t, ok)
	assert.Nil(t, result)
}

func TestType_Properties_Iterator(t *testing.T) {
	typ := schema.TestNewType("Person", location.SourceID{}, location.Span{}, "", false, false)
	p1 := schema.TestNewProperty("name", location.Span{}, "", nil, schema.DataTypeRef{}, false, false, schema.DeclaringScope{}, nil)
	p2 := schema.TestNewProperty("age", location.Span{}, "", nil, schema.DataTypeRef{}, false, false, schema.DeclaringScope{}, nil)
	schema.TestSetTypeProperties(typ, []*schema.Property{p1, p2})

	count := 0
	for prop := range typ.Properties() {
		assert.NotNil(t, prop)
		count++
	}
	assert.Equal(t, 2, count)
}

func TestType_PropertiesSlice(t *testing.T) {
	typ := schema.TestNewType("Person", location.SourceID{}, location.Span{}, "", false, false)
	p := schema.TestNewProperty("name", location.Span{}, "", nil, schema.DataTypeRef{}, false, false, schema.DeclaringScope{}, nil)
	schema.TestSetTypeProperties(typ, []*schema.Property{p})

	result := typ.PropertiesSlice()

	assert.Len(t, result, 1)
	assert.Same(t, p, result[0])
}

func TestType_AllProperties_Iterator(t *testing.T) {
	typ := schema.TestNewType("Person", location.SourceID{}, location.Span{}, "", false, false)
	p1 := schema.TestNewProperty("name", location.Span{}, "", nil, schema.DataTypeRef{}, false, false, schema.DeclaringScope{}, nil)
	p2 := schema.TestNewProperty("inherited", location.Span{}, "", nil, schema.DataTypeRef{}, false, false, schema.DeclaringScope{}, nil)
	schema.TestSetTypeAllProperties(typ, []*schema.Property{p1, p2})

	count := 0
	for prop := range typ.AllProperties() {
		assert.NotNil(t, prop)
		count++
	}
	assert.Equal(t, 2, count)
}

func TestType_AllPropertiesSlice(t *testing.T) {
	typ := schema.TestNewType("Person", location.SourceID{}, location.Span{}, "", false, false)
	p := schema.TestNewProperty("name", location.Span{}, "", nil, schema.DataTypeRef{}, false, false, schema.DeclaringScope{}, nil)
	schema.TestSetTypeAllProperties(typ, []*schema.Property{p})

	result := typ.AllPropertiesSlice()

	assert.Len(t, result, 1)
	assert.Same(t, p, result[0])
}

func TestType_PrimaryKeys_Iterator(t *testing.T) {
	typ := schema.TestNewType("Person", location.SourceID{}, location.Span{}, "", false, false)
	pk := schema.TestNewProperty("id", location.Span{}, "", nil, schema.DataTypeRef{}, false, true, schema.DeclaringScope{}, nil)
	schema.TestSetTypePrimaryKeys(typ, []*schema.Property{pk})

	count := 0
	for prop := range typ.PrimaryKeys() {
		assert.NotNil(t, prop)
		assert.True(t, prop.IsPrimaryKey())
		count++
	}
	assert.Equal(t, 1, count)
}

func TestType_PrimaryKeysSlice(t *testing.T) {
	typ := schema.TestNewType("Person", location.SourceID{}, location.Span{}, "", false, false)
	pk := schema.TestNewProperty("id", location.Span{}, "", nil, schema.DataTypeRef{}, false, true, schema.DeclaringScope{}, nil)
	schema.TestSetTypePrimaryKeys(typ, []*schema.Property{pk})

	result := typ.PrimaryKeysSlice()

	assert.Len(t, result, 1)
}

func TestType_HasPrimaryKey(t *testing.T) {
	typWithPK := schema.TestNewType("WithPK", location.SourceID{}, location.Span{}, "", false, false)
	pk := schema.TestNewProperty("id", location.Span{}, "", nil, schema.DataTypeRef{}, false, true, schema.DeclaringScope{}, nil)
	schema.TestSetTypePrimaryKeys(typWithPK, []*schema.Property{pk})

	typWithoutPK := schema.TestNewType("NoPK", location.SourceID{}, location.Span{}, "", false, false)

	assert.True(t, typWithPK.HasPrimaryKey())
	assert.False(t, typWithoutPK.HasPrimaryKey())
}

func TestType_Relation_Found(t *testing.T) {
	typ := schema.TestNewType("Person", location.SourceID{}, location.Span{}, "", false, false)
	rel := schema.TestNewRelation(
		schema.RelationAssociation,
		"WORKS_AT",
		"worksAt",
		schema.NewTypeRef("", "Company", location.Span{}),
		schema.TypeID{},
		location.Span{},
		"",
		false, false,
		"", false, false,
		"Person",
		nil,
	)
	schema.TestSetTypeAssociations(typ, []*schema.Relation{rel})

	result, ok := typ.Relation("WORKS_AT")

	assert.True(t, ok)
	assert.Same(t, rel, result)
}

func TestType_Relation_NotFound(t *testing.T) {
	typ := schema.TestNewType("Person", location.SourceID{}, location.Span{}, "", false, false)

	result, ok := typ.Relation("NONEXISTENT")

	assert.False(t, ok)
	assert.Nil(t, result)
}

func TestType_Associations_Iterator(t *testing.T) {
	typ := schema.TestNewType("Person", location.SourceID{}, location.Span{}, "", false, false)
	rel := schema.TestNewRelation(
		schema.RelationAssociation,
		"WORKS_AT",
		"worksAt",
		schema.NewTypeRef("", "Company", location.Span{}),
		schema.TypeID{},
		location.Span{},
		"",
		false, false,
		"", false, false,
		"Person",
		nil,
	)
	schema.TestSetTypeAssociations(typ, []*schema.Relation{rel})

	count := 0
	for r := range typ.Associations() {
		assert.NotNil(t, r)
		assert.True(t, r.IsAssociation())
		count++
	}
	assert.Equal(t, 1, count)
}

func TestType_AssociationsSlice(t *testing.T) {
	typ := schema.TestNewType("Person", location.SourceID{}, location.Span{}, "", false, false)
	rel := schema.TestNewRelation(
		schema.RelationAssociation,
		"WORKS_AT",
		"worksAt",
		schema.NewTypeRef("", "Company", location.Span{}),
		schema.TypeID{},
		location.Span{},
		"",
		false, false,
		"", false, false,
		"Person",
		nil,
	)
	schema.TestSetTypeAssociations(typ, []*schema.Relation{rel})

	result := typ.AssociationsSlice()

	assert.Len(t, result, 1)
}

func TestType_AllAssociations_Iterator(t *testing.T) {
	typ := schema.TestNewType("Person", location.SourceID{}, location.Span{}, "", false, false)
	rel := schema.TestNewRelation(
		schema.RelationAssociation,
		"WORKS_AT",
		"worksAt",
		schema.NewTypeRef("", "Company", location.Span{}),
		schema.TypeID{},
		location.Span{},
		"",
		false, false,
		"", false, false,
		"Person",
		nil,
	)
	schema.TestSetTypeAllAssociations(typ, []*schema.Relation{rel})

	count := 0
	for r := range typ.AllAssociations() {
		assert.NotNil(t, r)
		count++
	}
	assert.Equal(t, 1, count)
}

func TestType_AllAssociationsSlice(t *testing.T) {
	typ := schema.TestNewType("Person", location.SourceID{}, location.Span{}, "", false, false)
	rel := schema.TestNewRelation(
		schema.RelationAssociation,
		"WORKS_AT",
		"worksAt",
		schema.NewTypeRef("", "Company", location.Span{}),
		schema.TypeID{},
		location.Span{},
		"",
		false, false,
		"", false, false,
		"Person",
		nil,
	)
	schema.TestSetTypeAllAssociations(typ, []*schema.Relation{rel})

	result := typ.AllAssociationsSlice()

	assert.Len(t, result, 1)
}

func TestType_Compositions_Iterator(t *testing.T) {
	typ := schema.TestNewType("Person", location.SourceID{}, location.Span{}, "", false, false)
	comp := schema.TestNewRelation(
		schema.RelationComposition,
		"HAS_ADDRESS",
		"addresses",
		schema.NewTypeRef("", "Address", location.Span{}),
		schema.TypeID{},
		location.Span{},
		"",
		false, true,
		"", false, false,
		"Person",
		nil,
	)
	schema.TestSetTypeCompositions(typ, []*schema.Relation{comp})

	count := 0
	for r := range typ.Compositions() {
		assert.NotNil(t, r)
		assert.True(t, r.IsComposition())
		count++
	}
	assert.Equal(t, 1, count)
}

func TestType_CompositionsSlice(t *testing.T) {
	typ := schema.TestNewType("Person", location.SourceID{}, location.Span{}, "", false, false)
	comp := schema.TestNewRelation(
		schema.RelationComposition,
		"HAS_ADDRESS",
		"addresses",
		schema.NewTypeRef("", "Address", location.Span{}),
		schema.TypeID{},
		location.Span{},
		"",
		false, true,
		"", false, false,
		"Person",
		nil,
	)
	schema.TestSetTypeCompositions(typ, []*schema.Relation{comp})

	result := typ.CompositionsSlice()

	assert.Len(t, result, 1)
}

func TestType_AllCompositions_Iterator(t *testing.T) {
	typ := schema.TestNewType("Person", location.SourceID{}, location.Span{}, "", false, false)
	comp := schema.TestNewRelation(
		schema.RelationComposition,
		"HAS_ADDRESS",
		"addresses",
		schema.NewTypeRef("", "Address", location.Span{}),
		schema.TypeID{},
		location.Span{},
		"",
		false, true,
		"", false, false,
		"Person",
		nil,
	)
	schema.TestSetTypeAllCompositions(typ, []*schema.Relation{comp})

	count := 0
	for r := range typ.AllCompositions() {
		assert.NotNil(t, r)
		count++
	}
	assert.Equal(t, 1, count)
}

func TestType_AllCompositionsSlice(t *testing.T) {
	typ := schema.TestNewType("Person", location.SourceID{}, location.Span{}, "", false, false)
	comp := schema.TestNewRelation(
		schema.RelationComposition,
		"HAS_ADDRESS",
		"addresses",
		schema.NewTypeRef("", "Address", location.Span{}),
		schema.TypeID{},
		location.Span{},
		"",
		false, true,
		"", false, false,
		"Person",
		nil,
	)
	schema.TestSetTypeAllCompositions(typ, []*schema.Relation{comp})

	result := typ.AllCompositionsSlice()

	assert.Len(t, result, 1)
}

func TestType_Invariants_Iterator(t *testing.T) {
	typ := schema.TestNewType("Person", location.SourceID{}, location.Span{}, "", false, false)
	inv := schema.TestNewInvariant("age > 0", nil, location.Span{}, "Age must be positive")
	schema.TestSetTypeInvariants(typ, []*schema.Invariant{inv})

	count := 0
	for i := range typ.Invariants() {
		assert.NotNil(t, i)
		count++
	}
	assert.Equal(t, 1, count)
}

func TestType_InvariantsSlice(t *testing.T) {
	typ := schema.TestNewType("Person", location.SourceID{}, location.Span{}, "", false, false)
	inv := schema.TestNewInvariant("age > 0", nil, location.Span{}, "")
	schema.TestSetTypeInvariants(typ, []*schema.Invariant{inv})

	result := typ.InvariantsSlice()

	assert.Len(t, result, 1)
}

func TestType_Inherits_Iterator(t *testing.T) {
	typ := schema.TestNewType("Employee", location.SourceID{}, location.Span{}, "", false, false)
	parentRef := schema.NewTypeRef("", "Person", location.Span{})
	schema.TestSetTypeInherits(typ, []schema.TypeRef{parentRef})

	count := 0
	for ref := range typ.Inherits() {
		assert.Equal(t, "Person", ref.Name())
		count++
	}
	assert.Equal(t, 1, count)
}

func TestType_InheritsSlice(t *testing.T) {
	typ := schema.TestNewType("Employee", location.SourceID{}, location.Span{}, "", false, false)
	parentRef := schema.NewTypeRef("", "Person", location.Span{})
	schema.TestSetTypeInherits(typ, []schema.TypeRef{parentRef})

	result := typ.InheritsSlice()

	assert.Len(t, result, 1)
	assert.Equal(t, "Person", result[0].Name())
}

func TestType_SuperTypes_Iterator(t *testing.T) {
	typ := schema.TestNewType("Employee", location.SourceID{}, location.Span{}, "", false, false)
	sourceID := location.MustNewSourceID("test://schema")
	resolved := schema.NewResolvedTypeRef(
		schema.NewTypeRef("", "Person", location.Span{}),
		schema.NewTypeID(sourceID, "Person"),
	)
	schema.TestSetTypeSuperTypes(typ, []schema.ResolvedTypeRef{resolved})

	count := 0
	for ref := range typ.SuperTypes() {
		assert.Equal(t, "Person", ref.Name())
		count++
	}
	assert.Equal(t, 1, count)
}

func TestType_SuperTypesSlice(t *testing.T) {
	typ := schema.TestNewType("Employee", location.SourceID{}, location.Span{}, "", false, false)
	sourceID := location.MustNewSourceID("test://schema")
	resolved := schema.NewResolvedTypeRef(
		schema.NewTypeRef("", "Person", location.Span{}),
		schema.NewTypeID(sourceID, "Person"),
	)
	schema.TestSetTypeSuperTypes(typ, []schema.ResolvedTypeRef{resolved})

	result := typ.SuperTypesSlice()

	assert.Len(t, result, 1)
}

func TestType_SubTypes_Iterator(t *testing.T) {
	typ := schema.TestNewType("Person", location.SourceID{}, location.Span{}, "", false, false)
	sourceID := location.MustNewSourceID("test://schema")
	resolved := schema.NewResolvedTypeRef(
		schema.NewTypeRef("", "Employee", location.Span{}),
		schema.NewTypeID(sourceID, "Employee"),
	)
	schema.TestSetTypeSubTypes(typ, []schema.ResolvedTypeRef{resolved})

	count := 0
	for ref := range typ.SubTypes() {
		assert.Equal(t, "Employee", ref.Name())
		count++
	}
	assert.Equal(t, 1, count)
}

func TestType_SubTypesSlice(t *testing.T) {
	typ := schema.TestNewType("Person", location.SourceID{}, location.Span{}, "", false, false)
	sourceID := location.MustNewSourceID("test://schema")
	resolved := schema.NewResolvedTypeRef(
		schema.NewTypeRef("", "Employee", location.Span{}),
		schema.NewTypeID(sourceID, "Employee"),
	)
	schema.TestSetTypeSubTypes(typ, []schema.ResolvedTypeRef{resolved})

	result := typ.SubTypesSlice()

	assert.Len(t, result, 1)
}

func TestType_IsSuperTypeOf(t *testing.T) {
	sourceID := location.MustNewSourceID("test://schema")
	personType := schema.TestNewType("Person", sourceID, location.Span{}, "", false, false)
	employeeID := schema.NewTypeID(sourceID, "Employee")
	resolved := schema.NewResolvedTypeRef(
		schema.NewTypeRef("", "Employee", location.Span{}),
		employeeID,
	)
	schema.TestSetTypeSubTypes(personType, []schema.ResolvedTypeRef{resolved})

	assert.True(t, personType.IsSuperTypeOf(employeeID))
	assert.False(t, personType.IsSuperTypeOf(schema.NewTypeID(sourceID, "Unknown")))
}

func TestType_IsSubTypeOf(t *testing.T) {
	sourceID := location.MustNewSourceID("test://schema")
	employeeType := schema.TestNewType("Employee", sourceID, location.Span{}, "", false, false)
	personID := schema.NewTypeID(sourceID, "Person")
	resolved := schema.NewResolvedTypeRef(
		schema.NewTypeRef("", "Person", location.Span{}),
		personID,
	)
	schema.TestSetTypeSuperTypes(employeeType, []schema.ResolvedTypeRef{resolved})

	assert.True(t, employeeType.IsSubTypeOf(personID))
	assert.False(t, employeeType.IsSubTypeOf(schema.NewTypeID(sourceID, "Unknown")))
}

func TestType_CanonicalPropertyMap(t *testing.T) {
	typ := schema.TestNewType("Person", location.SourceID{}, location.Span{}, "", false, false)
	p1 := schema.TestNewProperty("FirstName", location.Span{}, "", nil, schema.DataTypeRef{}, false, false, schema.DeclaringScope{}, nil)
	p2 := schema.TestNewProperty("LastName", location.Span{}, "", nil, schema.DataTypeRef{}, false, false, schema.DeclaringScope{}, nil)
	schema.TestSetTypeAllProperties(typ, []*schema.Property{p1, p2})

	result := typ.CanonicalPropertyMap()

	assert.Equal(t, "FirstName", result["firstname"])
	assert.Equal(t, "LastName", result["lastname"])
}

func TestType_CanonicalPropertyMap_AfterSeal(t *testing.T) {
	typ := schema.TestNewType("Person", location.SourceID{}, location.Span{}, "", false, false)
	p := schema.TestNewProperty("Name", location.Span{}, "", nil, schema.DataTypeRef{}, false, false, schema.DeclaringScope{}, nil)
	schema.TestSetTypeAllProperties(typ, []*schema.Property{p})
	schema.TestSealType(typ)

	result := typ.CanonicalPropertyMap()

	assert.Equal(t, "Name", result["name"])
}

func TestType_IsSealed(t *testing.T) {
	typ := schema.TestNewType("Test", location.SourceID{}, location.Span{}, "", false, false)

	// New type should not be sealed
	assert.False(t, schema.TestIsSealedType(typ), "new type should not be sealed")

	// After sealing, IsSealed should return true
	schema.TestSealType(typ)
	assert.True(t, schema.TestIsSealedType(typ), "sealed type should report IsSealed() == true")
}

// TestType_CanonicalPropertyMap_Immutability verifies that CanonicalPropertyMap
// returns a defensive copy, not the internal map. Mutations to the returned map
// should not affect the internal state.
func TestType_CanonicalPropertyMap_Immutability(t *testing.T) {
	// Build a schema with properties to test immutability
	s, result := schema.NewBuilder().
		WithName("test").
		AddType("Person").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithProperty("firstName", schema.NewStringConstraint()).
		WithProperty("lastName", schema.NewStringConstraint()).
		Done().
		Build()

	require.False(t, result.HasErrors(), "unexpected build errors: %v", result)
	require.NotNil(t, s)

	person, ok := s.Type("Person")
	require.True(t, ok)

	// Get the canonical map
	canonMap := person.CanonicalPropertyMap()
	require.NotNil(t, canonMap)
	assert.Equal(t, "firstName", canonMap["firstname"])
	assert.Equal(t, "lastName", canonMap["lastname"])

	// Mutate the returned map
	originalFirst := canonMap["firstname"]
	canonMap["firstname"] = "CORRUPTED"
	delete(canonMap, "lastname")
	canonMap["injected"] = "INJECTED"

	// Get a fresh copy - should be unchanged
	freshMap := person.CanonicalPropertyMap()
	assert.Equal(t, originalFirst, freshMap["firstname"],
		"mutation of returned map should not affect internal state")
	assert.Equal(t, "lastName", freshMap["lastname"],
		"deletion from returned map should not affect internal state")
	_, hasInjected := freshMap["injected"]
	assert.False(t, hasInjected,
		"insertion to returned map should not affect internal state")
}
