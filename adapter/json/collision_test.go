package json

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
	s, result := schema.LoadSourcesWithEntry(t.Context(), sources, "entry.yammm", ".", schema.WithSourcesOnly())
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

// Mutation: neutralising the renderedNameCollision call in MarshalObject
// turns this red; forcing it to always error turns TestMarshalObject_Golden red.
func TestMarshalObject_RenderedNameCollisionRefused(t *testing.T) {
	t.Parallel()
	snap, localID, deepID := collidingSnapshot(t)

	_, err := New().MarshalObject(context.Background(), snap)
	if err == nil {
		t.Fatal("collision accepted: want an error naming both identities")
	}
	msg := err.Error()
	for _, want := range []string{localID.String(), deepID.String(), `"Beacon"`} {
		if !strings.Contains(msg, want) {
			t.Errorf("collision error %q does not name %s", msg, want)
		}
	}
}
