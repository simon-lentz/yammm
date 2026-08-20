package snapshot_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/instance/instancetest"
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
	logged_at Timestamp["2006-01-02 15:04:05"]
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

func loadTemporalSchema(t *testing.T) *schema.Schema {
	t.Helper()
	s, result := schema.LoadString(t.Context(), temporalTestSchema, "temporaltest.yammm")
	if result.HasErrors() {
		t.Fatalf("load temporaltest schema: %s", result)
	}
	return s
}

// canonicalText is the one spelling every position must reach, whichever
// representation the caller submitted.
const (
	canonicalCreated   = "2026-08-19T12:00:00.5Z"
	canonicalLogged    = "2026-08-19 12:00:00"
	canonicalRunID     = "0a35ef0f-9d40-4b6b-a0a1-0d1a5a0e1f2b"
	canonicalInstalled = "2026-08-19"
)

func temporalProps(id string, native bool) map[string]any {
	if native {
		return map[string]any{
			"id":         id,
			"created_at": time.Date(2026, 8, 19, 12, 0, 0, 500000000, time.UTC),
			"logged_at":  time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC),
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
func buildTemporalSnapshot(t *testing.T, s *schema.Schema, native bool) *graph.Snapshot {
	t.Helper()
	ctx := t.Context()
	v := instance.NewValidator(s)
	g := graph.New(s)

	station := mustValidate(t, v, "Station", map[string]any{"id": "st1"})
	g.Add(ctx, station)

	raw := instance.RawInstance{Properties: temporalProps("s1", native)}
	raw.Properties["feed"] = map[string]any{
		"_target_id": "st1",
		"seen_at":    edgeSeenAt(native),
	}
	sensor, res := v.ValidateOne(ctx, "Sensor", raw)
	if !res.OK() {
		t.Fatalf("validating Sensor: %s", res)
	}
	g.Add(ctx, sensor)

	reading := mustValidatePart(t, v, "Reading", map[string]any{
		"name":     "r1",
		"taken_at": edgeSeenAt(native),
	})
	g.AddComposed(ctx, "Sensor", graph.FormatKey("s1"), "READINGS", reading)

	return g.Snapshot()
}

func edgeSeenAt(native bool) any {
	if native {
		return time.Date(2026, 8, 19, 14, 0, 0, 0, time.UTC)
	}
	return "2026-08-19T14:00:00+00:00"
}

func mustValidate(t *testing.T, v *instance.Validator, typeName string, props map[string]any) *instance.ValidInstance {
	t.Helper()
	vi, res := v.ValidateOne(t.Context(), typeName, instance.RawInstance{Properties: props})
	if !res.OK() {
		t.Fatalf("validating %s: %s", typeName, res)
	}
	return vi
}

func mustValidatePart(t *testing.T, v *instance.Validator, typeName string, props map[string]any) *instance.ValidInstance {
	t.Helper()
	vis, res := v.ValidateForComposition(t.Context(), "Sensor", "READINGS", []instance.RawInstance{{Properties: props}})
	if !res.OK() {
		t.Fatalf("validating composed %s: %s", typeName, res)
	}
	return vis[0]
}

// TestMarshal_TemporalKindsReachOneFixpoint is the assertion the defect fails:
// a document holding all three kinds in either representation marshals to one
// byte sequence, and re-marshalling what it loads changes nothing.
func TestMarshal_TemporalKindsReachOneFixpoint(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := loadTemporalSchema(t)

	fromNative, result := snapshot.Marshal(ctx, buildTemporalSnapshot(t, s, true))
	if err := result.Err(); err != nil {
		t.Fatalf("Marshal (go-native): %v", err)
	}
	fromStrings, result := snapshot.Marshal(ctx, buildTemporalSnapshot(t, s, false))
	if err := result.Err(); err != nil {
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

	// The declared layout is satisfied by the text that was written under it,
	// which is the second defect closed as a consequence of the first.
	if _, err := time.Parse("2006-01-02 15:04:05", canonicalLogged); err != nil {
		t.Fatalf("test constant is not valid under its own layout: %v", err)
	}

	loaded, lres := snapshot.Load(ctx, fromNative, s)
	if err := lres.Err(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	again, r2 := snapshot.Marshal(ctx, loaded)
	if err := r2.Err(); err != nil {
		t.Fatalf("second Marshal: %v", err)
	}
	if string(again) != doc {
		t.Error("Marshal(Load(Marshal(x))) differs from Marshal(x): canonicalization is not a fixpoint")
	}
}

// bypassSnapshot builds a graph through instance.NewValidInstance, which
// receives no schema and runs no validation. It is the one path a value can
// take into a graph without being canonicalized, and it is why the wire and
// writer arms exist.
func bypassSnapshot(t *testing.T, s *schema.Schema, props map[string]any) *graph.Snapshot {
	t.Helper()
	g := graph.New(s)
	inst := instancetest.VI(
		"Sensor",
		instancetest.TypeID(mustTypeID(t, s, "Sensor")),
		instancetest.PK("s1"),
		instancetest.Props(props),
	)
	g.Add(t.Context(), inst)
	return g.Snapshot()
}

// TestMarshal_CanonicalizesABypassBuiltValue kills the wire arm. A validated
// value is already canonical by the time it reaches wireValue, so only a graph
// built through the bypass constructor can prove the arm runs.
func TestMarshal_CanonicalizesABypassBuiltValue(t *testing.T) {
	t.Parallel()
	s := loadTemporalSchema(t)

	snap := bypassSnapshot(t, s, map[string]any{
		"id":         "s1",
		"created_at": time.Date(2026, 8, 19, 12, 0, 0, 500000000, time.UTC),
		"logged_at":  time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC),
		"run_id":     uuid.MustParse(canonicalRunID),
		"installed":  time.Date(2026, 8, 19, 6, 0, 0, 0, time.UTC),
		"samples":    []any{time.Date(2026, 8, 19, 12, 0, 0, 500000000, time.UTC)},
	})

	data, result := snapshot.Marshal(context.Background(), snap)
	if err := result.Err(); err != nil {
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
			t.Errorf("bypass-built value not canonicalized on the wire: want %s\ngot: %s", want, doc)
		}
	}
}

// TestMarshal_HealsOrPassesThroughNonConformingText pins A-39's contract on the
// three new kinds, mirroring what TestMarshal_HealsNarrowedFloats pins for
// Float: the write arm heals what it can, preserves what it cannot, and never
// fails a write.
func TestMarshal_HealsOrPassesThroughNonConformingText(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := loadTemporalSchema(t)

	snap := bypassSnapshot(t, s, map[string]any{
		"id": "s1",
		// Healable: a non-canonical spelling of a valid instant.
		"created_at": "2026-08-19T12:00:00.500+00:00",
		// Unhealable: RFC 3339 text under a declared layout that rejects it.
		// This is what a pre-release document written under the old defect
		// holds, so erroring here would make yammm's own output unwritable.
		"logged_at": "2026-08-19T12:00:00Z",
		"run_id":    "not-a-uuid",
	})

	data, result := snapshot.Marshal(ctx, snap)
	if err := result.Err(); err != nil {
		t.Fatalf("Marshal refused a non-conforming value: %v", err)
	}
	doc := string(data)

	if !strings.Contains(doc, `"created_at":"`+canonicalCreated+`"`) {
		t.Errorf("the healable value did not heal\ngot: %s", doc)
	}
	if !strings.Contains(doc, `"logged_at":"2026-08-19T12:00:00Z"`) {
		t.Errorf("the unhealable value was not preserved byte-for-byte\ngot: %s", doc)
	}
	if !strings.Contains(doc, `"run_id":"not-a-uuid"`) {
		t.Errorf("the unparseable UUID was not preserved byte-for-byte\ngot: %s", doc)
	}

	// Healing is idempotent, so the healed document is the fixpoint.
	loaded, lres := snapshot.Load(ctx, data, s)
	if err := lres.Err(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	again, r2 := snapshot.Marshal(ctx, loaded)
	if err := r2.Err(); err != nil {
		t.Fatalf("second Marshal: %v", err)
	}
	if string(again) != doc {
		t.Error("the healed document is not a marshal fixpoint")
	}
}

// TestLoad_CanonicalizesNonConformingWireText proves the rebuild arm covers
// the load path. A .ys can hold a non-canonical spelling — this library wrote
// such documents before the rule existed, and nothing re-validates on load —
// so the graph a Load produces has to render it rather than carry it forward.
//
// The bytes are edited after marshalling, which is why the load skips the
// integrity check: the point is the value, not the hash.
func TestLoad_CanonicalizesNonConformingWireText(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := loadTemporalSchema(t)

	data, result := snapshot.Marshal(ctx, buildTemporalSnapshot(t, s, false))
	if err := result.Err(); err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	const canonical = `"created_at":"` + canonicalCreated + `"`
	const nonCanonical = `"created_at":"2026-08-19T12:00:00.500+00:00"`
	edited := strings.Replace(string(data), canonical, nonCanonical, 1)
	if edited == string(data) {
		t.Fatalf("fixture did not carry %s; the edit is a no-op", canonical)
	}

	loaded, lres := snapshot.Load(ctx, []byte(edited), s, snapshot.WithSkipIntegrityCheck())
	if err := lres.Err(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	sensor := loaded.InstancesOf(mustTypeID(t, s, "Sensor"))[0]
	v, ok := sensor.Property("created_at")
	if !ok {
		t.Fatal("created_at missing after load")
	}
	if got := v.Unwrap(); got != canonicalCreated {
		t.Errorf("loaded created_at = %#v, want %q — the rebuild arm did not render it", got, canonicalCreated)
	}
}

// nonConformingDocument returns a .ys whose created_at holds text no Timestamp
// constraint can render. The bytes are edited after marshalling because
// nothing in the library will write such a value — which is the point: a
// document can hold one, and until this option nothing could say so.
func nonConformingDocument(t *testing.T, s *schema.Schema) []byte {
	t.Helper()
	data, result := snapshot.Marshal(context.Background(), buildTemporalSnapshot(t, s, false))
	if err := result.Err(); err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	edited := strings.Replace(string(data),
		`"created_at":"`+canonicalCreated+`"`,
		`"created_at":"not-a-timestamp"`, 1)
	if edited == string(data) {
		t.Fatal("fixture did not carry created_at; the edit is a no-op")
	}
	return []byte(edited)
}

// TestLoad_ValueConformanceReportsAndStillReturns covers the option's whole
// contract in one place, both halves of it. Warning severity means Load
// returns the snapshot together with the finding: reporting a document is not
// refusing it.
func TestLoad_ValueConformanceReportsAndStillReturns(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := loadTemporalSchema(t)
	data := nonConformingDocument(t, s)

	loaded, result := snapshot.Load(ctx, data, s,
		snapshot.WithSkipIntegrityCheck(), snapshot.WithValueConformance())
	if err := result.Err(); err != nil {
		t.Fatalf("Load refused a non-conforming document: %v", err)
	}
	if loaded == nil {
		t.Fatal("Load returned no snapshot; the option must report, not reject")
	}
	if !result.HasCode(diag.W_SNAPSHOT_VALUE_NONCONFORMING) {
		t.Errorf("no conformance warning: %s", result)
	}
	if !result.HasWarnings() {
		t.Error("the finding is not a warning")
	}
}

// TestLoad_ValueConformanceIsOffByDefault is the control that tells the option
// from a hard-coded warning. Without it, a test asserting only the finding
// would pass against an implementation that always reports.
func TestLoad_ValueConformanceIsOffByDefault(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := loadTemporalSchema(t)
	data := nonConformingDocument(t, s)

	_, result := snapshot.Load(ctx, data, s, snapshot.WithSkipIntegrityCheck())
	if result.HasCode(diag.W_SNAPSHOT_VALUE_NONCONFORMING) {
		t.Errorf("a conformance warning without the option: %s", result)
	}
}

// TestLoad_ValueConformanceIsSilentOnAConformingDocument is the positive
// control. Without it the option could report everything and still pass.
func TestLoad_ValueConformanceIsSilentOnAConformingDocument(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := loadTemporalSchema(t)

	data, result := snapshot.Marshal(ctx, buildTemporalSnapshot(t, s, true))
	if err := result.Err(); err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	_, lres := snapshot.Load(ctx, data, s, snapshot.WithValueConformance())
	if lres.HasCode(diag.W_SNAPSHOT_VALUE_NONCONFORMING) {
		t.Errorf("a conforming document drew a conformance warning: %s", lres)
	}
}

// TestVerify_HonorsValueConformance is why the walk lives in the shared
// validation body rather than at materialization: Verify stops before a
// Snapshot exists, so a check placed later would be absent from the surface
// whose whole job is answering whether a document is sound.
func TestVerify_HonorsValueConformance(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := loadTemporalSchema(t)
	data := nonConformingDocument(t, s)

	if result := snapshot.Verify(ctx, data, s, snapshot.WithSkipIntegrityCheck()); result.HasCode(diag.W_SNAPSHOT_VALUE_NONCONFORMING) {
		t.Errorf("Verify reported without the option: %s", result)
	}
	result := snapshot.Verify(ctx, data, s,
		snapshot.WithSkipIntegrityCheck(), snapshot.WithValueConformance())
	if !result.HasCode(diag.W_SNAPSHOT_VALUE_NONCONFORMING) {
		t.Errorf("Verify did not report under the option: %s", result)
	}
}
