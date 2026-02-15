package instance_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/instance/path"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/schema/expr"
)

// --- Test Helpers ---

// makeTestSchema creates a schema with the given types.
func makeTestSchema(types ...*schema.Type) *schema.Schema {
	s := schema.NewSchema("test", location.SourceID{}, location.Span{}, "")
	s.SetTypes(types)
	s.Seal()
	return s
}

// makeType creates a type with the given properties.
func makeType(name string, isAbstract, isPart bool, props ...*schema.Property) *schema.Type {
	t := schema.NewType(name, location.SourceID{}, location.Span{}, "", isAbstract, isPart)
	t.SetProperties(props)
	t.SetAllProperties(props)

	// Separate PKs
	var pks []*schema.Property
	for _, p := range props {
		if p.IsPrimaryKey() {
			pks = append(pks, p)
		}
	}
	t.SetPrimaryKeys(pks)
	t.Seal()
	return t
}

// makeProp creates a property.
func makeProp(name string, constraint schema.Constraint, optional, isPK bool) *schema.Property {
	return schema.NewProperty(
		name,
		location.Span{},
		"",
		constraint,
		schema.DataTypeRef{},
		optional,
		isPK,
		schema.DeclaringScope{},
	)
}

// --- Tests ---

func TestValidator_ValidateOne_Success(t *testing.T) {
	// Create a simple Person type with id and name
	personType := makeType("Person", false, false,
		makeProp("id", schema.NewIntegerConstraint(), false, true),
		makeProp("name", schema.NewStringConstraint(), false, false),
		makeProp("age", schema.NewIntegerConstraint(), true, false),
	)
	s := makeTestSchema(personType)

	validator := instance.NewValidator(s)

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id":   int64(1),
			"name": "Alice",
			"age":  int64(30),
		},
	}

	valid, failure, err := validator.ValidateOne(context.Background(), "Person", raw)

	require.NoError(t, err)
	assert.Nil(t, failure)
	require.NotNil(t, valid)
	assert.Equal(t, "Person", valid.TypeName())
	assert.Equal(t, "[1]", valid.PrimaryKey().String())

	// Check property access
	nameVal, ok := valid.Property("name")
	require.True(t, ok)
	name, ok := nameVal.String()
	require.True(t, ok)
	assert.Equal(t, "Alice", name)
}

func TestValidator_ValidateOne_TypeNotFound(t *testing.T) {
	s := makeTestSchema() // Empty schema

	validator := instance.NewValidator(s)

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id": int64(1),
		},
	}

	valid, failure, err := validator.ValidateOne(context.Background(), "NonExistent", raw)

	require.NoError(t, err)
	assert.Nil(t, valid)
	require.NotNil(t, failure)
	assert.Contains(t, failure.Error(), "not found")
}

func TestValidator_ValidateOne_AbstractTypeRejected(t *testing.T) {
	abstractType := makeType("Entity", true, false,
		makeProp("id", schema.NewIntegerConstraint(), false, true),
	)
	s := makeTestSchema(abstractType)

	validator := instance.NewValidator(s)

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id": int64(1),
		},
	}

	valid, failure, err := validator.ValidateOne(context.Background(), "Entity", raw)

	require.NoError(t, err)
	assert.Nil(t, valid)
	require.NotNil(t, failure)
	assert.Contains(t, failure.Error(), "abstract")
}

func TestValidator_ValidateOne_PartTypeRejected(t *testing.T) {
	partType := makeType("Part", false, true,
		makeProp("id", schema.NewIntegerConstraint(), false, true),
	)
	s := makeTestSchema(partType)

	validator := instance.NewValidator(s)

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id": int64(1),
		},
	}

	valid, failure, err := validator.ValidateOne(context.Background(), "Part", raw)

	require.NoError(t, err)
	assert.Nil(t, valid)
	require.NotNil(t, failure)
	assert.Contains(t, failure.Error(), "part type")
}

func TestValidator_ValidateOne_MissingRequiredProperty(t *testing.T) {
	personType := makeType("Person", false, false,
		makeProp("id", schema.NewIntegerConstraint(), false, true),
		makeProp("name", schema.NewStringConstraint(), false, false), // Required
	)
	s := makeTestSchema(personType)

	validator := instance.NewValidator(s)

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id": int64(1),
			// Missing "name"
		},
	}

	valid, failure, err := validator.ValidateOne(context.Background(), "Person", raw)

	require.NoError(t, err)
	assert.Nil(t, valid)
	require.NotNil(t, failure)
	assert.Contains(t, failure.Error(), "missing required")
}

func TestValidator_ValidateOne_OptionalPropertyMissing(t *testing.T) {
	personType := makeType("Person", false, false,
		makeProp("id", schema.NewIntegerConstraint(), false, true),
		makeProp("nickname", schema.NewStringConstraint(), true, false), // Optional
	)
	s := makeTestSchema(personType)

	validator := instance.NewValidator(s)

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id": int64(1),
			// Missing "nickname" is OK
		},
	}

	valid, failure, err := validator.ValidateOne(context.Background(), "Person", raw)

	require.NoError(t, err)
	assert.Nil(t, failure)
	require.NotNil(t, valid)
}

func TestValidator_ValidateOne_TypeMismatch(t *testing.T) {
	personType := makeType("Person", false, false,
		makeProp("id", schema.NewIntegerConstraint(), false, true),
		makeProp("age", schema.NewIntegerConstraint(), false, false),
	)
	s := makeTestSchema(personType)

	validator := instance.NewValidator(s)

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id":  int64(1),
			"age": "not an integer", // Wrong type
		},
	}

	valid, failure, err := validator.ValidateOne(context.Background(), "Person", raw)

	require.NoError(t, err)
	assert.Nil(t, valid)
	require.NotNil(t, failure)
	assert.Contains(t, failure.Error(), "age")
}

func TestValidator_ValidateOne_UnknownField(t *testing.T) {
	personType := makeType("Person", false, false,
		makeProp("id", schema.NewIntegerConstraint(), false, true),
	)
	s := makeTestSchema(personType)

	validator := instance.NewValidator(s) // Default: don't allow unknown fields

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id":      int64(1),
			"unknown": "value", // Unknown field
		},
	}

	valid, failure, err := validator.ValidateOne(context.Background(), "Person", raw)

	require.NoError(t, err)
	assert.Nil(t, valid)
	require.NotNil(t, failure)
	assert.Contains(t, failure.Error(), "unknown")
}

func TestValidator_ValidateOne_AllowUnknownFields(t *testing.T) {
	personType := makeType("Person", false, false,
		makeProp("id", schema.NewIntegerConstraint(), false, true),
	)
	s := makeTestSchema(personType)

	validator := instance.NewValidator(s, instance.WithAllowUnknownFields(true))

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id":      int64(1),
			"unknown": "value", // Unknown field - should be ignored
		},
	}

	valid, failure, err := validator.ValidateOne(context.Background(), "Person", raw)

	require.NoError(t, err)
	assert.Nil(t, failure)
	require.NotNil(t, valid)
}

func TestValidator_ValidateOne_CaseInsensitivePropertyMatch(t *testing.T) {
	personType := makeType("Person", false, false,
		makeProp("id", schema.NewIntegerConstraint(), false, true),
		makeProp("firstName", schema.NewStringConstraint(), false, false),
	)
	s := makeTestSchema(personType)

	validator := instance.NewValidator(s) // Default: case-insensitive

	raw := instance.RawInstance{
		Properties: map[string]any{
			"ID":        int64(1), // Different case
			"FIRSTNAME": "Alice",  // Different case
		},
	}

	valid, failure, err := validator.ValidateOne(context.Background(), "Person", raw)

	require.NoError(t, err)
	assert.Nil(t, failure)
	require.NotNil(t, valid)

	// Property should be stored with canonical name
	firstNameVal, ok := valid.Property("firstName")
	require.True(t, ok)
	firstName, ok := firstNameVal.String()
	require.True(t, ok)
	assert.Equal(t, "Alice", firstName)
}

func TestValidator_ValidateOne_StrictPropertyNames(t *testing.T) {
	personType := makeType("Person", false, false,
		makeProp("id", schema.NewIntegerConstraint(), false, true),
		makeProp("firstName", schema.NewStringConstraint(), false, false),
	)
	s := makeTestSchema(personType)

	validator := instance.NewValidator(s, instance.WithStrictPropertyNames(true))

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id":        int64(1),
			"FIRSTNAME": "Alice", // Wrong case - should fail
		},
	}

	valid, failure, err := validator.ValidateOne(context.Background(), "Person", raw)

	require.NoError(t, err)
	assert.Nil(t, valid)
	require.NotNil(t, failure)
	// Should have both "unknown field" and "missing required" errors
}

func TestValidator_Validate_BatchSuccess(t *testing.T) {
	personType := makeType("Person", false, false,
		makeProp("id", schema.NewIntegerConstraint(), false, true),
		makeProp("name", schema.NewStringConstraint(), false, false),
	)
	s := makeTestSchema(personType)

	validator := instance.NewValidator(s)

	raws := []instance.RawInstance{
		{Properties: map[string]any{"id": int64(1), "name": "Alice"}},
		{Properties: map[string]any{"id": int64(2), "name": "Bob"}},
		{Properties: map[string]any{"id": int64(3), "name": "Charlie"}},
	}

	valid, failures, err := validator.Validate(context.Background(), "Person", raws)

	require.NoError(t, err)
	assert.Len(t, failures, 0)
	assert.Len(t, valid, 3)
}

func TestValidator_Validate_PartialSuccess(t *testing.T) {
	personType := makeType("Person", false, false,
		makeProp("id", schema.NewIntegerConstraint(), false, true),
		makeProp("name", schema.NewStringConstraint(), false, false),
	)
	s := makeTestSchema(personType)

	validator := instance.NewValidator(s)

	raws := []instance.RawInstance{
		{Properties: map[string]any{"id": int64(1), "name": "Alice"}},
		{Properties: map[string]any{"id": int64(2)}}, // Missing name
		{Properties: map[string]any{"id": int64(3), "name": "Charlie"}},
	}

	valid, failures, err := validator.Validate(context.Background(), "Person", raws)

	require.NoError(t, err)
	assert.Len(t, failures, 1)
	assert.Len(t, valid, 2)
}

func TestValidator_Validate_ContextCancellation(t *testing.T) {
	personType := makeType("Person", false, false,
		makeProp("id", schema.NewIntegerConstraint(), false, true),
	)
	s := makeTestSchema(personType)

	validator := instance.NewValidator(s)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	raw := instance.RawInstance{
		Properties: map[string]any{"id": int64(1)},
	}

	_, _, err := validator.ValidateOne(ctx, "Person", raw)

	require.Error(t, err)
	assert.True(t, errors.Is(err, context.Canceled))
}

func TestValidator_ValidateOne_ImmutableOutput(t *testing.T) {
	personType := makeType("Person", false, false,
		makeProp("id", schema.NewIntegerConstraint(), false, true),
		makeProp("name", schema.NewStringConstraint(), false, false),
	)
	s := makeTestSchema(personType)

	validator := instance.NewValidator(s)

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id":   int64(1),
			"name": "Alice",
		},
	}

	valid, failure, err := validator.ValidateOne(context.Background(), "Person", raw)
	require.NoError(t, err)
	require.Nil(t, failure)
	require.NotNil(t, valid)

	// Get the name before mutation
	nameVal, ok := valid.Property("name")
	require.True(t, ok)
	nameBefore, _ := nameVal.String()

	// Mutate the original raw instance
	raw.Properties["name"] = "Bob"

	// The ValidInstance should not be affected
	nameVal, ok = valid.Property("name")
	require.True(t, ok)
	nameAfter, _ := nameVal.String()

	assert.Equal(t, nameBefore, nameAfter, "ValidInstance should be immutable")
	assert.Equal(t, "Alice", nameAfter)
}

func TestValidator_ValidateOne_IntegerBounds(t *testing.T) {
	// Create type with bounded integer
	personType := makeType("Person", false, false,
		makeProp("id", schema.NewIntegerConstraint(), false, true),
		makeProp("age", schema.NewIntegerConstraintBounded(0, true, 150, true), false, false),
	)
	s := makeTestSchema(personType)

	validator := instance.NewValidator(s)

	tests := []struct {
		name    string
		age     int64
		wantErr bool
	}{
		{"valid", 30, false},
		{"min", 0, false},
		{"max", 150, false},
		{"below min", -1, true},
		{"above max", 151, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := instance.RawInstance{
				Properties: map[string]any{
					"id":  int64(1),
					"age": tt.age,
				},
			}

			valid, failure, err := validator.ValidateOne(context.Background(), "Person", raw)
			require.NoError(t, err)

			if tt.wantErr {
				assert.Nil(t, valid)
				assert.NotNil(t, failure)
			} else {
				assert.NotNil(t, valid)
				assert.Nil(t, failure)
			}
		})
	}
}

func TestValidator_ValidateOne_StringBounds(t *testing.T) {
	// Create type with bounded string
	personType := makeType("Person", false, false,
		makeProp("id", schema.NewIntegerConstraint(), false, true),
		makeProp("code", schema.NewStringConstraintBounded(3, 10), false, false),
	)
	s := makeTestSchema(personType)

	validator := instance.NewValidator(s)

	tests := []struct {
		name    string
		code    string
		wantErr bool
	}{
		{"valid", "ABC", false},
		{"min length", "ABC", false},
		{"max length", "ABCDEFGHIJ", false},
		{"too short", "AB", true},
		{"too long", "ABCDEFGHIJK", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := instance.RawInstance{
				Properties: map[string]any{
					"id":   int64(1),
					"code": tt.code,
				},
			}

			valid, failure, err := validator.ValidateOne(context.Background(), "Person", raw)
			require.NoError(t, err)

			if tt.wantErr {
				assert.Nil(t, valid)
				assert.NotNil(t, failure)
			} else {
				assert.NotNil(t, valid)
				assert.Nil(t, failure)
			}
		})
	}
}

// --- ValidateForComposition Tests ---

// makeTypeWithRelation creates a type with a composition relation to a part type.
func makeTypeWithRelation(typeName string, partType *schema.Type, relationName string) *schema.Type {
	t := schema.NewType(typeName, location.SourceID{}, location.Span{}, "", false, false)

	idProp := makeProp("id", schema.NewIntegerConstraint(), false, true)
	t.SetProperties([]*schema.Property{idProp})
	t.SetAllProperties([]*schema.Property{idProp})
	t.SetPrimaryKeys([]*schema.Property{idProp})

	// Create composition relation to part type
	rel := schema.NewRelation(
		schema.RelationComposition,
		relationName,
		relationName, // fieldName
		schema.NewTypeRef("", partType.Name(), location.Span{}),
		partType.ID(),
		location.Span{},
		"",
		true,     // optional
		true,     // many
		"",       // backref
		true,     // reverseOptional
		false,    // reverseMany
		typeName, // owner
		nil,      // no edge properties for composition
	)
	rel.Seal()

	t.SetCompositions([]*schema.Relation{rel})
	t.SetAllCompositions([]*schema.Relation{rel})
	t.Seal()
	return t
}

func TestValidator_ValidateForComposition_Success(t *testing.T) {
	// Create a part type
	addressType := makeType("Address", false, true, // isPart = true
		makeProp("id", schema.NewIntegerConstraint(), false, true),
		makeProp("street", schema.NewStringConstraint(), false, false),
	)

	// Create parent type with composition
	personType := makeTypeWithRelation("Person", addressType, "addresses")

	s := schema.NewSchema("test", location.SourceID{}, location.Span{}, "")
	s.SetTypes([]*schema.Type{personType, addressType})
	s.Seal()

	validator := instance.NewValidator(s)

	raws := []instance.RawInstance{
		{Properties: map[string]any{"id": int64(1), "street": "Main St"}},
		{Properties: map[string]any{"id": int64(2), "street": "Oak Ave"}},
	}

	valid, failures, err := validator.ValidateForComposition(context.Background(), "Person", "addresses", raws)

	require.NoError(t, err)
	assert.Len(t, failures, 0)
	assert.Len(t, valid, 2)
}

func TestValidator_ValidateForComposition_ParentTypeNotFound(t *testing.T) {
	s := makeTestSchema() // Empty schema

	validator := instance.NewValidator(s)

	raws := []instance.RawInstance{
		{Properties: map[string]any{"id": int64(1)}},
	}

	valid, failures, err := validator.ValidateForComposition(context.Background(), "NonExistent", "children", raws)

	require.NoError(t, err)
	assert.Nil(t, valid)
	require.Len(t, failures, 1)
	assert.Equal(t, instance.ErrTypeNotFound, failures[0].Result.IssuesSlice()[0].Code())
	assert.Contains(t, failures[0].Error(), "not found")
}

func TestValidator_ValidateForComposition_RelationNotFound(t *testing.T) {
	personType := makeType("Person", false, false,
		makeProp("id", schema.NewIntegerConstraint(), false, true),
	)
	s := makeTestSchema(personType)

	validator := instance.NewValidator(s)

	raws := []instance.RawInstance{
		{Properties: map[string]any{"id": int64(1)}},
	}

	valid, failures, err := validator.ValidateForComposition(context.Background(), "Person", "nonexistent", raws)

	require.NoError(t, err)
	assert.Nil(t, valid)
	require.Len(t, failures, 1)
	assert.Equal(t, instance.ErrCompositionNotFound, failures[0].Result.IssuesSlice()[0].Code())
	assert.Contains(t, failures[0].Error(), "not found")
}

func TestValidator_ValidateForComposition_ContextCancellation(t *testing.T) {
	addressType := makeType("Address", false, true,
		makeProp("id", schema.NewIntegerConstraint(), false, true),
	)
	personType := makeTypeWithRelation("Person", addressType, "addresses")

	s := schema.NewSchema("test", location.SourceID{}, location.Span{}, "")
	s.SetTypes([]*schema.Type{personType, addressType})
	s.Seal()

	validator := instance.NewValidator(s)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	raws := []instance.RawInstance{
		{Properties: map[string]any{"id": int64(1)}},
	}

	_, _, err := validator.ValidateForComposition(ctx, "Person", "addresses", raws)

	require.Error(t, err)
	assert.True(t, errors.Is(err, context.Canceled))
}

func TestValidator_ValidateForComposition_ParentTypeNotFound_EmptyInput(t *testing.T) {
	s := makeTestSchema()
	validator := instance.NewValidator(s)

	valid, failures, err := validator.ValidateForComposition(
		context.Background(), "NonExistent", "children", []instance.RawInstance{},
	)

	require.NoError(t, err)
	assert.Empty(t, valid)
	require.Len(t, failures, 1)
	assert.Equal(t, instance.ErrTypeNotFound, failures[0].Result.IssuesSlice()[0].Code())
}

func TestValidator_ValidateForComposition_TypeNotFound_PreservesProvenance(t *testing.T) {
	s := makeTestSchema()
	validator := instance.NewValidator(s)

	prov := instance.NewProvenance("test.json", path.Root().Key("items").Index(0), location.Span{})
	raws := []instance.RawInstance{
		{Properties: map[string]any{"id": int64(1)}, Provenance: prov},
	}

	_, failures, err := validator.ValidateForComposition(
		context.Background(), "NonExistent", "children", raws,
	)

	require.NoError(t, err)
	require.Len(t, failures, 1)
	issue := failures[0].Result.IssuesSlice()[0]
	assert.Equal(t, "test.json", issue.SourceName())
}

// --- Invariant Evaluation Tests ---

// makeTypeWithInvariant creates a type with an invariant expression.
func makeTypeWithInvariant(invName string, invExpr expr.Expression, props ...*schema.Property) *schema.Type {
	t := schema.NewType("Person", location.SourceID{}, location.Span{}, "", false, false)
	t.SetProperties(props)
	t.SetAllProperties(props)

	var pks []*schema.Property
	for _, p := range props {
		if p.IsPrimaryKey() {
			pks = append(pks, p)
		}
	}
	t.SetPrimaryKeys(pks)

	inv := schema.NewInvariant(invName, invExpr, location.Span{}, "")
	t.SetInvariants([]*schema.Invariant{inv})

	t.Seal()
	return t
}

func TestValidator_ValidateOne_InvariantPass(t *testing.T) {
	// Invariant: age >= 0
	// Expression: (>= ($ age) 0)
	invExpr := expr.SExpr{
		expr.Op(">="),
		expr.SExpr{expr.Op("$"), expr.NewLiteral("age")},
		expr.NewLiteral(int64(0)),
	}

	personType := makeTypeWithInvariant("age must be non-negative", invExpr,
		makeProp("id", schema.NewIntegerConstraint(), false, true),
		makeProp("age", schema.NewIntegerConstraint(), false, false),
	)
	s := makeTestSchema(personType)

	validator := instance.NewValidator(s)

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id":  int64(1),
			"age": int64(25),
		},
	}

	valid, failure, err := validator.ValidateOne(context.Background(), "Person", raw)

	require.NoError(t, err)
	assert.Nil(t, failure)
	require.NotNil(t, valid)
}

func TestValidator_ValidateOne_InvariantFail(t *testing.T) {
	// Invariant: age >= 0
	invExpr := expr.SExpr{
		expr.Op(">="),
		expr.SExpr{expr.Op("$"), expr.NewLiteral("age")},
		expr.NewLiteral(int64(0)),
	}

	personType := makeTypeWithInvariant("age must be non-negative", invExpr,
		makeProp("id", schema.NewIntegerConstraint(), false, true),
		makeProp("age", schema.NewIntegerConstraint(), false, false),
	)
	s := makeTestSchema(personType)

	validator := instance.NewValidator(s)

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id":  int64(1),
			"age": int64(-5), // Violates invariant
		},
	}

	valid, failure, err := validator.ValidateOne(context.Background(), "Person", raw)

	require.NoError(t, err)
	assert.Nil(t, valid)
	require.NotNil(t, failure)
	assert.Contains(t, failure.Error(), "age must be non-negative")
}

func TestValidator_ValidateOne_InvariantWithoutMessage(t *testing.T) {
	// Invariant without a name - should show "invariant failed"
	invExpr := expr.SExpr{
		expr.Op(">="),
		expr.SExpr{expr.Op("$"), expr.NewLiteral("age")},
		expr.NewLiteral(int64(0)),
	}

	personType := makeTypeWithInvariant("", invExpr, // Empty name
		makeProp("id", schema.NewIntegerConstraint(), false, true),
		makeProp("age", schema.NewIntegerConstraint(), false, false),
	)
	s := makeTestSchema(personType)

	validator := instance.NewValidator(s)

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id":  int64(1),
			"age": int64(-5),
		},
	}

	valid, failure, err := validator.ValidateOne(context.Background(), "Person", raw)

	require.NoError(t, err)
	assert.Nil(t, valid)
	require.NotNil(t, failure)
	assert.Contains(t, failure.Error(), "invariant failed")
}

func TestValidator_ValidateOne_InvariantNilExpression(t *testing.T) {
	// Invariant with nil expression should be skipped
	personType := makeTypeWithInvariant("test", nil,
		makeProp("id", schema.NewIntegerConstraint(), false, true),
	)
	s := makeTestSchema(personType)

	validator := instance.NewValidator(s)

	raw := instance.RawInstance{
		Properties: map[string]any{"id": int64(1)},
	}

	valid, failure, err := validator.ValidateOne(context.Background(), "Person", raw)

	require.NoError(t, err)
	assert.Nil(t, failure)
	require.NotNil(t, valid)
}

// --- P1.1 Property Path Uses Schema Name Tests ---

func TestValidator_PropertyPath_UsesSchemaName(t *testing.T) {
	// Property paths should use schema property names, not input field names.
	// When input uses different case (e.g., "firstname"), path should still use "FirstName".
	personType := makeType("Person", false, false,
		makeProp("id", schema.NewIntegerConstraint(), false, true),
		makeProp("FirstName", schema.NewIntegerConstraint(), false, false), // Integer type
	)
	s := makeTestSchema(personType)

	validator := instance.NewValidator(s)

	// Use different case in input - case-insensitive matching will find the property
	raw := instance.RawInstance{
		Properties: map[string]any{
			"id":        int64(1),
			"firstname": "not_an_int", // Wrong type (string instead of int), lowercase input name
		},
	}

	valid, failure, err := validator.ValidateOne(context.Background(), "Person", raw)

	require.NoError(t, err)
	assert.Nil(t, valid)
	require.NotNil(t, failure)

	// Check that the path uses schema property name "FirstName", not input name "firstname"
	issues := failure.Result.Issues()
	var foundPath string
	for issue := range issues {
		if issue.Path() != "" {
			foundPath = issue.Path()
			break
		}
	}

	assert.Contains(t, foundPath, "FirstName", "path should use schema property name")
	assert.NotContains(t, foundPath, "firstname", "path should not use input field name")
}

func TestValidator_PropertyPath_IncludesFieldDetailWhenDifferent(t *testing.T) {
	// When input field name differs from schema property name,
	// the diagnostic should include a "field" detail with the original input name.
	personType := makeType("Person", false, false,
		makeProp("id", schema.NewIntegerConstraint(), false, true),
		makeProp("FirstName", schema.NewIntegerConstraint(), false, false), // Integer to cause type error
	)
	s := makeTestSchema(personType)

	validator := instance.NewValidator(s)

	// Use different case in input
	raw := instance.RawInstance{
		Properties: map[string]any{
			"id":        int64(1),
			"firstname": "not_an_int", // Wrong type, lowercase input name
		},
	}

	valid, failure, err := validator.ValidateOne(context.Background(), "Person", raw)

	require.NoError(t, err)
	assert.Nil(t, valid)
	require.NotNil(t, failure)

	// Check that the issue has a "field" detail with the input field name
	issues := failure.Result.Issues()
	var foundFieldDetail bool
	for issue := range issues {
		for _, detail := range issue.Details() {
			if detail.Key == "field" && detail.Value == "firstname" {
				foundFieldDetail = true
				break
			}
		}
	}

	assert.True(t, foundFieldDetail, "should include 'field' detail with original input name when it differs from schema name")
}

// --- P1.3 Empty Input Slice Tests ---

func TestValidator_Validate_NilInput_ReturnsNil(t *testing.T) {
	// Validate(ctx, typeName, nil) should return (nil, nil, nil)
	personType := makeType("Person", false, false,
		makeProp("id", schema.NewIntegerConstraint(), false, true),
	)
	s := makeTestSchema(personType)

	validator := instance.NewValidator(s)

	valid, failures, err := validator.Validate(context.Background(), "Person", nil)

	require.NoError(t, err)
	assert.Nil(t, valid, "nil input should return nil valid slice")
	assert.Nil(t, failures, "nil input should return nil failures slice")
}

func TestValidator_Validate_EmptyInput_ReturnsEmptySlice(t *testing.T) {
	// Validate(ctx, typeName, []RawInstance{}) should return ([]*ValidInstance{}, nil, nil)
	personType := makeType("Person", false, false,
		makeProp("id", schema.NewIntegerConstraint(), false, true),
	)
	s := makeTestSchema(personType)

	validator := instance.NewValidator(s)

	valid, _, err := validator.Validate(context.Background(), "Person", []instance.RawInstance{})

	require.NoError(t, err)
	require.NotNil(t, valid, "empty input should return non-nil empty valid slice")
	assert.Empty(t, valid, "empty input should return empty valid slice")
}

func TestValidator_ValidateForComposition_NilInput_ReturnsNil(t *testing.T) {
	// ValidateForComposition with nil input should return (nil, nil, nil)
	addressType := makeType("Address", false, true,
		makeProp("id", schema.NewIntegerConstraint(), false, true),
	)
	personType := makeTypeWithRelation("Person", addressType, "addresses")

	s := schema.NewSchema("test", location.SourceID{}, location.Span{}, "")
	s.SetTypes([]*schema.Type{personType, addressType})
	s.Seal()

	validator := instance.NewValidator(s)

	valid, failures, err := validator.ValidateForComposition(context.Background(), "Person", "addresses", nil)

	require.NoError(t, err)
	assert.Nil(t, valid, "nil input should return nil valid slice")
	assert.Nil(t, failures, "nil input should return nil failures slice")
}

func TestValidator_ValidateForComposition_EmptyInput_ReturnsEmptySlice(t *testing.T) {
	// ValidateForComposition with empty input should return ([]*ValidInstance{}, nil, nil)
	addressType := makeType("Address", false, true,
		makeProp("id", schema.NewIntegerConstraint(), false, true),
	)
	personType := makeTypeWithRelation("Person", addressType, "addresses")

	s := schema.NewSchema("test", location.SourceID{}, location.Span{}, "")
	s.SetTypes([]*schema.Type{personType, addressType})
	s.Seal()

	validator := instance.NewValidator(s)

	valid, _, err := validator.ValidateForComposition(context.Background(), "Person", "addresses", []instance.RawInstance{})

	require.NoError(t, err)
	require.NotNil(t, valid, "empty input should return non-nil empty valid slice")
	assert.Empty(t, valid, "empty input should return empty valid slice")
}

// --- P1 Internal Error Sentinel Tests ---

func TestValidator_NilReceiver(t *testing.T) {
	var v *instance.Validator = nil

	t.Run("Validate", func(t *testing.T) {
		_, _, err := v.Validate(context.Background(), "Test", nil)
		require.Error(t, err, "expected error for nil receiver")
		assert.True(t, errors.Is(err, instance.ErrNilValidator), "expected ErrNilValidator, got %v", err)
		assert.True(t, errors.Is(err, instance.ErrInternalFailure), "expected ErrInternalFailure (parent), got %v", err)

		var internalErr *instance.InternalError
		require.True(t, errors.As(err, &internalErr), "expected InternalError type, got %T", err)
		assert.Equal(t, instance.KindNilValidator, internalErr.Kind, "expected KindNilValidator")
	})

	t.Run("ValidateOne", func(t *testing.T) {
		_, _, err := v.ValidateOne(context.Background(), "Test", instance.RawInstance{})
		require.Error(t, err, "expected error for nil receiver")
		assert.True(t, errors.Is(err, instance.ErrNilValidator), "expected ErrNilValidator, got %v", err)
		assert.True(t, errors.Is(err, instance.ErrInternalFailure), "expected ErrInternalFailure (parent), got %v", err)
	})

	t.Run("ValidateForComposition", func(t *testing.T) {
		_, _, err := v.ValidateForComposition(context.Background(), "Parent", "children", nil)
		require.Error(t, err, "expected error for nil receiver")
		assert.True(t, errors.Is(err, instance.ErrNilValidator), "expected ErrNilValidator, got %v", err)
		assert.True(t, errors.Is(err, instance.ErrInternalFailure), "expected ErrInternalFailure (parent), got %v", err)
	})
}

func TestNewValidator_NilSchemaPanics(t *testing.T) {
	defer func() {
		r := recover()
		require.NotNil(t, r, "expected panic for nil schema")
		assert.Contains(t, r, "nil schema", "panic message should mention nil schema")
	}()
	instance.NewValidator(nil)
}
