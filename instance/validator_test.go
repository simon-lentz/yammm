package instance_test

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/location/path"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/schema/expr"
)

// mustBuild builds a schema from a Builder, failing the test on error.
func mustBuild(t *testing.T, b *schema.Builder) *schema.Schema {
	t.Helper()
	s, result := b.Build()
	require.False(t, result.HasErrors(), "schema build: %s", result)
	return s
}

// mustLoadString loads a schema from YAMMM DSL source, failing the test on error.
func mustLoadString(t *testing.T, source, name string) *schema.Schema {
	t.Helper()
	s, result := schema.LoadString(t.Context(), source, name)
	require.False(t, result.HasErrors(), "schema load: %s", result)
	return s
}

func TestValidator_ValidateOne_Success(t *testing.T) {
	s := mustBuild(t, schema.NewBuilder().
		WithName("test").
		AddType("Person").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithProperty("name", schema.NewStringConstraint()).
		WithOptionalProperty("age", schema.NewIntegerConstraint()).
		Done())

	validator := instance.NewValidator(s)

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id":   "1",
			"name": "Alice",
			"age":  int64(30),
		},
	}

	valid, result := validator.ValidateOne(t.Context(), "Person", raw)

	require.True(t, result.OK())
	require.NotNil(t, valid)
	assert.Equal(t, "Person", valid.TypeName())
	assert.Equal(t, `["1"]`, valid.PrimaryKey().String())

	// Check property access
	nameVal, ok := valid.Property("name")
	require.True(t, ok)
	name, ok := nameVal.String()
	require.True(t, ok)
	assert.Equal(t, "Alice", name)
}

func TestValidator_ValidateOne_TypeNotFound(t *testing.T) {
	s := mustBuild(t, schema.NewBuilder().
		WithName("test").
		AddType("Placeholder").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		Done())

	validator := instance.NewValidator(s)

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id": "1",
		},
	}

	valid, result := validator.ValidateOne(t.Context(), "NonExistent", raw)

	assert.Nil(t, valid)
	require.False(t, result.OK())
	assert.Contains(t, result.String(), "not found")
}

func TestValidator_ValidateOne_AbstractTypeRejected(t *testing.T) {
	s := mustBuild(t, schema.NewBuilder().
		WithName("test").
		AddType("Entity").
		AsAbstract().
		WithPrimaryKey("id", schema.NewStringConstraint()).
		Done())

	validator := instance.NewValidator(s)

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id": "1",
		},
	}

	valid, result := validator.ValidateOne(t.Context(), "Entity", raw)

	assert.Nil(t, valid)
	require.False(t, result.OK())
	assert.Contains(t, result.String(), "abstract")
}

func TestValidator_ValidateOne_PartTypeRejected(t *testing.T) {
	s := mustBuild(t, schema.NewBuilder().
		WithName("test").
		AddType("Part").
		AsPart().
		WithPrimaryKey("id", schema.NewStringConstraint()).
		Done())

	validator := instance.NewValidator(s)

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id": "1",
		},
	}

	valid, result := validator.ValidateOne(t.Context(), "Part", raw)

	assert.Nil(t, valid)
	require.False(t, result.OK())
	assert.Contains(t, result.String(), "part type")
}

func TestValidator_ValidateOne_MissingRequiredProperty(t *testing.T) {
	s := mustBuild(t, schema.NewBuilder().
		WithName("test").
		AddType("Person").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithProperty("name", schema.NewStringConstraint()). // Required
		Done())

	validator := instance.NewValidator(s)

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id": "1",
			// Missing "name"
		},
	}

	valid, result := validator.ValidateOne(t.Context(), "Person", raw)

	assert.Nil(t, valid)
	require.False(t, result.OK())
	assert.Contains(t, result.String(), "missing required")
}

func TestValidator_ValidateOne_OptionalPropertyMissing(t *testing.T) {
	s := mustBuild(t, schema.NewBuilder().
		WithName("test").
		AddType("Person").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithOptionalProperty("nickname", schema.NewStringConstraint()). // Optional
		Done())

	validator := instance.NewValidator(s)

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id": "1",
			// Missing "nickname" is OK
		},
	}

	valid, result := validator.ValidateOne(t.Context(), "Person", raw)

	require.True(t, result.OK())
	require.NotNil(t, valid)
}

func TestValidator_ValidateOne_TypeMismatch(t *testing.T) {
	s := mustBuild(t, schema.NewBuilder().
		WithName("test").
		AddType("Person").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithProperty("age", schema.NewIntegerConstraint()).
		Done())

	validator := instance.NewValidator(s)

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id":  "1",
			"age": "not an integer", // Wrong type
		},
	}

	valid, result := validator.ValidateOne(t.Context(), "Person", raw)

	assert.Nil(t, valid)
	require.False(t, result.OK())
	assert.Contains(t, result.String(), "age")
}

func TestValidator_ValidateOne_UnknownField(t *testing.T) {
	s := mustBuild(t, schema.NewBuilder().
		WithName("test").
		AddType("Person").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		Done())

	validator := instance.NewValidator(s) // Default: don't allow unknown fields

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id":      "1",
			"unknown": "value", // Unknown field
		},
	}

	valid, result := validator.ValidateOne(t.Context(), "Person", raw)

	assert.Nil(t, valid)
	require.False(t, result.OK())
	assert.Contains(t, result.String(), "unknown")
}

func TestValidator_ValidateOne_AllowUnknownFields(t *testing.T) {
	s := mustBuild(t, schema.NewBuilder().
		WithName("test").
		AddType("Person").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		Done())

	validator := instance.NewValidator(s, instance.WithAllowUnknownFields(true))

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id":      "1",
			"unknown": "value", // Unknown field - should be ignored
		},
	}

	valid, result := validator.ValidateOne(t.Context(), "Person", raw)

	require.True(t, result.OK())
	require.NotNil(t, valid)
}

func TestValidator_ValidateOne_CaseInsensitivePropertyMatch(t *testing.T) {
	s := mustBuild(t, schema.NewBuilder().
		WithName("test").
		AddType("Person").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithProperty("firstName", schema.NewStringConstraint()).
		Done())

	validator := instance.NewValidator(s) // Default: case-insensitive

	raw := instance.RawInstance{
		Properties: map[string]any{
			"ID":        "1",     // Different case
			"FIRSTNAME": "Alice", // Different case
		},
	}

	valid, result := validator.ValidateOne(t.Context(), "Person", raw)

	require.True(t, result.OK())
	require.NotNil(t, valid)

	// Property should be stored with canonical name
	firstNameVal, ok := valid.Property("firstName")
	require.True(t, ok)
	firstName, ok := firstNameVal.String()
	require.True(t, ok)
	assert.Equal(t, "Alice", firstName)
}

func TestValidator_ValidateOne_StrictPropertyNames(t *testing.T) {
	s := mustBuild(t, schema.NewBuilder().
		WithName("test").
		AddType("Person").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithProperty("firstName", schema.NewStringConstraint()).
		Done())

	validator := instance.NewValidator(s, instance.WithStrictPropertyNames(true))

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id":        "1",
			"FIRSTNAME": "Alice", // Wrong case - should fail
		},
	}

	valid, result := validator.ValidateOne(t.Context(), "Person", raw)

	assert.Nil(t, valid)
	require.False(t, result.OK())
	// Should have both "unknown field" and "missing required" errors
}

func TestValidator_Validate_BatchSuccess(t *testing.T) {
	s := mustBuild(t, schema.NewBuilder().
		WithName("test").
		AddType("Person").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithProperty("name", schema.NewStringConstraint()).
		Done())

	validator := instance.NewValidator(s)

	raws := []instance.RawInstance{
		{Properties: map[string]any{"id": "1", "name": "Alice"}},
		{Properties: map[string]any{"id": "2", "name": "Bob"}},
		{Properties: map[string]any{"id": "3", "name": "Charlie"}},
	}

	valid, result := validator.Validate(t.Context(), "Person", raws)

	require.True(t, result.OK())
	assert.Len(t, valid, 3)
}

func TestValidator_Validate_PartialSuccess(t *testing.T) {
	s := mustBuild(t, schema.NewBuilder().
		WithName("test").
		AddType("Person").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithProperty("name", schema.NewStringConstraint()).
		Done())

	validator := instance.NewValidator(s)

	raws := []instance.RawInstance{
		{Properties: map[string]any{"id": "1", "name": "Alice"}},
		{Properties: map[string]any{"id": "2"}}, // Missing name
		{Properties: map[string]any{"id": "3", "name": "Charlie"}},
	}

	valid, result := validator.Validate(t.Context(), "Person", raws)

	require.False(t, result.OK())
	// Batch returns positional nils: 3 entries, with index 1 nil for the failure
	require.Len(t, valid, 3)
	assert.NotNil(t, valid[0])
	assert.Nil(t, valid[1])
	assert.NotNil(t, valid[2])
}

func TestValidator_Validate_ContextCancellation(t *testing.T) {
	s := mustBuild(t, schema.NewBuilder().
		WithName("test").
		AddType("Person").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		Done())

	validator := instance.NewValidator(s)

	ctx, cancel := context.WithCancel(t.Context())
	cancel() // Cancel immediately

	raw := instance.RawInstance{
		Properties: map[string]any{"id": "1"},
	}

	_, result := validator.ValidateOne(ctx, "Person", raw)

	require.True(t, result.HasFatal())
	// Verify context cancellation diagnostic code
	issues := slices.Collect(result.Issues())
	require.Len(t, issues, 1)
	assert.Equal(t, diag.E_CONTEXT_CANCELLED, issues[0].Code())
}

func TestValidator_ValidateOne_ImmutableOutput(t *testing.T) {
	s := mustBuild(t, schema.NewBuilder().
		WithName("test").
		AddType("Person").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithProperty("name", schema.NewStringConstraint()).
		Done())

	validator := instance.NewValidator(s)

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id":   "1",
			"name": "Alice",
		},
	}

	valid, result := validator.ValidateOne(t.Context(), "Person", raw)
	require.True(t, result.OK())
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
	s := mustBuild(t, schema.NewBuilder().
		WithName("test").
		AddType("Person").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithProperty("age", schema.IntegerBetween(0, 150)).
		Done())

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
					"id":  "1",
					"age": tt.age,
				},
			}

			valid, result := validator.ValidateOne(t.Context(), "Person", raw)

			if tt.wantErr {
				assert.Nil(t, valid)
				assert.False(t, result.OK())
			} else {
				assert.NotNil(t, valid)
				assert.True(t, result.OK())
			}
		})
	}
}

func TestValidator_ValidateOne_StringBounds(t *testing.T) {
	s := mustBuild(t, schema.NewBuilder().
		WithName("test").
		AddType("Person").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithProperty("code", schema.StringLenBetween(3, 10)).
		Done())

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
					"id":   "1",
					"code": tt.code,
				},
			}

			valid, result := validator.ValidateOne(t.Context(), "Person", raw)

			if tt.wantErr {
				assert.Nil(t, valid)
				assert.False(t, result.OK())
			} else {
				assert.NotNil(t, valid)
				assert.True(t, result.OK())
			}
		})
	}
}

func TestValidator_ValidateForComposition_Success(t *testing.T) {
	s := mustBuild(t, schema.NewBuilder().
		WithName("test").
		AddType("Address").
		AsPart().
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithProperty("street", schema.NewStringConstraint()).
		Done().
		AddType("Person").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithComposition("addresses", schema.LocalTypeRef("Address", location.Span{}), true, true).
		Done())

	validator := instance.NewValidator(s)

	raws := []instance.RawInstance{
		{Properties: map[string]any{"id": "1", "street": "Main St"}},
		{Properties: map[string]any{"id": "2", "street": "Oak Ave"}},
	}

	valid, result := validator.ValidateForComposition(t.Context(), "Person", "addresses", raws)

	require.True(t, result.OK())
	assert.Len(t, valid, 2)
}

func TestValidator_ValidateForComposition_ParentTypeNotFound(t *testing.T) {
	s := mustBuild(t, schema.NewBuilder().
		WithName("test").
		AddType("Placeholder").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		Done())

	validator := instance.NewValidator(s)

	raws := []instance.RawInstance{
		{Properties: map[string]any{"id": "1"}},
	}

	valid, result := validator.ValidateForComposition(t.Context(), "NonExistent", "children", raws)

	assert.Nil(t, valid)
	require.False(t, result.OK())
	issues := slices.Collect(result.Issues())
	require.GreaterOrEqual(t, len(issues), 1)
	assert.Equal(t, instance.ErrTypeNotFound, issues[0].Code())
	assert.Contains(t, result.String(), "not found")
}

func TestValidator_ValidateForComposition_RelationNotFound(t *testing.T) {
	s := mustBuild(t, schema.NewBuilder().
		WithName("test").
		AddType("Person").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		Done())

	validator := instance.NewValidator(s)

	raws := []instance.RawInstance{
		{Properties: map[string]any{"id": "1"}},
	}

	valid, result := validator.ValidateForComposition(t.Context(), "Person", "nonexistent", raws)

	assert.Nil(t, valid)
	require.False(t, result.OK())
	issues := slices.Collect(result.Issues())
	require.GreaterOrEqual(t, len(issues), 1)
	assert.Equal(t, instance.ErrCompositionNotFound, issues[0].Code())
	assert.Contains(t, result.String(), "not found")
}

func TestValidator_ValidateForComposition_ContextCancellation(t *testing.T) {
	s := mustBuild(t, schema.NewBuilder().
		WithName("test").
		AddType("Address").
		AsPart().
		WithPrimaryKey("id", schema.NewStringConstraint()).
		Done().
		AddType("Person").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithComposition("addresses", schema.LocalTypeRef("Address", location.Span{}), true, true).
		Done())

	validator := instance.NewValidator(s)

	ctx, cancel := context.WithCancel(t.Context())
	cancel() // Cancel immediately

	raws := []instance.RawInstance{
		{Properties: map[string]any{"id": "1"}},
	}

	_, result := validator.ValidateForComposition(ctx, "Person", "addresses", raws)

	require.True(t, result.HasFatal())
	issues := slices.Collect(result.Issues())
	require.Len(t, issues, 1)
	assert.Equal(t, diag.E_CONTEXT_CANCELLED, issues[0].Code())
}

func TestValidator_ValidateForComposition_ParentTypeNotFound_EmptyInput(t *testing.T) {
	s := mustBuild(t, schema.NewBuilder().
		WithName("test").
		AddType("Placeholder").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		Done())

	validator := instance.NewValidator(s)

	valid, result := validator.ValidateForComposition(
		t.Context(), "NonExistent", "children", []instance.RawInstance{},
	)

	assert.Empty(t, valid)
	require.False(t, result.OK())
	issues := slices.Collect(result.Issues())
	require.GreaterOrEqual(t, len(issues), 1)
	assert.Equal(t, instance.ErrTypeNotFound, issues[0].Code())
}

func TestValidator_ValidateForComposition_TypeNotFound_PreservesProvenance(t *testing.T) {
	s := mustBuild(t, schema.NewBuilder().
		WithName("test").
		AddType("Placeholder").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		Done())

	validator := instance.NewValidator(s)

	prov := location.NewProvenance("test.json", path.Root().Key("items").Index(0), location.Span{})
	raws := []instance.RawInstance{
		{Properties: map[string]any{"id": "1"}, Provenance: prov},
	}

	_, result := validator.ValidateForComposition(
		t.Context(), "NonExistent", "children", raws,
	)

	require.False(t, result.OK())
	issues := slices.Collect(result.Issues())
	require.GreaterOrEqual(t, len(issues), 1)
	assert.Equal(t, "test.json", issues[0].SourceName())
}

func TestValidator_ValidateOne_InvariantPass(t *testing.T) {
	// Invariant: age >= 0
	// Expression: (>= ($ age) 0)
	invExpr := expr.SExpr{
		expr.Op(">="),
		expr.SExpr{expr.Op("$"), expr.NewLiteral("age")},
		expr.NewLiteral(int64(0)),
	}

	s := mustBuild(t, schema.NewBuilder().
		WithName("test").
		AddType("Person").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithProperty("age", schema.NewIntegerConstraint()).
		WithInvariant("age must be non-negative", invExpr, "").
		Done())

	validator := instance.NewValidator(s)

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id":  "1",
			"age": int64(25),
		},
	}

	valid, result := validator.ValidateOne(t.Context(), "Person", raw)

	require.True(t, result.OK())
	require.NotNil(t, valid)
}

func TestValidator_ValidateOne_InvariantFail(t *testing.T) {
	// Invariant: age >= 0
	invExpr := expr.SExpr{
		expr.Op(">="),
		expr.SExpr{expr.Op("$"), expr.NewLiteral("age")},
		expr.NewLiteral(int64(0)),
	}

	s := mustBuild(t, schema.NewBuilder().
		WithName("test").
		AddType("Person").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithProperty("age", schema.NewIntegerConstraint()).
		WithInvariant("age must be non-negative", invExpr, "").
		Done())

	validator := instance.NewValidator(s)

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id":  "1",
			"age": int64(-5), // Violates invariant
		},
	}

	valid, result := validator.ValidateOne(t.Context(), "Person", raw)

	assert.Nil(t, valid)
	require.False(t, result.OK())
	assert.Contains(t, result.String(), "age must be non-negative")
}

// TestValidator_ValidateOne_InvariantWithoutMessage and
// TestValidator_ValidateOne_InvariantNilExpression have been removed:
// these tested unreachable states. The Builder rejects empty invariant names
// and nil expressions, and the parser always provides both.

// TestValidator_NilUnderscoreEquivalence verifies that _ and nil are
// interchangeable nil literals in invariant expressions. Both syntactic forms
// must produce identical validation behavior.
func TestValidator_NilUnderscoreEquivalence(t *testing.T) {
	t.Parallel()

	const schemaTemplate = `schema "%s"
type Record {
    id          String primary
    name        String required
    description String

    ! "description_when_present"
        description == %s || description != ""
}
`
	cases := []struct {
		name   string
		input  map[string]any
		wantOK bool
	}{
		{
			name:   "non_empty_passes",
			input:  map[string]any{"id": "1", "name": "A", "description": "hello"},
			wantOK: true,
		},
		{
			name:   "nil_passes",
			input:  map[string]any{"id": "2", "name": "B"},
			wantOK: true,
		},
		{
			name:   "empty_string_fails",
			input:  map[string]any{"id": "3", "name": "C", "description": ""},
			wantOK: false,
		},
	}

	for _, syntax := range []struct {
		label, literal string
	}{
		{"underscore", "_"},
		{"nil_keyword", "nil"},
	} {
		t.Run(syntax.label, func(t *testing.T) {
			t.Parallel()
			src := fmt.Sprintf(schemaTemplate, syntax.label, syntax.literal)
			s := mustLoadString(t, src, syntax.label)
			v := instance.NewValidator(s)

			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					t.Parallel()
					raw := instance.RawInstance{Properties: tc.input}
					valid, result := v.ValidateOne(t.Context(), "Record", raw)
					if tc.wantOK {
						assert.True(t, result.OK(), "expected pass: %s", result)
						assert.NotNil(t, valid)
					} else {
						assert.False(t, result.OK(), "expected fail")
						assert.Nil(t, valid)
					}
				})
			}
		})
	}
}

// TestValidator_InvariantLenPipeline verifies that the Len builtin works
// correctly via the pipe operator in parsed invariants. This is a regression
// test for VisitFcall body normalization (Issue 9, Bug 1), where the
// expression `description -> Len > 0` was incorrectly compiled.
func TestValidator_InvariantLenPipeline(t *testing.T) {
	t.Parallel()

	const src = `schema "len_pipe"
type Record {
    id          String primary
    name        String required
    description String

    ! "description_when_present"
        description == _ || description -> Len > 0
}
`
	s := mustLoadString(t, src, "len_pipe")
	v := instance.NewValidator(s)

	cases := []struct {
		name   string
		input  map[string]any
		wantOK bool
	}{
		{
			name:   "non_empty_passes_via_Len",
			input:  map[string]any{"id": "1", "name": "A", "description": "hello"},
			wantOK: true,
		},
		{
			name:   "nil_short_circuits_past_Len",
			input:  map[string]any{"id": "2", "name": "B"},
			wantOK: true,
		},
		{
			name:   "empty_string_fails_Len_check",
			input:  map[string]any{"id": "3", "name": "C", "description": ""},
			wantOK: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			raw := instance.RawInstance{Properties: tc.input}
			valid, result := v.ValidateOne(t.Context(), "Record", raw)
			if tc.wantOK {
				assert.True(t, result.OK(), "expected pass: %s", result)
				assert.NotNil(t, valid)
			} else {
				assert.False(t, result.OK(), "expected fail")
				assert.Nil(t, valid)
				hasInvariantFail := false
				for issue := range result.Issues() {
					if issue.Code() == diag.E_INVARIANT_FAIL {
						hasInvariantFail = true
						break
					}
				}
				assert.True(t, hasInvariantFail, "expected E_INVARIANT_FAIL")
			}
		})
	}
}

func TestValidator_PropertyPath_UsesSchemaName(t *testing.T) {
	// Property paths should use schema property names, not input field names.
	// When input uses different case (e.g., "firstname"), path should still use "firstName".
	s := mustBuild(t, schema.NewBuilder().
		WithName("test").
		AddType("Person").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithProperty("firstName", schema.NewIntegerConstraint()). // Integer type to cause mismatch
		Done())

	validator := instance.NewValidator(s)

	// Use different case in input - case-insensitive matching will find the property
	raw := instance.RawInstance{
		Properties: map[string]any{
			"id":        "1",
			"firstname": "not_an_int", // Wrong type (string instead of int), lowercase input name
		},
	}

	valid, result := validator.ValidateOne(t.Context(), "Person", raw)

	assert.Nil(t, valid)
	require.False(t, result.OK())

	// Check that the path uses schema property name "firstName", not input name "firstname"
	var foundPath string
	for issue := range result.Issues() {
		if issue.Path() != "" {
			foundPath = issue.Path()
			break
		}
	}

	assert.Contains(t, foundPath, "firstName", "path should use schema property name")
	assert.NotContains(t, foundPath, "firstname", "path should not use input field name")
}

func TestValidator_PropertyPath_IncludesFieldDetailWhenDifferent(t *testing.T) {
	// When input field name differs from schema property name,
	// the diagnostic should include a "field" detail with the original input name.
	s := mustBuild(t, schema.NewBuilder().
		WithName("test").
		AddType("Person").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithProperty("firstName", schema.NewIntegerConstraint()). // Integer to cause type error
		Done())

	validator := instance.NewValidator(s)

	// Use different case in input
	raw := instance.RawInstance{
		Properties: map[string]any{
			"id":        "1",
			"firstname": "not_an_int", // Wrong type, lowercase input name
		},
	}

	valid, result := validator.ValidateOne(t.Context(), "Person", raw)

	assert.Nil(t, valid)
	require.False(t, result.OK())

	// Check that the issue has a "field" detail with the input field name
	var foundFieldDetail bool
	for issue := range result.Issues() {
		for _, detail := range issue.Details() {
			if detail.Key == "field" && detail.Value == "firstname" {
				foundFieldDetail = true
				break
			}
		}
	}

	assert.True(t, foundFieldDetail, "should include 'field' detail with original input name when it differs from schema name")
}

func TestValidator_Validate_NilInput_ReturnsNil(t *testing.T) {
	// Validate(ctx, typeName, nil) should return (nil, OK result)
	s := mustBuild(t, schema.NewBuilder().
		WithName("test").
		AddType("Person").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		Done())

	validator := instance.NewValidator(s)

	valid, result := validator.Validate(t.Context(), "Person", nil)

	require.True(t, result.OK())
	assert.Nil(t, valid, "nil input should return nil valid slice")
}

func TestValidator_Validate_EmptyInput_ReturnsEmptySlice(t *testing.T) {
	// Validate(ctx, typeName, []RawInstance{}) should return ([]*ValidInstance{}, OK result)
	s := mustBuild(t, schema.NewBuilder().
		WithName("test").
		AddType("Person").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		Done())

	validator := instance.NewValidator(s)

	valid, result := validator.Validate(t.Context(), "Person", []instance.RawInstance{})

	require.True(t, result.OK())
	require.NotNil(t, valid, "empty input should return non-nil empty valid slice")
	assert.Empty(t, valid, "empty input should return empty valid slice")
}

func TestValidator_ValidateForComposition_NilInput_ReturnsNil(t *testing.T) {
	// ValidateForComposition with nil input should return (nil, OK result)
	s := mustBuild(t, schema.NewBuilder().
		WithName("test").
		AddType("Address").
		AsPart().
		WithPrimaryKey("id", schema.NewStringConstraint()).
		Done().
		AddType("Person").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithComposition("addresses", schema.LocalTypeRef("Address", location.Span{}), true, true).
		Done())

	validator := instance.NewValidator(s)

	valid, result := validator.ValidateForComposition(t.Context(), "Person", "addresses", nil)

	require.True(t, result.OK())
	assert.Nil(t, valid, "nil input should return nil valid slice")
}

func TestValidator_ValidateForComposition_EmptyInput_ReturnsEmptySlice(t *testing.T) {
	// ValidateForComposition with empty input should return ([]*ValidInstance{}, OK result)
	s := mustBuild(t, schema.NewBuilder().
		WithName("test").
		AddType("Address").
		AsPart().
		WithPrimaryKey("id", schema.NewStringConstraint()).
		Done().
		AddType("Person").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithComposition("addresses", schema.LocalTypeRef("Address", location.Span{}), true, true).
		Done())

	validator := instance.NewValidator(s)

	valid, result := validator.ValidateForComposition(t.Context(), "Person", "addresses", []instance.RawInstance{})

	require.True(t, result.OK())
	require.NotNil(t, valid, "empty input should return non-nil empty valid slice")
	assert.Empty(t, valid, "empty input should return empty valid slice")
}

func TestValidator_NilReceiver(t *testing.T) {
	var v *instance.Validator

	t.Run("Validate", func(t *testing.T) {
		require.Panics(t, func() {
			v.Validate(t.Context(), "Test", nil)
		})
	})

	t.Run("ValidateOne", func(t *testing.T) {
		require.Panics(t, func() {
			v.ValidateOne(t.Context(), "Test", instance.RawInstance{})
		})
	})

	t.Run("ValidateForComposition", func(t *testing.T) {
		require.Panics(t, func() {
			v.ValidateForComposition(t.Context(), "Parent", "children", nil)
		})
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

// Ownership Isolation Tests
//
// These tests verify that ValidInstance outputs are isolated from mutations
// to the original RawInstance data:
//
// "After Validate() returns, callers MAY mutate their original RawInstance
// values without affecting any ValidInstance outputs."

// TestOwnership_Isolation drives every mutate-after-validate scenario
// through one skeleton: build the schema, validate, mutate every retained
// alias into the raw input, then verify the ValidInstance kept the
// original values. Each row owns its schema, its tampering, and its
// scenario-specific assertions.
func TestOwnership_Isolation(t *testing.T) {
	type scenario struct {
		name     string
		schema   func(t *testing.T) *schema.Schema
		typeName string
		// raw returns the input plus a mutate func that tampers with every
		// alias the test retained into the raw data.
		raw    func() (instance.RawInstance, func())
		verify func(t *testing.T, valid *instance.ValidInstance)
	}

	compositionSchema := func(part, parent, relation string, extraProps ...string) func(t *testing.T) *schema.Schema {
		return func(t *testing.T) *schema.Schema {
			t.Helper()
			b := schema.NewBuilder().WithName("test").
				AddType(part).
				AsPart().
				WithPrimaryKey("id", schema.NewStringConstraint())
			for _, prop := range extraProps {
				b = b.WithProperty(prop, schema.NewStringConstraint())
			}
			return mustBuild(t, b.Done().
				AddType(parent).
				WithPrimaryKey("id", schema.NewStringConstraint()).
				WithComposition(relation, schema.LocalTypeRef(part, location.Span{}), true, true).
				Done())
		}
	}

	childProp := func(t *testing.T, valid *instance.ValidInstance, relation string, idx int, prop, want string) {
		t.Helper()
		composed, ok := composedOf(valid, relation)
		require.True(t, ok)
		composedSlice, ok := composed.Slice()
		require.True(t, ok)
		require.Greater(t, composedSlice.Len(), idx)
		child := composedSlice.Get(idx).Unwrap().(*instance.ValidInstance)
		val, ok := child.Property(prop)
		require.True(t, ok)
		got, ok := val.String()
		require.True(t, ok)
		assert.Equal(t, want, got, "isolation failed: %s[%d].%s was mutated", relation, idx, prop)
	}

	scenarios := []scenario{
		{
			name:     "nested map",
			schema:   compositionSchema("Address", "Person", "addresses", "street", "city"),
			typeName: "Person",
			raw: func() (instance.RawInstance, func()) {
				addressData := map[string]any{"id": "100", "street": "Original Street", "city": "Original City"}
				raw := instance.RawInstance{Properties: map[string]any{"id": "1", "addresses": []any{addressData}}}
				return raw, func() {
					addressData["street"] = "Mutated Street"
					addressData["city"] = "Mutated City"
				}
			},
			verify: func(t *testing.T, valid *instance.ValidInstance) {
				t.Helper()
				childProp(t, valid, "addresses", 0, "street", "Original Street")
				childProp(t, valid, "addresses", 0, "city", "Original City")
			},
		},
		{
			name:     "nested slice",
			schema:   compositionSchema("Note", "Document", "notes", "text"),
			typeName: "Document",
			raw: func() (instance.RawInstance, func()) {
				note1 := map[string]any{"id": "1", "text": "Original Note 1"}
				note2 := map[string]any{"id": "2", "text": "Original Note 2"}
				rawData := map[string]any{"id": "1", "notes": []any{note1, note2}}
				return instance.RawInstance{Properties: rawData}, func() {
					note1["text"] = "Mutated Note 1"
					note2["text"] = "Mutated Note 2"
					rawData["notes"] = []any{map[string]any{"id": "999", "text": "New Note"}}
				}
			},
			verify: func(t *testing.T, valid *instance.ValidInstance) {
				t.Helper()
				composed, ok := composedOf(valid, "notes")
				require.True(t, ok)
				composedSlice, ok := composed.Slice()
				require.True(t, ok)
				require.Equal(t, 2, composedSlice.Len(), "original slice length should be preserved")
				childProp(t, valid, "notes", 0, "text", "Original Note 1")
				childProp(t, valid, "notes", 1, "text", "Original Note 2")
			},
		},
		{
			name:     "composition replacement",
			schema:   compositionSchema("Item", "Order", "items", "name"),
			typeName: "Order",
			raw: func() (instance.RawInstance, func()) {
				itemData := map[string]any{"id": "100", "name": "Original Item"}
				compositionArray := []any{itemData}
				rawData := map[string]any{"id": "1", "items": compositionArray}
				return instance.RawInstance{Properties: rawData}, func() {
					itemData["name"] = "Mutated Item"
					compositionArray[0] = map[string]any{"id": "999", "name": "Replaced Item"}
					rawData["items"] = nil
				}
			},
			verify: func(t *testing.T, valid *instance.ValidInstance) {
				t.Helper()
				childProp(t, valid, "items", 0, "name", "Original Item")
			},
		},
		{
			name:     "deeply nested composition",
			schema:   compositionSchema("Detail", "Container", "details", "value"),
			typeName: "Container",
			raw: func() (instance.RawInstance, func()) {
				detailData := map[string]any{"id": "1", "value": "Original Value"}
				raw := instance.RawInstance{Properties: map[string]any{"id": "1", "details": []any{detailData}}}
				return raw, func() {
					detailData["value"] = "Mutated Value"
					detailData["id"] = "888"
				}
			},
			verify: func(t *testing.T, valid *instance.ValidInstance) {
				t.Helper()
				childProp(t, valid, "details", 0, "value", "Original Value")
				composed, _ := composedOf(valid, "details")
				composedSlice, _ := composed.Slice()
				child := composedSlice.Get(0).Unwrap().(*instance.ValidInstance)
				assert.Equal(t, `["1"]`, child.PrimaryKey().String(), "isolation failed: PK was mutated")
			},
		},
		{
			name: "edge properties",
			schema: func(t *testing.T) *schema.Schema {
				t.Helper()
				return mustLoadString(t, `schema "test"
type Company {
    id String primary
}
type Person {
    id String primary
    --> employer (_) Company {
        role String required
        startDate String
    }
}`, "test.yammm")
			},
			typeName: "Person",
			raw: func() (instance.RawInstance, func()) {
				edgeData := map[string]any{"_target_id": "42", "role": "Original Role", "startDate": "2024-01-01"}
				raw := instance.RawInstance{Properties: map[string]any{"id": "1", "employer": edgeData}}
				return raw, func() {
					edgeData["role"] = "Mutated Role"
					edgeData["startDate"] = "2099-12-31"
					edgeData["_target_id"] = "999"
				}
			},
			verify: func(t *testing.T, valid *instance.ValidInstance) {
				t.Helper()
				edge, ok := valid.Edge("employer")
				require.True(t, ok)
				targets := edge.Targets()
				require.Len(t, targets, 1)
				roleVal, ok := targets[0].Property("role")
				require.True(t, ok)
				role, _ := roleVal.String()
				assert.Equal(t, "Original Role", role, "isolation failed: role was mutated")
				dateVal, ok := targets[0].Property("startDate")
				require.True(t, ok)
				date, _ := dateVal.String()
				assert.Equal(t, "2024-01-01", date, "isolation failed: startDate was mutated")
				assert.Equal(t, `["42"]`, targets[0].TargetKey().String(), "isolation failed: target key was mutated")
			},
		},
	}

	for _, tt := range scenarios {
		t.Run(tt.name, func(t *testing.T) {
			validator := instance.NewValidator(tt.schema(t))
			raw, mutate := tt.raw()

			valid, result := validator.ValidateOne(t.Context(), tt.typeName, raw)
			require.True(t, result.OK())
			require.NotNil(t, valid)

			mutate()
			tt.verify(t, valid)
		})
	}
}
