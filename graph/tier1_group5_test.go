package graph_test

import (
	"context"
	"testing"
	"time"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
)

// oneKeyedPartSchema declares a (one) composition to a keyed part type.
func oneKeyedPartSchema(t *testing.T) *schema.Schema {
	t.Helper()
	const src = `schema "one_keyed"

type Order {
	id String primary
	*-> ADDRESS (one) Address
}

part type Address {
	line String primary
}
`
	s, res := schema.LoadString(t.Context(), src, "one_keyed.yammm")
	if res.HasErrors() {
		t.Fatalf("load: %s", res)
	}
	return s
}

// TestAddComposed_OneOverflow_RebuiltKeylessOccupantReportsNoStandIn drives
// the (one)-overflow site through the one public path that installs a keyed
// part's occupant with no key: RebuildSnapshot checks a part's identity and
// not its key, so a loaded document can hold one. The primary_key detail must
// then be absent, not the literal "[]" the zero key renders.
func TestAddComposed_OneOverflow_RebuiltKeylessOccupantReportsNoStandIn(t *testing.T) {
	t.Parallel()
	s := oneKeyedPartSchema(t)
	orderID := mustTypeID(t, s, "Order")
	addressID := mustTypeID(t, s, "Address")

	snap, res := graph.RebuildSnapshot(s, graph.SnapshotParts{
		Types: []schema.TypeID{orderID},
		Instances: map[schema.TypeID][]graph.InstanceParts{
			orderID: {{
				TypeName:   "Order",
				TypeID:     orderID,
				PrimaryKey: immutable.WrapKey([]any{"o1"}),
				Properties: immutable.WrapProperties(map[string]any{"id": "o1"}),
				Composed: map[string][]graph.InstanceParts{
					"ADDRESS": {{
						TypeName:   "Address",
						TypeID:     addressID,
						Properties: immutable.WrapProperties(map[string]any{"line": "first"}),
					}},
				},
			}},
		},
	})
	if res.HasErrors() {
		t.Fatalf("RebuildSnapshot: %s", res)
	}

	g := graph.NewFromSnapshot(s, snap)
	second := instance.NewValidInstance("Address", addressID,
		immutable.WrapKey([]any{"second"}),
		immutable.WrapProperties(map[string]any{"line": "second"}),
		nil, nil, nil)
	result := g.AddComposed(t.Context(), orderID, graph.FormatKey("o1"), "ADDRESS", second)
	if result.OK() {
		t.Fatal("a second child on a (one) slot was accepted")
	}
	assertHasCode(t, result, diag.E_DUPLICATE_COMPOSED_PK)

	for issue := range result.Issues() {
		if issue.Code() != diag.E_DUPLICATE_COMPOSED_PK {
			continue
		}
		for _, d := range issue.Details() {
			if d.Key == diag.DetailKeyPrimaryKey {
				t.Errorf("primary_key detail = %q; the occupant carries no key, so the detail must be absent", d.Value)
			}
		}
	}
}

// canonGroup5Schema declares a canonicalizing kind at every position the Add
// path fills: a root property, a composed child's key and a child property.
func canonGroup5Schema(t *testing.T) *schema.Schema {
	t.Helper()
	const src = `schema "canon_add_path"

type Sensor {
	id String primary
	created_at Timestamp
	*-> READINGS (_:many) Reading
}

part type Reading {
	taken_at Timestamp primary
	logged_at Timestamp
}
`
	s, res := schema.LoadString(t.Context(), src, "canon_add_path.yammm")
	if res.HasErrors() {
		t.Fatalf("load: %s", res)
	}
	return s
}

// TestCanonicalization_AddAndRebuildAgree is the second implementation for
// the Add path: one record built through Graph.Add and the same record
// rebuilt from parts must hold the same key and the same properties at every
// position, because the rebuild path canonicalizes both and the graph shares
// one canonicalizer with it.
func TestCanonicalization_AddAndRebuildAgree(t *testing.T) {
	t.Parallel()
	s := canonGroup5Schema(t)
	sensorID := mustTypeID(t, s, "Sensor")
	readingID := mustTypeID(t, s, "Reading")

	const spelled = "2020-01-02T03:04:05+00:00"
	parsed, err := time.Parse(time.RFC3339, spelled)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	reading := instance.NewValidInstance("Reading", readingID,
		immutable.WrapKey([]any{spelled}),
		immutable.WrapProperties(map[string]any{"taken_at": spelled, "logged_at": parsed}),
		nil, nil, nil)
	sensor := instance.NewValidInstance("Sensor", sensorID,
		immutable.WrapKey([]any{"s1"}),
		immutable.WrapProperties(map[string]any{"id": "s1", "created_at": parsed}),
		nil, map[string]immutable.Value{"READINGS": immutable.Wrap([]any{reading})}, nil)

	g := graph.New(s)
	if r := g.Add(t.Context(), sensor); !r.OK() {
		t.Fatalf("add: %s", r)
	}
	added := g.Snapshot()

	rebuilt, res := graph.RebuildSnapshot(s, graph.SnapshotParts{
		Types: []schema.TypeID{sensorID},
		Instances: map[schema.TypeID][]graph.InstanceParts{
			sensorID: {{
				TypeName:   "Sensor",
				TypeID:     sensorID,
				PrimaryKey: immutable.WrapKey([]any{"s1"}),
				Properties: immutable.WrapProperties(map[string]any{"id": "s1", "created_at": parsed}),
				Composed: map[string][]graph.InstanceParts{
					"READINGS": {{
						TypeName:   "Reading",
						TypeID:     readingID,
						PrimaryKey: immutable.WrapKey([]any{spelled}),
						Properties: immutable.WrapProperties(map[string]any{"taken_at": spelled, "logged_at": parsed}),
					}},
				},
			}},
		},
	})
	if res.HasErrors() {
		t.Fatalf("RebuildSnapshot: %s", res)
	}

	a := added.InstancesOf(sensorID)[0]
	b := rebuilt.InstancesOf(sensorID)[0]
	assertSameInstance(t, "Sensor", a, b)
	assertSameInstance(t, "Reading", a.Composed("READINGS")[0], b.Composed("READINGS")[0])
}

// assertSameInstance compares the key and every property of two instances by
// the rendered form the wire carries.
func assertSameInstance(t *testing.T, what string, a, b *graph.Instance) {
	t.Helper()
	if a.PrimaryKey().String() != b.PrimaryKey().String() {
		t.Errorf("%s key: Add %s, RebuildSnapshot %s", what, a.PrimaryKey(), b.PrimaryKey())
	}
	for name := range b.Properties().SortedKeys() {
		want, _ := b.Properties().Get(name)
		got, ok := a.Properties().Get(name)
		if !ok {
			t.Errorf("%s property %q: absent on the Add path", what, name)
			continue
		}
		if got.Unwrap() != want.Unwrap() {
			t.Errorf("%s property %q: Add %v (%T), RebuildSnapshot %v (%T)",
				what, name, got.Unwrap(), got.Unwrap(), want.Unwrap(), want.Unwrap())
		}
	}
}

// TestBatchAssembler_Finalize_SnapshotIsTakenOnce pins that a retry after a
// cancelled Finalize reuses the snapshot the first call built: the graph is
// unchanged between the two, so the clone of every instance and edge happens
// once, while the cancelled outcome itself stays unmemoized.
func TestBatchAssembler_Finalize_SnapshotIsTakenOnce(t *testing.T) {
	t.Parallel()
	s := batchAssemblerTestSchema(t)
	ba := graph.NewBatchAssembler(t.Context(), s)
	if err := ba.Add("Person", personRaw("alice", "Alice")); err != nil {
		t.Fatalf("Add: %v", err)
	}

	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	first, err := ba.Finalize(cancelled)
	if err == nil {
		t.Fatal("Finalize with a cancelled context reported success")
	}
	if first.Snapshot == nil {
		t.Fatal("the cancelled Finalize returned a nil Snapshot")
	}

	second, err := ba.Finalize(t.Context())
	if err != nil {
		t.Fatalf("the retry failed: %v", err)
	}
	if second.Snapshot != first.Snapshot {
		t.Error("the retry re-took the snapshot; the graph did not change between the two calls")
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

// rawKeyedEventParts builds parts holding one Event whose key is spelled
// non-canonically, plus one Note addressing it under the given spelling.
func rawKeyedEventParts(t *testing.T, s *schema.Schema, edgeSpelling string) graph.SnapshotParts {
	t.Helper()
	eventID, noteID := mustTypeID(t, s, "Event"), mustTypeID(t, s, "Note")
	const raw = "2020-01-02T03:04:05+00:00"
	return graph.SnapshotParts{
		Types: []schema.TypeID{eventID, noteID},
		Instances: map[schema.TypeID][]graph.InstanceParts{
			eventID: {{
				TypeName: "Event", TypeID: eventID, PrimaryKey: immutable.WrapKey([]any{raw}),
				Properties: immutable.WrapProperties(map[string]any{"observed_at": raw}),
			}},
			noteID: {{
				TypeName: "Note", TypeID: noteID, PrimaryKey: immutable.WrapKey([]any{"n1"}),
				Properties: immutable.WrapProperties(map[string]any{"id": "n1"}),
			}},
		},
		Edges: []graph.EdgeParts{{
			Relation: "ABOUT", SourceType: noteID, SourceKey: immutable.WrapKey([]any{"n1"}),
			TargetType: eventID, TargetKey: immutable.WrapKey([]any{edgeSpelling}),
		}},
	}
}

// TestRebuildSnapshot_IndexesByTheCanonicalKey pins that the rebuilt
// snapshot's index holds the key the instance carries, so InstanceByKey by an
// instance's own key hits and the caller's raw spelling does not.
func TestRebuildSnapshot_IndexesByTheCanonicalKey(t *testing.T) {
	t.Parallel()
	s := linkedTimestampSchema(t)
	eventID := mustTypeID(t, s, "Event")
	parts := rawKeyedEventParts(t, s, "2020-01-02T03:04:05Z")
	parts.Edges = nil
	snap, res := graph.RebuildSnapshot(s, parts)
	if res.HasErrors() {
		t.Fatalf("RebuildSnapshot: %s", res)
	}
	inst := snap.InstancesOf(eventID)[0]
	if got, want := inst.PrimaryKey().String(), graph.FormatKey("2020-01-02T03:04:05Z"); got != want {
		t.Fatalf("instance key = %s, want %s", got, want)
	}
	if _, ok := snap.InstanceByKey(eventID, inst.PrimaryKey().String()); !ok {
		t.Error("InstanceByKey by the instance's own key misses")
	}
	if _, ok := snap.InstanceByKey(eventID, graph.FormatKey("2020-01-02T03:04:05+00:00")); ok {
		t.Error("InstanceByKey by the raw spelling hits; the index is keyed by a spelling the instance does not carry")
	}
}

// TestRebuildSnapshot_EdgeResolvesAgainstARawSpelledKey pins that an edge
// resolves against a rebuilt instance whichever spelling either side used.
func TestRebuildSnapshot_EdgeResolvesAgainstARawSpelledKey(t *testing.T) {
	t.Parallel()
	s := linkedTimestampSchema(t)
	for _, spelling := range []string{"2020-01-02T03:04:05+00:00", "2020-01-02T03:04:05Z"} {
		snap, res := graph.RebuildSnapshot(s, rawKeyedEventParts(t, s, spelling))
		if res.HasErrors() {
			t.Fatalf("edge target spelled %q: %s", spelling, res)
		}
		if n := len(snap.Edges()); n != 1 {
			t.Errorf("edge target spelled %q: edges = %d, want 1", spelling, n)
		}
	}
}

// TestRebuildSnapshot_RefusesTwoInstancesAtOneCanonicalAddress pins the
// invariant the index enforces: two parts that spell one key two ways are
// one address, and a caller handing in both is refused rather than given a
// snapshot whose slice and index disagree.
func TestRebuildSnapshot_RefusesTwoInstancesAtOneCanonicalAddress(t *testing.T) {
	t.Parallel()
	s := linkedTimestampSchema(t)
	eventID := mustTypeID(t, s, "Event")
	event := func(at string) graph.InstanceParts {
		return graph.InstanceParts{
			TypeName: "Event", TypeID: eventID, PrimaryKey: immutable.WrapKey([]any{at}),
			Properties: immutable.WrapProperties(map[string]any{"observed_at": at}),
		}
	}
	snap, res := graph.RebuildSnapshot(s, graph.SnapshotParts{
		Types:     []schema.TypeID{eventID},
		Instances: map[schema.TypeID][]graph.InstanceParts{eventID: {event("2020-01-02T03:04:05Z"), event("2020-01-02T03:04:05+00:00")}},
	})
	if snap != nil || !res.HasFatal() {
		t.Fatalf("two spellings of one key were admitted as two instances: snap=%v %s", snap != nil, res)
	}
	assertHasCode(t, res, diag.E_INTERNAL)
}
