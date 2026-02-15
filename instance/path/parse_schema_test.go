package path

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/schema/build"
)

func TestParseWithSchema_ValidPaths(t *testing.T) {
	sch := buildTestSchema(t)

	tests := []struct {
		name  string
		input string
	}{
		{"integer PK", "$.Person[id=42]"},
		{"string PK", `$.User[email="test@example.com"]`},
		{"boolean PK", "$.Flag[enabled=true]"},
		{"composite PK", `$.Enrollment[region="us",studentId=12345]`},
		{"float PK", "$.Metric[value=3.14]"},
		{"nested path", "$.Person[id=1].WORKS_AT[0]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, err := ParseWithSchema(tt.input, sch)
			require.NoError(t, err)
			assert.NotNil(t, b)
		})
	}
}

func TestParseWithSchema_TypeMismatch(t *testing.T) {
	sch := buildTestSchema(t)

	tests := []struct {
		name   string
		input  string
		errMsg string
	}{
		{"string for integer PK", `$.Person[id="42"]`, "expected integer"},
		{"integer for string PK", `$.User[email=42]`, "expected string"},
		{"string for boolean PK", `$.Flag[enabled="true"]`, "expected boolean"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseWithSchema(tt.input, sch)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errMsg)
		})
	}
}

func TestParseWithSchema_UnknownPKField(t *testing.T) {
	sch := buildTestSchema(t)

	_, err := ParseWithSchema("$.Person[unknown=42]", sch)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a primary key")
}

func TestParseWithSchema_TypeNotFound(t *testing.T) {
	sch := buildTestSchema(t)

	_, err := ParseWithSchema("$.NonExistent[id=42]", sch)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found in schema")
}

func TestParseWithSchema_NilSchema(t *testing.T) {
	_, err := ParseWithSchema("$.Person[id=42]", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "schema is nil")
}

func TestParseWithSchema_SyntaxError(t *testing.T) {
	sch := buildTestSchema(t)

	_, err := ParseWithSchema("$[", sch)
	require.Error(t, err)
	// Syntax errors should be caught by Parse, not schema validation
}

func TestParseWithSchema_RootOnly(t *testing.T) {
	sch := buildTestSchema(t)

	// Root path should fail because there's no type
	_, err := ParseWithSchema("$", sch)
	require.NoError(t, err) // Root path is valid but has no PK to validate
}

func TestParseWithSchema_ArrayIndexOnly(t *testing.T) {
	sch := buildTestSchema(t)

	// Path with array index only - no type name, so no type validation needed
	// This is a valid path syntactically, and since there's no type context
	// we can't validate any PK segments (there aren't any)
	b, err := ParseWithSchema("$[0]", sch)
	require.NoError(t, err)
	assert.NotNil(t, b)
}

func TestParseWithSchema_FloatAcceptsInteger(t *testing.T) {
	sch := buildTestSchema(t)

	// Float constraints should accept integer values from the parser
	// (since JSON doesn't distinguish between 3 and 3.0 without a decimal)
	b, err := ParseWithSchema("$.Metric[value=3]", sch)
	require.NoError(t, err)
	assert.NotNil(t, b)
}

// buildTestSchema creates a test schema with various PK types.
func buildTestSchema(t *testing.T) *schema.Schema {
	t.Helper()

	sch, result := build.NewBuilder().
		WithName("test").
		AddType("Person").
		WithPrimaryKey("id", schema.NewIntegerConstraint()).
		WithProperty("name", schema.NewStringConstraint()).
		WithRelation("WORKS_AT", schema.NewTypeRef("", "Company", location.Span{}), false, false).
		Done().
		AddType("User").
		WithPrimaryKey("email", schema.NewStringConstraint()).
		Done().
		AddType("Flag").
		WithPrimaryKey("enabled", schema.NewBooleanConstraint()).
		Done().
		AddType("Enrollment").
		WithPrimaryKey("region", schema.NewStringConstraint()).
		WithPrimaryKey("studentId", schema.NewIntegerConstraint()).
		Done().
		AddType("Metric").
		WithPrimaryKey("value", schema.NewFloatConstraint()).
		Done().
		AddType("Company").
		WithPrimaryKey("id", schema.NewIntegerConstraint()).
		Done().
		Build()

	require.True(t, result.OK(), "schema build failed: %v", result)
	return sch
}
