package snapshot_test

import (
	"slices"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/instancetest"
	"github.com/simon-lentz/yammm/internal/snapparts"
	"github.com/simon-lentz/yammm/internal/snappartstest"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/location/path"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

// dropReasonSchema declares a required association, so a graph can hold an
// unresolved record whose reason is "absent" — the shape whose stated target
// key and edge properties the wire cannot carry.
func dropReasonSchema(t *testing.T) schema.Schema {
	t.Helper()
	const src = `schema "drop_reason"

type Target {
	id String primary
}

type Source {
	id String primary
	--> LINK (one) Target {
		note String
	}
}
`
	s, res := schema.LoadString(t.Context(), src, "drop_reason.yammm")
	if res.HasErrors() {
		t.Fatalf("load: %s", res)
	}
	return s
}

// TestMarshal_ADuplicateDrawsNoDropWarning pins that the wire carries a
// duplicate's record whole: its issue lives in the graph's Snapshot.Diagnostics,
// which are transient, so marshalling loses nothing a warning would name.
func TestMarshal_ADuplicateDrawsNoDropWarning(t *testing.T) {
	t.Parallel()
	s := dropReasonSchema(t)

	row := func(key string) instance.ValidInstance {
		return instancetest.Valid(t, s, "Target", map[string]any{"id": key})
	}
	g := graph.New(s)
	if r := g.Add(t.Context(), row("t1")); !r.OK() {
		t.Fatalf("add: %s", r)
	}
	if r := g.Add(t.Context(), row("t1")); r.OK() {
		t.Fatal("a duplicate primary key was accepted")
	}
	snap := g.Snapshot()
	if len(slices.Collect(snap.Duplicates())) != 1 {
		t.Fatalf("duplicates = %d, want 1", len(slices.Collect(snap.Duplicates())))
	}

	data, mres := snapshot.Marshal(t.Context(), snap)
	if mres.HasCode(diag.W_SNAPSHOT_VALUE_DROPPED) {
		t.Errorf("the deliberate silent drop was marked: %s", mres)
	}
	loaded, lres := snapshot.Load(t.Context(), data, s)
	if lres.HasErrors() {
		t.Fatalf("load: %s", lres)
	}
	if got := len(slices.Collect(loaded.Duplicates())); got != 1 {
		t.Errorf("the round trip holds %d duplicates, want 1", got)
	}
}

// TestLoad_ProvenanceSurvivesOnlyWhenTheDocumentHadOne pins what graph's
// compareProvenance rests on: a loaded instance has a provenance only when the
// document's instance had one, and the span of that provenance is zero.
func TestLoad_ProvenanceSurvivesOnlyWhenTheDocumentHadOne(t *testing.T) {
	t.Parallel()
	s := dropReasonSchema(t)
	targetType, _ := s.Type("Target")

	build := func(prov location.Provenance) graph.Snapshot {
		t.Helper()
		snap := snappartstest.Loaded(t, s, snapparts.Parts{
			Types: []schema.TypeID{targetType.ID()},
			Instances: []snapparts.Instance{
				{
					TypeID:     targetType.ID(),
					PrimaryKey: immutable.WrapKey([]any{"t1"}),
					Properties: immutable.WrapProperties(map[string]any{"id": "t1"}),
					Provenance: prov,
				},
			},
		})
		return snap
	}
	roundTrip := func(snap graph.Snapshot) graph.Instance {
		t.Helper()
		data, mres := snapshot.Marshal(t.Context(), snap)
		if mres.HasErrors() {
			t.Fatalf("marshal: %s", mres)
		}
		loaded, lres := snapshot.Load(t.Context(), data, s)
		if lres.HasErrors() {
			t.Fatalf("load: %s", lres)
		}
		return slices.Collect(loaded.InstancesOf(targetType.ID()))[0]
	}

	// With a provenance in the document: one comes back, and its span is zero.
	written := location.NewProvenance("a.json", path.Root(), location.Span{Start: location.Position{Line: 7, Column: 3}})
	got := roundTrip(build(written)).Provenance()
	if got.IsZero() {
		t.Fatal("a document whose instance carried a provenance loaded without one")
	}
	if got.SourceName() != "a.json" {
		t.Errorf("source name = %q, want a.json", got.SourceName())
	}
	if span := got.Span(); span.Start.Line != 0 || span.Start.Column != 0 {
		t.Errorf("span = %d:%d, want the zero span the decoder builds", span.Start.Line, span.Start.Column)
	}

	// With none: none comes back, which the unconditional claim denied.
	if p := roundTrip(build(location.Provenance{})).Provenance(); !p.IsZero() {
		t.Errorf("a document whose instance carried no provenance loaded with %v", p)
	}
}
