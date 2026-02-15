package schema_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

func TestNewDataType(t *testing.T) {
	constraint := schema.NewStringConstraint()
	span := location.Span{
		Source: location.MustNewSourceID("test://datatype"),
		Start:  location.Position{Line: 1, Column: 1, Byte: 0},
		End:    location.Position{Line: 1, Column: 10, Byte: 10},
	}
	doc := "Email address type"

	dt := schema.NewDataType("Email", constraint, span, doc)

	assert.NotNil(t, dt)
	assert.Equal(t, "Email", dt.Name())
	assert.Equal(t, constraint, dt.Constraint())
	assert.Equal(t, span, dt.Span())
	assert.Equal(t, doc, dt.Documentation())
}

func TestDataType_Name(t *testing.T) {
	dt := schema.NewDataType("PhoneNumber", nil, location.Span{}, "")

	assert.Equal(t, "PhoneNumber", dt.Name())
}

func TestDataType_Constraint(t *testing.T) {
	tests := []struct {
		name       string
		constraint schema.Constraint
	}{
		{"with string constraint", schema.NewStringConstraint()},
		{"with integer constraint", schema.NewIntegerConstraint()},
		{"with nil constraint", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dt := schema.NewDataType("Test", tt.constraint, location.Span{}, "")
			assert.Equal(t, tt.constraint, dt.Constraint())
		})
	}
}

func TestDataType_Span(t *testing.T) {
	span := location.Span{
		Source: location.MustNewSourceID("test://span"),
		Start:  location.Position{Line: 5, Column: 1, Byte: 40},
		End:    location.Position{Line: 5, Column: 20, Byte: 60},
	}

	dt := schema.NewDataType("Money", nil, span, "")

	result := dt.Span()
	assert.Equal(t, span.Source, result.Source)
	assert.Equal(t, 5, result.Start.Line)
	assert.Equal(t, 40, result.Start.Byte)
}

func TestDataType_Documentation(t *testing.T) {
	tests := []struct {
		name string
		doc  string
	}{
		{"with documentation", "A currency amount with precision"},
		{"empty documentation", ""},
		{"multiline documentation", "Line 1\nLine 2\nLine 3"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dt := schema.NewDataType("Test", nil, location.Span{}, tt.doc)
			assert.Equal(t, tt.doc, dt.Documentation())
		})
	}
}
