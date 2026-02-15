package schema_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// --- DeclaringScopeKind Tests ---

func TestDeclaringScopeKind_String_Type(t *testing.T) {
	kind := schema.ScopeType

	assert.Equal(t, "type", kind.String())
}

func TestDeclaringScopeKind_String_Relation(t *testing.T) {
	kind := schema.ScopeRelation

	assert.Equal(t, "relation", kind.String())
}

func TestDeclaringScopeKind_String_Unknown(t *testing.T) {
	// Use an invalid value to test default case
	kind := schema.DeclaringScopeKind(99)

	assert.Equal(t, "unknown", kind.String())
}

// --- DeclaringScope Tests ---

func TestTypeScope(t *testing.T) {
	typeRef := schema.NewTypeRef("", "Person", location.Span{})

	scope := schema.TypeScope(typeRef)

	assert.Equal(t, schema.ScopeType, scope.Kind())
	assert.True(t, scope.IsType())
	assert.False(t, scope.IsRelation())
	assert.Equal(t, typeRef, scope.TypeRef())
}

func TestRelationScope(t *testing.T) {
	scope := schema.RelationScope("WORKS_AT")

	assert.Equal(t, schema.ScopeRelation, scope.Kind())
	assert.False(t, scope.IsType())
	assert.True(t, scope.IsRelation())
	assert.Equal(t, "WORKS_AT", scope.RelationName())
}

func TestDeclaringScope_Kind(t *testing.T) {
	typeScope := schema.TypeScope(schema.NewTypeRef("", "Test", location.Span{}))
	relScope := schema.RelationScope("REL")

	assert.Equal(t, schema.ScopeType, typeScope.Kind())
	assert.Equal(t, schema.ScopeRelation, relScope.Kind())
}

func TestDeclaringScope_IsType(t *testing.T) {
	typeScope := schema.TypeScope(schema.NewTypeRef("", "Test", location.Span{}))
	relScope := schema.RelationScope("REL")

	assert.True(t, typeScope.IsType())
	assert.False(t, relScope.IsType())
}

func TestDeclaringScope_IsRelation(t *testing.T) {
	typeScope := schema.TypeScope(schema.NewTypeRef("", "Test", location.Span{}))
	relScope := schema.RelationScope("REL")

	assert.False(t, typeScope.IsRelation())
	assert.True(t, relScope.IsRelation())
}

func TestDeclaringScope_TypeRef(t *testing.T) {
	typeRef := schema.NewTypeRef("alias", "Person", location.Span{})
	scope := schema.TypeScope(typeRef)

	result := scope.TypeRef()

	assert.Equal(t, "alias", result.Qualifier())
	assert.Equal(t, "Person", result.Name())
}

func TestDeclaringScope_TypeRef_ZeroForRelation(t *testing.T) {
	scope := schema.RelationScope("REL")

	result := scope.TypeRef()

	assert.True(t, result.IsZero())
}

func TestDeclaringScope_RelationName(t *testing.T) {
	scope := schema.RelationScope("MANAGES")

	assert.Equal(t, "MANAGES", scope.RelationName())
}

func TestDeclaringScope_RelationName_EmptyForType(t *testing.T) {
	scope := schema.TypeScope(schema.NewTypeRef("", "Test", location.Span{}))

	assert.Equal(t, "", scope.RelationName())
}

func TestDeclaringScope_TypeName_Valid(t *testing.T) {
	scope := schema.TypeScope(schema.NewTypeRef("", "Employee", location.Span{}))

	assert.Equal(t, "Employee", scope.TypeName())
}

func TestDeclaringScope_TypeName_WithQualifier(t *testing.T) {
	scope := schema.TypeScope(schema.NewTypeRef("org", "Employee", location.Span{}))

	// TypeName returns just the name, not the qualifier
	assert.Equal(t, "Employee", scope.TypeName())
}

func TestDeclaringScope_TypeName_Panics(t *testing.T) {
	scope := schema.RelationScope("REL")

	assert.Panics(t, func() {
		scope.TypeName()
	})
}

func TestDeclaringScope_String_Type(t *testing.T) {
	scope := schema.TypeScope(schema.NewTypeRef("", "Person", location.Span{}))

	assert.Equal(t, "Person", scope.String())
}

func TestDeclaringScope_String_QualifiedType(t *testing.T) {
	scope := schema.TypeScope(schema.NewTypeRef("users", "Person", location.Span{}))

	assert.Equal(t, "users.Person", scope.String())
}

func TestDeclaringScope_String_Relation(t *testing.T) {
	scope := schema.RelationScope("EMPLOYS")

	assert.Equal(t, "EMPLOYS", scope.String())
}

func TestDeclaringScope_String_Unknown(t *testing.T) {
	// Create a scope with unknown kind (zero value but not initialized properly)
	scope := schema.DeclaringScope{}

	// Default kind is ScopeType (0), with empty typeRef, returns empty string
	assert.Equal(t, "", scope.String())
}

// --- Property Tests ---

func TestNewProperty(t *testing.T) {
	constraint := schema.NewStringConstraint()
	span := location.Span{
		Source: location.MustNewSourceID("test://property"),
		Start:  location.Position{Line: 5, Column: 3, Byte: 50},
		End:    location.Position{Line: 5, Column: 20, Byte: 67},
	}
	doc := "The user's name"
	scope := schema.TypeScope(schema.NewTypeRef("", "User", location.Span{}))

	p := schema.NewProperty("name", span, doc, constraint, schema.DataTypeRef{}, false, false, scope)

	assert.NotNil(t, p)
	assert.Equal(t, "name", p.Name())
	assert.Equal(t, span, p.Span())
	assert.Equal(t, doc, p.Documentation())
	assert.Equal(t, constraint, p.Constraint())
	assert.False(t, p.IsOptional())
	assert.True(t, p.IsRequired())
	assert.False(t, p.IsPrimaryKey())
	assert.Equal(t, scope, p.DeclaringScope())
}

func TestProperty_Accessors(t *testing.T) {
	constraint := schema.NewIntegerConstraint()
	span := location.Span{
		Source: location.MustNewSourceID("test://accessors"),
		Start:  location.Position{Line: 10, Column: 1, Byte: 100},
		End:    location.Position{Line: 10, Column: 15, Byte: 115},
	}
	scope := schema.RelationScope("OWNS")

	p := schema.NewProperty("quantity", span, "Item count", constraint, schema.DataTypeRef{}, true, true, scope)

	assert.Equal(t, "quantity", p.Name())
	assert.Equal(t, span, p.Span())
	assert.Equal(t, "Item count", p.Documentation())
	assert.Equal(t, constraint, p.Constraint())
	assert.True(t, p.IsOptional())
	assert.False(t, p.IsRequired())
	assert.True(t, p.IsPrimaryKey())
	assert.Equal(t, scope, p.DeclaringScope())
}

func TestProperty_IsOptional(t *testing.T) {
	optionalProp := schema.NewProperty("opt", location.Span{}, "", nil, schema.DataTypeRef{}, true, false, schema.DeclaringScope{})
	requiredProp := schema.NewProperty("req", location.Span{}, "", nil, schema.DataTypeRef{}, false, false, schema.DeclaringScope{})

	assert.True(t, optionalProp.IsOptional())
	assert.False(t, requiredProp.IsOptional())
}

func TestProperty_IsRequired(t *testing.T) {
	optionalProp := schema.NewProperty("opt", location.Span{}, "", nil, schema.DataTypeRef{}, true, false, schema.DeclaringScope{})
	requiredProp := schema.NewProperty("req", location.Span{}, "", nil, schema.DataTypeRef{}, false, false, schema.DeclaringScope{})

	assert.False(t, optionalProp.IsRequired())
	assert.True(t, requiredProp.IsRequired())
}

func TestProperty_IsPrimaryKey(t *testing.T) {
	pkProp := schema.NewProperty("id", location.Span{}, "", nil, schema.DataTypeRef{}, false, true, schema.DeclaringScope{})
	normalProp := schema.NewProperty("name", location.Span{}, "", nil, schema.DataTypeRef{}, false, false, schema.DeclaringScope{})

	assert.True(t, pkProp.IsPrimaryKey())
	assert.False(t, normalProp.IsPrimaryKey())
}

func TestProperty_Equal_Identical(t *testing.T) {
	constraint := schema.NewStringConstraint()
	p1 := schema.NewProperty("name", location.Span{}, "", constraint, schema.DataTypeRef{}, false, false, schema.DeclaringScope{})
	p2 := schema.NewProperty("name", location.Span{}, "", constraint, schema.DataTypeRef{}, false, false, schema.DeclaringScope{})

	assert.True(t, p1.Equal(p2))
	assert.True(t, p2.Equal(p1))
}

func TestProperty_Equal_DifferentName(t *testing.T) {
	constraint := schema.NewStringConstraint()
	p1 := schema.NewProperty("name", location.Span{}, "", constraint, schema.DataTypeRef{}, false, false, schema.DeclaringScope{})
	p2 := schema.NewProperty("title", location.Span{}, "", constraint, schema.DataTypeRef{}, false, false, schema.DeclaringScope{})

	assert.False(t, p1.Equal(p2))
}

func TestProperty_Equal_DifferentOptional(t *testing.T) {
	constraint := schema.NewStringConstraint()
	p1 := schema.NewProperty("name", location.Span{}, "", constraint, schema.DataTypeRef{}, true, false, schema.DeclaringScope{})
	p2 := schema.NewProperty("name", location.Span{}, "", constraint, schema.DataTypeRef{}, false, false, schema.DeclaringScope{})

	assert.False(t, p1.Equal(p2))
}

func TestProperty_Equal_DifferentPrimaryKey(t *testing.T) {
	constraint := schema.NewStringConstraint()
	p1 := schema.NewProperty("id", location.Span{}, "", constraint, schema.DataTypeRef{}, false, true, schema.DeclaringScope{})
	p2 := schema.NewProperty("id", location.Span{}, "", constraint, schema.DataTypeRef{}, false, false, schema.DeclaringScope{})

	assert.False(t, p1.Equal(p2))
}

func TestProperty_Equal_DifferentConstraint(t *testing.T) {
	p1 := schema.NewProperty("value", location.Span{}, "", schema.NewStringConstraint(), schema.DataTypeRef{}, false, false, schema.DeclaringScope{})
	p2 := schema.NewProperty("value", location.Span{}, "", schema.NewIntegerConstraint(), schema.DataTypeRef{}, false, false, schema.DeclaringScope{})

	assert.False(t, p1.Equal(p2))
}

func TestProperty_Equal_NilConstraints(t *testing.T) {
	p1 := schema.NewProperty("value", location.Span{}, "", nil, schema.DataTypeRef{}, false, false, schema.DeclaringScope{})
	p2 := schema.NewProperty("value", location.Span{}, "", nil, schema.DataTypeRef{}, false, false, schema.DeclaringScope{})

	assert.True(t, p1.Equal(p2))
}

func TestProperty_Equal_OneNilConstraint(t *testing.T) {
	p1 := schema.NewProperty("value", location.Span{}, "", nil, schema.DataTypeRef{}, false, false, schema.DeclaringScope{})
	p2 := schema.NewProperty("value", location.Span{}, "", schema.NewStringConstraint(), schema.DataTypeRef{}, false, false, schema.DeclaringScope{})

	assert.False(t, p1.Equal(p2))
}

func TestProperty_Equal_NilProperty(t *testing.T) {
	p1 := schema.NewProperty("name", location.Span{}, "", nil, schema.DataTypeRef{}, false, false, schema.DeclaringScope{})
	var p2 *schema.Property = nil

	assert.False(t, p1.Equal(p2))
	assert.False(t, p2.Equal(p1))
}

func TestProperty_Equal_BothNil(t *testing.T) {
	var p1 *schema.Property = nil
	var p2 *schema.Property = nil

	assert.True(t, p1.Equal(p2))
}

func TestProperty_Equal_IgnoresSpanAndDoc(t *testing.T) {
	constraint := schema.NewStringConstraint()
	span1 := location.Span{Start: location.Position{Line: 1}}
	span2 := location.Span{Start: location.Position{Line: 99}}

	p1 := schema.NewProperty("name", span1, "Doc 1", constraint, schema.DataTypeRef{}, false, false, schema.DeclaringScope{})
	p2 := schema.NewProperty("name", span2, "Doc 2", constraint, schema.DataTypeRef{}, false, false, schema.DeclaringScope{})

	// Equal should ignore span and doc (declaration-site specific)
	assert.True(t, p1.Equal(p2))
}
