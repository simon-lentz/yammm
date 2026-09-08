package snapshot_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

// TestUpdateMetadataOrReMarshal_FallbackKeepsCreatedAtBytes pins that the
// fallback carries a foreign header's created_at byte-for-byte, as the fast
// path does: fractional seconds and a non-UTC offset survive Load + Marshal.
func TestUpdateMetadataOrReMarshal_FallbackKeepsCreatedAtBytes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := testSchema(t)
	snap := buildSnapshot(t, s,
		mustValidInstance(t, s, "Company", []any{"c1"}, map[string]any{"id": "c1", "title": "Acme"}))
	stamped := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	data, res := snapshot.Marshal(ctx, snap, snapshot.WithCreatedAt(stamped))
	if res.HasErrors() {
		t.Fatalf("marshal: %v", res)
	}

	// A foreign writer's stamp: sub-second precision and an offset.
	const foreign = "2026-08-17T14:00:00.123456789+02:00"
	foreignDoc := bytes.Replace(data,
		[]byte(`"created_at":"2026-08-17T12:00:00Z"`),
		[]byte(`"created_at":"`+foreign+`"`), 1)
	if bytes.Equal(foreignDoc, data) {
		t.Fatal("fixture shape changed; created_at not found")
	}
	foreignDoc = rehashDocument(t, foreignDoc)

	// The same JSON-insignificant space the non-Marshal-shape test uses, so
	// the fast path refuses the document while every read path accepts it.
	spaced := bytes.Replace(foreignDoc, []byte(`},"types":`), []byte(`} ,"types":`), 1)
	if bytes.Equal(spaced, foreignDoc) {
		t.Fatal("fixture shape changed; header boundary not found")
	}
	spaced = rehashDocument(t, spaced)
	if _, fastRes := snapshot.UpdateMetadata(ctx, spaced, nil); !hasCode(fastRes, diag.E_UPDATE_METADATA_BODY_OFFSET) {
		t.Fatalf("the fast path accepted a non-Marshal shape: %v", fastRes)
	}

	out, outRes := snapshot.UpdateMetadataOrReMarshal(ctx, spaced, map[string]string{"stage": "fallback"}, s)
	if outRes.HasErrors() {
		t.Fatalf("the fallback failed: %v", outRes)
	}
	if !hasCode(outRes, diag.W_UPDATE_METADATA_FALLBACK) {
		t.Fatalf("no W_UPDATE_METADATA_FALLBACK warning: %v", outRes)
	}
	if hasCode(outRes, diag.W_SNAPSHOT_VALUE_DROPPED) {
		t.Errorf("the fallback reported dropping a created_at it could carry: %v", outRes)
	}
	if !bytes.Contains(out, []byte(`"created_at":"`+foreign+`"`)) {
		hdr, _ := snapshot.HeaderOnly(ctx, out)
		t.Errorf("created_at after the fallback = %q, want %q byte-for-byte", hdr.CreatedAt, foreign)
	}
}

// linkedTimestampSchema declares a Timestamp-keyed target and a type that
// addresses it through an association.
func linkedTimestampSchema(t *testing.T) *schema.Schema {
	t.Helper()
	const src = `schema "linked"

type Event {
	observed_at Timestamp primary
}

type Note {
	id String primary
	--> ABOUT (_) Event
}
`
	s, res := schema.LoadString(t.Context(), src, "linked.yammm")
	if res.HasErrors() {
		t.Fatalf("load: %s", res)
	}
	return s
}

// foreignSpellingDocument writes a document holding one Event and one Note
// addressing it, then rewrites the Event's key and property — and, when
// edgeToo is set, the edge's target key — to a non-canonical spelling of the
// same instant, as a foreign writer would, and re-signs it.
func foreignSpellingDocument(t *testing.T, s *schema.Schema, edgeToo bool) []byte {
	t.Helper()
	const canon = "2020-01-02T03:04:05Z"
	const raw = "2020-01-02T03:04:05+00:00"
	eventID, _ := s.Type("Event")
	noteID, _ := s.Type("Note")
	event := instance.NewValidInstance("Event", eventID.ID(), immutable.WrapKey([]any{canon}),
		immutable.WrapProperties(map[string]any{"observed_at": canon}), nil, nil, nil)
	edges := map[string]*instance.ValidEdgeData{"ABOUT": instance.NewValidEdgeData([]instance.ValidEdgeTarget{
		instance.NewValidEdgeTarget(immutable.WrapKey([]any{canon}), immutable.WrapProperties(nil)),
	})}
	note := instance.NewValidInstance("Note", noteID.ID(), immutable.WrapKey([]any{"n1"}),
		immutable.WrapProperties(map[string]any{"id": "n1"}), edges, nil, nil)
	g := graph.New(s)
	for _, inst := range []*instance.ValidInstance{event, note} {
		if r := g.Add(t.Context(), inst); !r.OK() {
			t.Fatalf("add: %s", r)
		}
	}
	data, mr := snapshot.Marshal(t.Context(), g.Snapshot())
	if mr.HasErrors() {
		t.Fatalf("marshal: %s", mr)
	}
	// The edge's target key is the last occurrence of the canonical text; the
	// Event's key and property come before it in the instances section.
	if edgeToo {
		return rehashDocument(t, bytes.ReplaceAll(data, []byte(canon), []byte(raw)))
	}
	last := bytes.LastIndex(data, []byte(canon))
	head := bytes.ReplaceAll(data[:last], []byte(canon), []byte(raw))
	return rehashDocument(t, append(head, data[last:]...))
}

// TestLoad_ForeignSpellingOfAKeyLoadsAndResolves pins that a document whose
// Timestamp key a foreign writer spelled non-canonically loads, indexes the
// instance by the key it carries, and resolves the edge that addresses it
// under either spelling.
func TestLoad_ForeignSpellingOfAKeyLoadsAndResolves(t *testing.T) {
	t.Parallel()
	s := linkedTimestampSchema(t)
	eventID, _ := s.Type("Event")
	for _, edgeToo := range []bool{true, false} {
		doc := foreignSpellingDocument(t, s, edgeToo)
		snap, res := snapshot.Load(t.Context(), doc, s)
		if res.HasErrors() {
			t.Fatalf("edge raw too = %v: Load: %s", edgeToo, res)
		}
		inst := snap.InstancesOf(eventID.ID())[0]
		if _, ok := snap.InstanceByKey(eventID.ID(), inst.PrimaryKey().String()); !ok {
			t.Errorf("edge raw too = %v: InstanceByKey by the instance's own key misses", edgeToo)
		}
		if n := len(snap.Edges()); n != 1 {
			t.Errorf("edge raw too = %v: edges = %d, want 1", edgeToo, n)
		}
	}
}

// TestLoad_TwoSpellingsOfOneKeyAreOneDuplicate pins the reader's uniqueness
// check on identity rather than on spelling: two roots keyed by two spellings
// of one instant are refused as a duplicate, by Load and by Verify alike.
func TestLoad_TwoSpellingsOfOneKeyAreOneDuplicate(t *testing.T) {
	t.Parallel()
	s := linkedTimestampSchema(t)
	eventID, _ := s.Type("Event")
	const first = "2020-01-02T03:04:05Z"
	const second = "2021-06-07T08:09:10Z"
	g := graph.New(s)
	for _, at := range []string{first, second} {
		ev := instance.NewValidInstance("Event", eventID.ID(), immutable.WrapKey([]any{at}),
			immutable.WrapProperties(map[string]any{"observed_at": at}), nil, nil, nil)
		if r := g.Add(t.Context(), ev); !r.OK() {
			t.Fatalf("add: %s", r)
		}
	}
	data, mr := snapshot.Marshal(t.Context(), g.Snapshot())
	if mr.HasErrors() {
		t.Fatalf("marshal: %s", mr)
	}
	// Respell the second root as the first instant, in the other form.
	doc := rehashDocument(t, bytes.ReplaceAll(data, []byte(second), []byte("2020-01-02T03:04:05+00:00")))

	snap, res := snapshot.Load(t.Context(), doc, s)
	if snap != nil || !res.HasErrors() {
		t.Fatalf("two spellings of one key loaded as two roots: %s", res)
	}
	if !hasCode(res, diag.E_DUPLICATE_PK) {
		t.Errorf("Load: want E_DUPLICATE_PK, got %s", res)
	}
	if vres := snapshot.Verify(t.Context(), doc, s); !hasCode(vres, diag.E_DUPLICATE_PK) {
		t.Errorf("Verify: want E_DUPLICATE_PK, got %s", vres)
	}
}

// TestLoad_UnresolvedTargetIsCanonicalized pins the reader's half of A-352:
// a document whose unresolved record carries a non-canonical spelling of the
// target instant loads with the canonical target key, as the graph would
// have written it.
func TestLoad_UnresolvedTargetIsCanonicalized(t *testing.T) {
	t.Parallel()
	s := linkedTimestampSchema(t)
	noteID, _ := s.Type("Note")
	const canon = "2020-01-02T03:04:05Z"
	const raw = "2020-01-02T03:04:05+00:00"
	edges := map[string]*instance.ValidEdgeData{"ABOUT": instance.NewValidEdgeData([]instance.ValidEdgeTarget{
		instance.NewValidEdgeTarget(immutable.WrapKey([]any{canon}), immutable.WrapProperties(nil)),
	})}
	note := instance.NewValidInstance("Note", noteID.ID(), immutable.WrapKey([]any{"n1"}),
		immutable.WrapProperties(map[string]any{"id": "n1"}), edges, nil, nil)
	g := graph.New(s)
	if r := g.Add(t.Context(), note); !r.OK() {
		t.Fatalf("add: %s", r)
	}
	data, mr := snapshot.Marshal(t.Context(), g.Snapshot())
	if mr.HasErrors() {
		t.Fatalf("marshal: %s", mr)
	}
	if !bytes.Contains(data, []byte(canon)) {
		t.Fatal("fixture shape changed; the unresolved target key not found")
	}
	doc := rehashDocument(t, bytes.ReplaceAll(data, []byte(canon), []byte(raw)))
	snap, res := snapshot.Load(t.Context(), doc, s)
	if res.HasErrors() {
		t.Fatalf("Load: %s", res)
	}
	if u := snap.Unresolved(); len(u) != 1 || u[0].TargetKey != graph.FormatKey(canon) {
		t.Errorf("unresolved target after Load = %v, want %s", u, graph.FormatKey(canon))
	}
}
