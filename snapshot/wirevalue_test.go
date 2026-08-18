package snapshot_test

import (
	"context"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/instance/instancetest"
	"github.com/simon-lentz/yammm/internal/yammmtest"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
	"github.com/simon-lentz/yammm/snapshot/snapshottest"
)

// wireTestSchema declares a float in every wire position the emitter must
// cover: node property, aliased float, list element, vector element, edge
// property (resolved and unresolved share the relation), composed child, and
// — via a duplicate instance — the diagnostics section.
const wireTestSchema = `schema "wiretest"

type Scale = Float[0.0, 100.0]

type Station {
	id String primary
}

part type Part {
	name String primary
	weight Float
}

type Sensor {
	id String primary
	reading Float
	scale Scale
	samples List<Float>
	embedding Vector[3]
	--> FEED (_) Station {
		gain Float
	}
	*-> PARTS (many) Part
}
`

func loadWireTestSchema(t *testing.T) *schema.Schema {
	t.Helper()
	s, result := schema.LoadString(t.Context(), wireTestSchema, "wiretest.yammm")
	if result.HasErrors() {
		t.Fatalf("load wiretest schema: %s", result)
	}
	return s
}

// sensorProps returns a full float-bearing property set with the given
// reading value, so callers control the one field under test.
func sensorProps(id string, reading any, samples []any) map[string]any {
	return map[string]any{
		"id":        id,
		"reading":   reading,
		"scale":     float64(5),
		"samples":   samples,
		"embedding": []any{0.25, float64(1), float64(2)},
	}
}

func buildWireTestSnapshot(t *testing.T, s *schema.Schema, reading any, samples []any) *graph.Snapshot {
	t.Helper()
	ctx := context.Background()
	g := graph.New(s)

	station := mustValidInstance(t, s, "Station", []any{"st1"}, map[string]any{"id": "st1"})
	g.Add(ctx, station)

	edge := map[string]*instance.ValidEdgeData{"FEED": instance.NewValidEdgeData([]instance.ValidEdgeTarget{
		instance.NewValidEdgeTarget(wrapKey("st1"), wrapProps(map[string]any{"gain": float64(2)})),
	})}
	sensor := instancetest.VI(
		"Sensor",
		instancetest.TypeID(mustTypeID(t, s, "Sensor")),
		instancetest.PK("s1"),
		instancetest.Props(sensorProps("s1", reading, samples)),
		instancetest.Edges(edge),
	)
	g.Add(ctx, sensor)

	part := mustValidPartInstance(t, s, "Part", []any{"p1"}, map[string]any{"name": "p1", "weight": float64(3)})
	g.AddComposed(ctx, "Sensor", graph.FormatKey("s1"), "PARTS", part)

	// Same PK as sensor: rejected into the duplicates section, floats intact.
	dup := instancetest.VI(
		"Sensor",
		instancetest.TypeID(mustTypeID(t, s, "Sensor")),
		instancetest.PK("s1"),
		instancetest.Props(sensorProps("s1", float64(7), []any{float64(4)})),
	)
	g.Add(ctx, dup)

	// Edge to a missing station: an unresolved (target_missing) record whose
	// gain property rides the diagnostics section.
	ghostEdge := map[string]*instance.ValidEdgeData{"FEED": instance.NewValidEdgeData([]instance.ValidEdgeTarget{
		instance.NewValidEdgeTarget(wrapKey("ghost"), wrapProps(map[string]any{"gain": float64(2)})),
	})}
	stray := instancetest.VI(
		"Sensor",
		instancetest.TypeID(mustTypeID(t, s, "Sensor")),
		instancetest.PK("s2"),
		instancetest.Props(sensorProps("s2", float64(6), []any{float64(8)})),
		instancetest.Edges(ghostEdge),
	)
	g.Add(ctx, stray)

	return g.Snapshot()
}

// TestMarshal_FloatEmissionEveryPosition pins the emitted form at every
// float-bearing wire position and the two closure properties behind the
// design: an exact-comparison round trip (no narrowing anywhere) and the
// byte fixpoint Marshal(Load(Marshal(x))) == Marshal(x) — the arm a
// TypeID-keyed composed-child resolver would silently fail.
func TestMarshal_FloatEmissionEveryPosition(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := loadWireTestSchema(t)
	snap := buildWireTestSnapshot(t, s, float64(1860000), []any{float64(1), 2.5})

	data, result := snapshot.Marshal(ctx, snap)
	if err := result.Err(); err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	doc := string(data)

	for _, want := range []string{
		`"reading":1860000.0`,        // whole float node property
		`"scale":5.0`,                // aliased float
		`"samples":[1.0,2.5]`,        // list elements, whole and fractional
		`"embedding":[0.25,1.0,2.0]`, // vector elements
		`"weight":3.0`,               // composed child property
		`"reading":7.0`,              // duplicate-section property
		`"reading":6.0`,              // unresolved-source node property
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("marshal output lacks %s", want)
		}
	}
	if got := strings.Count(doc, `"gain":2.0`); got != 2 {
		t.Errorf("gain emitted %d times in float form, want 2 (resolved + unresolved edge)", got)
	}

	loaded, lres := snapshot.Load(ctx, data, s)
	if err := lres.Err(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	snapshottest.DiffSnapshots(t, snap, loaded)

	data2, r2 := snapshot.Marshal(ctx, loaded)
	if err := r2.Err(); err != nil {
		t.Fatalf("second Marshal: %v", err)
	}
	if string(data2) != doc {
		t.Error("Marshal(Load(Marshal(x))) differs from Marshal(x): emission is not a fixpoint")
	}

	// The indented branch shares the emitter; spot-check one form.
	indented, ir := snapshot.Marshal(ctx, snap, snapshot.WithIndent("\t"))
	if err := ir.Err(); err != nil {
		t.Fatalf("indented Marshal: %v", err)
	}
	if !strings.Contains(string(indented), `"reading": 1860000.0`) {
		t.Error("indented marshal output lacks the decimal form")
	}

	// Byte-exact regression pin for the float-bearing document shape.
	yammmtest.Golden(t, "marshal_float_representative.ys", indented)
}

// TestMarshal_HealsNarrowedFloats pins the healing rule. Load never
// re-validates, so a document's int-shaped floats reach memory as
// int64 — built here directly, which is the identical in-memory state — and
// the next Marshal emits them in decimal form. An int64 beyond 2^53 passes
// through instead: float64 conversion could corrupt it, and no narrowed
// float can have produced it.
func TestMarshal_HealsNarrowedFloats(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := loadWireTestSchema(t)
	const beyondExact = int64(1)<<53 + 1
	snap := buildWireTestSnapshot(t, s, int64(1860000), []any{int64(3), beyondExact})

	data, result := snapshot.Marshal(ctx, snap)
	if err := result.Err(); err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	doc := string(data)

	if !strings.Contains(doc, `"reading":1860000.0`) {
		t.Error("narrowed int64 under a Float constraint did not heal to decimal form")
	}
	if !strings.Contains(doc, `"samples":[3.0,9007199254740993]`) {
		t.Error("list healing wrong: want elementwise heal with beyond-2^53 passthrough")
	}

	// The healed document is already the fixpoint.
	loaded, lres := snapshot.Load(ctx, data, s)
	if err := lres.Err(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	data2, r2 := snapshot.Marshal(ctx, loaded)
	if err := r2.Err(); err != nil {
		t.Fatalf("second Marshal: %v", err)
	}
	if string(data2) != doc {
		t.Error("healed document is not a marshal fixpoint")
	}
}

// Healing changes a value's dynamic type on the first pass, so an int-shaped
// snapshot is not its own round-trip fixpoint — but healing is idempotent, so
// the loaded document is. This is the exception [snapshottest.AssertRoundTrip]
// documents, pinned rather than left to prose.
func TestRoundTrip_NarrowedFloatStabilisesAfterOnePass(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := loadWireTestSchema(t)
	narrowed := buildWireTestSnapshot(t, s, int64(1860000), []any{int64(3)})

	data, result := snapshot.Marshal(ctx, narrowed)
	if err := result.Err(); err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	healed, result := snapshot.Load(ctx, data, s)
	if err := result.Err(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	sensor, ok := healed.InstanceByKey(tidOf(t, s, "Sensor"), graph.FormatKey("s1"))
	if !ok {
		t.Fatal("sensor s1 missing after the healing pass")
	}
	reading, ok := sensor.Property("reading")
	if !ok {
		t.Fatal("reading property missing")
	}
	if _, isFloat := reading.Unwrap().(float64); !isFloat {
		t.Fatalf("reading = %T, want float64 — the narrowed whole float did not heal", reading.Unwrap())
	}

	snapshottest.AssertRoundTrip(t, healed, s)
}

func wrapKey(parts ...any) immutable.Key { return immutable.WrapKey(parts) }

func wrapProps(m map[string]any) immutable.Properties { return immutable.WrapProperties(m) }

// TestAssertRoundTrip_Float32Property drives a float32 through the round-trip
// criterion: the exact float64 widening compares equal under DiffSnapshots,
// and converting the loaded value back returns the original 32 bits.
func TestAssertRoundTrip_Float32Property(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := loadIdentitySchema(t)
	anchorID := mustTypeIDIn(t, s, "", "Anchor")

	want := float32(0.1)
	built, result := graph.RebuildSnapshot(s, graph.SnapshotParts{
		Types: []schema.TypeID{anchorID},
		Instances: map[schema.TypeID][]graph.InstanceParts{
			anchorID: {{
				TypeName:   tagForm(s, anchorID),
				TypeID:     anchorID,
				PrimaryKey: immutable.WrapKey([]any{"a1"}),
				Properties: immutable.WrapProperties(map[string]any{"id": "a1", "depth": want}),
			}},
		},
	})
	if result.HasErrors() {
		t.Fatalf("assembling: %s", result)
	}

	snapshottest.AssertRoundTrip(t, built, s)

	data, res := snapshot.Marshal(ctx, built)
	if err := res.Err(); err != nil {
		t.Fatalf("marshal: %v", err)
	}
	loaded, res := snapshot.Load(ctx, data, s)
	if err := res.Err(); err != nil {
		t.Fatalf("load: %v", err)
	}
	inst, ok := loaded.InstanceByKey(anchorID, graph.FormatKey("a1"))
	if !ok {
		t.Fatal("instance missing after round trip")
	}
	v, ok := inst.Property("depth")
	if !ok {
		t.Fatal("depth missing after round trip")
	}
	got, ok := v.Unwrap().(float64)
	if !ok {
		t.Fatalf("depth returned as %T, want float64", v.Unwrap())
	}
	if float32(got) != want {
		t.Errorf("32-bit fidelity lost: went in %v, returned %v", want, got)
	}
}

// listIntSchema declares a List whose element constraint is not Float, which
// is the arm the float healer must leave alone.
const listIntSchema = `schema "listint"

type Tally {
	id String primary
	counts List<Integer>
}
`

// TestMarshal_ListOfIntegerElementsStayIntShaped pins the KindList arm's
// recursion into a non-Float element constraint. Routing list elements
// through wireNumeric instead of wireValue heals Integer elements into
// floats, which no List<Float> fixture can detect.
func TestMarshal_ListOfIntegerElementsStayIntShaped(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	s, result := schema.LoadString(ctx, listIntSchema, "listint.yammm")
	if result.HasErrors() {
		t.Fatalf("load listint schema: %s", result)
	}

	g := graph.New(s)
	g.Add(ctx, mustValidInstance(t, s, "Tally", []any{"t1"}, map[string]any{
		"id":     "t1",
		"counts": []any{int64(1), int64(2), int64(3)},
	}))

	data, res := snapshot.Marshal(ctx, g.Snapshot())
	if err := res.Err(); err != nil {
		t.Fatalf("marshal: %v", err)
	}
	doc := string(data)

	if !strings.Contains(doc, `"counts":[1,2,3]`) {
		t.Errorf("Integer list elements did not emit int-shaped:\n%s", doc)
	}
	if strings.Contains(doc, "1.0") {
		t.Errorf("Integer list elements were healed into floats:\n%s", doc)
	}
}

// TestMarshal_Float64ArmEmitsAtFullWidth pins the width wireNumeric's float64
// arm emits at. A value carrying more mantissa than float32 holds renders
// differently at bitSize 32, so emitting it through wireFloat32 silently
// truncates a property the document declared as a Float.
func TestMarshal_Float64ArmEmitsAtFullWidth(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := loadWireTestSchema(t)

	// float32(3.141592653589793) renders "3.1415927": the two forms diverge
	// at the ninth digit, so neither is a substring of the other.
	const wide = 3.141592653589793
	snap := buildWireTestSnapshot(t, s, wide, []any{float64(1), 2.5})

	data, res := snapshot.Marshal(ctx, snap)
	if err := res.Err(); err != nil {
		t.Fatalf("marshal: %v", err)
	}
	doc := string(data)

	if !strings.Contains(doc, "3.141592653589793") {
		t.Errorf("float64 property did not emit at 64-bit width:\n%s", doc)
	}
	if strings.Contains(doc, "3.1415927") {
		t.Errorf("float64 property emitted at 32-bit width:\n%s", doc)
	}
}
