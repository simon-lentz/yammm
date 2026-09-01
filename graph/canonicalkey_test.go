package graph_test

import (
	"testing"
	"time"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// TestGraph_EdgeResolvesAcrossKeySpellings pins the half the canonicalizer's
// own doc warned about: "canonicalizing a caller-supplied key would move it in
// the instance index and every edge endpoint would have to move with it."
//
// An edge written with one spelling of a Timestamp key must resolve against a
// target instance written with another, because both denote one instant.
func TestGraph_EdgeResolvesAcrossKeySpellings(t *testing.T) {
	s, res := schema.NewBuilder().
		WithName("linked").
		WithSourceID(location.MustNewSourceID("test://linked.yammm")).
		AddType("Event").
		WithPrimaryKey("observed_at", schema.TimestampConstraint{}).
		Done().
		AddType("Note").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithRelation("ABOUT", schema.NewTypeRef("", "Event", location.Span{}), false, false).
		Done().
		Build()
	if res.HasErrors() {
		t.Fatalf("schema: %s", res)
	}
	eventID, _ := s.Type("Event")
	noteID, _ := s.Type("Note")

	// The target instance carries the instant as a time.Time.
	parsed, err := time.Parse(time.RFC3339, "2020-01-02T03:04:05Z")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	event := instance.NewValidInstance("Event", eventID.ID(),
		immutable.WrapKey([]any{parsed}),
		immutable.WrapProperties(map[string]any{"observed_at": parsed}),
		nil, nil, nil)

	// The edge addresses it with a DIFFERENT spelling of the same instant.
	edges := map[string]*instance.ValidEdgeData{
		"ABOUT": instance.NewValidEdgeData([]instance.ValidEdgeTarget{
			instance.NewValidEdgeTarget(
				immutable.WrapKey([]any{"2020-01-02T03:04:05+00:00"}),
				immutable.WrapProperties(nil),
			),
		}),
	}
	note := instance.NewValidInstance("Note", noteID.ID(),
		immutable.WrapKey([]any{"n1"}),
		immutable.WrapProperties(map[string]any{"id": "n1"}),
		edges, nil, nil)

	g := graph.New(s)
	if r := g.Add(t.Context(), event); !r.OK() {
		t.Fatalf("add event: %s", r)
	}
	if r := g.Add(t.Context(), note); !r.OK() {
		t.Fatalf("add note: %s", r)
	}

	snap := g.Snapshot()
	if n := len(snap.Unresolved()); n != 0 {
		t.Errorf("the edge did not resolve across two spellings of one instant: %d unresolved", n)
		for _, u := range snap.Unresolved() {
			t.Logf("  unresolved: %s -> %s reason=%s", u.Relation, u.TargetKey, u.Reason)
		}
	}
	if n := len(snap.Edges()); n != 1 {
		t.Errorf("edges = %d, want 1", n)
	}
}
