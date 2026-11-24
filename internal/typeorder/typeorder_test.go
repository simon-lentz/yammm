package typeorder_test

import (
	"testing"

	"github.com/simon-lentz/yammm/internal/typeorder"
	"github.com/simon-lentz/yammm/schema"
)

// TestCompare_SchemaNameThenTypeName pins the order over one closure whose
// entry file's path sorts after the library's while its schema name sorts
// before it, so neither a path order nor a type-name order passes.
func TestCompare_SchemaNameThenTypeName(t *testing.T) {
	t.Parallel()
	s, err := schema.LoadSourcesWithEntry(t.Context(), map[string][]byte{
		"z/main.yammm":   []byte("schema \"app\"\n\nimport \"lib/item\" as x\n\ntype Item {\n\tid String primary\n}\n\ntype Zed {\n\tid String primary\n}\n"),
		"lib/item.yammm": []byte("schema \"lib\"\n\ntype Item {\n\tid String primary\n}\n"),
	}, "z/main.yammm", schema.WithSourcesOnly(true), schema.WithModuleRoot("/project"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	id := func(name string) schema.TypeID {
		typ, ok := s.ResolveTypeName(name)
		if !ok {
			t.Fatalf("the entry schema resolves no %s", name)
		}
		return typ.ID()
	}
	appItem, appZed, libItem := id("Item"), id("Zed"), id("x.Item")
	if appItem.String() < libItem.String() {
		t.Fatal("fixture is vacuous: the entry's path sorts before the library's")
	}

	o := typeorder.New(s)
	for _, c := range []struct {
		name        string
		first, then schema.TypeID
	}{
		{"one type name, by schema name", appItem, libItem},
		{"schema name before type name", appZed, libItem},
		{"one schema, by type name", appItem, appZed},
	} {
		if got := o.Compare(c.first, c.then); got >= 0 {
			t.Errorf("%s: Compare = %d, want negative", c.name, got)
		}
		if got := o.Compare(c.then, c.first); got <= 0 {
			t.Errorf("%s: reversed Compare = %d, want positive", c.name, got)
		}
	}
	if got := o.Compare(libItem, libItem); got != 0 {
		t.Errorf("Compare of one identity = %d, want 0", got)
	}
}
