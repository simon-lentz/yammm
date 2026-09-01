package neo4j

import (
	"context"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/schema"
)

// The colliding fixture: entry and deep both declare Beacon, and deep is
// reachable only through base, so both identities render the bare name
// "Beacon". Graph.Add refuses a transitively imported type, so the collision
// is built through graph.RebuildSnapshot — the path a persisted document takes.
const collideEntrySource = `schema "entry"

import "base.yammm" as base

type Beacon {
	id String primary
	power Float
}
`

const collideBaseSource = `schema "base"

import "deep.yammm" as deep

type Basin {
	id String primary
}
`

const collideDeepSource = `schema "deep"

type Beacon {
	id String primary
	power Float
}
`

// collidingSnapshot builds a snapshot holding both Beacons, whose rendered
// names collide.
func collidingSnapshot(t *testing.T) (*graph.Snapshot, schema.TypeID, schema.TypeID) {
	t.Helper()
	sources := map[string][]byte{
		"entry.yammm": []byte(collideEntrySource),
		"base.yammm":  []byte(collideBaseSource),
		"deep.yammm":  []byte(collideDeepSource),
	}
	s, result := schema.LoadSourcesWithEntry(t.Context(), sources, "entry.yammm", ".", schema.WithSourcesOnly(true))
	if result.HasErrors() {
		t.Fatalf("load colliding fixture: %s", result)
	}

	localTyp, ok := s.Type("Beacon")
	if !ok {
		t.Fatal("local Beacon not found in entry schema")
	}
	localID := localTyp.ID()

	base, ok := s.ImportByAlias("base")
	if !ok || base.Schema() == nil {
		t.Fatal("import alias base did not resolve")
	}
	deep, ok := base.Schema().ImportByAlias("deep")
	if !ok || deep.Schema() == nil {
		t.Fatal("import alias deep did not resolve")
	}
	deepTyp, ok := deep.Schema().Type("Beacon")
	if !ok {
		t.Fatal("transitive Beacon not found in deep schema")
	}
	deepID := deepTyp.ID()

	if schema.TagForm(s, localID) != schema.TagForm(s, deepID) {
		t.Fatalf("fixture is vacuous: the two Beacons render different names (%q, %q)",
			schema.TagForm(s, localID), schema.TagForm(s, deepID))
	}

	beacon := func(id schema.TypeID, key string) graph.InstanceParts {
		return graph.InstanceParts{
			TypeName:   schema.TagForm(s, id),
			TypeID:     id,
			PrimaryKey: immutable.WrapKey([]any{key}),
			Properties: immutable.WrapProperties(map[string]any{"id": key, "power": float64(1)}),
		}
	}
	snap, res := graph.RebuildSnapshot(s, graph.SnapshotParts{
		Types: []schema.TypeID{localID, deepID},
		Instances: map[schema.TypeID][]graph.InstanceParts{
			localID: {beacon(localID, "local1")},
			deepID:  {beacon(deepID, "deep1")},
		},
	})
	if res.HasErrors() {
		t.Fatalf("assembling colliding snapshot: %s", res)
	}
	return snap, localID, deepID
}

// Two identities that render one TAG FORM are written under DISTINCT labels,
// and the writer no longer refuses them.
//
// Before v0.12.0 keyed GraphShape.Types by rendered name, such a pair really
// would have shared one shape, and the write path refused the snapshot to stop
// it. Keying by schema.TypeID removed the sharing: each identity gets its own
// shape, and Adapter.Label composes the label from the DECLARING schema's name,
// so the two land on entry__Beacon and deep__Beacon. The refusal outlived what
// it protected against and was blocking snapshots the identity-keyed path
// writes correctly.
//
// Mutation: keying shape.Types by schema.TagForm instead of by TypeID collapses
// the pair and turns this red.
func TestBatchNodeQueries_TagFormCollisionWritesDistinctLabels(t *testing.T) {
	t.Parallel()
	snap, localID, deepID := collidingSnapshot(t)
	ctx := context.Background()
	a := New()

	shape, res := a.ShapeForSchema(ctx, snap.Schema())
	if shape == nil {
		t.Fatalf("ShapeForSchema refused a closure whose labels do not collide: %s", res)
	}
	localLabel, deepLabel := shape.Types[localID].Label, shape.Types[deepID].Label
	if localLabel == deepLabel {
		t.Fatalf("the two identities share label %q; the premise of this test is gone", localLabel)
	}

	queries, err := a.BatchNodeQueries(ctx, snap, shape)
	if err != nil {
		t.Fatalf("writer refused a snapshot the identity-keyed path handles: %v", err)
	}
	seen := map[string]bool{}
	for _, q := range queries {
		if q.Kind != NodeMerge {
			continue
		}
		for _, label := range []string{localLabel, deepLabel} {
			if strings.Contains(q.Statement, label) {
				seen[label] = true
			}
		}
	}
	for _, label := range []string{localLabel, deepLabel} {
		if !seen[label] {
			t.Errorf("no MERGE was emitted for label %q", label)
		}
	}
}

// The edge path takes the same snapshot without refusing it.
func TestBatchEdgeQueries_TagFormCollisionAccepted(t *testing.T) {
	t.Parallel()
	snap, _, _ := collidingSnapshot(t)
	ctx := context.Background()
	a := New()

	shape, res := a.ShapeForSchema(ctx, snap.Schema())
	if shape == nil {
		t.Fatalf("ShapeForSchema: %s", res)
	}
	if _, err := a.BatchEdgeQueries(ctx, snap, shape); err != nil {
		t.Errorf("edge path refused a tag-form collision the identity-keyed path handles: %v", err)
	}
}

// A nil GraphShape is refused by name rather than failing later with a
// per-type "no shape" error that does not say the shape was never built.
func TestBatchNodeQueries_NilShapeRefused(t *testing.T) {
	t.Parallel()
	snap, _, _ := collidingSnapshot(t)
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
	snap, _, _ := collidingSnapshot(t)

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
	snap, _, _ := collidingSnapshot(t)

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
