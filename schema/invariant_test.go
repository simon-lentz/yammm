package schema_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/schema/expr"
)

func TestNewInvariant(t *testing.T) {
	span := location.Span{
		Source: location.MustNewSourceID("test://invariant"),
		Start:  location.Position{Line: 10, Column: 1, Byte: 100},
		End:    location.Position{Line: 10, Column: 50, Byte: 150},
	}
	doc := "Age must be positive"
	e := expr.NewLiteral(true) // use a real expression

	inv := schema.NewInvariant("age must be positive", e, span, doc)

	assert.NotNil(t, inv)
	assert.Equal(t, "age must be positive", inv.Name())
	assert.Equal(t, e, inv.Expression())
	assert.Equal(t, span, inv.Span())
	assert.Equal(t, doc, inv.Documentation())
}

func TestInvariant_Name(t *testing.T) {
	tests := []struct {
		name     string
		message  string
		expected string
	}{
		{"simple message", "must be valid", "must be valid"},
		{"detailed message", "start date must be before end date", "start date must be before end date"},
		{"empty message", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inv := schema.NewInvariant(tt.message, nil, location.Span{}, "")
			assert.Equal(t, tt.expected, inv.Name())
		})
	}
}

func TestInvariant_Expression(t *testing.T) {
	tests := []struct {
		name string
		expr expr.Expression
	}{
		{"literal expression", expr.NewLiteral(42)},
		{"nil expression", nil},
		{"boolean expression", expr.NewLiteral(true)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inv := schema.NewInvariant("test", tt.expr, location.Span{}, "")
			assert.Equal(t, tt.expr, inv.Expression())
		})
	}
}

func TestInvariant_Span(t *testing.T) {
	span := location.Span{
		Source: location.MustNewSourceID("test://span"),
		Start:  location.Position{Line: 15, Column: 5, Byte: 200},
		End:    location.Position{Line: 15, Column: 40, Byte: 235},
	}

	inv := schema.NewInvariant("test", nil, span, "")

	result := inv.Span()
	assert.Equal(t, span.Source, result.Source)
	assert.Equal(t, 15, result.Start.Line)
	assert.Equal(t, 5, result.Start.Column)
	assert.Equal(t, 200, result.Start.Byte)
}

func TestInvariant_Documentation(t *testing.T) {
	tests := []struct {
		name string
		doc  string
	}{
		{"with documentation", "Ensures the value is within valid bounds"},
		{"empty documentation", ""},
		{"multiline documentation", "Line 1: Description\nLine 2: Details"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inv := schema.NewInvariant("test", nil, location.Span{}, tt.doc)
			assert.Equal(t, tt.doc, inv.Documentation())
		})
	}
}
