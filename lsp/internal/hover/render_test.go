package hover

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/simon-lentz/yammm/internal/yammmtest"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"

	"github.com/simon-lentz/yammm/lsp/internal/symbols"
)

// TestRenderSymbol_Golden renders one symbol of every kind — schema,
// import, concrete/abstract/part type, property, association, composition,
// datatype, and the root-relative source-path variants — and pins the
// complete markdown output in a single sectioned golden. The golden owns
// the exact layout; no substring re-checks needed.
func TestRenderSymbol_Golden(t *testing.T) {
	t.Parallel()

	mustType := func(t *testing.T, s *schema.Schema, name string) *schema.Type {
		t.Helper()
		typ, ok := s.Type(name)
		require.True(t, ok)
		return typ
	}

	cases := []struct {
		name  string
		root  string
		build func(t *testing.T) *symbols.Symbol
	}{
		{
			name: "schema",
			build: func(t *testing.T) *symbols.Symbol {
				t.Helper()
				return &symbols.Symbol{Name: "MySchema", Kind: symbols.SymbolSchema}
			},
		},
		{
			name: "import",
			build: func(t *testing.T) *symbols.Symbol {
				t.Helper()
				sources := map[string][]byte{
					"/test/main.yammm":  []byte("schema \"main\"\nimport \"./parts\" as parts\n"),
					"/test/parts.yammm": []byte("schema \"parts\"\ntype Wheel {\n  id String primary\n}\n"),
				}
				s, result := schema.LoadSourcesWithEntry(t.Context(), sources, "/test/main.yammm", "/test")
				require.False(t, result.HasErrors(), "load: %s", result)
				var imp *schema.Import
				for i := range s.Imports() {
					imp = i
					break
				}
				require.NotNil(t, imp)
				return &symbols.Symbol{Name: "parts", Kind: symbols.SymbolImport, Data: imp}
			},
		},
		{
			name: "documented type with project root",
			root: "/project",
			build: func(t *testing.T) *symbols.Symbol {
				t.Helper()
				s, result := schema.NewBuilder().
					WithName("test").
					WithSourceID(location.MustNewSourceID("test://types.yammm")).
					AddType("Person").
					WithTypeDocumentation("A person entity.").
					WithPrimaryKey("id", schema.NewStringConstraint()).
					Done().
					Build()
				require.False(t, result.HasErrors())
				return &symbols.Symbol{Name: "Person", Kind: symbols.SymbolType, SourceID: s.SourceID(), Data: mustType(t, s, "Person")}
			},
		},
		{
			name: "abstract type",
			build: func(t *testing.T) *symbols.Symbol {
				t.Helper()
				s, result := schema.NewBuilder().
					WithName("test").
					WithSourceID(location.MustNewSourceID("test://types.yammm")).
					AddType("Entity").
					AsAbstract().
					WithPrimaryKey("id", schema.NewStringConstraint()).
					Done().
					Build()
				require.False(t, result.HasErrors())
				return &symbols.Symbol{Name: "Entity", Kind: symbols.SymbolType, SourceID: s.SourceID(), Data: mustType(t, s, "Entity")}
			},
		},
		{
			name: "part type",
			build: func(t *testing.T) *symbols.Symbol {
				t.Helper()
				s, result := schema.NewBuilder().
					WithName("test").
					WithSourceID(location.MustNewSourceID("test://types.yammm")).
					AddType("Wheel").
					AsPart().
					WithPrimaryKey("id", schema.NewStringConstraint()).
					Done().
					Build()
				require.False(t, result.HasErrors())
				return &symbols.Symbol{Name: "Wheel", Kind: symbols.SymbolType, SourceID: s.SourceID(), Data: mustType(t, s, "Wheel")}
			},
		},
		{
			name: "property",
			build: func(t *testing.T) *symbols.Symbol {
				t.Helper()
				s, result := schema.NewBuilder().
					WithName("test").
					WithSourceID(location.MustNewSourceID("test://p.yammm")).
					AddType("Person").
					WithPrimaryKey("id", schema.NewStringConstraint()).
					WithProperty("name", schema.NewStringConstraint()).
					Done().
					Build()
				require.False(t, result.HasErrors())
				prop, ok := mustType(t, s, "Person").Property("name")
				require.True(t, ok)
				return &symbols.Symbol{Name: "name", Kind: symbols.SymbolProperty, ParentName: "Person", Data: prop}
			},
		},
		{
			name: "association",
			build: func(t *testing.T) *symbols.Symbol {
				t.Helper()
				s, result := schema.NewBuilder().
					WithName("test").
					WithSourceID(location.MustNewSourceID("test://test.yammm")).
					AddType("Address").
					WithPrimaryKey("id", schema.NewStringConstraint()).
					Done().
					AddType("Person").
					WithPrimaryKey("id", schema.NewStringConstraint()).
					WithRelation("ADDRESSES", schema.LocalTypeRef("Address", location.Span{}), false, true).
					Done().
					Build()
				require.False(t, result.HasErrors())
				rel, ok := mustType(t, s, "Person").Relation("ADDRESSES")
				require.True(t, ok)
				return &symbols.Symbol{Name: "ADDRESSES", Kind: symbols.SymbolAssociation, ParentName: "Person", Data: rel}
			},
		},
		{
			name: "composition",
			build: func(t *testing.T) *symbols.Symbol {
				t.Helper()
				s, result := schema.NewBuilder().
					WithName("test").
					WithSourceID(location.MustNewSourceID("test://test.yammm")).
					AddType("Wheel").
					AsPart().
					WithPrimaryKey("id", schema.NewStringConstraint()).
					Done().
					AddType("Car").
					WithPrimaryKey("id", schema.NewStringConstraint()).
					WithComposition("WHEELS", schema.LocalTypeRef("Wheel", location.Span{}), false, true).
					Done().
					Build()
				require.False(t, result.HasErrors())
				rel, ok := mustType(t, s, "Car").Relation("WHEELS")
				require.True(t, ok)
				return &symbols.Symbol{Name: "WHEELS", Kind: symbols.SymbolComposition, ParentName: "Car", Data: rel}
			},
		},
		{
			name: "datatype",
			build: func(t *testing.T) *symbols.Symbol {
				t.Helper()
				s, result := schema.LoadString(t.Context(), "schema \"test\"\n/* A short name string. */\ntype ShortName = String", "test.yammm")
				require.False(t, result.HasErrors(), "load: %s", result)
				dt, ok := s.DataType("ShortName")
				require.True(t, ok)
				return &symbols.Symbol{Name: "ShortName", Kind: symbols.SymbolDataType, Data: dt}
			},
		},
		{
			name: "type with source path relative to root",
			root: "/project",
			build: func(t *testing.T) *symbols.Symbol {
				t.Helper()
				sourceID, err := location.SourceIDFromAbsolutePath("/project/schemas/person.yammm")
				require.NoError(t, err)
				s, result := schema.NewBuilder().
					WithName("test").
					WithSourceID(sourceID).
					AddType("TestType").
					WithPrimaryKey("id", schema.NewStringConstraint()).
					Done().
					Build()
				require.False(t, result.HasErrors())
				return &symbols.Symbol{Name: "TestType", Kind: symbols.SymbolType, SourceID: sourceID, Data: mustType(t, s, "TestType")}
			},
		},
		{
			name: "type with absolute source path when no root",
			build: func(t *testing.T) *symbols.Symbol {
				t.Helper()
				sourceID, err := location.SourceIDFromAbsolutePath("/project/person.yammm")
				require.NoError(t, err)
				s, result := schema.NewBuilder().
					WithName("test").
					WithSourceID(sourceID).
					AddType("TestType").
					WithPrimaryKey("id", schema.NewStringConstraint()).
					Done().
					Build()
				require.False(t, result.HasErrors())
				return &symbols.Symbol{Name: "TestType", Kind: symbols.SymbolType, SourceID: sourceID, Data: mustType(t, s, "TestType")}
			},
		},
	}

	var out strings.Builder
	for _, tc := range cases {
		out.WriteString("=== " + tc.name + " ===\n")
		out.WriteString(RenderSymbol(tc.build(t), tc.root))
		out.WriteString("\n")
	}
	yammmtest.Golden(t, "render_symbol", []byte(out.String()))
}
