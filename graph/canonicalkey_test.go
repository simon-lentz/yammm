package graph_test

import (
	"slices"
	"testing"
	"time"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/internal/instancetest"
	"github.com/simon-lentz/yammm/schema"
)

// TestGraph_EdgeResolvesAcrossKeySpellings pins the half the canonicalizer's
// own doc warned about: "canonicalizing a caller-supplied key would move it in
// the instance index and every edge endpoint would have to move with it."
//
// An edge written with one spelling of a Timestamp key must resolve against a
// target instance written with another, because both denote one instant.
func TestGraph_EdgeResolvesAcrossKeySpellings(t *testing.T) {
	s, res := schema.LoadString(t.Context(), `schema "linked"

type Event {
	observed_at Timestamp primary
}

type Note {
	id String primary
	--> ABOUT (one) Event
}
`, "linked.yammm")
	if res.HasErrors() {
		t.Fatalf("schema: %s", res)
	}

	// The target instance carries the instant as a time.Time.
	parsed, err := time.Parse(time.RFC3339, "2020-01-02T03:04:05Z")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	event := instancetest.Valid(t, s, "Event", map[string]any{"observed_at": parsed})

	// The edge addresses it with a DIFFERENT spelling of the same instant.
	note := instancetest.Valid(t, s, "Note", map[string]any{
		"id":    "n1",
		"about": map[string]any{"_target_observed_at": "2020-01-02T03:04:05+00:00"},
	})

	g := graph.New(s)
	if r := g.Add(t.Context(), event); !r.OK() {
		t.Fatalf("add event: %s", r)
	}
	if r := g.Add(t.Context(), note); !r.OK() {
		t.Fatalf("add note: %s", r)
	}

	snap := g.Snapshot()
	if n := len(slices.Collect(snap.Unresolved())); n != 0 {
		t.Errorf("the edge did not resolve across two spellings of one instant: %d unresolved", n)
		for u := range snap.Unresolved() {
			t.Logf("  unresolved: %s -> %s reason=%s", u.Relation(), u.TargetKey(), u.Reason())
		}
	}
	if n := len(slices.Collect(snap.Edges())); n != 1 {
		t.Errorf("edges = %d, want 1", n)
	}
}

// TestAdd_HoldsTheStoredFormAtEveryPropertyPosition is the Add path's twin of
// TestRebuild_CanonicalizesEveryPropertyPosition: a Timestamp reaches
// Properties() as the text its constraint stores, whether it is a root's
// property, a composed child's key property or a child's other property.
func TestAdd_HoldsTheStoredFormAtEveryPropertyPosition(t *testing.T) {
	t.Parallel()
	s := loadSchemaAt(t, `schema "composed_spelling"

type Run {
	id String primary
	started_at Timestamp
	*-> SAMPLES (_:many) Sample
}

part type Sample {
	at Timestamp primary
}
`, "composed_spelling.yammm")
	runID := mustTypeID(t, s, "Run")

	const when = "2020-01-02T03:04:05+00:00"
	const wantText = "2020-01-02T03:04:05Z"

	run := instancetest.Valid(t, s, "Run", map[string]any{
		"id":         "r1",
		"started_at": when,
		"samples":    []any{map[string]any{"at": when}},
	})

	g := graph.New(s)
	if r := g.Add(t.Context(), run); !r.OK() {
		t.Fatalf("add: %s", r)
	}
	snap := g.Snapshot()
	root := slices.Collect(snap.InstancesOf(runID))[0]
	assertProp(t, root.Properties(), "started_at", wantText)
	child := slices.Collect(root.Composed("SAMPLES"))[0]
	assertProp(t, child.Properties(), "at", wantText)
	if got, want := child.PrimaryKey().String(), graph.FormatKey(wantText); got != want {
		t.Errorf("child key = %s, want %s", got, want)
	}
}
