package build_test

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/schema/build"
	"github.com/simon-lentz/yammm/schema/expr"
)

func TestBuilder_SimpleType(t *testing.T) {
	s, result := build.NewBuilder().
		WithName("test").
		AddType("Person").
		WithProperty("name", schema.NewStringConstraint()).
		WithProperty("age", schema.NewIntegerConstraint()).
		Done().
		Build()

	require.NotNil(t, s)
	assert.Equal(t, "test", s.Name())
	assert.False(t, result.HasErrors())

	typ, ok := s.Type("Person")
	require.True(t, ok)
	assert.Equal(t, "Person", typ.Name())

	// Check properties
	props := typ.PropertiesSlice()
	assert.Len(t, props, 2)
}

func TestBuilder_MultipleTypes(t *testing.T) {
	s, result := build.NewBuilder().
		WithName("multi").
		AddType("Person").
		WithProperty("name", schema.NewStringConstraint()).
		Done().
		AddType("Company").
		WithProperty("title", schema.NewStringConstraint()).
		Done().
		Build()

	require.NotNil(t, s)
	assert.False(t, result.HasErrors())

	_, ok := s.Type("Person")
	require.True(t, ok)

	_, ok = s.Type("Company")
	require.True(t, ok)
}

func TestBuilder_OptionalProperty(t *testing.T) {
	s, result := build.NewBuilder().
		WithName("test").
		AddType("Person").
		WithProperty("name", schema.NewStringConstraint()).
		WithOptionalProperty("nickname", schema.NewStringConstraint()).
		Done().
		Build()

	require.NotNil(t, s)
	assert.False(t, result.HasErrors())

	typ, ok := s.Type("Person")
	require.True(t, ok)

	for prop := range typ.Properties() {
		if prop.Name() == "nickname" {
			assert.True(t, prop.IsOptional())
		} else {
			assert.False(t, prop.IsOptional())
		}
	}
}

func TestBuilder_PrimaryKey(t *testing.T) {
	s, result := build.NewBuilder().
		WithName("test").
		AddType("Person").
		WithPrimaryKey("id", schema.NewUUIDConstraint()).
		WithProperty("name", schema.NewStringConstraint()).
		Done().
		Build()

	require.NotNil(t, s)
	assert.False(t, result.HasErrors())

	typ, ok := s.Type("Person")
	require.True(t, ok)

	for prop := range typ.Properties() {
		if prop.Name() == "id" {
			assert.True(t, prop.IsPrimaryKey())
		} else {
			assert.False(t, prop.IsPrimaryKey())
		}
	}
}

func TestBuilder_Relation(t *testing.T) {
	s, result := build.NewBuilder().
		WithName("test").
		AddType("Person").
		WithProperty("name", schema.NewStringConstraint()).
		WithRelation("employer", schema.NewTypeRef("", "Company", location.Span{}), false, false).
		Done().
		AddType("Company").
		WithProperty("title", schema.NewStringConstraint()).
		Done().
		Build()

	require.NotNil(t, s)
	assert.False(t, result.HasErrors())

	typ, ok := s.Type("Person")
	require.True(t, ok)

	// Relations are split into associations and compositions
	rels := typ.AssociationsSlice()
	require.Len(t, rels, 1)
	assert.Equal(t, "employer", rels[0].Name())
}

func TestBuilder_AbstractType(t *testing.T) {
	s, result := build.NewBuilder().
		WithName("test").
		AddType("Base").
		AsAbstract().
		WithProperty("id", schema.NewUUIDConstraint()).
		Done().
		AddType("Person").
		Extends(schema.NewTypeRef("", "Base", location.Span{})).
		WithProperty("name", schema.NewStringConstraint()).
		Done().
		Build()

	require.NotNil(t, s)
	assert.False(t, result.HasErrors())

	base, ok := s.Type("Base")
	require.True(t, ok)
	assert.True(t, base.IsAbstract())

	person, ok := s.Type("Person")
	require.True(t, ok)
	assert.False(t, person.IsAbstract())
}

func TestBuilder_PartType(t *testing.T) {
	s, result := build.NewBuilder().
		WithName("test").
		AddType("Wheel").
		AsPart().
		WithProperty("size", schema.NewIntegerConstraint()).
		Done().
		Build()

	require.NotNil(t, s)
	assert.False(t, result.HasErrors())

	wheel, ok := s.Type("Wheel")
	require.True(t, ok)
	assert.True(t, wheel.IsPart())
}

func TestBuilder_DataType(t *testing.T) {
	emailPattern := regexp.MustCompile(`^[a-z]+@[a-z]+\.[a-z]+$`)
	s, result := build.NewBuilder().
		WithName("test").
		AddDataType("Email", schema.NewPatternConstraint([]*regexp.Regexp{emailPattern})).
		AddType("Person").
		WithProperty("email", schema.NewAliasConstraint("Email", nil)).
		Done().
		Build()

	require.NotNil(t, s)
	assert.False(t, result.HasErrors())

	dt, ok := s.DataType("Email")
	require.True(t, ok)
	assert.Equal(t, "Email", dt.Name())
}

func TestBuilder_WithSourceID(t *testing.T) {
	srcID := location.MustNewSourceID("test://builder/test.yammm")

	s, result := build.NewBuilder().
		WithName("test").
		WithSourceID(srcID).
		AddType("Person").
		WithProperty("name", schema.NewStringConstraint()).
		Done().
		Build()

	require.NotNil(t, s)
	assert.False(t, result.HasErrors())
	assert.Equal(t, srcID, s.SourceID())
}

func TestBuilder_AddImportRequiresSourceID(t *testing.T) {
	// AddImport without WithSourceID should fail
	// Builder never returns Go errors; all issues are diagnostics
	s, result := build.NewBuilder().
		WithName("test").
		AddImport("./other", "other").
		AddType("Person").
		WithProperty("name", schema.NewStringConstraint()).
		Done().
		Build()

	require.Nil(t, s, "schema should be nil when errors exist")
	assert.True(t, result.HasErrors())
	// Verify diagnostic contains the message
	issues := result.IssuesSlice()
	require.NotEmpty(t, issues)
	assert.Contains(t, issues[0].Message(), "requires WithSourceID")
}

func TestBuilder_AddImportWithSourceID(t *testing.T) {
	// Create a schema to import
	importedSchema, importResult := build.NewBuilder().
		WithName("common").
		WithSourceID(location.MustNewSourceID("test://common.yammm")).
		AddType("Base").
		WithProperty("id", schema.NewUUIDConstraint()).
		Done().
		Build()
	require.NotNil(t, importedSchema)
	require.False(t, importResult.HasErrors())

	// Create a registry with the imported schema
	registry := schema.NewRegistry()
	err := registry.Register(importedSchema)
	require.NoError(t, err)

	// Build a schema that imports the other
	// Note: For builder imports, the path should match the schema name in the registry
	s, result := build.NewBuilder().
		WithName("test").
		WithSourceID(location.MustNewSourceID("test://main.yammm")).
		WithRegistry(registry).
		AddImport("common", "common"). // path="common" matches schema name
		AddType("Person").
		WithProperty("name", schema.NewStringConstraint()).
		Done().
		Build()

	require.False(t, result.HasErrors(), "result: %v", result.Messages())
	require.NotNil(t, s)
	_ = result
}

func TestBuilder_Documentation(t *testing.T) {
	s, result := build.NewBuilder().
		WithName("test").
		WithDocumentation("This is the test schema.").
		AddType("Person").
		WithTypeDocumentation("A person entity.").
		WithProperty("name", schema.NewStringConstraint()).
		Done().
		Build()

	require.NotNil(t, s)
	assert.False(t, result.HasErrors())

	assert.Equal(t, "This is the test schema.", s.Documentation())

	typ, ok := s.Type("Person")
	require.True(t, ok)
	assert.Equal(t, "A person entity.", typ.Documentation())
}

func TestBuilder_Composition(t *testing.T) {
	s, result := build.NewBuilder().
		WithName("test").
		AddType("Car").
		WithProperty("model", schema.NewStringConstraint()).
		WithComposition("wheels", schema.NewTypeRef("", "Wheel", location.Span{}), false, true).
		Done().
		AddType("Wheel").
		AsPart().
		WithProperty("size", schema.NewIntegerConstraint()).
		Done().
		Build()

	require.NotNil(t, s)
	assert.False(t, result.HasErrors())

	car, ok := s.Type("Car")
	require.True(t, ok)

	rels := car.CompositionsSlice()
	require.Len(t, rels, 1)
	assert.Equal(t, "wheels", rels[0].Name())
	assert.Equal(t, schema.RelationComposition, rels[0].Kind())
}

func TestBuilder_ConstraintTypes(t *testing.T) {
	emailPattern := regexp.MustCompile(`^[a-z]+@[a-z]+\.[a-z]+$`)

	s, result := build.NewBuilder().
		WithName("test").
		AddType("AllConstraints").
		WithProperty("str", schema.NewStringConstraintBounded(1, 100)).
		WithProperty("num", schema.NewIntegerConstraintBounded(0, true, 1000, true)).
		WithProperty("flt", schema.NewFloatConstraintBounded(0.0, true, 1.0, true)).
		WithProperty("flag", schema.NewBooleanConstraint()).
		WithProperty("id", schema.NewUUIDConstraint()).
		WithProperty("created", schema.NewTimestampConstraint()).
		WithProperty("born", schema.NewDateConstraint()).
		WithProperty("status", schema.NewEnumConstraint([]string{"active", "inactive"})).
		WithProperty("email", schema.NewPatternConstraint([]*regexp.Regexp{emailPattern})).
		WithProperty("embedding", schema.NewVectorConstraint(128)).
		Done().
		Build()

	require.NotNil(t, s)
	assert.False(t, result.HasErrors())

	typ, ok := s.Type("AllConstraints")
	require.True(t, ok)

	props := typ.PropertiesSlice()
	assert.Len(t, props, 10)
}

func TestBuilder_EmptyNameFails(t *testing.T) {
	// Building without calling WithName() should fail
	s, result := build.NewBuilder().
		AddType("Person").
		WithProperty("name", schema.NewStringConstraint()).
		Done().
		Build()

	require.Nil(t, s, "schema should be nil when name is missing")
	assert.True(t, result.HasErrors())

	issues := result.IssuesSlice()
	require.NotEmpty(t, issues)
	assert.Contains(t, issues[0].Message(), "schema name is required")
}

func TestBuilder_EmptyTypeNameFails(t *testing.T) {
	// Adding a type with empty name should fail
	s, result := build.NewBuilder().
		WithName("test").
		AddType(""). // Empty type name
		WithProperty("name", schema.NewStringConstraint()).
		Done().
		Build()

	require.Nil(t, s, "schema should be nil when type name is empty")
	assert.True(t, result.HasErrors())

	issues := result.IssuesSlice()
	require.NotEmpty(t, issues)
	assert.Contains(t, issues[0].Message(), "type name cannot be empty")
}

func TestBuilder_NilConstraintFails(t *testing.T) {
	// Adding a property with nil constraint should fail
	s, result := build.NewBuilder().
		WithName("test").
		AddType("Person").
		WithProperty("name", nil). // Nil constraint
		Done().
		Build()

	require.Nil(t, s, "schema should be nil when constraint is nil")
	assert.True(t, result.HasErrors())

	issues := result.IssuesSlice()
	require.NotEmpty(t, issues)
	assert.Contains(t, issues[0].Message(), "nil constraint")
}

func TestBuilder_EmptyPropertyNameFails(t *testing.T) {
	// Adding a property with empty name should fail
	s, result := build.NewBuilder().
		WithName("test").
		AddType("Person").
		WithProperty("", schema.NewStringConstraint()). // Empty property name
		Done().
		Build()

	require.Nil(t, s, "schema should be nil when property name is empty")
	assert.True(t, result.HasErrors())

	issues := result.IssuesSlice()
	require.NotEmpty(t, issues)
	assert.Contains(t, issues[0].Message(), "property name cannot be empty")
}

func TestBuilder_EmptyRelationNameFails(t *testing.T) {
	// Adding a relation with empty name should fail
	s, result := build.NewBuilder().
		WithName("test").
		AddType("Person").
		WithProperty("name", schema.NewStringConstraint()).
		WithRelation("", schema.NewTypeRef("", "Company", location.Span{}), false, false). // Empty relation name
		Done().
		AddType("Company").
		WithProperty("title", schema.NewStringConstraint()).
		Done().
		Build()

	require.Nil(t, s, "schema should be nil when relation name is empty")
	assert.True(t, result.HasErrors())

	issues := result.IssuesSlice()
	require.NotEmpty(t, issues)
	assert.Contains(t, issues[0].Message(), "relation name cannot be empty")
}

func TestBuilder_WithIssueLimit(t *testing.T) {
	// Test that WithIssueLimit affects the collector
	s, result := build.NewBuilder().
		WithName("test").
		WithIssueLimit(5). // Set custom limit
		AddType("Person").
		WithProperty("name", schema.NewStringConstraint()).
		Done().
		Build()

	require.NotNil(t, s)
	assert.False(t, result.HasErrors())
}

func TestBuilder_CrossSchemaInheritance(t *testing.T) {
	// Create base schema with a type to inherit from
	baseSchema, baseResult := build.NewBuilder().
		WithName("base").
		WithSourceID(location.MustNewSourceID("test://base.yammm")).
		AddType("Entity").
		AsAbstract().
		WithProperty("id", schema.NewUUIDConstraint()).
		WithProperty("created", schema.NewTimestampConstraint()).
		Done().
		Build()

	require.NotNil(t, baseSchema)
	require.False(t, baseResult.HasErrors())

	// Create registry and register base schema
	registry := schema.NewRegistry()
	err := registry.Register(baseSchema)
	require.NoError(t, err)

	// Create derived schema that imports and extends the base type
	derivedSchema, derivedResult := build.NewBuilder().
		WithName("derived").
		WithSourceID(location.MustNewSourceID("test://derived.yammm")).
		WithRegistry(registry).
		AddImport("base", "base"). // Declare import: path="base" (schema name), alias="base"
		AddType("Person").
		Extends(schema.NewTypeRef("base", "Entity", location.Span{})). // qualifier="base" matches import alias
		WithProperty("name", schema.NewStringConstraint()).
		Done().
		Build()

	require.False(t, derivedResult.HasErrors(), "derivedResult: %v", derivedResult.Messages())
	require.NotNil(t, derivedSchema)

	// Verify Person type exists
	personType, ok := derivedSchema.Type("Person")
	require.True(t, ok)

	// Verify inherited properties are accessible via AllProperties
	allProps := personType.AllPropertiesSlice()
	propNames := make([]string, len(allProps))
	for i, p := range allProps {
		propNames[i] = p.Name()
	}

	assert.Contains(t, propNames, "id", "should inherit 'id' from Entity")
	assert.Contains(t, propNames, "created", "should inherit 'created' from Entity")
	assert.Contains(t, propNames, "name", "should have own 'name' property")

	// Verify import is properly wired
	imp, ok := derivedSchema.ImportByAlias("base")
	require.True(t, ok, "should have import with alias 'base'")
	assert.Equal(t, baseSchema.SourceID(), imp.ResolvedSourceID())
	assert.Equal(t, baseSchema, imp.Schema())
}

func TestBuilder_DefaultSourceIDIsZero(t *testing.T) {
	// If WithSourceID() not called, SourceID defaults to zero
	s, result := build.NewBuilder().
		WithName("test").
		AddType("Person").
		WithProperty("name", schema.NewStringConstraint()).
		Done().
		Build()

	require.False(t, result.HasErrors())
	require.NotNil(t, s)
	assert.True(t, s.SourceID().IsZero(), "SourceID should be zero when WithSourceID() not called")
}

func TestBuilder_SyntheticSourceIDValidation_RejectsAbsolutePath(t *testing.T) {
	// Synthetic SourceID that looks like absolute path should be rejected
	s, result := build.NewBuilder().
		WithName("test").
		WithSourceID(location.NewSourceID("/absolute/path/schema.yammm")). // Invalid!
		AddType("Person").
		WithProperty("name", schema.NewStringConstraint()).
		Done().
		Build()

	require.True(t, result.HasErrors())
	assert.Nil(t, s)

	// Check for E_INVALID_SYNTHETIC_ID
	hasError := false
	for issue := range result.Issues() {
		if issue.Code() == diag.E_INVALID_SYNTHETIC_ID {
			hasError = true
			break
		}
	}
	assert.True(t, hasError, "should emit E_INVALID_SYNTHETIC_ID for absolute path")
}

func TestBuilder_SyntheticSourceIDValidation_AcceptsSchemePrefix(t *testing.T) {
	// Synthetic SourceID with scheme prefix should be accepted
	s, result := build.NewBuilder().
		WithName("test").
		WithSourceID(location.NewSourceID("test://unit/person.yammm")). // Valid!
		AddType("Person").
		WithProperty("name", schema.NewStringConstraint()).
		Done().
		Build()

	require.False(t, result.HasErrors(), "result: %v", result)
	require.NotNil(t, s)
	assert.Equal(t, "test://unit/person.yammm", s.SourceID().String())
}

func TestBuilder_FileBackedSourceIDSkipsValidation(t *testing.T) {
	// File-backed SourceIDs skip synthetic validation
	// Use SourceIDFromAbsolutePath which doesn't require file existence
	fileID, err := location.SourceIDFromAbsolutePath("/project/schemas/test.yammm")
	require.NoError(t, err)

	s, result := build.NewBuilder().
		WithName("test").
		WithSourceID(fileID).
		AddType("Person").
		WithProperty("name", schema.NewStringConstraint()).
		Done().
		Build()

	// Should succeed because file-backed IDs are not validated as synthetic
	require.False(t, result.HasErrors())
	require.NotNil(t, s)
	assert.True(t, s.SourceID().IsFilePath())
}

func TestBuilder_ImportWithoutRegistry_ReturnsError(t *testing.T) {
	// Imports require registry for resolution
	s, result := build.NewBuilder().
		WithName("test").
		WithSourceID(location.MustNewSourceID("test://test.yammm")).
		AddImport("other", "other").
		AddType("Person").
		WithProperty("name", schema.NewStringConstraint()).
		Done().
		Build()

	require.True(t, result.HasErrors())
	assert.Nil(t, s)

	// Check for E_IMPORT_RESOLVE
	hasError := false
	for issue := range result.Issues() {
		if issue.Code() == diag.E_IMPORT_RESOLVE {
			hasError = true
			break
		}
	}
	assert.True(t, hasError, "should emit E_IMPORT_RESOLVE when no registry provided")
}

func TestBuilder_ImportNotFoundInRegistry_ReturnsError(t *testing.T) {
	// Create empty registry
	registry := schema.NewRegistry()

	s, result := build.NewBuilder().
		WithName("test").
		WithSourceID(location.MustNewSourceID("test://test.yammm")).
		WithRegistry(registry).
		AddImport("nonexistent", "alias").
		AddType("Person").
		WithProperty("name", schema.NewStringConstraint()).
		Done().
		Build()

	require.True(t, result.HasErrors())
	assert.Nil(t, s)

	// Check for E_IMPORT_RESOLVE
	hasError := false
	for issue := range result.Issues() {
		if issue.Code() == diag.E_IMPORT_RESOLVE {
			hasError = true
			break
		}
	}
	assert.True(t, hasError, "should emit E_IMPORT_RESOLVE when import not found")
}

func TestBuilder_CrossSchemaRelation(t *testing.T) {
	// Create base schema with a type to reference
	baseSchema, baseResult := build.NewBuilder().
		WithName("base").
		WithSourceID(location.MustNewSourceID("test://base.yammm")).
		AddType("Organization").
		WithProperty("name", schema.NewStringConstraint()).
		Done().
		Build()

	require.NotNil(t, baseSchema)
	require.False(t, baseResult.HasErrors())

	// Create registry and register base schema
	registry := schema.NewRegistry()
	err := registry.Register(baseSchema)
	require.NoError(t, err)

	// Create derived schema with relation to imported type
	derivedSchema, derivedResult := build.NewBuilder().
		WithName("derived").
		WithSourceID(location.MustNewSourceID("test://derived.yammm")).
		WithRegistry(registry).
		AddImport("base", "base").
		AddType("Person").
		WithProperty("name", schema.NewStringConstraint()).
		WithRelation("employer", schema.NewTypeRef("base", "Organization", location.Span{}), true, false).
		Done().
		Build()

	require.False(t, derivedResult.HasErrors(), "derivedResult: %v", derivedResult.Messages())
	require.NotNil(t, derivedSchema)

	// Verify relation target is resolved
	personType, ok := derivedSchema.Type("Person")
	require.True(t, ok)

	var employerRel *schema.Relation
	for rel := range personType.Associations() {
		if rel.Name() == "employer" {
			employerRel = rel
			break
		}
	}
	require.NotNil(t, employerRel, "should have 'employer' relation")

	// The target should be the Organization type from base schema
	assert.Equal(t, "Organization", employerRel.Target().Name())
	assert.Equal(t, "base", employerRel.Target().Qualifier())
}

func TestBuilder_WithInvariant(t *testing.T) {
	// Create a simple invariant expression: age > 0
	// This constructs: (> ($ "age") 0)
	ageExpr := expr.SExpr{
		expr.Op(">"),
		expr.SExpr{expr.Op("$"), expr.NewLiteral("age")},
		expr.NewLiteral(int64(0)),
	}

	s, result := build.NewBuilder().
		WithName("test").
		AddType("Person").
		WithProperty("age", schema.NewIntegerConstraint()).
		WithInvariant("age must be positive", ageExpr, "Validates that age is greater than zero").
		Done().
		Build()

	require.NotNil(t, s)
	assert.False(t, result.HasErrors())

	typ, ok := s.Type("Person")
	require.True(t, ok)

	invariants := typ.InvariantsSlice()
	require.Len(t, invariants, 1)
	assert.Equal(t, "age must be positive", invariants[0].Name())
	assert.Equal(t, "Validates that age is greater than zero", invariants[0].Documentation())
	assert.NotNil(t, invariants[0].Expression())
}

func TestBuilder_WithInvariant_Multiple(t *testing.T) {
	// Multiple invariants on a single type
	posAge := expr.SExpr{expr.Op(">"), expr.SExpr{expr.Op("$"), expr.NewLiteral("age")}, expr.NewLiteral(int64(0))}
	maxAge := expr.SExpr{expr.Op("<"), expr.SExpr{expr.Op("$"), expr.NewLiteral("age")}, expr.NewLiteral(int64(150))}

	s, result := build.NewBuilder().
		WithName("test").
		AddType("Person").
		WithProperty("age", schema.NewIntegerConstraint()).
		WithInvariant("age must be positive", posAge, "").
		WithInvariant("age must be reasonable", maxAge, "").
		Done().
		Build()

	require.NotNil(t, s)
	assert.False(t, result.HasErrors())

	typ, ok := s.Type("Person")
	require.True(t, ok)

	invariants := typ.InvariantsSlice()
	assert.Len(t, invariants, 2)
}

func TestBuilder_WithInvariant_EmptyNameFails(t *testing.T) {
	ageExpr := expr.SExpr{expr.Op(">"), expr.SExpr{expr.Op("$"), expr.NewLiteral("age")}, expr.NewLiteral(int64(0))}

	s, result := build.NewBuilder().
		WithName("test").
		AddType("Person").
		WithProperty("age", schema.NewIntegerConstraint()).
		WithInvariant("", ageExpr, ""). // Empty name
		Done().
		Build()

	require.Nil(t, s)
	assert.True(t, result.HasErrors())

	issues := result.IssuesSlice()
	require.NotEmpty(t, issues)
	assert.Contains(t, issues[0].Message(), "invariant name cannot be empty")
}

func TestBuilder_WithInvariant_NilExpression(t *testing.T) {
	// nil expression should be rejected with E_INVALID_INVARIANT
	s, result := build.NewBuilder().
		WithName("test").
		AddType("Person").
		WithProperty("age", schema.NewIntegerConstraint()).
		WithInvariant("always true", nil, "").
		Done().
		Build()

	assert.Nil(t, s)
	assert.True(t, result.HasErrors())

	// Verify we get E_INVALID_INVARIANT
	issues := result.IssuesSlice()
	require.Len(t, issues, 1)
	assert.Equal(t, diag.E_INVALID_INVARIANT, issues[0].Code())
	assert.Contains(t, issues[0].Message(), "nil expression")
}

func TestBuilder_ZeroSourceIDWithImports_Fails(t *testing.T) {
	// Zero SourceID with imports should fail even if sourceIDSet is true
	registry := schema.NewRegistry()

	s, result := build.NewBuilder().
		WithName("test").
		WithSourceID(location.SourceID{}). // Zero value - explicitly set but zero
		WithRegistry(registry).
		AddImport("common", "common").
		AddType("Person").
		WithProperty("name", schema.NewStringConstraint()).
		Done().
		Build()

	require.Nil(t, s)
	assert.True(t, result.HasErrors())

	// Verify E_MISSING_SOURCE_ID is emitted
	hasError := false
	for issue := range result.Issues() {
		if issue.Code() == diag.E_MISSING_SOURCE_ID {
			hasError = true
			break
		}
	}
	assert.True(t, hasError, "should emit E_MISSING_SOURCE_ID for zero SourceID with imports")
}

func TestBuilder_SyntheticSourceID_WithImportResolver(t *testing.T) {
	// Create a schema to import
	importedSchema, importResult := build.NewBuilder().
		WithName("common").
		WithSourceID(location.MustNewSourceID("test://common.yammm")).
		AddType("Base").
		WithProperty("id", schema.NewUUIDConstraint()).
		Done().
		Build()

	require.NotNil(t, importedSchema)
	require.False(t, importResult.HasErrors())

	// Create a registry with the imported schema
	registry := schema.NewRegistry()
	err := registry.Register(importedSchema)
	require.NoError(t, err)

	// Create resolver for relative paths
	resolver := func(path string) (location.SourceID, bool) {
		if path == "./common" {
			return importedSchema.SourceID(), true
		}
		return location.SourceID{}, false
	}

	// Build a schema with synthetic SourceID and resolver
	s, result := build.NewBuilder().
		WithName("main").
		WithSourceID(location.MustNewSourceID("test://main.yammm")).
		WithRegistry(registry).
		WithImportResolver(resolver).
		AddImport("./common", "common"). // Relative path resolved via resolver
		AddType("Person").
		Extends(schema.NewTypeRef("common", "Base", location.Span{})).
		WithProperty("name", schema.NewStringConstraint()).
		Done().
		Build()

	require.False(t, result.HasErrors(), "result: %v", result.Messages())
	require.NotNil(t, s)

	// Verify the import was resolved
	imports := s.ImportsSlice()
	require.Len(t, imports, 1)
	assert.Equal(t, "./common", imports[0].Path())
	assert.Equal(t, "common", imports[0].Alias())
}

func TestBuilder_SyntheticSourceID_RelativePathWithoutResolver_Fails(t *testing.T) {
	// Create a schema to import
	importedSchema, importResult := build.NewBuilder().
		WithName("common").
		WithSourceID(location.MustNewSourceID("test://common.yammm")).
		AddType("Base").
		WithProperty("id", schema.NewUUIDConstraint()).
		Done().
		Build()

	require.NotNil(t, importedSchema)
	require.False(t, importResult.HasErrors())

	// Create a registry with the imported schema
	registry := schema.NewRegistry()
	err := registry.Register(importedSchema)
	require.NoError(t, err)

	// Build a schema with synthetic SourceID but NO resolver
	// Using relative path should produce helpful error
	s, result := build.NewBuilder().
		WithName("main").
		WithSourceID(location.MustNewSourceID("test://main.yammm")).
		WithRegistry(registry).
		// Note: No WithImportResolver() call
		AddImport("./common", "common"). // Relative path without resolver
		AddType("Person").
		WithProperty("name", schema.NewStringConstraint()).
		Done().
		Build()

	require.True(t, result.HasErrors())
	assert.Nil(t, s)

	// Verify helpful error message about resolver requirement
	hasResolverError := false
	for issue := range result.Issues() {
		if issue.Code() == diag.E_IMPORT_RESOLVE {
			if regexp.MustCompile(`WithImportResolver`).MatchString(issue.Message()) {
				hasResolverError = true
				break
			}
		}
	}
	assert.True(t, hasResolverError, "should emit helpful error about WithImportResolver() requirement")
}

func TestBuilder_SyntheticSourceID_SchemaNameFallback(t *testing.T) {
	// Create a schema to import using schema name (not relative path)
	importedSchema, importResult := build.NewBuilder().
		WithName("common").
		WithSourceID(location.MustNewSourceID("test://common.yammm")).
		AddType("Base").
		WithProperty("id", schema.NewUUIDConstraint()).
		Done().
		Build()

	require.NotNil(t, importedSchema)
	require.False(t, importResult.HasErrors())

	// Create a registry with the imported schema
	registry := schema.NewRegistry()
	err := registry.Register(importedSchema)
	require.NoError(t, err)

	// Build a schema using schema name (not relative path) - should work without resolver
	s, result := build.NewBuilder().
		WithName("main").
		WithSourceID(location.MustNewSourceID("test://main.yammm")).
		WithRegistry(registry).
		// No WithImportResolver() - but using schema name, not relative path
		AddImport("common", "common"). // Schema name, not relative path
		AddType("Person").
		Extends(schema.NewTypeRef("common", "Base", location.Span{})).
		WithProperty("name", schema.NewStringConstraint()).
		Done().
		Build()

	require.False(t, result.HasErrors(), "result: %v", result.Messages())
	require.NotNil(t, s)

	// Verify the import was resolved
	imports := s.ImportsSlice()
	require.Len(t, imports, 1)
	assert.Equal(t, "common", imports[0].Path())
}

func TestBuilder_DuplicateImportByResolvedSourceID(t *testing.T) {
	// Two imports that resolve to the same SourceID should fail
	// This tests the builder's enforcement of duplicate import detection

	// Create a schema to import
	commonSchema, commonResult := build.NewBuilder().
		WithName("common").
		WithSourceID(location.MustNewSourceID("test://common.yammm")).
		AddType("Base").
		WithProperty("id", schema.NewUUIDConstraint()).
		Done().
		Build()
	require.NotNil(t, commonSchema)
	require.False(t, commonResult.HasErrors())

	// Create registry and register the schema
	registry := schema.NewRegistry()
	err := registry.Register(commonSchema)
	require.NoError(t, err)

	// Create an import resolver that maps different paths to the same schema
	resolver := func(path string) (location.SourceID, bool) {
		// Both "./common" and "./lib/common" resolve to the same schema
		if path == "./common" || path == "./lib/common" {
			return commonSchema.SourceID(), true
		}
		return location.SourceID{}, false
	}

	// Build a schema that imports the same schema twice with different paths
	s, result := build.NewBuilder().
		WithName("test").
		WithSourceID(location.MustNewSourceID("test://test.yammm")).
		WithRegistry(registry).
		WithImportResolver(resolver).
		AddImport("./common", "common").        // First import
		AddImport("./lib/common", "libcommon"). // Second import - resolves to same SourceID
		AddType("Person").
		WithProperty("name", schema.NewStringConstraint()).
		Done().
		Build()

	// Should fail with E_DUPLICATE_IMPORT
	require.Nil(t, s, "schema should be nil when duplicate imports detected")
	assert.True(t, result.HasErrors())

	// Verify E_DUPLICATE_IMPORT is emitted with spec-compliant format
	hasError := false
	for issue := range result.Issues() {
		if issue.Code() == diag.E_DUPLICATE_IMPORT {
			hasError = true
			// Verify the message references the resolved schema
			assert.Contains(t, issue.Message(), "imported multiple times")
			assert.Contains(t, issue.Message(), "test://common.yammm")
			break
		}
	}
	assert.True(t, hasError, "should emit E_DUPLICATE_IMPORT when two imports resolve to the same schema")
}

// =============================================================================
// Additional Coverage Tests for resolveImportPath
// =============================================================================

func TestBuilder_FileBackedSourceID_RelativeImport(t *testing.T) {
	// Test Case 1: File-backed SourceID with relative import
	// Create a file-backed SourceID (simulating a schema loaded from disk)
	mainID, err := location.SourceIDFromAbsolutePath("/project/schemas/main.yammm")
	require.NoError(t, err)

	// Create the imported schema with file-backed SourceID
	helperID, err := location.SourceIDFromAbsolutePath("/project/schemas/helper.yammm")
	require.NoError(t, err)

	helperSchema, helperResult := build.NewBuilder().
		WithName("helper").
		WithSourceID(helperID).
		AddType("Base").
		WithProperty("id", schema.NewUUIDConstraint()).
		Done().
		Build()
	require.NotNil(t, helperSchema)
	require.False(t, helperResult.HasErrors())

	// Register helper schema
	registry := schema.NewRegistry()
	err = registry.Register(helperSchema)
	require.NoError(t, err)

	// Build main schema with file-backed SourceID and relative import
	mainSchema, mainResult := build.NewBuilder().
		WithName("main").
		WithSourceID(mainID).
		WithRegistry(registry).
		AddImport("./helper", "helper"). // Relative path from /project/schemas/
		AddType("Person").
		Extends(schema.NewTypeRef("helper", "Base", location.Span{})).
		WithProperty("name", schema.NewStringConstraint()).
		Done().
		Build()

	require.False(t, mainResult.HasErrors(), "result: %v", mainResult.Messages())
	require.NotNil(t, mainSchema)

	// Verify the import was resolved
	imports := mainSchema.ImportsSlice()
	require.Len(t, imports, 1)
	assert.Equal(t, "./helper", imports[0].Path())
}

func TestBuilder_FileBackedSourceID_RelativeImport_NotFound(t *testing.T) {
	// Test Case 1: File-backed SourceID with relative import that doesn't exist
	mainID, err := location.SourceIDFromAbsolutePath("/project/schemas/main.yammm")
	require.NoError(t, err)

	// Create empty registry (no helper registered)
	registry := schema.NewRegistry()

	// Build main schema with file-backed SourceID and relative import that won't be found
	mainSchema, mainResult := build.NewBuilder().
		WithName("main").
		WithSourceID(mainID).
		WithRegistry(registry).
		AddImport("./nonexistent", "alias"). // Won't resolve
		AddType("Person").
		WithProperty("name", schema.NewStringConstraint()).
		Done().
		Build()

	require.True(t, mainResult.HasErrors())
	assert.Nil(t, mainSchema)

	// Verify we get E_IMPORT_RESOLVE
	hasError := false
	for issue := range mainResult.Issues() {
		if issue.Code() == diag.E_IMPORT_RESOLVE {
			hasError = true
			break
		}
	}
	assert.True(t, hasError, "should emit E_IMPORT_RESOLVE when import not found")
}

func TestBuilder_FileBackedSourceID_ParentDirImport(t *testing.T) {
	// Test Case 1: File-backed SourceID with ../ relative import
	// Main schema is in /project/schemas/sub/main.yammm
	mainID, err := location.SourceIDFromAbsolutePath("/project/schemas/sub/main.yammm")
	require.NoError(t, err)

	// Helper schema is in /project/schemas/helper.yammm
	helperID, err := location.SourceIDFromAbsolutePath("/project/schemas/helper.yammm")
	require.NoError(t, err)

	helperSchema, helperResult := build.NewBuilder().
		WithName("helper").
		WithSourceID(helperID).
		AddType("Base").
		WithProperty("id", schema.NewUUIDConstraint()).
		Done().
		Build()
	require.NotNil(t, helperSchema)
	require.False(t, helperResult.HasErrors())

	// Register helper schema
	registry := schema.NewRegistry()
	err = registry.Register(helperSchema)
	require.NoError(t, err)

	// Build main schema with ../ relative import
	mainSchema, mainResult := build.NewBuilder().
		WithName("main").
		WithSourceID(mainID).
		WithRegistry(registry).
		AddImport("../helper", "helper"). // Go up one directory
		AddType("Person").
		Extends(schema.NewTypeRef("helper", "Base", location.Span{})).
		WithProperty("name", schema.NewStringConstraint()).
		Done().
		Build()

	require.False(t, mainResult.HasErrors(), "result: %v", mainResult.Messages())
	require.NotNil(t, mainSchema)
}

func TestBuilder_ImportResolver_NotFound(t *testing.T) {
	// Test Case 2: Synthetic SourceID with resolver that returns false
	registry := schema.NewRegistry()

	// Create resolver that never finds anything
	resolver := func(path string) (location.SourceID, bool) {
		return location.SourceID{}, false
	}

	s, result := build.NewBuilder().
		WithName("test").
		WithSourceID(location.MustNewSourceID("test://main.yammm")).
		WithRegistry(registry).
		WithImportResolver(resolver).
		AddImport("./missing", "alias").
		AddType("Person").
		WithProperty("name", schema.NewStringConstraint()).
		Done().
		Build()

	require.True(t, result.HasErrors())
	assert.Nil(t, s)

	// Verify E_IMPORT_RESOLVE error
	hasError := false
	for issue := range result.Issues() {
		if issue.Code() == diag.E_IMPORT_RESOLVE {
			hasError = true
			break
		}
	}
	assert.True(t, hasError, "should emit E_IMPORT_RESOLVE when resolver returns false")
}

func TestBuilder_ImportResolver_ResolvesToUnregisteredSchema(t *testing.T) {
	// Test Case 2: Resolver returns a SourceID but schema not in registry
	registry := schema.NewRegistry()

	// Create resolver that returns a SourceID for an unregistered schema
	resolver := func(path string) (location.SourceID, bool) {
		if path == "./missing" {
			return location.MustNewSourceID("test://missing.yammm"), true
		}
		return location.SourceID{}, false
	}

	s, result := build.NewBuilder().
		WithName("test").
		WithSourceID(location.MustNewSourceID("test://main.yammm")).
		WithRegistry(registry).
		WithImportResolver(resolver).
		AddImport("./missing", "alias").
		AddType("Person").
		WithProperty("name", schema.NewStringConstraint()).
		Done().
		Build()

	require.True(t, result.HasErrors())
	assert.Nil(t, s)
}

func TestBuilder_DuplicateImportByAlias(t *testing.T) {
	// Test that duplicate import aliases are detected
	commonSchema, commonResult := build.NewBuilder().
		WithName("common").
		WithSourceID(location.MustNewSourceID("test://common.yammm")).
		AddType("Base").
		WithProperty("id", schema.NewUUIDConstraint()).
		Done().
		Build()
	require.NotNil(t, commonSchema)
	require.False(t, commonResult.HasErrors())

	other, otherResult := build.NewBuilder().
		WithName("other").
		WithSourceID(location.MustNewSourceID("test://other.yammm")).
		AddType("Thing").
		WithProperty("name", schema.NewStringConstraint()).
		Done().
		Build()
	require.NotNil(t, other)
	require.False(t, otherResult.HasErrors())

	registry := schema.NewRegistry()
	require.NoError(t, registry.Register(commonSchema))
	require.NoError(t, registry.Register(other))

	// Try to import with duplicate alias
	s, result := build.NewBuilder().
		WithName("test").
		WithSourceID(location.MustNewSourceID("test://test.yammm")).
		WithRegistry(registry).
		AddImport("common", "lib").
		AddImport("other", "lib"). // Same alias as above
		AddType("Person").
		WithProperty("name", schema.NewStringConstraint()).
		Done().
		Build()

	require.True(t, result.HasErrors())
	assert.Nil(t, s)

	// Verify duplicate alias error - may be E_IMPORT_ALIAS_COLLISION or E_DUPLICATE_IMPORT
	hasError := false
	for issue := range result.Issues() {
		if issue.Code() == diag.E_IMPORT_ALIAS_COLLISION ||
			issue.Code() == diag.E_DUPLICATE_IMPORT ||
			issue.Code() == diag.E_IMPORT_RESOLVE {
			hasError = true
			break
		}
	}
	assert.True(t, hasError, "should emit import error for duplicate aliases")
}

func TestBuilder_ReservedKeywordAlias(t *testing.T) {
	// Test that reserved keywords are rejected as import aliases
	commonSchema, commonResult := build.NewBuilder().
		WithName("common").
		WithSourceID(location.MustNewSourceID("test://common.yammm")).
		AddType("Base").
		WithProperty("id", schema.NewUUIDConstraint()).
		Done().
		Build()
	require.NotNil(t, commonSchema)
	require.False(t, commonResult.HasErrors())

	registry := schema.NewRegistry()
	require.NoError(t, registry.Register(commonSchema))

	// Try to use "type" as alias (reserved keyword)
	s, result := build.NewBuilder().
		WithName("test").
		WithSourceID(location.MustNewSourceID("test://test.yammm")).
		WithRegistry(registry).
		AddImport("common", "type"). // Reserved keyword
		AddType("Person").
		WithProperty("name", schema.NewStringConstraint()).
		Done().
		Build()

	require.True(t, result.HasErrors())
	assert.Nil(t, s)

	// Verify reserved keyword error
	hasError := false
	for issue := range result.Issues() {
		if issue.Code() == diag.E_INVALID_ALIAS {
			hasError = true
			break
		}
	}
	assert.True(t, hasError, "should emit E_INVALID_ALIAS for reserved alias")
}
