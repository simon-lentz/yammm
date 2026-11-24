package hover

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/simon-lentz/yammm/internal/yammmtest"
	"github.com/simon-lentz/yammm/schema"

	"github.com/simon-lentz/yammm/internal/lsp/internal/symbols"
)

// TestRenderSymbol_Golden renders one symbol of every kind — schema,
// import, concrete/abstract/part type, property, association, composition,
// datatype, and the root-relative source-path variants — and pins the
// complete markdown output in a single sectioned golden. The golden owns
// the exact layout; no substring re-checks needed.
func TestRenderSymbol_Golden(t *testing.T) {
	t.Parallel()

	mustType := func(t *testing.T, s schema.Schema, name string) schema.Type {
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
					yammmtest.HostAbs("/test/main.yammm"):  []byte("schema \"main\"\nimport \"./parts\" as parts\n"),
					yammmtest.HostAbs("/test/parts.yammm"): []byte("schema \"parts\"\ntype Wheel {\n  id String primary\n}\n"),
				}
				s, err := schema.LoadSourcesWithEntry(t.Context(), sources, yammmtest.HostAbs("/test/main.yammm"), schema.WithModuleRoot(yammmtest.HostAbs("/test")))
				require.NoError(t, err, "load: %s", err)
				var imp schema.Import
				for i := range s.Imports() {
					imp = i
					break
				}
				require.False(t, imp.IsZero())
				return &symbols.Symbol{Name: "parts", Kind: symbols.SymbolImport, Data: imp}
			},
		},
		{
			name: "documented type with project root",
			root: "/project",
			build: func(t *testing.T) *symbols.Symbol {
				t.Helper()
				s, err := schema.LoadString(t.Context(), `schema "test"

/* A person entity. */
type Person {
	id String primary
}
`, "types.yammm")
				require.NoError(t, err)
				return &symbols.Symbol{Name: "Person", Kind: symbols.SymbolType, SourceID: s.SourceID(), Data: mustType(t, s, "Person")}
			},
		},
		{
			name: "abstract type",
			build: func(t *testing.T) *symbols.Symbol {
				t.Helper()
				s, err := schema.LoadString(t.Context(), `schema "test"

abstract type Entity {
	id String primary
}
`, "types.yammm")
				require.NoError(t, err)
				return &symbols.Symbol{Name: "Entity", Kind: symbols.SymbolType, SourceID: s.SourceID(), Data: mustType(t, s, "Entity")}
			},
		},
		{
			name: "part type",
			build: func(t *testing.T) *symbols.Symbol {
				t.Helper()
				s, err := schema.LoadString(t.Context(), `schema "test"

part type Wheel {
	id String primary
}
`, "types.yammm")
				require.NoError(t, err)
				return &symbols.Symbol{Name: "Wheel", Kind: symbols.SymbolType, SourceID: s.SourceID(), Data: mustType(t, s, "Wheel")}
			},
		},
		{
			name: "property",
			build: func(t *testing.T) *symbols.Symbol {
				t.Helper()
				s, err := schema.LoadString(t.Context(), `schema "test"

type Person {
	id String primary
	name String required
}
`, "p.yammm")
				require.NoError(t, err)
				prop, ok := mustType(t, s, "Person").Property("name")
				require.True(t, ok)
				return &symbols.Symbol{Name: "name", Kind: symbols.SymbolProperty, ParentName: "Person", Data: prop}
			},
		},
		{
			name: "association",
			build: func(t *testing.T) *symbols.Symbol {
				t.Helper()
				s, err := schema.LoadString(t.Context(), `schema "test"

type Address {
	id String primary
}

type Person {
	id String primary
	--> ADDRESSES (one:many) Address
}
`, "test.yammm")
				require.NoError(t, err)
				rel, ok := mustType(t, s, "Person").Relation("ADDRESSES")
				require.True(t, ok)
				return &symbols.Symbol{Name: "ADDRESSES", Kind: symbols.SymbolAssociation, ParentName: "Person", Data: rel}
			},
		},
		{
			name: "composition",
			build: func(t *testing.T) *symbols.Symbol {
				t.Helper()
				s, err := schema.LoadString(t.Context(), `schema "test"

part type Wheel {
	id String primary
}

type Car {
	id String primary
	*-> WHEELS (one:many) Wheel
}
`, "test.yammm")
				require.NoError(t, err)
				rel, ok := mustType(t, s, "Car").Relation("WHEELS")
				require.True(t, ok)
				return &symbols.Symbol{Name: "WHEELS", Kind: symbols.SymbolComposition, ParentName: "Car", Data: rel}
			},
		},
		{
			name: "datatype",
			build: func(t *testing.T) *symbols.Symbol {
				t.Helper()
				s, err := schema.LoadString(t.Context(), "schema \"test\"\n/* A short name string. */\ntype ShortName = String", "test.yammm")
				require.NoError(t, err, "load: %s", err)
				dt, ok := s.DataType("ShortName")
				require.True(t, ok)
				return &symbols.Symbol{Name: "ShortName", Kind: symbols.SymbolDataType, Data: dt}
			},
		},
		{
			name: "type with source path relative to root",
			root: yammmtest.HostAbs("/project"),
			build: func(t *testing.T) *symbols.Symbol {
				t.Helper()
				entry := yammmtest.HostAbs("/project/schemas/person.yammm")
				s, err := schema.LoadSourcesWithEntry(t.Context(), map[string][]byte{entry: []byte(`schema "test"

type TestType {
	id String primary
}
`)}, entry)
				require.NoError(t, err)
				return &symbols.Symbol{Name: "TestType", Kind: symbols.SymbolType, SourceID: s.SourceID(), Data: mustType(t, s, "TestType")}
			},
		},
		{
			name: "type with absolute source path when no root",
			build: func(t *testing.T) *symbols.Symbol {
				t.Helper()
				entry := yammmtest.HostAbs("/project/person.yammm")
				s, err := schema.LoadSourcesWithEntry(t.Context(), map[string][]byte{entry: []byte(`schema "test"

type TestType {
	id String primary
}
`)}, entry)
				require.NoError(t, err)
				return &symbols.Symbol{Name: "TestType", Kind: symbols.SymbolType, SourceID: s.SourceID(), Data: mustType(t, s, "TestType")}
			},
		},
	}

	var out strings.Builder
	for _, tc := range cases {
		out.WriteString("=== " + tc.name + " ===\n")
		out.WriteString(RenderSymbol(tc.build(t), tc.root))
		out.WriteString("\n")
	}
	// The golden spells each fixture identity the Unix way. Only the exact host
	// identity is rewritten, so an identity that lost its volume stays and fails.
	rendered := out.String()
	for _, fixture := range []string{"/test/parts.yammm", "/project/person.yammm"} {
		host := "`" + filepath.ToSlash(yammmtest.HostAbs(fixture)) + "`"
		if !strings.Contains(rendered, host) {
			t.Errorf("rendered hover does not name the fixture identity %s", host)
		}
		rendered = strings.ReplaceAll(rendered, host, "`"+fixture+"`")
	}
	yammmtest.Golden(t, "render_symbol", []byte(rendered))
}
