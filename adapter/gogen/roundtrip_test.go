package gogen_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/simon-lentz/yammm/adapter/gogen"
	"github.com/simon-lentz/yammm/adapter/gogen/internal/temporal"
	adapterjson "github.com/simon-lentz/yammm/adapter/json"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/yammmtest"
)

// generatedFixturePath is the compiled copy of the temporal fixture's output,
// the one generated package this module builds so a test can decode into it.
const generatedFixturePath = "internal/temporal/temporal_gen.go"

// TestTemporal_GeneratedPackageIsCurrent keeps the compiled fixture equal to
// what Marshal emits today; a drift here means the round-trip test below is
// decoding into stale types.
func TestTemporal_GeneratedPackageIsCurrent(t *testing.T) {
	got, err := gogen.Marshal(loadSchema(t, "temporal"))
	if err != nil {
		t.Fatal(err)
	}
	if yammmtest.Update() {
		if err := os.WriteFile(generatedFixturePath, got, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(filepath.Clean(generatedFixturePath))
	if err != nil {
		t.Fatalf("read %s (run this package's tests with -update to create it): %v", generatedFixturePath, err)
	}
	yammmtest.Diff(t, string(want), string(got))
}

// TestRoundTrip_Temporal is the sentence the generated temporal types make
// true: a document adapter/json wrote from validated data decodes into the
// generated Graph, and re-encoding it reproduces the document.
func TestRoundTrip_Temporal(t *testing.T) {
	ctx := context.Background()
	s := loadSchema(t, "temporal")
	v := instance.NewValidator(s)

	raw := instance.RawInstance{Properties: map[string]any{
		"id":             "s1",
		"installed":      "2026-08-21",
		"decommissioned": time.Date(2026, 8, 22, 0, 30, 0, 0, time.FixedZone("", 2*60*60)),
		"created_at":     "2026-08-21T09:30:00.5Z",
		"seen_wall":      "2026-08-21 09:30:00",
		"seen_at":        "2026-08-21T09:30:00.250000000Z",
		"day":            "2026-08-21",
		"stamp":          "2026-08-21T09:30:00Z",
		"wall":           "2026-08-21 09:30:00",
		"days":           []any{"2026-08-21", "2026-08-22"},
		"walls":          []any{"2026-08-21 09:30:00"},
		"has_reading": []any{
			map[string]any{"at": "2026-08-21 10:00:00", "on": "2026-08-21"},
		},
		"in_casing": []any{map[string]any{"serial": "c1"}},
		"feeds": map[string]any{
			"_target_id": "s1",
			"since":      "2026-08-21 11:00:00",
		},
	}}
	sensor, res := v.ValidateOne(ctx, "Sensor", raw)
	if !res.OK() {
		t.Fatalf("validate: %s", res)
	}
	g := graph.New(s)
	if r := g.Add(ctx, sensor); !r.OK() {
		t.Fatalf("add: %s", r)
	}
	doc, err := adapterjson.New().MarshalObject(ctx, g.Snapshot())
	if err != nil {
		t.Fatalf("MarshalObject: %v", err)
	}

	var got temporal.Graph
	if err := json.Unmarshal(doc, &got); err != nil {
		t.Fatalf("decode into the generated Graph: %v\n%s", err, doc)
	}
	if len(got.Sensor) != 1 {
		t.Fatalf("decoded %d sensors, want 1 — the Graph key does not pair with the document:\n%s", len(got.Sensor), doc)
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
	// A (one) composition is an array too, and the association's flattened
	// _target_ field carries the key.
	if len(sn.InCasing) != 1 || sn.InCasing[0].Serial != "c1" {
		t.Errorf("in_casing decoded as %+v, want one casing c1", sn.InCasing)
	}
	if sn.Feeds == nil || sn.Feeds.TargetID != "s1" {
		t.Errorf("feeds decoded as %+v, want _target_id s1", sn.Feeds)
	}

	again, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("re-encode: %v", err)
	}
	var wantDoc, gotDoc any
	if err := json.Unmarshal(doc, &wantDoc); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(again, &gotDoc); err != nil {
		t.Fatal(err)
	}
	yammmtest.Diff(t, wantDoc, gotDoc)
}
