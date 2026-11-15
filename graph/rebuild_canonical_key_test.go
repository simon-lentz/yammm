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
// form a validator stores. The document's keys are the stored text, as the
// reader requires; its properties spell the instant another way.
func TestCanonicalization_AddAndRebuildAgree(t *testing.T) {
	t.Parallel()
	s := canonicalKeySchema(t)
	sensorID := mustTypeID(t, s, "Sensor")
	readingID := mustTypeID(t, s, "Reading")

	const spelled, stored = "2020-01-02T03:04:05+00:00", "2020-01-02T03:04:05Z"
	parsed, err := time.Parse(time.RFC3339, spelled)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	siteID := mustTypeID(t, s, "Site")

	site := instancetest.Valid(t, s, "Site", map[string]any{"established_at": spelled})
	// The edge names its target by the RAW spelling: the validator stores the
	// canonical text, which the document's edge holds.
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
				PrimaryKey: immutable.WrapKey([]any{stored}),
				Properties: immutable.WrapProperties(map[string]any{"established_at": spelled}),
			},
			{
				TypeID:     sensorID,
				PrimaryKey: immutable.WrapKey([]any{"s1"}),
				Properties: immutable.WrapProperties(map[string]any{"id": "s1", "created_at": spelled}),
				Composed: map[string][]snapparts.Instance{
					"READINGS": {{
						TypeID:     readingID,
						PrimaryKey: immutable.WrapKey([]any{stored}),
						Properties: immutable.WrapProperties(map[string]any{"taken_at": spelled, "logged_at": spelled}),
					}},
				},
			},
		},
		Edges: []snapparts.Edge{{
			Relation:   "SITE",
			SourceType: sensorID,
			SourceKey:  immutable.WrapKey([]any{"s1"}),
			TargetKey:  immutable.WrapKey([]any{stored}),
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
		case got.Any() != want.Any():
			t.Errorf("%s property %q: Add %v (%T), rebuild %v (%T)",
				what, name, got.Any(), got.Any(), want.Any(), want.Any())
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
