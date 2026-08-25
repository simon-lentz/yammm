package csv

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/instancetest"
	"github.com/simon-lentz/yammm/schema"
)

// The instant every case below writes, and the three texts it renders to.
var canonicalWhen = time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)

const (
	canonicalRFC3339 = "2026-08-19T12:00:00Z"
	canonicalLayout  = "2026-08-19 12:00:00"
	canonicalGoPrint = "2026-08-19 12:00:00 +0000 UTC"
	canonicalUUIDStr = "0a35ef0f-9d40-4b6b-a0a1-0d1a5a0e1f2b"
)

// bypassCanonicalSnapshot builds a graph through instance.NewValidInstance,
// which receives no schema and runs no validation. It is the only way a
// non-canonical value reaches a writer, and therefore the only thing that can
// prove the writer's own arm runs.
func bypassCanonicalSnapshot(t *testing.T, s *schema.Schema) *graph.Snapshot {
	t.Helper()
	ctx := context.Background()
	g := graph.New(s)

	stationID, ok := s.Type("Station")
	if !ok {
		t.Fatal("Station missing from the fixture schema")
	}
	sensorID, ok := s.Type("Sensor")
	if !ok {
		t.Fatal("Sensor missing from the fixture schema")
	}

	station := instancetest.VI(
		"Station",
		instancetest.TypeID(stationID.ID()),
		instancetest.PK(canonicalWhen),
		instancetest.Props(map[string]any{"at": canonicalWhen}),
	)
	if res := g.Add(ctx, station); res.HasErrors() {
		t.Fatalf("adding Station: %s", res)
	}

	sensor := instancetest.VI(
		"Sensor",
		instancetest.TypeID(sensorID.ID()),
		instancetest.PK("s1"),
		instancetest.Props(map[string]any{
			"id":         "s1",
			"created_at": canonicalWhen,
			"run_id":     uuid.MustParse(canonicalUUIDStr),
			"installed":  canonicalWhen,
			"samples":    []any{canonicalWhen, "2026-08-19T13:00:00+00:00"},
		}),
		instancetest.Edges(map[string]*instance.ValidEdgeData{
			"FEED": instance.NewValidEdgeData([]instance.ValidEdgeTarget{
				instance.NewValidEdgeTarget(immutable.WrapKey([]any{canonicalWhen}), immutable.Properties{}),
			}),
		}),
	)
	if res := g.Add(ctx, sensor); res.HasErrors() {
		t.Fatalf("adding Sensor: %s", res)
	}

	return g.Snapshot()
}

// canonicalTestSchema gives Station a declared-layout Timestamp primary key,
// so a foreign key pointing at it renders through that layout or not at all.
// Inline rather than a testdata fixture: every .yammm in the tree joins the
// parser's frozen golden corpus, and this schema is about the writer.
func canonicalTestSchema(t *testing.T) *schema.Schema {
	t.Helper()
	const src = `schema "csv_canonical"

type Station {
	at Timestamp["2006-01-02 15:04:05"] primary
}

type Sensor {
	id String primary
	created_at Timestamp
	run_id UUID
	installed Date
	samples List<Timestamp>
	--> FEED (one) Station
}
`
	s, result := schema.LoadString(t.Context(), src, "csv_canonical.yammm")
	if result.HasErrors() {
		t.Fatalf("load canonical schema: %s", result)
	}
	return s
}

func marshalCanonicalFixture(t *testing.T) map[string]string {
	t.Helper()
	s := canonicalTestSchema(t)
	output, err := New().MarshalSnapshot(context.Background(), bypassCanonicalSnapshot(t, s))
	if err != nil {
		t.Fatalf("MarshalSnapshot: %v", err)
	}
	got := make(map[string]string, len(output))
	for name, data := range output {
		got[name] = string(data)
	}
	return got
}

// TestMarshalSnapshot_CanonicalizesBypassBuiltProperties kills the property
// arm. Without it a time.Time falls to fmt.Sprint and writes Go's default
// layout into a cell whose schema declares RFC 3339.
func TestMarshalSnapshot_CanonicalizesBypassBuiltProperties(t *testing.T) {
	t.Parallel()
	sensor := marshalCanonicalFixture(t)["Sensor"]

	for _, want := range []string{canonicalRFC3339, canonicalUUIDStr, "2026-08-19"} {
		if !strings.Contains(sensor, want) {
			t.Errorf("Sensor CSV lacks %q\ngot: %s", want, sensor)
		}
	}
	if strings.Contains(sensor, canonicalGoPrint) {
		t.Errorf("a bypass-built time.Time rendered through Go's default layout\ngot: %s", sensor)
	}
}

// TestMarshalSnapshot_CanonicalizesListElements kills the list-element
// recursion. A suite with only scalar cases cannot tell it from dead code,
// because the scalar arm would keep every scalar assertion green.
func TestMarshalSnapshot_CanonicalizesListElements(t *testing.T) {
	t.Parallel()
	sensor := marshalCanonicalFixture(t)["Sensor"]

	want := canonicalRFC3339 + "|2026-08-19T13:00:00Z"
	if !strings.Contains(sensor, want) {
		t.Errorf("List<Timestamp> did not canonicalize elementwise: want %q\ngot: %s", want, sensor)
	}
}

// TestMarshalSnapshot_CanonicalizesForeignKeyColumns kills the FK arm. The
// value lives on the association's TARGET type, so the row's own type is not
// enough to reach its constraint — and the target's key here is a
// declared-layout Timestamp, which nothing else in the row renders.
func TestMarshalSnapshot_CanonicalizesForeignKeyColumns(t *testing.T) {
	t.Parallel()
	out := marshalCanonicalFixture(t)

	// The control: the target's own row renders through the declared layout,
	// so the FK column has something to agree with.
	if !strings.Contains(out["Station"], canonicalLayout) {
		t.Fatalf("Station's own key column did not render through its layout\ngot: %s", out["Station"])
	}

	sensor := out["Sensor"]
	if !strings.Contains(sensor, canonicalLayout) {
		t.Errorf("the foreign-key column did not render through the target's layout\ngot: %s", sensor)
	}
	if strings.Contains(sensor, canonicalGoPrint) {
		t.Errorf("the foreign-key column rendered through Go's default layout\ngot: %s", sensor)
	}
}
