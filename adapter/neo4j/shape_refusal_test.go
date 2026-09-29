package neo4j

import (
	"context"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/schema"
)

// The GraphShape a write path will not accept.
//
// Each test below refuses a shape rather than a snapshot, so the snapshot is a
// fixture alone: one addressable root is enough, and the assertions are about a
// nil, unbuilt or foreign shape.

const shapeFixtureSource = `schema "entry"

type Beacon {
	id String primary
	power Float
}
`

// shapeFixtureSnapshot builds the smallest snapshot a write path accepts.
func shapeFixtureSnapshot(t *testing.T) *graph.Snapshot {
	t.Helper()
	s, res := schema.LoadString(t.Context(), shapeFixtureSource, "entry.yammm")
	if res.HasErrors() {
		t.Fatalf("load shape fixture: %s", res)
	}
	typ, ok := s.Type("Beacon")
	if !ok {
		t.Fatal("Beacon not found")
	}
	id := typ.ID()

	snap, rres := graph.RebuildSnapshot(s, graph.SnapshotParts{
		Types: []schema.TypeID{id},
		Instances: []graph.InstanceParts{
			{
				TypeID:     id,
				PrimaryKey: immutable.WrapKey([]any{"b1"}),
				Properties: immutable.WrapProperties(map[string]any{"id": "b1", "power": float64(1)}),
			},
		},
	})
	if rres.HasErrors() {
		t.Fatalf("assembling the shape fixture: %s", rres)
	}
	return snap
}

// A nil GraphShape is refused by name rather than failing later with a
// per-type "no shape" error that does not say the shape was never built.
func TestBatchNodeQueries_NilShapeRefused(t *testing.T) {
	t.Parallel()
	snap := shapeFixtureSnapshot(t)
	_, err := New().BatchNodeQueries(context.Background(), snap, nil)
	if err == nil || !strings.Contains(err.Error(), "nil GraphShape") {
		t.Errorf("nil shape error = %v, want one naming the nil GraphShape", err)
	}
}

// A shape the adapter did not build is refused, because it carries no key
// constraints: merge keys would reach the driver uncoerced while the same
// properties are coerced from the schema, and a MERGE whose key type disagrees
// with the stored property matches nothing and duplicates the node on every
// re-ingestion.
//
// Mutation: dropping the schemaID check in requireShapeFor turns this red.
func TestBatchNodeQueries_UnbuiltShapeRefused(t *testing.T) {
	t.Parallel()
	snap := shapeFixtureSnapshot(t)

	_, err := New().BatchNodeQueries(context.Background(), snap, &GraphShape{
		Types: map[schema.TypeID]NodeShape{},
	})
	if err == nil {
		t.Fatal("a shape the adapter did not build was accepted")
	}
	if !strings.Contains(err.Error(), "not built by Adapter.ShapeForSchema") {
		t.Errorf("error %q does not say the shape was not built", err)
	}
}

// A shape built from a DIFFERENT schema is refused. Unconstructibility alone
// does not cover this: the shape came from a legitimate constructor, and
// partially-overlapping schemas would match some TypeIDs and silently miss
// others.
//
// Mutation: dropping the schemaID equality check turns this red.
func TestBatchNodeQueries_ShapeFromAnotherSchemaRefused(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	a := New()
	snap := shapeFixtureSnapshot(t)

	other, res := schema.LoadString(ctx, "schema \"unrelated\"\n\ntype Widget {\n\tid String primary\n}\n", "other.yammm")
	if other == nil {
		t.Fatalf("load unrelated schema: %s", res)
	}
	otherShape, sres := a.ShapeForSchema(ctx, other)
	if otherShape == nil {
		t.Fatalf("ShapeForSchema: %s", sres)
	}

	_, err := a.BatchNodeQueries(ctx, snap, otherShape)
	if err == nil {
		t.Fatal("a shape built from another schema was accepted")
	}
	if !strings.Contains(err.Error(), "but the snapshot carries schema") {
		t.Errorf("error %q does not name the schema mismatch", err)
	}
}
