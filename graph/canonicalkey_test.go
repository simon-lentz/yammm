package graph_test

import (
	"testing"
	"time"

	"github.com/simon-lentz/yammm/diag"
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

// composedSpellingSchema declares a (many) composition to a part type keyed
// by a Timestamp, so two siblings can address one instant two ways.
func composedSpellingSchema(t *testing.T) *schema.Schema {
	t.Helper()
	const src = `schema "composed_spelling"

type Run {
	id String primary
	started_at Timestamp
	*-> SAMPLES (_:many) Sample
}

part type Sample {
	at Timestamp primary
}
`
	s, res := schema.LoadString(t.Context(), src, "composed_spelling.yammm")
	if res.HasErrors() {
		t.Fatalf("load: %s", res)
	}
	return s
}

// TestGraph_ComposedSiblingsAreDuplicatesAcrossKeySpellings pins the sibling
// half of the rule TestGraph_TimestampKeyIdentityIsCanonical pins for roots:
// two children of one (many) slot whose keys spell one instant two ways are
// one child, on the inline path and on the streamed path alike.
func TestGraph_ComposedSiblingsAreDuplicatesAcrossKeySpellings(t *testing.T) {
	t.Parallel()
	s := composedSpellingSchema(t)
	runID := mustTypeID(t, s, "Run")
	sampleID := mustTypeID(t, s, "Sample")

	const spelled = "2020-01-02T03:04:05+00:00"
	parsed, err := time.Parse(time.RFC3339, spelled)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	sample := func(at any) *instance.ValidInstance {
		return instance.NewValidInstance("Sample", sampleID,
			immutable.WrapKey([]any{at}),
			immutable.WrapProperties(map[string]any{"at": at}),
			nil, nil, nil)
	}

	t.Run("inline", func(t *testing.T) {
		t.Parallel()
		run := instance.NewValidInstance("Run", runID,
			immutable.WrapKey([]any{"r1"}),
			immutable.WrapProperties(map[string]any{"id": "r1"}),
			nil, map[string]immutable.Value{
				"SAMPLES": immutable.Wrap([]any{sample(spelled), sample(parsed)}),
			}, nil)
		result := graph.New(s).Add(t.Context(), run)
		if result.OK() {
			t.Fatal("two spellings of one Timestamp key were accepted as two siblings")
		}
		assertHasCode(t, result, diag.E_DUPLICATE_COMPOSED_PK)
	})

	t.Run("streamed", func(t *testing.T) {
		t.Parallel()
		g := graph.New(s)
		run := instance.NewValidInstance("Run", runID,
			immutable.WrapKey([]any{"r1"}),
			immutable.WrapProperties(map[string]any{"id": "r1"}),
			nil, nil, nil)
		if r := g.Add(t.Context(), run); !r.OK() {
			t.Fatalf("add run: %s", r)
		}
		if r := g.AddComposed(t.Context(), runID, graph.FormatKey("r1"), "SAMPLES", sample(parsed)); !r.OK() {
			t.Fatalf("first sample: %s", r)
		}
		// The second child spells the instant the way the occupant does not.
		result := g.AddComposed(t.Context(), runID, graph.FormatKey("r1"), "SAMPLES", sample(spelled))
		if result.OK() {
			t.Fatal("a second spelling of the sibling's Timestamp key was accepted")
		}
		assertHasCode(t, result, diag.E_DUPLICATE_COMPOSED_PK)
	})
}

// TestGraph_AddCanonicalizesEveryPropertyPosition is the Add path's twin of
// TestRebuildSnapshot_CanonicalizesEveryPropertyPosition: a Timestamp reaches
// Properties() as the text its constraint stores, whether it is a root's
// property, a composed child's key property or a child's other property.
func TestGraph_AddCanonicalizesEveryPropertyPosition(t *testing.T) {
	t.Parallel()
	s := composedSpellingSchema(t)
	runID := mustTypeID(t, s, "Run")
	sampleID := mustTypeID(t, s, "Sample")

	when := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	const wantText = "2020-01-02T03:04:05Z"

	sample := instance.NewValidInstance("Sample", sampleID,
		immutable.WrapKey([]any{when}),
		immutable.WrapProperties(map[string]any{"at": when}),
		nil, nil, nil)
	run := instance.NewValidInstance("Run", runID,
		immutable.WrapKey([]any{"r1"}),
		immutable.WrapProperties(map[string]any{"id": "r1", "started_at": when}),
		nil, map[string]immutable.Value{"SAMPLES": immutable.Wrap([]any{sample})}, nil)

	g := graph.New(s)
	if r := g.Add(t.Context(), run); !r.OK() {
		t.Fatalf("add: %s", r)
	}
	snap := g.Snapshot()
	root := snap.InstancesOf(runID)[0]
	assertProp(t, root.Properties(), "started_at", wantText)
	child := root.Composed("SAMPLES")[0]
	assertProp(t, child.Properties(), "at", wantText)
	if got, want := child.PrimaryKey().String(), graph.FormatKey(wantText); got != want {
		t.Errorf("child key = %s, want %s", got, want)
	}
}
