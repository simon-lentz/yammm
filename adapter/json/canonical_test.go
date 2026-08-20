package json

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/instance/instancetest"
	"github.com/simon-lentz/yammm/schema"
)

// The instant every case below writes, and the texts it renders to.
var canonicalWhen = time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)

const (
	canonicalRFC3339 = "2026-08-19T12:00:00Z"
	canonicalLayout  = "2026-08-19 12:00:00"
	canonicalGoPrint = "2026-08-19 12:00:00 +0000 UTC"
	canonicalUUIDStr = "0a35ef0f-9d40-4b6b-a0a1-0d1a5a0e1f2b"
)

// canonicalTestSchema gives Station a declared-layout Timestamp primary key,
// so a foreign key pointing at it renders through that layout or not at all.
func canonicalTestSchema(t *testing.T) *schema.Schema {
	t.Helper()
	const src = `schema "json_canonical"

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
	s, result := schema.LoadString(t.Context(), src, "json_canonical.yammm")
	if result.HasErrors() {
		t.Fatalf("load canonical schema: %s", result)
	}
	return s
}

// marshalBypassBuilt builds a graph through instance.NewValidInstance, which
// receives no schema and runs no validation. It is the only way a
// non-canonical value reaches this writer, and therefore the only thing that
// can prove the writer's own arm runs.
func marshalBypassBuilt(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	s := canonicalTestSchema(t)
	g := graph.New(s)

	station := instancetest.VI(
		"Station",
		instancetest.TypeID(mustTypeID(t, s, "Station")),
		instancetest.PK(canonicalWhen),
		instancetest.Props(map[string]any{"at": canonicalWhen}),
	)
	if res := g.Add(ctx, station); res.HasErrors() {
		t.Fatalf("adding Station: %s", res)
	}

	sensor := instancetest.VI(
		"Sensor",
		instancetest.TypeID(mustTypeID(t, s, "Sensor")),
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

	data, err := New().MarshalObject(ctx, g.Snapshot())
	if err != nil {
		t.Fatalf("MarshalObject: %v", err)
	}
	return string(data)
}

// TestMarshalObject_CanonicalizesBypassBuiltProperties kills the property arm.
// A uuid.UUID is [16]byte, so without the arm encoding/json writes it as an
// array of sixteen numbers rather than a string.
func TestMarshalObject_CanonicalizesBypassBuiltProperties(t *testing.T) {
	t.Parallel()
	got := marshalBypassBuilt(t)

	for _, want := range []string{
		`"created_at":"` + canonicalRFC3339 + `"`,
		`"run_id":"` + canonicalUUIDStr + `"`,
		`"installed":"2026-08-19"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output lacks %s\ngot: %s", want, got)
		}
	}
	if strings.Contains(got, `"run_id":[`) {
		t.Errorf("a uuid.UUID marshalled as a byte array\ngot: %s", got)
	}
}

// TestMarshalObject_CanonicalizesListElements kills the list-element
// recursion. A suite with only scalar cases cannot tell it from dead code.
func TestMarshalObject_CanonicalizesListElements(t *testing.T) {
	t.Parallel()
	got := marshalBypassBuilt(t)

	want := `"samples":["` + canonicalRFC3339 + `","2026-08-19T13:00:00Z"]`
	if !strings.Contains(got, want) {
		t.Errorf("List<Timestamp> did not canonicalize elementwise: want %s\ngot: %s", want, got)
	}
}

// TestMarshalObject_CanonicalizesForeignKeyComponents kills the foreign-key
// arm. The value lives on the association's TARGET type, so the row's own type
// cannot reach its constraint — and encoding/json renders a time.Time as
// RFC 3339, which is the wrong text under a declared layout.
func TestMarshalObject_CanonicalizesForeignKeyComponents(t *testing.T) {
	t.Parallel()
	got := marshalBypassBuilt(t)

	// The control: the target's own property renders through the declared
	// layout, so the FK has something to agree with.
	if !strings.Contains(got, `"at":"`+canonicalLayout+`"`) {
		t.Fatalf("Station's own key property did not render through its layout\ngot: %s", got)
	}
	if !strings.Contains(got, `"feed":["`+canonicalLayout+`"]`) {
		t.Errorf("the foreign key did not render through the target's layout\ngot: %s", got)
	}
	if strings.Contains(got, `"feed":["`+canonicalRFC3339+`"]`) {
		t.Errorf("the foreign key rendered as RFC 3339 rather than the target's layout\ngot: %s", got)
	}
	if strings.Contains(got, canonicalGoPrint) {
		t.Errorf("a value rendered through Go's default time layout\ngot: %s", got)
	}
}
