package parse_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/internal/source"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/schema/internal/parse"
)

// registerSource is a helper to register a schema source for testing.
func registerSource(t *testing.T, reg *source.Registry, content, name string) location.SourceID {
	t.Helper()
	sourceID := location.MustNewSourceID("test://" + name)
	err := reg.Register(sourceID, []byte(content))
	require.NoError(t, err)
	return sourceID
}

func TestParser_SimpleSchema(t *testing.T) {
	schemaSource := `schema "test"

type Person {
	name String required
}`

	reg := source.NewRegistry()
	sourceID := registerSource(t, reg, schemaSource, "test.yammm")
	collector := diag.NewCollector(0)

	parser := parse.NewParser(sourceID, collector, reg, reg)
	model := parser.Parse([]byte(schemaSource))

	require.NotNil(t, model)
	assert.Equal(t, "test", model.Name)
	require.Len(t, model.Types, 1)
	assert.Equal(t, "Person", model.Types[0].Name)
	require.Len(t, model.Types[0].Properties, 1)
	assert.Equal(t, "name", model.Types[0].Properties[0].Name)
	assert.False(t, model.Types[0].Properties[0].Optional)

	result := collector.Result()
	assert.True(t, result.OK(), "expected no errors, got: %v", result)
}

func TestParser_TypeWithInheritance(t *testing.T) {
	schemaSource := `schema "test"

type Animal {
	species String
}

type Pet extends Animal {
	nickname String
}`

	reg := source.NewRegistry()
	sourceID := registerSource(t, reg, schemaSource, "test.yammm")
	collector := diag.NewCollector(0)

	parser := parse.NewParser(sourceID, collector, reg, reg)
	model := parser.Parse([]byte(schemaSource))

	require.NotNil(t, model)
	require.Len(t, model.Types, 2)

	// Check Pet inherits from Animal
	pet := model.Types[1]
	assert.Equal(t, "Pet", pet.Name)
	require.Len(t, pet.Inherits, 1)
	assert.Equal(t, "Animal", pet.Inherits[0].Name)
	assert.Empty(t, pet.Inherits[0].Qualifier)

	result := collector.Result()
	assert.True(t, result.OK(), "expected no errors, got: %v", result)
}

func TestParser_Association(t *testing.T) {
	schemaSource := `schema "test"

type Person {
	name String required
	--> friends Car
}`

	reg := source.NewRegistry()
	sourceID := registerSource(t, reg, schemaSource, "test.yammm")
	collector := diag.NewCollector(0)

	parser := parse.NewParser(sourceID, collector, reg, reg)
	model := parser.Parse([]byte(schemaSource))

	require.NotNil(t, model)
	require.Len(t, model.Types, 1)

	person := model.Types[0]
	require.Len(t, person.Relations, 1)

	rel := person.Relations[0]
	assert.Equal(t, parse.RelationAssociation, rel.Kind)
	assert.Equal(t, "friends", rel.Name)
	assert.Equal(t, "Car", rel.Target.Name)

	result := collector.Result()
	assert.True(t, result.OK(), "expected no errors, got: %v", result)
}

func TestParser_Composition(t *testing.T) {
	schemaSource := `schema "test"

part type Wheel {
	size Integer
}

type Car {
	*-> wheels Wheel
}`

	reg := source.NewRegistry()
	sourceID := registerSource(t, reg, schemaSource, "test.yammm")
	collector := diag.NewCollector(0)

	parser := parse.NewParser(sourceID, collector, reg, reg)
	model := parser.Parse([]byte(schemaSource))

	require.NotNil(t, model)
	require.Len(t, model.Types, 2)

	// Check Wheel is marked as part
	wheel := model.Types[0]
	assert.True(t, wheel.IsPart)
	assert.Equal(t, "Wheel", wheel.Name)

	// Check Car has composition to Wheel
	car := model.Types[1]
	require.Len(t, car.Relations, 1)

	rel := car.Relations[0]
	assert.Equal(t, parse.RelationComposition, rel.Kind)
	assert.Equal(t, "wheels", rel.Name)
	assert.Equal(t, "Wheel", rel.Target.Name)

	result := collector.Result()
	assert.True(t, result.OK(), "expected no errors, got: %v", result)
}

func TestParser_AbstractType(t *testing.T) {
	schemaSource := `schema "test"

abstract type Entity {
	id UUID primary
}`

	reg := source.NewRegistry()
	sourceID := registerSource(t, reg, schemaSource, "test.yammm")
	collector := diag.NewCollector(0)

	parser := parse.NewParser(sourceID, collector, reg, reg)
	model := parser.Parse([]byte(schemaSource))

	require.NotNil(t, model)
	require.Len(t, model.Types, 1)

	entity := model.Types[0]
	assert.True(t, entity.IsAbstract)
	assert.Equal(t, "Entity", entity.Name)

	// Check primary key property
	require.Len(t, entity.Properties, 1)
	assert.True(t, entity.Properties[0].IsPrimaryKey)

	result := collector.Result()
	assert.True(t, result.OK(), "expected no errors, got: %v", result)
}

func TestParser_DataType(t *testing.T) {
	schemaSource := `schema "test"

type Name = String[1, 100]

type Person {
	name Name required
}`

	reg := source.NewRegistry()
	sourceID := registerSource(t, reg, schemaSource, "test.yammm")
	collector := diag.NewCollector(0)

	parser := parse.NewParser(sourceID, collector, reg, reg)
	model := parser.Parse([]byte(schemaSource))

	require.NotNil(t, model)
	require.Len(t, model.DataTypes, 1)

	dt := model.DataTypes[0]
	assert.Equal(t, "Name", dt.Name) // datatype names preserve declared case

	// M3 fix: Verify AliasConstraint preserves case for references
	require.Len(t, model.Types, 1)
	require.Len(t, model.Types[0].Properties, 1)

	prop := model.Types[0].Properties[0]
	assert.Equal(t, "name", prop.Name) // property name is lowercase

	// The constraint should be an AliasConstraint with preserved case
	alias, ok := prop.Constraint.(schema.AliasConstraint)
	require.True(t, ok, "property constraint should be an AliasConstraint")
	assert.Equal(t, "Name", alias.DataTypeName(),
		"AliasConstraint must preserve datatype reference case")

	result := collector.Result()
	assert.True(t, result.OK(), "expected no errors, got: %v", result)
}

func TestParser_QualifiedDataTypeReference_PreservesCase(t *testing.T) {
	schemaSource := `schema "test"

import "types.yammm" as types

type Person {
	email types.Email required
}`

	reg := source.NewRegistry()
	sourceID := registerSource(t, reg, schemaSource, "test.yammm")
	collector := diag.NewCollector(0)

	parser := parse.NewParser(sourceID, collector, reg, reg)
	model := parser.Parse([]byte(schemaSource))

	require.NotNil(t, model)
	require.Len(t, model.Types, 1)
	require.Len(t, model.Types[0].Properties, 1)

	prop := model.Types[0].Properties[0]
	alias, ok := prop.Constraint.(schema.AliasConstraint)
	require.True(t, ok, "property constraint should be an AliasConstraint")

	// Qualified reference: "types.Email" (not "types.email")
	assert.Equal(t, "types.Email", alias.DataTypeName(),
		"qualified datatype reference must preserve case")

	result := collector.Result()
	assert.True(t, result.OK(), "expected no errors, got: %v", result)
}

func TestParser_Import(t *testing.T) {
	schemaSource := `schema "test"

import "parts.yammm" as parts

type Car {
	*-> wheels parts.Wheel
}`

	reg := source.NewRegistry()
	sourceID := registerSource(t, reg, schemaSource, "car.yammm")
	collector := diag.NewCollector(0)

	parser := parse.NewParser(sourceID, collector, reg, reg)
	model := parser.Parse([]byte(schemaSource))

	require.NotNil(t, model)
	require.Len(t, model.Imports, 1)

	imp := model.Imports[0]
	assert.Equal(t, "parts.yammm", imp.Path)
	assert.Equal(t, "parts", imp.Alias)

	// Check qualified type reference
	require.Len(t, model.Types, 1)
	require.Len(t, model.Types[0].Relations, 1)

	rel := model.Types[0].Relations[0]
	assert.Equal(t, "parts", rel.Target.Qualifier)
	assert.Equal(t, "Wheel", rel.Target.Name)

	result := collector.Result()
	assert.True(t, result.OK(), "expected no errors, got: %v", result)
}

func TestParser_ImportDerivedAlias(t *testing.T) {
	schemaSource := `schema "test"

import "parts.yammm"
`

	reg := source.NewRegistry()
	sourceID := registerSource(t, reg, schemaSource, "main.yammm")
	collector := diag.NewCollector(0)

	parser := parse.NewParser(sourceID, collector, reg, reg)
	model := parser.Parse([]byte(schemaSource))

	require.NotNil(t, model)
	require.Len(t, model.Imports, 1)

	imp := model.Imports[0]
	assert.Equal(t, "parts.yammm", imp.Path)
	assert.Equal(t, "parts", imp.Alias) // Derived from path

	result := collector.Result()
	assert.True(t, result.OK(), "expected no errors, got: %v", result)
}

func TestParser_ReservedKeywordAlias(t *testing.T) {
	schemaSource := `schema "test"

import "foo.yammm" as type
`

	reg := source.NewRegistry()
	sourceID := registerSource(t, reg, schemaSource, "test.yammm")
	collector := diag.NewCollector(0)

	parser := parse.NewParser(sourceID, collector, reg, reg)
	_ = parser.Parse([]byte(schemaSource))

	result := collector.Result()
	assert.False(t, result.OK(), "expected error for reserved keyword alias")
}

func TestParser_Invariant(t *testing.T) {
	schemaSource := `schema "test"

type Person {
	age Integer
	! "Age must be positive" age > 0
}`

	reg := source.NewRegistry()
	sourceID := registerSource(t, reg, schemaSource, "test.yammm")
	collector := diag.NewCollector(0)

	parser := parse.NewParser(sourceID, collector, reg, reg)
	model := parser.Parse([]byte(schemaSource))

	require.NotNil(t, model)
	require.Len(t, model.Types, 1)
	require.Len(t, model.Types[0].Invariants, 1)

	inv := model.Types[0].Invariants[0]
	assert.Equal(t, "Age must be positive", inv.Name)

	// Verify expression is compiled
	require.NotNil(t, inv.Expr, "invariant expression should be compiled")
	assert.Equal(t, ">", inv.Expr.Op(), "expression should be a > comparison")

	result := collector.Result()
	assert.True(t, result.OK(), "expected no errors, got: %v", result)
}

func TestParser_EnumConstraint(t *testing.T) {
	schemaSource := `schema "test"

type Order {
	status Enum["pending", "shipped", "delivered"] required
}`

	reg := source.NewRegistry()
	sourceID := registerSource(t, reg, schemaSource, "test.yammm")
	collector := diag.NewCollector(0)

	parser := parse.NewParser(sourceID, collector, reg, reg)
	model := parser.Parse([]byte(schemaSource))

	require.NotNil(t, model)
	require.Len(t, model.Types, 1)
	require.Len(t, model.Types[0].Properties, 1)

	prop := model.Types[0].Properties[0]
	assert.NotNil(t, prop.Constraint)

	result := collector.Result()
	assert.True(t, result.OK(), "expected no errors, got: %v", result)
}

func TestParser_SyntaxError(t *testing.T) {
	schemaSource := `schema "test"

type Person {
	name String required
	invalid syntax here
}`

	reg := source.NewRegistry()
	sourceID := registerSource(t, reg, schemaSource, "test.yammm")
	collector := diag.NewCollector(0)

	parser := parse.NewParser(sourceID, collector, reg, reg)
	_ = parser.Parse([]byte(schemaSource))

	result := collector.Result()
	assert.False(t, result.OK(), "expected syntax error")
}

func TestParser_SyntaxError_HasByteOffset(t *testing.T) {
	// Schema with syntax error on line 5
	schemaSource := `schema "test"

type Person {
	name String required
	invalid syntax here
}`

	reg := source.NewRegistry()
	sourceID := registerSource(t, reg, schemaSource, "test.yammm")
	collector := diag.NewCollector(0)

	parser := parse.NewParser(sourceID, collector, reg, reg)
	_ = parser.Parse([]byte(schemaSource))

	result := collector.Result()
	require.False(t, result.OK(), "expected syntax error")

	// Find the syntax error issue
	var syntaxErr diag.Issue
	found := false
	for issue := range result.Issues() {
		if issue.Code() == diag.E_SYNTAX {
			syntaxErr = issue
			found = true
			break
		}
	}
	require.True(t, found, "expected E_SYNTAX issue")

	// Verify span has byte offset (not -1)
	span := syntaxErr.Span()
	assert.True(t, span.Start.HasByte(),
		"syntax error span should have byte offset, got Byte=%d", span.Start.Byte)
	assert.GreaterOrEqual(t, span.Start.Byte, 0,
		"syntax error span byte offset should be >= 0, got %d", span.Start.Byte)
}

func TestTypeRef_String(t *testing.T) {
	tests := []struct {
		name      string
		ref       parse.TypeRef
		expected  string
		qualified bool
	}{
		{
			name:      "local type",
			ref:       parse.TypeRef{Name: "Person"},
			expected:  "Person",
			qualified: false,
		},
		{
			name:      "qualified type",
			ref:       parse.TypeRef{Qualifier: "parts", Name: "Wheel"},
			expected:  "parts.Wheel",
			qualified: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.ref.String())
			assert.Equal(t, tt.qualified, tt.ref.IsQualified())
		})
	}
}

func TestRelationKind_String(t *testing.T) {
	assert.Equal(t, "association", parse.RelationAssociation.String())
	assert.Equal(t, "composition", parse.RelationComposition.String())
}

func TestSpanBuilder_FromToken(t *testing.T) {
	// Simple ASCII source
	schemaSource := "schema \"test\""

	reg := source.NewRegistry()
	sourceID := registerSource(t, reg, schemaSource, "test.yammm")

	builder := parse.NewSpanBuilder(sourceID, reg, reg)

	// FromToken with nil returns zero span
	span := builder.FromToken(nil)
	assert.True(t, span.IsZero())
}

func TestSpanBuilder_FromContext(t *testing.T) {
	reg := source.NewRegistry()
	sourceID := registerSource(t, reg, "test", "test.yammm")

	builder := parse.NewSpanBuilder(sourceID, reg, reg)

	// FromContext with nil returns zero span
	span := builder.FromContext(nil)
	assert.True(t, span.IsZero())
}

func TestMustPositionAt_Exported(t *testing.T) {
	schemaSource := "test"

	reg := source.NewRegistry()
	sourceID := registerSource(t, reg, schemaSource, "test.yammm")

	pos := parse.MustPositionAt(reg, sourceID, 0)
	assert.Equal(t, 1, pos.Line)
	assert.Equal(t, 1, pos.Column)
}

func TestMustPositionAt_PanicOnUnknownSource(t *testing.T) {
	reg := source.NewRegistry()
	unknownID := location.NewSourceID("unknown.yammm")

	assert.Panics(t, func() {
		parse.MustPositionAt(reg, unknownID, 0)
	})
}
