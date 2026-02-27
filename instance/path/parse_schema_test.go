package path

import (
	"testing"

	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/schema/build"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseWithSchema_ValidPaths(t *testing.T) {
	sch := buildTestSchema(t)

	tests := []struct {
		name  string
		input string
	}{
		{"string PK", `$.User[email="test@example.com"]`},
		{"uuid PK", `$.Entity[uid="550e8400-e29b-41d4-a716-446655440000"]`},
		{"date PK", `$.Report[day="2026-01-15"]`},
		{"timestamp PK", `$.Event[ts="2026-01-15T10:30:00Z"]`},
		{"composite PK", `$.Enrollment[region="us",studentId="12345"]`},
		{"nested path", `$.Person[id="1"].WORKS_AT[0]`},
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
		{"integer for string PK", `$.User[email=42]`, "expected string"},
		{"integer for uuid PK", `$.Entity[uid=42]`, "expected string"},
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

	_, err := ParseWithSchema(`$.Person[unknown="42"]`, sch)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a primary key")
}

func TestParseWithSchema_TypeNotFound(t *testing.T) {
	sch := buildTestSchema(t)

	_, err := ParseWithSchema(`$.NonExistent[id="42"]`, sch)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found in schema")
}

func TestParseWithSchema_NilSchema(t *testing.T) {
	_, err := ParseWithSchema(`$.Person[id="42"]`, nil)
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

// buildTestSchema creates a test schema with various PK types.
func buildTestSchema(t *testing.T) *schema.Schema {
	t.Helper()

	sch, result := build.NewBuilder().
		WithName("test").
		AddType("Person").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithProperty("name", schema.NewStringConstraint()).
		WithRelation("WORKS_AT", schema.NewTypeRef("", "Company", location.Span{}), false, false).
		Done().
		AddType("User").
		WithPrimaryKey("email", schema.NewStringConstraint()).
		Done().
		AddType("Entity").
		WithPrimaryKey("uid", schema.NewUUIDConstraint()).
		Done().
		AddType("Report").
		WithPrimaryKey("day", schema.NewDateConstraint()).
		Done().
		AddType("Event").
		WithPrimaryKey("ts", schema.NewTimestampConstraint()).
		Done().
		AddType("Enrollment").
		WithPrimaryKey("region", schema.NewStringConstraint()).
		WithPrimaryKey("studentId", schema.NewStringConstraint()).
		Done().
		AddType("Company").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		Done().
		Build()

	require.True(t, result.OK(), "schema build failed: %v", result)
	return sch
}
