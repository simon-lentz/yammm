package graph_test

import (
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/internal/instancetest"
	"github.com/simon-lentz/yammm/schema"
)

const closureOrderEntry = `schema "app"

import "lib/item" as x

type Item {
	id String primary
	--> ALSO (_:many) x.Item
	--> ZED (_:many) Item
}

type Zed {
	id String primary
	--> TO (_:many) Item
}
`

const closureOrderLib = `schema "lib"

type Item {
	id String primary
	--> AAA (_:many) Item
}
`

// TestSnapshot_ClosureOrdersBySchemaNameThenTypeName pins every order a
// snapshot states over the types of a closure. The keys and relation names
// run against the type order, so an order that drops or reverses a type arm
// reads otherwise; the entry's path sorts after the library's, so a path order
// reads otherwise too.
func TestSnapshot_ClosureOrdersBySchemaNameThenTypeName(t *testing.T) {
	t.Parallel()
	s, err := schema.LoadSourcesWithEntry(t.Context(), map[string][]byte{
		"z/main.yammm":   []byte(closureOrderEntry),
		"lib/item.yammm": []byte(closureOrderLib),
	}, "z/main.yammm", schema.WithSourcesOnly(true), schema.WithModuleRoot("/project"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	targets := func(keys ...string) []any {
		var out []any
		for _, k := range keys {
			out = append(out, map[string]any{"_target_id": k})
		}
		return out
	}
	g := graph.New(s)
	for _, add := range []struct {
		typeName string
		props    map[string]any
	}{
		{"x.Item", map[string]any{"id": "k", "aaa": targets("k", "gone")}},
		{"x.Item", map[string]any{"id": "k", "aaa": targets("gone")}},
		{"Zed", map[string]any{"id": "a", "to": targets("z", "missing")}},
		{"Zed", map[string]any{"id": "a"}},
		{"Item", map[string]any{"id": "z", "also": targets("k", "gone"), "zed": targets("z", "gone")}},
		{"Item", map[string]any{"id": "z"}},
	} {
		err := g.Add(t.Context(), instancetest.Valid(t, s, add.typeName, add.props))
		if err != nil && !errors.Is(err, diag.E_DUPLICATE_PK) {
			t.Fatalf("add %s %v: %v", add.typeName, add.props["id"], err)
		}
	}
	snap := g.Snapshot()
	name := func(id schema.TypeID) string { return schema.TagForm(s, id) }

	var types, edges, unresolved, duplicates []string
	for _, id := range snap.Types() {
		types = append(types, name(id))
	}
	for e := range snap.Edges() {
		edges = append(edges, fmt.Sprintf("%s%s %s %s%s",
			name(e.Source().TypeID()), e.Source().PrimaryKey(), e.Relation(), name(e.Target().TypeID()), e.Target().PrimaryKey()))
	}
	for u := range snap.Unresolved() {
		unresolved = append(unresolved, fmt.Sprintf("%s%s %s %s[%s]",
			name(u.Source().TypeID()), u.Source().PrimaryKey(), u.Relation(), name(u.TargetType()), u.TargetKey()))
	}
	for d := range snap.Duplicates() {
		duplicates = append(duplicates, name(d.Instance().TypeID())+d.Instance().PrimaryKey().String())
	}

	for _, c := range []struct {
		what      string
		got, want []string
	}{
		{"Types", types, []string{"Item", "Zed", "x.Item"}},
		{"Edges", edges, []string{
			`Item["z"] ALSO x.Item["k"]`,
			`Item["z"] ZED Item["z"]`,
			`Zed["a"] TO Item["z"]`,
			`x.Item["k"] AAA x.Item["k"]`,
		}},
		{"Unresolved", unresolved, []string{
			`Item["z"] ALSO x.Item[["gone"]]`,
			`Item["z"] ZED Item[["gone"]]`,
			`Zed["a"] TO Item[["missing"]]`,
			`x.Item["k"] AAA x.Item[["gone"]]`,
		}},
		{"Duplicates", duplicates, []string{`Item["z"]`, `Zed["a"]`, `x.Item["k"]`}},
	} {
		if !slices.Equal(c.got, c.want) {
			t.Errorf("%s:\n got %q\nwant %q", c.what, c.got, c.want)
		}
	}
}
