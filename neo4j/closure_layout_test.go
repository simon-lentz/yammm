package neo4j

import (
	"slices"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/internal/instancetest"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/schema/cypher"
)

const layoutEntry = `schema "app"

import "lib/item" as x

type Item {
	id String primary
	--> HOLDS (_:one) x.Item
	*-> LINES (_:many) Line
	*-> MORE (_:many) x.Line
}

part type Line {
	code String primary
}
`

const layoutLib = `schema "lib"

type Item {
	id String primary
	--> NEXT (_:one) Item
}

part type Line {
	code String primary
}
`

// loadLayout loads layoutEntry from entryPath and layoutLib from lib/item.yammm
// under one module root. Moving the entry file moves its source path past the
// library's while both texts and both schema names stay the same. Both schemas
// declare Item and Line, so only the schema name tells their identities apart.
func loadLayout(t *testing.T, entryPath string) schema.Schema {
	t.Helper()
	s, err := schema.LoadSourcesWithEntry(t.Context(), map[string][]byte{
		entryPath:        []byte(layoutEntry),
		"lib/item.yammm": []byte(layoutLib),
	}, entryPath, schema.WithSourcesOnly(true), schema.WithModuleRoot("/project"))
	if err != nil {
		t.Fatalf("load with the entry at %s: %v", entryPath, err)
	}
	return s
}

// layoutStatements returns the statements NodeQueries and EdgeQueries write for
// a graph in which each schema's type sources an edge and the root composes a
// part of each schema, in the order the two calls return them.
func layoutStatements(t *testing.T, s schema.Schema) []string {
	t.Helper()
	target := func(key string) map[string]any { return map[string]any{"_target_id": key} }
	g := graph.New(s)
	for _, add := range []struct {
		typeName string
		props    map[string]any
	}{
		{"x.Item", map[string]any{"id": "k"}},
		{"x.Item", map[string]any{"id": "j", "next": target("k")}},
		{"Item", map[string]any{
			"id": "a", "holds": target("k"),
			"lines": []any{map[string]any{"code": "l"}},
			"more":  []any{map[string]any{"code": "m"}},
		}},
	} {
		if err := g.Add(t.Context(), instancetest.Valid(t, s, add.typeName, add.props)); err != nil {
			t.Fatalf("add %s %v: %v", add.typeName, add.props["id"], err)
		}
	}
	snap := g.Snapshot()
	nodes, err := NodeQueries(t.Context(), cypher.Config{}, snap)
	if err != nil {
		t.Fatalf("NodeQueries: %v", err)
	}
	edges, err := EdgeQueries(t.Context(), cypher.Config{}, snap)
	if err != nil {
		t.Fatalf("EdgeQueries: %v", err)
	}
	if len(edges) != 2 {
		t.Fatalf("fixture is vacuous: %d edge queries, want one per signature, 2", len(edges))
	}
	var statements []string
	for _, q := range nodes {
		statements = append(statements, q.Statement)
	}
	for _, q := range edges {
		statements = append(statements, q.Statement)
	}
	return statements
}

// TestQueries_ClosureLayoutDoesNotMoveStatements pins that the statements a
// snapshot writes, and their order, follow the schema names, not the source
// paths: one closure loaded with its entry file at two places writes one list.
func TestQueries_ClosureLayoutDoesNotMoveStatements(t *testing.T) {
	t.Parallel()

	atRoot, inDir := loadLayout(t, "main.yammm"), loadLayout(t, "a/main.yammm")
	if atRoot.SourceID().String() < "/project/lib" == (inDir.SourceID().String() < "/project/lib") {
		t.Fatal("fixture is vacuous: the two layouts order the entry and the library alike")
	}
	a, b := layoutStatements(t, atRoot), layoutStatements(t, inDir)
	if !slices.Equal(a, b) {
		t.Errorf("one closure in two layouts writes two statement lists:\n%q\n---\n%q", a, b)
	}
}

const orderEntry = `schema "app"

import "lib/item" as x

type Item {
	id String primary
	--> ALSO (_:many) x.Item
	--> ZED (_:many) Item
	*-> MORE (_:many) x.Line
	*-> LINES (_:many) Line
}

part type Line {
	code String primary
}
`

const orderLib = `schema "lib"

type Item {
	id String primary
	--> AAA (_:many) Item
}

part type Line {
	code String primary
}
`

// TestQueries_OrderBySchemaNameThenTypeName pins the order of the statements
// EdgeQueries returns, by source type, then relationship type, and the part
// labels a CompositionReplace statement lists. The relationship names and the
// declaration order of the compositions run against the type order, and the
// entry's path sorts after the library's.
func TestQueries_OrderBySchemaNameThenTypeName(t *testing.T) {
	t.Parallel()
	s, err := schema.LoadSourcesWithEntry(t.Context(), map[string][]byte{
		"z/main.yammm":   []byte(orderEntry),
		"lib/item.yammm": []byte(orderLib),
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
		{"x.Item", map[string]any{"id": "k"}},
		{"x.Item", map[string]any{"id": "j", "aaa": targets("k", "j")}},
		{"Item", map[string]any{
			"id": "a", "also": targets("k", "j"), "zed": targets("a", "b"),
			"lines": []any{map[string]any{"code": "l"}},
			"more":  []any{map[string]any{"code": "m"}},
		}},
		{"Item", map[string]any{"id": "b", "also": targets("j")}},
	} {
		if err := g.Add(t.Context(), instancetest.Valid(t, s, add.typeName, add.props)); err != nil {
			t.Fatalf("add %s %v: %v", add.typeName, add.props["id"], err)
		}
	}
	snap := g.Snapshot()

	edges, err := EdgeQueries(t.Context(), cypher.Config{}, snap)
	if err != nil {
		t.Fatalf("EdgeQueries: %v", err)
	}
	var relations []string
	for _, q := range edges {
		_, rest, _ := strings.Cut(q.Statement, "[r:")
		rel, _, _ := strings.Cut(rest, "]")
		relations = append(relations, rel)
	}
	if want := []string{"ALSO", "ZED", "AAA"}; !slices.Equal(relations, want) {
		t.Errorf("EdgeQueries writes relationship types %q, want %q", relations, want)
	}

	nodes, err := NodeQueries(t.Context(), cypher.Config{}, snap)
	if err != nil {
		t.Fatalf("NodeQueries: %v", err)
	}
	var replaced bool
	for _, q := range nodes {
		if q.Kind != CompositionReplace {
			continue
		}
		replaced = true
		if !strings.Contains(q.Statement, "(:app__Line|lib__Line)") {
			t.Errorf("CompositionReplace statement lists its part labels out of order:\n%s", q.Statement)
		}
	}
	if !replaced {
		t.Fatal("fixture is vacuous: NodeQueries wrote no CompositionReplace statement")
	}
}
