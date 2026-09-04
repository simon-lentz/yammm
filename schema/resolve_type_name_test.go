package schema_test

import (
	"testing"

	"github.com/simon-lentz/yammm/schema"
)

// ResolveTypeName is the one entry-relative by-name resolve: a bare name for
// a type the schema declares, "alias.Name" for a type of a directly imported
// schema, and nothing else.
func TestSchema_ResolveTypeName(t *testing.T) {
	m := map[string][]byte{
		"entry.yammm": []byte(`schema "geo"

import "common.yammm" as common

type Anchor {
    id String primary
}
`),
		"common.yammm": []byte(`schema "common"

import "parts.yammm" as parts

type Region {
    id String primary
    *-> CELLS (many) parts.Cell
}
`),
		"parts.yammm": []byte(`schema "parts"

part type Cell {
    id String primary
}
`),
	}
	s, res := schema.LoadSourcesWithEntry(t.Context(), m, "entry.yammm", ".", schema.WithSourcesOnly(true))
	if !res.OK() {
		t.Fatalf("load: %s", res)
	}
	cases := []struct {
		name string
		want string // the resolved type's name, or "" for not found
	}{
		{"Anchor", "Anchor"},
		{"common.Region", "Region"},
		{"Region", ""},     // a directly imported type is not addressable bare
		{"parts.Cell", ""}, // a transitively imported type has no name form
		{"common.parts.Cell", ""},
		{".Anchor", ""}, // a leading dot is not an empty alias
		{"Anchor.", ""},
		{"", ""},
		{"nope.Anchor", ""},
	}
	for _, c := range cases {
		typ, ok := s.ResolveTypeName(c.name)
		if c.want == "" {
			if ok {
				t.Errorf("ResolveTypeName(%q) resolved to %s, want not found", c.name, typ.Name())
			}
			continue
		}
		if !ok || typ.Name() != c.want {
			t.Errorf("ResolveTypeName(%q) = %v, %v; want %s", c.name, typ, ok, c.want)
		}
	}
}
