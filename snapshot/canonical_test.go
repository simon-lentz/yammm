package snapshot_test

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/simon-lentz/yammm/internal/snapparts"

	"github.com/google/uuid"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/snappartstest"
	"github.com/simon-lentz/yammm/internal/yammmtest"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

// temporalTestSchema puts a canonicalizing kind in every wire position the
// rule has to reach: node property, declared-format node property, aliased
// timestamp, list element, edge property and composed child.
const temporalTestSchema = `schema "temporaltest"

type EventTime = Timestamp

type Station {
	id String primary
}

part type Reading {
	name String primary
	taken_at Timestamp
}

type Sensor {
	id String primary
	created_at Timestamp
	logged_at Timestamp
	aliased EventTime
	run_id UUID
	installed Date
	samples List<Timestamp>
	--> FEED (_) Station {
		seen_at Timestamp
	}
	*-> READINGS (many) Reading
}
`

func loadTemporalSchema(t *testing.T) schema.Schema {
	t.Helper()
	s, err := schema.LoadString(t.Context(), temporalTestSchema, "temporaltest.yammm")
	if err != nil {
		t.Fatalf("load temporaltest schema: %s", err)
	}
	return s
}

// canonicalText is the one spelling every position must reach, whichever
// representation the caller submitted.
const (
	canonicalCreated   = "2026-08-19T12:00:00.5Z"
	canonicalLogged    = "2026-08-19T12:00:00+02:00"
	canonicalRunID     = "0a35ef0f-9d40-4b6b-a0a1-0d1a5a0e1f2b"
	canonicalInstalled = "2026-08-19"
)

func temporalProps(id string, native bool) map[string]any {
	if native {
		return map[string]any{
			"id":         id,
			"created_at": time.Date(2026, 8, 19, 12, 0, 0, 500000000, time.UTC),
			"logged_at":  time.Date(2026, 8, 19, 12, 0, 0, 0, time.FixedZone("", 2*60*60)),
			"aliased":    time.Date(2026, 8, 19, 12, 0, 0, 500000000, time.UTC),
			"run_id":     uuid.MustParse(canonicalRunID),
			"installed":  time.Date(2026, 8, 19, 6, 0, 0, 0, time.UTC),
			"samples": []any{
				time.Date(2026, 8, 19, 12, 0, 0, 500000000, time.UTC),
				"2026-08-19T13:00:00+00:00",
			},
		}
	}
	return map[string]any{
		"id":         id,
		"created_at": "2026-08-19T12:00:00.500Z",
		"logged_at":  canonicalLogged,
		"aliased":    "2026-08-19T12:00:00.500Z",
		"run_id":     strings.ToUpper(canonicalRunID),
		"installed":  canonicalInstalled,
		"samples":    []any{"2026-08-19T12:00:00.500Z", "2026-08-19T13:00:00+00:00"},
	}
}

// buildTemporalSnapshot validates every instance, so the graph holds what the
// validation arm produced.
func buildTemporalSnapshot(t *testing.T, s schema.Schema, native bool) graph.Snapshot {
	t.Helper()
	ctx := t.Context()
	v := instance.NewValidator(s)
	g := graph.New(s)

	station := mustValidate(t, v, "Station", map[string]any{"id": "st1"})
	if err := g.Add(ctx, station); err != nil {
		t.Errorf("Add: %v", err)
	}

	raw := instance.RawInstance{Properties: temporalProps("s1", native)}
	raw.Properties["feed"] = map[string]any{
		"_target_id": "st1",
		"seen_at":    edgeSeenAt(native),
	}
	raw.Properties["readings"] = []any{map[string]any{
		"name":     "r1",
		"taken_at": edgeSeenAt(native),
	}}
	sensor, err := v.ValidateOne(ctx, "Sensor", raw)
	if err != nil {
		t.Fatalf("validating Sensor: %s", err)
	}
	if err := g.Add(ctx, sensor); err != nil {
		t.Errorf("Add: %v", err)
	}

	return g.Snapshot()
}

func edgeSeenAt(native bool) any {
	if native {
		return time.Date(2026, 8, 19, 14, 0, 0, 0, time.UTC)
	}
	return "2026-08-19T14:00:00+00:00"
}

func mustValidate(t *testing.T, v *instance.Validator, typeName string, props map[string]any) instance.ValidInstance {
	t.Helper()
	vi, err := v.ValidateOne(t.Context(), typeName, instance.RawInstance{Properties: props})
	if err != nil {
		t.Fatalf("validating %s: %s", typeName, err)
	}
	return vi
}

// TestMarshal_TemporalKindsReachOneFixpoint is the assertion the defect fails:
// a document holding all three kinds in either representation marshals to one
// byte sequence, and re-marshalling what it loads changes nothing.
func TestMarshal_TemporalKindsReachOneFixpoint(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := loadTemporalSchema(t)

	fromNative, readErr := snapshot.Marshal(ctx, buildTemporalSnapshot(t, s, true))
	if err := readErr; err != nil {
		t.Fatalf("Marshal (go-native): %v", err)
	}
	fromStrings, readErr := snapshot.Marshal(ctx, buildTemporalSnapshot(t, s, false))
	if err := readErr; err != nil {
		t.Fatalf("Marshal (string): %v", err)
	}

	if string(fromNative) != string(fromStrings) {
		t.Errorf("the two representations of one document marshal differently:\n go-native: %s\n strings:   %s",
			fromNative, fromStrings)
	}

	doc := string(fromNative)
	for _, want := range []string{
		`"created_at":"` + canonicalCreated + `"`,
		`"logged_at":"` + canonicalLogged + `"`,
		`"aliased":"` + canonicalCreated + `"`,
		`"run_id":"` + canonicalRunID + `"`,
		`"installed":"` + canonicalInstalled + `"`,
		`"samples":["` + canonicalCreated + `","2026-08-19T13:00:00Z"]`,
		`"seen_at":"2026-08-19T14:00:00Z"`,
		`"taken_at":"2026-08-19T14:00:00Z"`,
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("marshal output lacks %s\ngot: %s", want, doc)
		}
	}

	loaded, readErr2 := snapshot.Unmarshal(ctx, fromNative, s)
	if err := readErr2; err != nil {
		t.Fatalf("Load: %v", err)
	}
	again, r2 := snapshot.Marshal(ctx, loaded)
	if err := r2; err != nil {
		t.Fatalf("second Marshal: %v", err)
	}
	if string(again) != doc {
		t.Error("Marshal(Load(Marshal(x))) differs from Marshal(x): canonicalization is not a fixpoint")
	}
}

// loadedSensor loads a document holding Sensor s1 with props, text no validator
// stored.
func loadedSensor(t *testing.T, s schema.Schema, props map[string]any) graph.Snapshot {
	t.Helper()
	return snappartstest.Loaded(t, s, snapparts.Parts{Instances: []snapparts.Instance{{
		TypeID: mustTypeID(t, s, "Sensor"), PrimaryKey: immutable.WrapKey([]any{"s1"}),
		Properties: immutable.WrapProperties(props),
	}}})
}

// TestMarshal_CanonicalizesALoadedValue holds text a document spells another way
// to the canonical form on the wire: a Timestamp at the offset +00:00 and with
// trailing zeros, a UUID in upper case, and a list element.
func TestMarshal_CanonicalizesALoadedValue(t *testing.T) {
	t.Parallel()
	s := loadTemporalSchema(t)

	snap := loadedSensor(t, s, map[string]any{
		"id":         "s1",
		"created_at": "2026-08-19T12:00:00.500+00:00",
		"logged_at":  canonicalLogged,
		"run_id":     strings.ToUpper(canonicalRunID),
		"installed":  canonicalInstalled,
		"samples":    []any{"2026-08-19T12:00:00.500+00:00"},
	})

	data, readErr := snapshot.Marshal(context.Background(), snap)
	if err := readErr; err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	doc := string(data)

	for _, want := range []string{
		`"created_at":"` + canonicalCreated + `"`,
		`"logged_at":"` + canonicalLogged + `"`,
		`"run_id":"` + canonicalRunID + `"`,
		`"installed":"` + canonicalInstalled + `"`,
		`"samples":["` + canonicalCreated + `"]`,
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("value not canonicalized on the wire: want %s\ngot: %s", want, doc)
		}
	}
}

// TestMarshal_HealsOrPassesThroughNonConformingText pins the write contract on
// Timestamp, Date and UUID: the write arm heals what it can, preserves what it
// cannot, and never fails a write.
func TestMarshal_HealsOrPassesThroughNonConformingText(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := loadTemporalSchema(t)

	snap := loadedSensor(t, s, map[string]any{
		"id": "s1",
		// Healable: a non-canonical spelling of a valid instant.
		"created_at": "2026-08-19T12:00:00.500+00:00",
		// Unhealable: wall-clock text, which is not RFC 3339. A stored
		// document can hold it, so erroring here would make it unwritable.
		"logged_at": "2026-08-19 12:00:00",
		"run_id":    "not-a-uuid",
	})

	data, readErr := snapshot.Marshal(ctx, snap)
	if err := readErr; err != nil {
		t.Fatalf("Marshal refused a non-conforming value: %v", err)
	}
	doc := string(data)

	if !strings.Contains(doc, `"created_at":"`+canonicalCreated+`"`) {
		t.Errorf("the healable value did not heal\ngot: %s", doc)
	}
	if !strings.Contains(doc, `"logged_at":"2026-08-19 12:00:00"`) {
		t.Errorf("the unhealable value was not preserved byte-for-byte\ngot: %s", doc)
	}
	if !strings.Contains(doc, `"run_id":"not-a-uuid"`) {
		t.Errorf("the unparseable UUID was not preserved byte-for-byte\ngot: %s", doc)
	}

	// Healing is idempotent, so the healed document is the fixpoint.
	loaded, readErr2 := snapshot.Unmarshal(ctx, data, s)
	if err := readErr2; err != nil {
		t.Fatalf("Load: %v", err)
	}
	again, r2 := snapshot.Marshal(ctx, loaded)
	if err := r2; err != nil {
		t.Fatalf("second Marshal: %v", err)
	}
	if string(again) != doc {
		t.Error("the healed document is not a marshal fixpoint")
	}
}

// TestLoad_CanonicalizesNonConformingWireText proves the load path renders what
// it reads. A .ys can hold a non-canonical spelling — this library wrote
// such documents before the rule existed, and nothing re-validates on load —
// so the graph a Load produces has to render it rather than carry it forward.
//
// The bytes are edited after marshalling, which is why the load skips the
// integrity check: the point is the value, not the hash.
func TestLoad_CanonicalizesNonConformingWireText(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := loadTemporalSchema(t)

	data, readErr := snapshot.Marshal(ctx, buildTemporalSnapshot(t, s, false))
	if err := readErr; err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	const canonical = `"created_at":"` + canonicalCreated + `"`
	const nonCanonical = `"created_at":"2026-08-19T12:00:00.500+00:00"`
	edited := strings.Replace(string(data), canonical, nonCanonical, 1)
	if edited == string(data) {
		t.Fatalf("fixture did not carry %s; the edit is a no-op", canonical)
	}

	loaded, readErr2 := snapshot.Unmarshal(ctx, []byte(edited), s, snapshot.WithIntegrityCheck(false))
	if err := readErr2; err != nil {
		t.Fatalf("Load: %v", err)
	}

	sensor := slices.Collect(loaded.InstancesOf(mustTypeID(t, s, "Sensor")))[0]
	v, ok := sensor.Property("created_at")
	if !ok {
		t.Fatal("created_at missing after load")
	}
	if got := v.Any(); got != canonicalCreated {
		t.Errorf("loaded created_at = %#v, want %q — Load did not render it", got, canonicalCreated)
	}
}

// TestMarshal_TemporalGoldenBytes pins the wire bytes of every canonical
// spelling the validator produces for Timestamp, UUID and Date — node
// property, alias, list element, edge property
// and composed child — so a change to a stored form is a reviewed golden
// diff rather than a silent move of every persisted key.
func TestMarshal_TemporalGoldenBytes(t *testing.T) {
	t.Parallel()
	s := loadTemporalSchema(t)
	snap := buildTemporalSnapshot(t, s, true)

	data, readErr := snapshot.Marshal(t.Context(), snap, snapshot.WithIndent("\t"))
	if err := readErr; err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	yammmtest.Golden(t, "marshal_temporal_representative.ys", data)
}
