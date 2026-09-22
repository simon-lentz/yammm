package gogen_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	want, err := os.ReadFile(filepath.Clean(generatedFixturePath))
	if err != nil {
		t.Fatalf("read %s (restore it from git: this package does not build without it): %v", generatedFixturePath, err)
	}
	if yammmtest.Update() {
		refuseStaleFixture(t, generatedFixturePath, want, got)
	}
	yammmtest.Diff(t, string(want), string(got))
}

// fatalReporter is the part of testing.TB refuseStaleFixture reports through.
type fatalReporter interface {
	Helper()
	Fatalf(format string, args ...any)
}

// refuseStaleFixture rewrites the compiled fixture at path when got differs
// from want, its content, and then fails the run. The running binary is linked
// against the old copy, so only the next run's round trip compiles the new one.
func refuseStaleFixture(tb fatalReporter, path string, want, got []byte) {
	tb.Helper()
	if bytes.Equal(want, got) {
		return
	}
	if err := os.WriteFile(path, got, 0o600); err != nil {
		tb.Fatalf("rewrite %s: %v", path, err)
		return
	}
	tb.Fatalf("rewrote %s; run the tests again so the round trip compiles it", path)
}

// fatalRecorder records Fatalf calls where a testing.T would stop the test.
type fatalRecorder struct{ fatals []string }

func (r *fatalRecorder) Helper() {}

func (r *fatalRecorder) Fatalf(format string, args ...any) {
	r.fatals = append(r.fatals, fmt.Sprintf(format, args...))
}

func TestRefuseStaleFixture(t *testing.T) {
	t.Parallel()

	t.Run("a current fixture is left alone", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "fixture.go")
		if err := os.WriteFile(path, []byte("current"), 0o600); err != nil {
			t.Fatal(err)
		}
		var rec fatalRecorder
		refuseStaleFixture(&rec, path, []byte("current"), []byte("current"))
		if len(rec.fatals) != 0 {
			t.Errorf("fatals = %q, want none", rec.fatals)
		}
	})

	t.Run("a stale fixture is rewritten and the run fails", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "fixture.go")
		if err := os.WriteFile(path, []byte("stale"), 0o600); err != nil {
			t.Fatal(err)
		}
		var rec fatalRecorder
		refuseStaleFixture(&rec, path, []byte("stale"), []byte("fresh"))
		onDisk, err := os.ReadFile(filepath.Clean(path))
		if err != nil {
			t.Fatal(err)
		}
		if string(onDisk) != "fresh" {
			t.Errorf("fixture = %q, want it rewritten to %q", onDisk, "fresh")
		}
		if len(rec.fatals) != 1 || !strings.Contains(rec.fatals[0], "rewrote "+path) {
			t.Errorf("fatals = %q, want one naming the rewrite of %s", rec.fatals, path)
		}
	})

	t.Run("a fixture that cannot be written fails the run", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "absent", "fixture.go")
		var rec fatalRecorder
		refuseStaleFixture(&rec, path, []byte("stale"), []byte("fresh"))
		if len(rec.fatals) != 1 || !strings.Contains(rec.fatals[0], "rewrite "+path) {
			t.Errorf("fatals = %q, want one naming the failed rewrite of %s", rec.fatals, path)
		}
	})
}

// TestRoundTrip_Temporal is the sentence the generated types make true: a
// document adapter/json wrote from validated data decodes into the generated
// Graph, and re-encoding it reproduces the document. It holds an empty list,
// which the library keeps apart from an absent one, a to-many association, and
// a sensor whose required association is not yet set, which the document
// leaves out rather than writing null.
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
		"labels":         []any{},
		"has_reading": []any{
			map[string]any{"at": "2026-08-21 10:00:00", "on": "2026-08-21"},
		},
		"in_casing": []any{map[string]any{"serial": "c1"}},
		"feeds": map[string]any{
			"_target_id": "s1",
			"since":      "2026-08-21 11:00:00",
		},
		"neighbours": []any{map[string]any{"_target_id": "s2"}},
	}}
	unlinked := instance.RawInstance{Properties: map[string]any{
		"id":         "s2",
		"installed":  "2026-08-23",
		"created_at": "2026-08-23T09:30:00Z",
		"seen_at":    "2026-08-23T09:30:00.000000000Z",
		"in_casing":  []any{map[string]any{"serial": "c2"}},
	}}
	g := graph.New(s)
	for _, r := range []instance.RawInstance{raw, unlinked} {
		sensor, res := v.ValidateOne(ctx, "Sensor", r)
		if !res.OK() {
			t.Fatalf("validate: %s", res)
		}
		if res := g.Add(ctx, sensor); !res.OK() {
			t.Fatalf("add: %s", res)
		}
	}
	doc, err := adapterjson.New().MarshalObject(ctx, g.Snapshot())
	if err != nil {
		t.Fatalf("MarshalObject: %v", err)
	}

	var got temporal.Graph
	if err := json.Unmarshal(doc, &got); err != nil {
		t.Fatalf("decode into the generated Graph: %v\n%s", err, doc)
	}
	if len(got.Sensor) != 2 {
		t.Fatalf("decoded %d sensors, want 2 — the Graph key does not pair with the document:\n%s", len(got.Sensor), doc)
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
	var wantDoc, gotDoc any
	if err := json.Unmarshal(doc, &wantDoc); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(again, &gotDoc); err != nil {
		t.Fatal(err)
	}
	yammmtest.Diff(t, wantDoc, gotDoc)
}
