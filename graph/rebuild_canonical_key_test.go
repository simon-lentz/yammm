package graph_test

import (
	"fmt"
	"maps"
	"slices"
	"testing"
	"time"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/internal/instancetest"
	"github.com/simon-lentz/yammm/internal/snapparts"
	"github.com/simon-lentz/yammm/schema"
)

// canonicalKeySchema declares a canonicalizing kind at every position the Add
// path fills: a root property, a composed child's key and a child property,
// and an association whose TARGET KEY canonicalizes, so the agreement test
// reaches an edge position as well as an instance one.
func canonicalKeySchema(t *testing.T) schema.Schema {
	t.Helper()
	s, res := schema.LoadString(t.Context(), canonicalKeySource, "canon_add_path.yammm")
	if res.HasErrors() {
		t.Fatalf("load: %s", res)
	}
	return s
}

const canonicalKeySource = `schema "canon_add_path"

type Site {
	established_at Timestamp primary
}

type Sensor {
	id String primary
	created_at Timestamp
	*-> READINGS (_:many) Reading
	--> SITE (_) Site {
		label String
	}
}

part type Reading {
	taken_at Timestamp primary
	logged_at Timestamp
}
`

// TestCanonicalization_AddAndRebuildAgree is the second implementation for
// the Add path: one record built through Graph.Add, its Timestamps handed over as
// time.Time, and the same record rebuilt from a document's text hold the same key
// and the same properties at every position, because the rebuild path stores the
// form a validator stores.
func TestCanonicalization_AddAndRebuildAgree(t *testing.T) {
	t.Parallel()
	s := canonicalKeySchema(t)
	sensorID := mustTypeID(t, s, "Sensor")
	readingID := mustTypeID(t, s, "Reading")

	const spelled = "2020-01-02T03:04:05+00:00"
	parsed, err := time.Parse(time.RFC3339, spelled)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	siteID := mustTypeID(t, s, "Site")

	site := instancetest.Valid(t, s, "Site", map[string]any{"established_at": spelled})
	// The edge names its target by the RAW spelling: both paths canonicalize
	// an edge address, so the position must agree whichever spelling is used.
	sensor := instancetest.Valid(t, s, "Sensor", map[string]any{
		"id":         "s1",
		"created_at": parsed,
		"site":       map[string]any{"_target_established_at": spelled, "label": "north"},
		"readings":   []any{map[string]any{"taken_at": spelled, "logged_at": parsed}},
	})

	g := graph.New(s)
	if r := g.Add(t.Context(), site); !r.OK() {
		t.Fatalf("add site: %s", r)
	}
	if r := g.Add(t.Context(), sensor); !r.OK() {
		t.Fatalf("add: %s", r)
	}
	added := g.Snapshot()

	rebuilt := rebuild(s, snapparts.Parts{
		Types: []schema.TypeID{sensorID, siteID},
		Instances: []snapparts.Instance{
			{
				TypeID:     siteID,
				PrimaryKey: immutable.WrapKey([]any{spelled}),
				Properties: immutable.WrapProperties(map[string]any{"established_at": spelled}),
			},
			{
				TypeID:     sensorID,
				PrimaryKey: immutable.WrapKey([]any{"s1"}),
				Properties: immutable.WrapProperties(map[string]any{"id": "s1", "created_at": spelled}),
				Composed: map[string][]snapparts.Instance{
					"READINGS": {{
						TypeID:     readingID,
						PrimaryKey: immutable.WrapKey([]any{spelled}),
						Properties: immutable.WrapProperties(map[string]any{"taken_at": spelled, "logged_at": spelled}),
					}},
				},
			},
		},
		Edges: []snapparts.Edge{{
			Relation:   "SITE",
			SourceType: sensorID,
			SourceKey:  immutable.WrapKey([]any{"s1"}),
			TargetKey:  immutable.WrapKey([]any{spelled}),
			Properties: immutable.WrapProperties(map[string]any{"label": "north"}),
		}},
	})

	a := slices.Collect(added.InstancesOf(sensorID))[0]
	b := slices.Collect(rebuilt.InstancesOf(sensorID))[0]
	assertSameInstance(t, "Sensor", a, b)
	assertSameInstance(t, "Reading", slices.Collect(a.Composed("READINGS"))[0], slices.Collect(b.Composed("READINGS"))[0])
	assertSameInstance(t, "Site", slices.Collect(added.InstancesOf(siteID))[0], slices.Collect(rebuilt.InstancesOf(siteID))[0])
	assertSameEdges(t, added, rebuilt)
}

// assertSameProperties compares two property sets over the UNION of their
// names, so a name present on either side alone is reported. Iterating one
// side's names cannot see the other side dropping a property.
func assertSameProperties(t *testing.T, what string, a, b immutable.Properties) {
	t.Helper()
	names := map[string]bool{}
	for name := range a.Keys() {
		names[name] = true
	}
	for name := range b.Keys() {
		names[name] = true
	}
	for _, name := range slices.Sorted(maps.Keys(names)) {
		got, inAdd := a.Get(name)
		want, inRebuild := b.Get(name)
		switch {
		case !inAdd:
			t.Errorf("%s property %q: absent on the Add path", what, name)
		case !inRebuild:
			t.Errorf("%s property %q: absent on the rebuild path", what, name)
		case got.Unwrap() != want.Unwrap():
			t.Errorf("%s property %q: Add %v (%T), rebuild %v (%T)",
				what, name, got.Unwrap(), got.Unwrap(), want.Unwrap(), want.Unwrap())
		}
	}
}

// assertSameInstance compares the key and every property of two instances by
// the rendered form the wire carries, over both sides' name sets.
func assertSameInstance(t *testing.T, what string, a, b graph.Instance) {
	t.Helper()
	if a.PrimaryKey().String() != b.PrimaryKey().String() {
		t.Errorf("%s key: Add %s, rebuild %s", what, a.PrimaryKey(), b.PrimaryKey())
	}
	assertSameProperties(t, what, a.Properties(), b.Properties())
}

// edgePosition renders the address an edge occupies: its relation and both
// endpoints as the graph holds them. Two edges agree on position when this
// string is equal, and the properties at that position are then comparable.
func edgePosition(e graph.Edge) string {
	return fmt.Sprintf("%s: %s%s -> %s%s",
		e.Relation(),
		e.Source().TypeName(), e.Source().PrimaryKey(),
		e.Target().TypeName(), e.Target().PrimaryKey())
}

// assertSameEdges compares every edge position of two snapshots and the
// properties at each. An instance-only comparison cannot see an edge address
// or an edge property diverging between the two paths, which is the half of
// the agreement contract [assertSameInstance] does not reach.
func assertSameEdges(t *testing.T, added, rebuilt graph.Snapshot) {
	t.Helper()
	index := func(snap graph.Snapshot) map[string]graph.Edge {
		out := map[string]graph.Edge{}
		for e := range snap.Edges() {
			pos := edgePosition(e)
			if _, dup := out[pos]; dup {
				t.Errorf("two edges share the position %s; the comparison cannot pair them", pos)
			}
			out[pos] = e
		}
		return out
	}
	a, b := index(added), index(rebuilt)
	positions := map[string]bool{}
	for pos := range a {
		positions[pos] = true
	}
	for pos := range b {
		positions[pos] = true
	}
	for _, pos := range slices.Sorted(maps.Keys(positions)) {
		ae, inAdd := a[pos]
		be, inRebuild := b[pos]
		switch {
		case !inAdd:
			t.Errorf("edge position %s: absent on the Add path", pos)
		case !inRebuild:
			t.Errorf("edge position %s: absent on the rebuild path", pos)
		default:
			assertSameProperties(t, "edge "+pos, ae.Properties(), be.Properties())
		}
	}
}

// linkedTimestampSchema declares a Timestamp-keyed target and a type that
// addresses it through an association.
func linkedTimestampSchema(t *testing.T) schema.Schema {
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
func rawKeyedEventParts(t *testing.T, s schema.Schema, edgeSpelling string) snapparts.Parts {
	t.Helper()
	eventID, noteID := mustTypeID(t, s, "Event"), mustTypeID(t, s, "Note")
	const raw = "2020-01-02T03:04:05+00:00"
	return snapparts.Parts{
		Types: []schema.TypeID{eventID, noteID},
		Instances: []snapparts.Instance{
			{
				TypeID: eventID, PrimaryKey: immutable.WrapKey([]any{raw}),
				Properties: immutable.WrapProperties(map[string]any{"observed_at": raw}),
			},
			{
				TypeID: noteID, PrimaryKey: immutable.WrapKey([]any{"n1"}),
				Properties: immutable.WrapProperties(map[string]any{"id": "n1"}),
			},
		},
		Edges: []snapparts.Edge{{
			Relation: "ABOUT", SourceType: noteID, SourceKey: immutable.WrapKey([]any{"n1"}),
			TargetKey: immutable.WrapKey([]any{edgeSpelling}),
		}},
	}
}

// TestRebuild_IndexesByTheCanonicalKey pins that the rebuilt snapshot's instance
// carries the canonical key and is found by it and by the raw spelling alike;
// [TestRebuild_IndexKeyIsTheCanonicalKey] pins the index's own keying, which a
// lookup that canonicalizes cannot show.
func TestRebuild_IndexesByTheCanonicalKey(t *testing.T) {
	t.Parallel()
	s := linkedTimestampSchema(t)
	eventID := mustTypeID(t, s, "Event")
	parts := rawKeyedEventParts(t, s, "2020-01-02T03:04:05Z")
	parts.Edges = nil
	snap := rebuild(s, parts)
	inst := slices.Collect(snap.InstancesOf(eventID))[0]
	if got, want := inst.PrimaryKey().String(), graph.FormatKey("2020-01-02T03:04:05Z"); got != want {
		t.Fatalf("instance key = %s, want %s", got, want)
	}
	if _, ok := snap.InstanceByKey(eventID, inst.PrimaryKey().String()); !ok {
		t.Error("InstanceByKey by the instance's own key misses")
	}
	if got, ok := snap.InstanceByKey(eventID, graph.FormatKey("2020-01-02T03:04:05+00:00")); !ok || got != inst {
		t.Errorf("InstanceByKey by the raw spelling = %v, %v; want the one instance, found", got, ok)
	}
}

// TestRebuild_EdgeResolvesAgainstARawSpelledKey pins that an edge resolves
// against a rebuilt instance whichever spelling either side used.
func TestRebuild_EdgeResolvesAgainstARawSpelledKey(t *testing.T) {
	t.Parallel()
	s := linkedTimestampSchema(t)
	for _, spelling := range []string{"2020-01-02T03:04:05+00:00", "2020-01-02T03:04:05Z"} {
		snap := rebuild(s, rawKeyedEventParts(t, s, spelling))
		if n := len(slices.Collect(snap.Edges())); n != 1 {
			t.Errorf("edge target spelled %q: edges = %d, want 1", spelling, n)
		}
	}
}

// TestRebuild_CanonicalizesEveryComponentOfACompositeKey pins that each component
// of a composite key is read against its own key property's constraint, not only
// the first.
func TestRebuild_CanonicalizesEveryComponentOfACompositeKey(t *testing.T) {
	t.Parallel()
	s := loadAs(t, `schema "ev"

type Ev {
	name String primary
	at Timestamp primary
	u UUID primary
}
`)
	ev := mustTypeID(t, s, "Ev")
	const (
		rawAt   = "2026-01-02T03:04:05+00:00"
		canonAt = "2026-01-02T03:04:05Z"
		rawU    = "6BA7B810-9DAD-11D1-80B4-00C04FD430C8"
		canonU  = "6ba7b810-9dad-11d1-80b4-00c04fd430c8"
	)
	snap := rebuild(s, snapparts.Parts{Instances: []snapparts.Instance{{
		TypeID:     ev,
		PrimaryKey: immutable.WrapKey([]any{"x", rawAt, rawU}),
		Properties: immutable.WrapProperties(map[string]any{"name": "x", "at": rawAt, "u": rawU}),
	}}})
	got := slices.Collect(snap.InstancesOf(ev))[0].PrimaryKey().String()
	if want := graph.FormatKey("x", canonAt, canonU); got != want {
		t.Errorf("key = %s, want %s", got, want)
	}
}

// An address compares under the schema's canonical form: a root's stored key, its
// key property and a duplicate's key are one address however each is spelled.
func TestRebuild_AnAddressComparesCanonically(t *testing.T) {
	t.Parallel()
	const (
		zulu   = "2024-01-01T00:00:00Z"
		offset = "2024-01-01T00:00:00+00:00"
	)
	s := loadAs(t, `schema "stamped"

type Run {
	at Timestamp primary
	*-> TAGS (_:many) Stamp
}

part type Stamp {
	at Timestamp primary
}
`)
	run, stamp := mustTypeID(t, s, "Run"), mustTypeID(t, s, "Stamp")
	root := func(key string) snapparts.Instance {
		return snapparts.Instance{TypeID: run, PrimaryKey: immutable.WrapKey([]any{key}), Properties: immutable.WrapProperties(map[string]any{"at": key})}
	}
	want := graph.FormatKey(zulu)

	dups := slices.Collect(rebuild(s, snapparts.Parts{
		Instances:  []snapparts.Instance{root(zulu)},
		Duplicates: []snapparts.Instance{root(offset)},
	}).Duplicates())
	if len(dups) != 1 || dups[0].Instance().PrimaryKey().String() != want || dups[0].Conflict().PrimaryKey().String() != want {
		t.Errorf("duplicates = %v, want one at %s whose conflict is the root there", dups, want)
	}

	holder := root(zulu)
	holder.Composed = map[string][]snapparts.Instance{"TAGS": {{
		TypeID: stamp, PrimaryKey: immutable.WrapKey([]any{offset}), Properties: immutable.WrapProperties(map[string]any{"at": zulu}),
	}}}
	snap := rebuild(s, snapparts.Parts{Instances: []snapparts.Instance{holder}})
	children := slices.Collect(slices.Collect(snap.InstancesOf(run))[0].Composed("TAGS"))
	if len(children) != 1 || children[0].PrimaryKey().String() != want {
		t.Errorf("children = %v, want one at %s", children, want)
	}
}
