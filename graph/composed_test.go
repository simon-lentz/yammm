package graph_test

import (
	"slices"
	"testing"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/internal/instancetest"
	"github.com/simon-lentz/yammm/schema"
)

func TestAdd_OneCompositionAttachesItsChild(t *testing.T) {
	s := testSchemaWithOneComposition(t)
	g := graph.New(s)

	child := partData(t, s, "Child", []any{"c1"}, map[string]any{"name": "Child 1"})
	parent := parentWith(t, s, "Parent", []any{"p1"}, map[string]any{"name": "Parent 1"}, "CHILD", child)
	if err := g.Add(t.Context(), parent).Err(); err != nil {
		t.Errorf("Add should succeed: %v", err)
	}

	parents := slices.Collect(g.Snapshot().InstancesOf(mustTypeID(t, s, "Parent")))
	if len(parents) != 1 {
		t.Fatalf("Expected 1 parent, got %d", len(parents))
	}
	assertComposedCount(t, parents[0], "CHILD", 1)
}

func TestAdd_ManyCompositionAttachesKeyedChildren(t *testing.T) {
	s := testSchemaWithComposition(t)
	g := graph.New(s)

	var children []map[string]any
	for _, id := range []string{"c1", "c2", "c3"} {
		children = append(children, partData(t, s, "Child", []any{id}, map[string]any{"name": "Child " + id}))
	}
	parent := parentWith(t, s, "Parent", []any{"p1"}, map[string]any{"name": "Parent 1"}, "CHILDREN", children...)
	if err := g.Add(t.Context(), parent).Err(); err != nil {
		t.Errorf("Add should succeed: %v", err)
	}

	parents := slices.Collect(g.Snapshot().InstancesOf(mustTypeID(t, s, "Parent")))
	assertComposedCount(t, parents[0], "CHILDREN", 3)
	if attached := slices.Collect(parents[0].Composed("CHILDREN")); len(attached) != 3 {
		t.Fatalf("Expected 3 children, got %d", len(attached))
	}
}

func TestAdd_ManyCompositionKeepsEveryKeylessChild(t *testing.T) {
	s := testSchemaWithPKLessChild(t)
	g := graph.New(s)

	var items []map[string]any
	for range 3 {
		items = append(items, map[string]any{"value": "item"})
	}
	container := parentWith(t, s, "Container", []any{"box1"}, map[string]any{"name": "Box 1"}, "ITEMS", items...)
	if err := g.Add(t.Context(), container).Err(); err != nil {
		t.Errorf("Add should succeed: %v", err)
	}

	containers := slices.Collect(g.Snapshot().InstancesOf(mustTypeID(t, s, "Container")))
	assertComposedCount(t, containers[0], "ITEMS", 3)
}

// TestAdd_InheritingPartTypeChild_PassesTheKeyRule pins the composed-child key
// rule against inheritance, the one shape this package's corpus never declared:
// the rule reads the type's primary keys, and an inherited key has to be in
// that set or every validated child of an extending part type is refused.
func TestAdd_InheritingPartTypeChild_PassesTheKeyRule(t *testing.T) {
	ctx := t.Context()
	const src = `schema "fleet"

part type Base {
	id String primary
}

part type Wheel extends Base {
	position String required
}

type Car {
	vin String primary

	*-> WHEELS (many) Wheel
}
`
	s, res := schema.LoadString(ctx, src, "test://inheriting_part.yammm")
	if res.HasErrors() {
		t.Fatalf("schema load: %s", res.String())
	}

	inst := instancetest.Valid(t, s, "Car", map[string]any{
		"vin": "v1",
		"wheels": []any{
			map[string]any{"id": "w1", "position": "left"},
			map[string]any{"id": "w2", "position": "right"},
		},
	})

	g := graph.New(s)
	if r := g.Add(ctx, inst); !r.OK() {
		t.Fatalf("a validated child of an extending part type was refused: %s", r.String())
	}
	car := slices.Collect(g.Snapshot().InstancesOf(inst.Type().ID()))[0]
	if n := car.ComposedCount("WHEELS"); n != 2 {
		t.Fatalf("children attached = %d, want 2", n)
	}
}
