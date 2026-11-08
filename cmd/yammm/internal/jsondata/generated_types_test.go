package jsondata_test

import (
	"bytes"
	"encoding/json"
	"maps"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/jsondata"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/temporal"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

// temporalDocument holds an empty list, which the library keeps apart from an
// absent one, a to-many association, a (one) composition, and a sensor whose
// required association is not yet set.
const temporalDocument = `{"Sensor": [
	{
		"id": "s1", "installed": "2026-08-21", "decommissioned": "2026-08-22",
		"created_at": "2026-08-21T09:30:00.5Z", "seen_wall": "2026-08-21 09:30:00",
		"seen_at": "2026-08-21T09:30:00.250000000Z", "day": "2026-08-21",
		"stamp": "2026-08-21T09:30:00Z", "wall": "2026-08-21 09:30:00",
		"days": ["2026-08-21", "2026-08-22"], "walls": ["2026-08-21 09:30:00"], "labels": [],
		"has_reading": [{"at": "2026-08-21 10:00:00", "on": "2026-08-21"}],
		"in_casing": [{"serial": "c1"}],
		"feeds": {"_target_id": "s1", "since": "2026-08-21 11:00:00"},
		"neighbours": [{"_target_id": "s2"}]
	},
	{
		"id": "s2", "installed": "2026-08-23", "created_at": "2026-08-23T09:30:00Z",
		"seen_at": "2026-08-23T09:30:00.000000000Z", "in_casing": [{"serial": "c2"}]
	}
]}`

// readSnapshot reads doc as the CLI reads a data file, adds every instance to
// a graph, and returns the graph's .ys document. Any diagnostic fails the test.
func readSnapshot(t *testing.T, s schema.Schema, doc []byte) []byte {
	t.Helper()
	ctx := t.Context()
	parsed, res := jsondata.Parse(ctx, location.NewSourceID("test://temporal.json"), doc)
	if !res.OK() {
		t.Fatalf("parse: %s\n%s", res, doc)
	}
	v := instance.NewValidator(s)
	g := graph.New(s)
	for _, typeName := range slices.Sorted(maps.Keys(parsed)) {
		for _, raw := range parsed[typeName] {
			valid, res := v.ValidateOne(ctx, typeName, raw)
			if !res.OK() {
				t.Fatalf("validate: %s\n%s", res, doc)
			}
			if res := g.Add(ctx, valid); !res.OK() {
				t.Fatalf("add: %s\n%s", res, doc)
			}
		}
	}
	ys, res := snapshot.Marshal(ctx, g.Snapshot())
	if res.HasErrors() {
		t.Fatalf("marshal: %s", res)
	}
	return ys
}

// TestRoundTrip_Temporal is the sentence the generated types make true: a
// document the CLI reads decodes into the generated Graph with no key left
// over, and what the Graph encodes the CLI reads back to the same graph.
func TestRoundTrip_Temporal(t *testing.T) {
	path, err := filepath.Abs(filepath.Join("..", "..", "..", "..", "schema", "gogen", "testdata", "temporal.yammm"))
	if err != nil {
		t.Fatal(err)
	}
	s, res := schema.Load(t.Context(), path)
	if res.HasErrors() {
		t.Fatalf("load: %s", res)
	}
	doc := []byte(temporalDocument)
	want := readSnapshot(t, s, doc)

	var got temporal.Graph
	dec := json.NewDecoder(bytes.NewReader(doc))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&got); err != nil {
		t.Fatalf("decode into the generated Graph: %v", err)
	}
	if len(got.Sensor) != 2 {
		t.Fatalf("decoded %d sensors, want 2: the Graph key does not pair with the document", len(got.Sensor))
	}
	sn := got.Sensor[0]
	utc := func(y, mo, d, h, mi, sec, ns int) time.Time {
		return time.Date(y, time.Month(mo), d, h, mi, sec, ns, time.UTC)
	}
	for name, tc := range map[string]struct{ got, want time.Time }{
		"installed":      {sn.Installed.Time, utc(2026, 8, 21, 0, 0, 0, 0)},
		"decommissioned": {sn.Decommissioned.Time, utc(2026, 8, 22, 0, 0, 0, 0)},
		"created_at":     {sn.CreatedAt, utc(2026, 8, 21, 9, 30, 0, 500_000_000)},
		"seen_wall":      {sn.SeenWall.Time, utc(2026, 8, 21, 9, 30, 0, 0)},
		"seen_at":        {sn.SeenAt.Time, utc(2026, 8, 21, 9, 30, 0, 250_000_000)},
		"day":            {sn.Day.Time, utc(2026, 8, 21, 0, 0, 0, 0)},
		"stamp":          {sn.Stamp.Time, utc(2026, 8, 21, 9, 30, 0, 0)},
		"wall":           {sn.Wall.Time, utc(2026, 8, 21, 9, 30, 0, 0)},
		"reading.at":     {sn.HasReading[0].At.Time, utc(2026, 8, 21, 10, 0, 0, 0)},
		"reading.on":     {sn.HasReading[0].On.Time, utc(2026, 8, 21, 0, 0, 0, 0)},
		"feeds.since":    {sn.Feeds.Since.Time, utc(2026, 8, 21, 11, 0, 0, 0)},
	} {
		if !tc.got.Equal(tc.want) {
			t.Errorf("%s decoded as %s, want %s", name, tc.got, tc.want)
		}
	}
	if len(sn.Days) != 2 || len(sn.Walls) != 1 {
		t.Errorf("lists decoded as %d days and %d walls, want 2 and 1", len(sn.Days), len(sn.Walls))
	}
	if len(sn.InCasing) != 1 || sn.InCasing[0].Serial != "c1" {
		t.Errorf("in_casing decoded as %+v, want one casing c1", sn.InCasing)
	}
	if sn.Feeds == nil || sn.Feeds.TargetID != "s1" {
		t.Errorf("feeds decoded as %+v, want _target_id s1", sn.Feeds)
	}
	if len(sn.Neighbours) != 1 || sn.Neighbours[0].TargetID != "s2" {
		t.Errorf("neighbours decoded as %+v, want one edge to s2", sn.Neighbours)
	}
	if sn.Labels == nil || len(sn.Labels) != 0 {
		t.Errorf("labels decoded as %#v, want a present empty list", sn.Labels)
	}

	again, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("re-encode: %v", err)
	}
	// The validator reads a null optional property as an absent one, so the
	// graphs below cannot tell a written null from a left-out key.
	if bytes.Contains(again, []byte("null")) {
		t.Errorf("the re-encoded document writes a null:\n%s", again)
	}
	if back := readSnapshot(t, s, again); !bytes.Equal(back, want) {
		t.Errorf("the re-encoded document reads to another graph:\n%s\nwant:\n%s", back, want)
	}
}
