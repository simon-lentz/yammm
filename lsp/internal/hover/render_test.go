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

	sources := map[string][]byte{
		"/test/main.yammm":  []byte("schema \"main\"\nimport \"./parts\" as parts\n"),
		"/test/parts.yammm": []byte("schema \"parts\"\ntype Wheel {\n  id String primary\n}\n"),
	}
	s, result, err := schema.LoadSources(t.Context(), sources, "/test")
	require.NoError(t, err)
	require.False(t, result.HasErrors(), "load: %s", result)

	// Get first import
	var imp *schema.Import
	for i := range s.Imports() {
		imp = i
		break
	}
	require.NotNil(t, imp)

	sym := &symbols.Symbol{
		Name: "parts",
		Kind: symbols.SymbolImport,
		Data: imp,
	}

	hover := RenderSymbol(sym, "")

	assert.Contains(t, hover, "**import**")
	assert.Contains(t, hover, "./parts")
	assert.Contains(t, hover, "parts")
}

func TestHoverForType(t *testing.T) {
	t.Parallel()

	s, result := schema.NewBuilder().
		WithName("test").
		WithSourceID(location.MustNewSourceID("test://types.yammm")).
		AddType("Person").
		WithTypeDocumentation("A person entity.").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		Done().
		Build()
	require.False(t, result.HasErrors())

	typ, ok := s.Type("Person")
	require.True(t, ok)

	sym := &symbols.Symbol{
		Name:     "Person",
		Kind:     symbols.SymbolType,
		SourceID: s.SourceID(),
		Data:     typ,
	}

	hover := RenderSymbol(sym, "/project")

	assert.Contains(t, hover, "**type**")
	assert.Contains(t, hover, "Person")
	assert.Contains(t, hover, "A person entity.")
}

func TestHoverForType_Abstract(t *testing.T) {
	t.Parallel()

	s, result := schema.NewBuilder().
		WithName("test").
		WithSourceID(location.MustNewSourceID("test://types.yammm")).
		AddType("Entity").
		AsAbstract().
		WithPrimaryKey("id", schema.NewStringConstraint()).
		Done().
		Build()
	require.False(t, result.HasErrors())

	typ, ok := s.Type("Entity")
	require.True(t, ok)

	sym := &symbols.Symbol{
		Name:     "Entity",
		Kind:     symbols.SymbolType,
		SourceID: s.SourceID(),
		Data:     typ,
	}

	hover := RenderSymbol(sym, "")
	assert.Contains(t, hover, "**abstract type**")
}

func TestHoverForType_Part(t *testing.T) {
	t.Parallel()

	s, result := schema.NewBuilder().
		WithName("test").
		WithSourceID(location.MustNewSourceID("test://types.yammm")).
		AddType("Wheel").
		AsPart().
		WithPrimaryKey("id", schema.NewStringConstraint()).
		Done().
		Build()
	require.False(t, result.HasErrors())

	typ, ok := s.Type("Wheel")
	require.True(t, ok)

	sym := &symbols.Symbol{
		Name:     "Wheel",
		Kind:     symbols.SymbolType,
		SourceID: s.SourceID(),
		Data:     typ,
	}

	hover := RenderSymbol(sym, "")
	assert.Contains(t, hover, "**part type**")
}

func TestHoverForProperty(t *testing.T) {
	t.Parallel()

	s, result := schema.NewBuilder().
		WithName("test").
		WithSourceID(location.MustNewSourceID("test://p.yammm")).
		AddType("Person").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithProperty("name", schema.NewStringConstraint()).
		Done().
		Build()
	require.False(t, result.HasErrors())

	typ, ok := s.Type("Person")
	require.True(t, ok)

	prop, ok := typ.Property("name")
	require.True(t, ok)

	sym := &symbols.Symbol{
		Name:       "name",
		Kind:       symbols.SymbolProperty,
		ParentName: "Person",
		Data:       prop,
	}

	hover := RenderSymbol(sym, "")

	assert.Contains(t, hover, "**property**")
	assert.Contains(t, hover, "Person.name")
}

func TestHoverForProperty_Required(t *testing.T) {
	t.Parallel()

	s, result := schema.NewBuilder().
		WithName("test").
		WithSourceID(location.MustNewSourceID("test://p.yammm")).
		AddType("User").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithProperty("email", schema.NewStringConstraint()).
		Done().
		Build()
	require.False(t, result.HasErrors())

	typ, ok := s.Type("User")
	require.True(t, ok)

	prop, ok := typ.Property("email")
	require.True(t, ok)

	sym := &symbols.Symbol{
		Name:       "email",
		Kind:       symbols.SymbolProperty,
		ParentName: "User",
		Data:       prop,
	}

	hover := RenderSymbol(sym, "")
	assert.Contains(t, hover, "**property**")
}

func TestHoverForRelation_Association(t *testing.T) {
	t.Parallel()

	s, result := schema.NewBuilder().
		WithName("test").
		WithSourceID(location.MustNewSourceID("test://test.yammm")).
		AddType("Address").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		Done().
		AddType("Person").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithRelation("addresses", schema.LocalTypeRef("Address", location.Span{}), false, true).
		Done().
		Build()
	require.False(t, result.HasErrors())

	typ, ok := s.Type("Person")
	require.True(t, ok)

	rel, ok := typ.Relation("addresses")
	require.True(t, ok)

	sym := &symbols.Symbol{
		Name:       "addresses",
		Kind:       symbols.SymbolAssociation,
		ParentName: "Person",
		Data:       rel,
	}

	hover := RenderSymbol(sym, "")

	assert.Contains(t, hover, "**association**")
	assert.Contains(t, hover, "-->")
	assert.Contains(t, hover, "many")
	assert.Contains(t, hover, "Address")
}

func TestHoverForRelation_Composition(t *testing.T) {
	t.Parallel()

	s, result := schema.NewBuilder().
		WithName("test").
		WithSourceID(location.MustNewSourceID("test://test.yammm")).
		AddType("Wheel").
		AsPart().
		WithPrimaryKey("id", schema.NewStringConstraint()).
		Done().
		AddType("Car").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithComposition("wheels", schema.LocalTypeRef("Wheel", location.Span{}), false, true).
		Done().
		Build()
	require.False(t, result.HasErrors())

	typ, ok := s.Type("Car")
	require.True(t, ok)

	rel, ok := typ.Relation("wheels")
	require.True(t, ok)

	sym := &symbols.Symbol{
		Name:       "wheels",
		Kind:       symbols.SymbolComposition,
		ParentName: "Car",
		Data:       rel,
	}

	hover := RenderSymbol(sym, "")

	assert.Contains(t, hover, "**composition**")
	assert.Contains(t, hover, "*->")
}

func TestHoverForDataType(t *testing.T) {
	t.Parallel()

	s, result, err := schema.LoadString(t.Context(), "schema \"test\"\n/* A short name string. */\ntype ShortName = String", "test.yammm")
	require.NoError(t, err)
	require.False(t, result.HasErrors(), "load: %s", result)

	dt, ok := s.DataType("ShortName")
	require.True(t, ok)

	sym := &symbols.Symbol{
		Name: "ShortName",
		Kind: symbols.SymbolDataType,
		Data: dt,
	}

	hover := RenderSymbol(sym, "")

	assert.Contains(t, hover, "**datatype**")
	assert.Contains(t, hover, "ShortName")
	assert.Contains(t, hover, "A short name string.")
}

func TestRelativeSourcePath(t *testing.T) {
	t.Parallel()

	schemaSourceID, err := location.SourceIDFromAbsolutePath("/project/schemas/person.yammm")
	require.NoError(t, err)
	personSourceID, err := location.SourceIDFromAbsolutePath("/project/person.yammm")
	require.NoError(t, err)

	// Build a minimal schema to get a real *Type for each sub-test.
	buildType := func(sourceID location.SourceID) *schema.Type {
		s, result := schema.NewBuilder().
			WithName("test").
			WithSourceID(sourceID).
			AddType("TestType").
			WithPrimaryKey("id", schema.NewStringConstraint()).
			Done().
			Build()
		require.False(t, result.HasErrors())
		typ, ok := s.Type("TestType")
		require.True(t, ok)
		return typ
	}

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
				Data:     buildType(tt.sourceID),
			}
			result := RenderSymbol(sym, tt.root)
			assert.Contains(t, result, tt.want)
		})
	}
}
