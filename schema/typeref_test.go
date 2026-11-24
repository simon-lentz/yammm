package schema_test

import (
	"testing"

	"github.com/simon-lentz/yammm/internal/yammmtest/sourcetest"

	"github.com/stretchr/testify/assert"

	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

func TestNewTypeRef(t *testing.T) {
	span := location.Span{
		Source: sourcetest.ID("test://typeref"),
		Start:  location.Position{Line: 5, Column: 10, Offset: 50},
		End:    location.Position{Line: 5, Column: 20, Offset: 60},
	}

	ref := schema.TestNewTypeRef("users", "Person", span)

	assert.Equal(t, "users", ref.Qualifier())
	assert.Equal(t, "Person", ref.Name())
	assert.Equal(t, span, ref.Span())
	assert.NotEmpty(t, ref.Qualifier())
}

func TestLocalTypeRef(t *testing.T) {
	span := location.Span{
		Source: sourcetest.ID("test://local"),
		Start:  location.Position{Line: 1, Column: 1, Offset: 0},
		End:    location.Position{Line: 1, Column: 10, Offset: 10},
	}

	ref := schema.TestLocalTypeRef("Customer", span)

	assert.Empty(t, ref.Qualifier())
	assert.Equal(t, "Customer", ref.Name())
	assert.Equal(t, span, ref.Span())
	assert.Empty(t, ref.Qualifier())
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
			ref := schema.TestNewTypeRef(tt.qualifier, "Type", location.Span{})
			assert.Equal(t, tt.expected, ref.Qualifier())
		})
	}
}

func TestTypeRef_Name(t *testing.T) {
	ref := schema.TestNewTypeRef("", "Employee", location.Span{})

	assert.Equal(t, "Employee", ref.Name())
}

func TestTypeRef_Span(t *testing.T) {
	span := location.Span{
		Source: sourcetest.ID("test://span"),
		Start:  location.Position{Line: 10, Column: 5, Offset: 100},
		End:    location.Position{Line: 10, Column: 15, Offset: 110},
	}

	ref := schema.TestNewTypeRef("", "Type", span)

	result := ref.Span()
	assert.Equal(t, span.Source, result.Source)
	assert.Equal(t, 10, result.Start.Line)
}

func TestTypeRef_IsZero(t *testing.T) {
	zero := schema.TypeRef{}
	nonZero := schema.TestNewTypeRef("", "Type", location.Span{})

	assert.True(t, zero.IsZero())
	assert.False(t, nonZero.IsZero())
}

func TestTypeRef_IsZero_WithSpan(t *testing.T) {
	// Not zero if span is non-zero
	span := location.Span{Start: location.Position{Line: 1}}
	ref := schema.TestNewTypeRef("", "", span)

	assert.False(t, ref.IsZero())
}

func TestTypeRef_String_Qualified(t *testing.T) {
	ref := schema.TestNewTypeRef("common", "Money", location.Span{})

	assert.Equal(t, "common.Money", ref.String())
}

func TestTypeRef_String_Local(t *testing.T) {
	ref := schema.TestNewTypeRef("", "Customer", location.Span{})

	assert.Equal(t, "Customer", ref.String())
}

func TestDataTypeRef_Accessors(t *testing.T) {
	span := location.Span{
		Source: sourcetest.ID("test://dt-accessors"),
		Start:  location.Position{Line: 5, Column: 1, Offset: 50},
		End:    location.Position{Line: 5, Column: 20, Offset: 70},
	}

	ref := schema.TestNewDataTypeRef("common", "Currency", span)

	assert.Equal(t, "common", ref.Qualifier())
	assert.Equal(t, "Currency", ref.Name())
	assert.Equal(t, span, ref.Span())
}

func TestDataTypeRef_IsZero(t *testing.T) {
	zero := schema.DataTypeRef{}
	nonZero := schema.TestNewDataTypeRef("", "Type", location.Span{})

	assert.True(t, zero.IsZero())
	assert.False(t, nonZero.IsZero())
}

func TestDataTypeRef_String_Qualified(t *testing.T) {
	ref := schema.TestNewDataTypeRef("common", "Money", location.Span{})

	assert.Equal(t, "common.Money", ref.String())
}

func TestDataTypeRef_String_Local(t *testing.T) {
	ref := schema.TestNewDataTypeRef("", "Email", location.Span{})

	assert.Equal(t, "Email", ref.String())
}
