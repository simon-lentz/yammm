package hover

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"

	"github.com/simon-lentz/yammm/lsp/internal/symbols"
)

func TestHoverForSchema(t *testing.T) {
	t.Parallel()

	sym := &symbols.Symbol{
		Name: "MySchema",
		Kind: symbols.SymbolSchema,
	}

	result := RenderSymbol(sym, "")

	assert.Contains(t, result, "**schema**")
	assert.Contains(t, result, "MySchema")
}

func TestHoverForImport(t *testing.T) {
	t.Parallel()

	sourceID := location.MustNewSourceID("test://main.yammm")
	importedID := location.MustNewSourceID("test://parts.yammm")
	span := location.Range(sourceID, 1, 1, 1, 30)

	imp := schema.InternalNewImport("./parts", "parts", importedID, span)

	sym := &symbols.Symbol{
		Name: "parts",
		Kind: symbols.SymbolImport,
		Data: imp,
	}

	result := RenderSymbol(sym, "")

	assert.Contains(t, result, "**import**")
	assert.Contains(t, result, "./parts")
	assert.Contains(t, result, "parts")
}

func TestHoverForType(t *testing.T) {
	t.Parallel()

	sourceID := location.MustNewSourceID("test://types.yammm")
	span := location.Range(sourceID, 1, 1, 10, 1)

	typ := schema.InternalNewType("Person", sourceID, span, "A person entity.", false, false)

	sym := &symbols.Symbol{
		Name:     "Person",
		Kind:     symbols.SymbolType,
		SourceID: sourceID,
		Data:     typ,
	}

	result := RenderSymbol(sym, "/project")

	assert.Contains(t, result, "**type**")
	assert.Contains(t, result, "Person")
	assert.Contains(t, result, "A person entity.")
}

func TestHoverForType_Abstract(t *testing.T) {
	t.Parallel()

	sourceID := location.MustNewSourceID("test://types.yammm")
	span := location.Range(sourceID, 1, 1, 10, 1)

	typ := schema.InternalNewType("Entity", sourceID, span, "", true, false)

	sym := &symbols.Symbol{
		Name:     "Entity",
		Kind:     symbols.SymbolType,
		SourceID: sourceID,
		Data:     typ,
	}

	result := RenderSymbol(sym, "")
	assert.Contains(t, result, "**abstract type**")
}

func TestHoverForType_Part(t *testing.T) {
	t.Parallel()

	sourceID := location.MustNewSourceID("test://types.yammm")
	span := location.Range(sourceID, 1, 1, 10, 1)

	typ := schema.InternalNewType("Wheel", sourceID, span, "", false, true)

	sym := &symbols.Symbol{
		Name:     "Wheel",
		Kind:     symbols.SymbolType,
		SourceID: sourceID,
		Data:     typ,
	}

	result := RenderSymbol(sym, "")
	assert.Contains(t, result, "**part type**")
}

func TestHoverForProperty(t *testing.T) {
	t.Parallel()

	span := location.Range(location.MustNewSourceID("test://p.yammm"), 1, 1, 1, 20)

	prop := schema.InternalNewProperty("name", span, "The person's name.", nil, schema.DataTypeRef{}, false, false, schema.DeclaringScope{})

	sym := &symbols.Symbol{
		Name:       "name",
		Kind:       symbols.SymbolProperty,
		ParentName: "Person",
		Data:       prop,
	}

	result := RenderSymbol(sym, "")

	assert.Contains(t, result, "**property**")
	assert.Contains(t, result, "Person.name")
	assert.Contains(t, result, "The person's name.")
}

func TestHoverForProperty_Required(t *testing.T) {
	t.Parallel()

	span := location.Range(location.MustNewSourceID("test://p.yammm"), 1, 1, 1, 20)

	prop := schema.InternalNewProperty("email", span, "", nil, schema.DataTypeRef{}, false, false, schema.DeclaringScope{})

	sym := &symbols.Symbol{
		Name:       "email",
		Kind:       symbols.SymbolProperty,
		ParentName: "User",
		Data:       prop,
	}

	result := RenderSymbol(sym, "")
	assert.Contains(t, result, "**property**")
}

func TestHoverForRelation_Association(t *testing.T) {
	t.Parallel()

	targetRef := schema.NewTypeRef("", "Address", location.Span{})

	rel := schema.InternalNewRelation(
		schema.RelationAssociation,
		"ADDRESSES",
		"addresses",
		targetRef,
		schema.TypeID{},
		location.Span{},
		"",
		false,
		true,
		"",
		false,
		false,
		"",
		nil,
	)

	sym := &symbols.Symbol{
		Name:       "ADDRESSES",
		Kind:       symbols.SymbolAssociation,
		ParentName: "Person",
		Data:       rel,
	}

	result := RenderSymbol(sym, "")

	assert.Contains(t, result, "**association**")
	assert.Contains(t, result, "-->")
	assert.Contains(t, result, "many")
	assert.Contains(t, result, "Address")
}

func TestHoverForRelation_Composition(t *testing.T) {
	t.Parallel()

	targetRef := schema.NewTypeRef("", "Wheel", location.Span{})

	rel := schema.InternalNewRelation(
		schema.RelationComposition,
		"WHEELS",
		"wheels",
		targetRef,
		schema.TypeID{},
		location.Span{},
		"",
		false,
		true,
		"",
		false,
		false,
		"",
		nil,
	)

	sym := &symbols.Symbol{
		Name:       "WHEELS",
		Kind:       symbols.SymbolComposition,
		ParentName: "Car",
		Data:       rel,
	}

	result := RenderSymbol(sym, "")

	assert.Contains(t, result, "**composition**")
	assert.Contains(t, result, "*->")
}

func TestHoverForInvariant(t *testing.T) {
	t.Parallel()

	inv := schema.InternalNewInvariant("age must be positive", nil, location.Span{}, "Ensures age is valid.")

	sym := &symbols.Symbol{
		Name:       "age must be positive",
		Kind:       symbols.SymbolInvariant,
		ParentName: "Person",
		Data:       inv,
	}

	result := RenderSymbol(sym, "")

	assert.Contains(t, result, "**invariant**")
	assert.Contains(t, result, "age must be positive")
	assert.Contains(t, result, "Ensures age is valid.")
}

func TestHoverForDataType(t *testing.T) {
	t.Parallel()

	constraint := schema.NewStringConstraint()
	dt := schema.InternalNewDataType("ShortName", constraint, location.Span{}, "A short name string.")

	sym := &symbols.Symbol{
		Name: "ShortName",
		Kind: symbols.SymbolDataType,
		Data: dt,
	}

	result := RenderSymbol(sym, "")

	assert.Contains(t, result, "**datatype**")
	assert.Contains(t, result, "ShortName")
	assert.Contains(t, result, "A short name string.")
}

func TestRelativeSourcePath(t *testing.T) {
	t.Parallel()

	schemaSourceID, err := location.SourceIDFromAbsolutePath("/project/schemas/person.yammm")
	require.NoError(t, err)
	personSourceID, err := location.SourceIDFromAbsolutePath("/project/person.yammm")
	require.NoError(t, err)

	tests := []struct {
		name     string
		sourceID location.SourceID
		root     string
		want     string
	}{
		{"relative path within root", schemaSourceID, "/project", "./schemas/person.yammm"},
		{"no root", personSourceID, "", "/project/person.yammm"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sym := &symbols.Symbol{
				Name:     "TestType",
				Kind:     symbols.SymbolType,
				SourceID: tt.sourceID,
				Data:     schema.InternalNewType("TestType", tt.sourceID, location.Span{}, "", false, false),
			}
			result := RenderSymbol(sym, tt.root)
			assert.Contains(t, result, tt.want)
		})
	}
}
