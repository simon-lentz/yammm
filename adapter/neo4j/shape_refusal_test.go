package neo4j

import (
	"context"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/internal/snapparts"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/internal/snappartstest"
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
func shapeFixtureSnapshot(t *testing.T) graph.Snapshot {
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

	return snappartstest.Loaded(t, s, snapparts.Parts{
		Types: []schema.TypeID{id},
		Instances: []snapparts.Instance{
			{
				TypeID:     id,
				PrimaryKey: immutable.WrapKey([]any{"b1"}),
				Properties: immutable.WrapProperties(map[string]any{"id": "b1", "power": float64(1)}),
			},
		},
	})
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
// Mutation: dropping the zero-schema check in requireShapeFor turns this red.
func TestBatchNodeQueries_UnbuiltShapeRefused(t *testing.T) {
	t.Parallel()
	snap := shapeFixtureSnapshot(t)

	_, err := New().BatchNodeQueries(context.Background(), snap, &GraphShape{
		types: map[schema.TypeID]nodeShape{},
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
// Mutation: dropping the schema comparison turns this red.
func TestBatchNodeQueries_ShapeFromAnotherSchemaRefused(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	a := New()
	snap := shapeFixtureSnapshot(t)

	other, res := schema.LoadString(ctx, "schema \"unrelated\"\n\ntype Widget {\n\tid String primary\n}\n", "other.yammm")
	if other.IsZero() {
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

// Two loads of one source name share a SourceID and nothing else, so a shape from
// the first can hold a String key constraint where the snapshot's load holds a
// Date. The merge key would then reach the driver as a string beside a native
// date in the properties.
func TestBatchQueries_ShapeFromAnotherLoadOfOneSourceRefused(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	a := New()
	snap := shapeFixtureSnapshot(t)

	sources := map[string]string{
		"the same text": shapeFixtureSource,
		"another key":   strings.Replace(shapeFixtureSource, "id String primary", "id Date primary", 1),
	}
	for name, src := range sources {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			other, res := schema.LoadString(ctx, src, "entry.yammm")
			if other.IsZero() {
				t.Fatalf("second load: %s", res)
			}
			if other.SourceID() != snap.Schema().SourceID() {
				t.Fatalf("second load's SourceID = %s, want the snapshot's %s", other.SourceID(), snap.Schema().SourceID())
			}
			shape, sres := a.ShapeForSchema(ctx, other)
			if shape == nil {
				t.Fatalf("ShapeForSchema: %s", sres)
			}
			if _, err := a.BatchNodeQueries(ctx, snap, shape); err == nil || !strings.Contains(err.Error(), "another load") {
				t.Errorf("BatchNodeQueries error = %v, want one naming another load", err)
			}
			if _, err := a.BatchEdgeQueries(ctx, snap, shape); err == nil || !strings.Contains(err.Error(), "another load") {
				t.Errorf("BatchEdgeQueries error = %v, want one naming another load", err)
			}
		})
	}
}
