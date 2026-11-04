package snapshot_test

import (
	"context"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/internal/snapparts"
	"github.com/simon-lentz/yammm/internal/snappartstest"
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

func loadWireTestSchema(t *testing.T) schema.Schema {
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

// buildWireTestSnapshot loads a document stating reading and samples as given,
// and a float at every other position. An integer at a Float is one no validator
// stores, and the golden's header claims nothing about its values, so the
// fixture is a document.
func buildWireTestSnapshot(t *testing.T, s schema.Schema, reading any, samples []any) graph.Snapshot {
	t.Helper()
	stationID := mustTypeID(t, s, "Station")
	sensorID := mustTypeID(t, s, "Sensor")
	gain := wrapProps(map[string]any{"gain": float64(2)})
	sensor := func(id string, reading any, samples []any) snapparts.Instance {
		return snapparts.Instance{TypeID: sensorID, PrimaryKey: wrapKey(id), Properties: wrapProps(sensorProps(id, reading, samples))}
	}

	s1 := sensor("s1", reading, samples)
	s1.Composed = map[string][]snapparts.Instance{"PARTS": {{
		TypeID: mustTypeID(t, s, "Part"), PrimaryKey: wrapKey("p1"),
		Properties: wrapProps(map[string]any{"name": "p1", "weight": float64(3)}),
	}}}
	return snappartstest.Loaded(t, s, snapparts.Parts{
		Attestation: &snapparts.Attestation{Associations: true},
		Instances: []snapparts.Instance{
			{TypeID: stationID, PrimaryKey: wrapKey("st1"), Properties: wrapProps(map[string]any{"id": "st1"})},
			s1,
			sensor("s2", float64(6), []any{float64(8)}),
		},
		Edges: []snapparts.Edge{{
			Relation: "FEED", SourceType: sensorID, SourceKey: wrapKey("s1"), TargetKey: wrapKey("st1"), Properties: gain,
		}},
		// Same key as s1, floats intact.
		Duplicates: []snapparts.Instance{sensor("s1", float64(7), []any{float64(4)})},
		// A missing station: the gain property rides the diagnostics section.
		Unresolved: []snapparts.Unresolved{{
			SourceType: sensorID, SourceKey: wrapKey("s2"), Relation: "FEED", TargetKey: wrapKey("ghost"),
			Reason: "target_missing", Properties: gain,
		}},
	})
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

// An integer a document states at a Float reads back as its nearest float64, so
// one past 2^53 rounds, and the next write states each as a float and is the
// fixpoint: no writer rewrites an integer for the reader's sake.
func TestRoundTrip_AnIntegerAtAFloatReadsAsItsNearestFloat64(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := loadWireTestSchema(t)
	const beyondExact = int64(1)<<53 + 1
	snap := buildWireTestSnapshot(t, s, int64(1860000), []any{int64(3), beyondExact})

	sensorID := mustTypeID(t, s, "Sensor")
	inst, ok := snap.InstanceByKey(sensorID, graph.FormatKey("s1"))
	if !ok {
		t.Fatal("the sensor did not load")
	}
	props := inst.Properties().Clone()
	yammmtest.Diff(t, map[string]any{"reading": float64(1860000), "samples": []any{float64(3), float64(9007199254740992)}},
		map[string]any{"reading": props["reading"], "samples": props["samples"]})

	data, result := snapshot.Marshal(ctx, snap)
	if err := result.Err(); err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if doc := string(data); !strings.Contains(doc, `"reading":1860000.0`) || !strings.Contains(doc, `"samples":[3.0,9007199254740992.0]`) {
		t.Fatalf("the loaded floats were not written as floats:\n%s", doc)
	}
	loaded, lres := snapshot.Load(ctx, data, s)
	if err := lres.Err(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	data2, r2 := snapshot.Marshal(ctx, loaded)
	if err := r2.Err(); err != nil {
		t.Fatalf("second Marshal: %v", err)
	}
	if string(data2) != string(data) {
		t.Error("the document written from a load is not a marshal fixpoint")
	}
}

func wrapKey(parts ...any) immutable.Key { return immutable.WrapKey(parts) }

func wrapProps(m map[string]any) immutable.Properties { return immutable.WrapProperties(m) }

// listIntSchema declares a List whose element constraint is not Float, so its
// integer elements are written as held.
const listIntSchema = `schema "listint"

type Tally {
	id String primary
	counts List<Integer>
}
`

// TestMarshal_ListOfIntegerElementsStayIntShaped pins the KindList arm's
// recursion into a non-Float element constraint: an Integer element is written
// int-shaped, which no List<Float> fixture can detect.
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

// TestMarshal_Float64ArmEmitsAtFullWidth pins the width wireFloat emits a
// float64 at. A value carrying more mantissa than float32 holds renders
// differently at bitSize 32, which would truncate a property the document
// declared as a Float.
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
