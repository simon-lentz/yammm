package snapshot_test

import (
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/location/path"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

// group6Schema declares a required association, so a graph can hold an
// unresolved record whose reason is "absent" — the shape whose stated target
// key and edge properties the wire cannot carry.
func group6Schema(t *testing.T) *schema.Schema {
	t.Helper()
	const src = `schema "group6"

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
	s, res := schema.LoadString(t.Context(), src, "group6.yammm")
	if res.HasErrors() {
		t.Fatalf("load: %s", res)
	}
	return s
}

// TestMarshal_DropsUnderAReasonTheWireRefuses_AreMarked drives the two
// W_SNAPSHOT_VALUE_DROPPED sites the registry doc enumerates. Both were
// asserted only ABSENT — one test requires the code not to appear on a clean
// write — so the emitting arms themselves ran in no test and the doc's claim
// about them rested on reading. Removing either arm turns this red.
func TestMarshal_DropsUnderAReasonTheWireRefuses_AreMarked(t *testing.T) {
	t.Parallel()
	s := group6Schema(t)
	sourceType, _ := s.Type("Source")

	// A stated target key and edge properties under reason "absent" is the
	// shape the wire cannot hold.
	targetType, _ := s.Type("Target")
	snap, res := graph.RebuildSnapshot(s, graph.SnapshotParts{
		Types: []schema.TypeID{sourceType.ID(), targetType.ID()},
		Instances: map[schema.TypeID][]graph.InstanceParts{sourceType.ID(): {{
			TypeName: "Source", TypeID: sourceType.ID(),
			PrimaryKey: immutable.WrapKey([]any{"s1"}),
			Properties: immutable.WrapProperties(map[string]any{"id": "s1"}),
		}}},
		Unresolved: []graph.UnresolvedParts{{
			Relation:   "LINK",
			SourceType: sourceType.ID(), SourceKey: immutable.WrapKey([]any{"s1"}),
			TargetType: targetType.ID(), TargetKey: immutable.WrapKey([]any{"t1"}),
			Properties: immutable.WrapProperties(map[string]any{"note": "kept nowhere"}),
			Required:   true,
			Reason:     "absent",
		}},
	})
	if res.HasErrors() {
		t.Fatalf("RebuildSnapshot: %s", res)
	}

	_, mres := snapshot.Marshal(t.Context(), snap)
	var keyMarked, propsMarked bool
	for is := range mres.Issues() {
		if is.Code() != diag.W_SNAPSHOT_VALUE_DROPPED {
			continue
		}
		if strings.Contains(is.Message(), "target key") {
			keyMarked = true
		}
		if strings.Contains(is.Message(), "edge properties") {
			propsMarked = true
		}
	}
	if !keyMarked {
		t.Errorf("the dropped target key was not marked: %s", mres)
	}
	if !propsMarked {
		t.Errorf("the dropped edge properties were not marked: %s", mres)
	}
}

// TestMarshal_ADuplicatesDiagnosticIsDroppedUnmarked pins the DELIBERATE silent
// drop the registry doc now names. The wire has no field for a duplicate's
// Diagnostic, so it is discarded without the warning — and a consumer that
// gated loss detection on the code alone would be told nothing was lost.
func TestMarshal_ADuplicatesDiagnosticIsDroppedUnmarked(t *testing.T) {
	t.Parallel()
	s := group6Schema(t)
	targetType, _ := s.Type("Target")

	row := func(key string) *instance.ValidInstance {
		return instance.NewValidInstance("Target", targetType.ID(),
			immutable.WrapKey([]any{key}),
			immutable.WrapProperties(map[string]any{"id": key}), nil, nil, nil)
	}
	g := graph.New(s)
	if r := g.Add(t.Context(), row("t1")); !r.OK() {
		t.Fatalf("add: %s", r)
	}
	if r := g.Add(t.Context(), row("t1")); r.OK() {
		t.Fatal("a duplicate primary key was accepted")
	}
	snap := g.Snapshot()
	if len(snap.Duplicates()) != 1 {
		t.Fatalf("duplicates = %d, want 1", len(snap.Duplicates()))
	}
	if snap.Duplicates()[0].Diagnostic.Code() == (diag.Code{}) {
		t.Fatal("the duplicate carries no diagnostic, so this test asserts nothing")
	}

	data, mres := snapshot.Marshal(t.Context(), snap)
	if mres.HasCode(diag.W_SNAPSHOT_VALUE_DROPPED) {
		t.Errorf("the deliberate silent drop was marked: %s", mres)
	}
	loaded, lres := snapshot.Load(t.Context(), data, s)
	if lres.HasErrors() {
		t.Fatalf("load: %s", lres)
	}
	if got := loaded.Duplicates()[0].Diagnostic.Code(); got != (diag.Code{}) {
		t.Errorf("the round trip kept the diagnostic as %q; the wire has no field for it", got)
	}
}

// TestLoad_ProvenanceSurvivesOnlyWhenTheDocumentHadOne pins the claim
// graph.provenanceKeyOf's godoc rests on. It asserted unconditionally that a
// loaded instance carries a provenance with a zero span, while its own closing
// sentence conditioned the same claim — and the writer emits none for an
// instance without one, so the opening was false for any document this library
// writes from such a graph.
func TestLoad_ProvenanceSurvivesOnlyWhenTheDocumentHadOne(t *testing.T) {
	t.Parallel()
	s := group6Schema(t)
	targetType, _ := s.Type("Target")

	build := func(prov *location.Provenance) *graph.Snapshot {
		t.Helper()
		snap, res := graph.RebuildSnapshot(s, graph.SnapshotParts{
			Types: []schema.TypeID{targetType.ID()},
			Instances: map[schema.TypeID][]graph.InstanceParts{targetType.ID(): {{
				TypeName: "Target", TypeID: targetType.ID(),
				PrimaryKey: immutable.WrapKey([]any{"t1"}),
				Properties: immutable.WrapProperties(map[string]any{"id": "t1"}),
				Provenance: prov,
			}}},
		})
		if res.HasErrors() {
			t.Fatalf("RebuildSnapshot: %s", res)
		}
		return snap
	}
	roundTrip := func(snap *graph.Snapshot) *graph.Instance {
		t.Helper()
		data, mres := snapshot.Marshal(t.Context(), snap)
		if mres.HasErrors() {
			t.Fatalf("marshal: %s", mres)
		}
		loaded, lres := snapshot.Load(t.Context(), data, s)
		if lres.HasErrors() {
			t.Fatalf("load: %s", lres)
		}
		return loaded.InstancesOf(targetType.ID())[0]
	}

	// With a provenance in the document: one comes back, and its span is zero.
	written := location.NewProvenance("a.json", path.Root(), location.Span{Start: location.Position{Line: 7, Column: 3}})
	got := roundTrip(build(written)).Provenance()
	if got == nil {
		t.Fatal("a document whose instance carried a provenance loaded without one")
	}
	if got.SourceName() != "a.json" {
		t.Errorf("source name = %q, want a.json", got.SourceName())
	}
	if span := got.Span(); span.Start.Line != 0 || span.Start.Column != 0 {
		t.Errorf("span = %d:%d, want the zero span the decoder builds", span.Start.Line, span.Start.Column)
	}

	// With none: none comes back, which the unconditional claim denied.
	if p := roundTrip(build(nil)).Provenance(); p != nil {
		t.Errorf("a document whose instance carried no provenance loaded with %v", p)
	}
}
