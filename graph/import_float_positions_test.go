package graph_test

import (
	"encoding/json"
	"math"
	"slices"
	"strconv"
	"testing"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/internal/snapparts"
	"github.com/simon-lentz/yammm/internal/snappartstest"
	"github.com/simon-lentz/yammm/snapshot/snapshottest"
)

// floatPositionsSchema declares, at every position a property can stand, the
// kind kind names: Integer for the snapshot's schema, Float for the importing one.
func floatPositionsSchema(kind, vector string) string {
	return "schema \"entry\"\n\n" +
		"part type P {\n\tname String primary\n\tn " + kind + "\n}\n\n" +
		"type U {\n\tid String primary\n}\n\n" +
		"type T {\n\tid String primary\n\tn " + kind + "\n\tns List<" + kind + ">\n\tv " + vector + "\n" +
		"\t--> E (_) U {\n\t\tw " + kind + "\n\t}\n" +
		"\t--> M (_) U {\n\t\tw " + kind + "\n\t}\n" +
		"\t*-> PS (many) P\n}\n"
}

// An import under another schema stores what the validator stores: where the
// snapshot's schema declares an Integer and the importing one a Float, every
// number at the Float is its nearest float64, as Graph.Add would store it.
func TestNewFromSnapshot_ANumberAtAFloatImportsAsItsFloat64(t *testing.T) {
	t.Parallel()
	built := loadEntry(t, floatPositionsSchema("Integer", "List<Integer>"))
	bound := loadEntry(t, floatPositionsSchema("Float", "Vector[2]"))
	tid, uid, pid := typeIDOf(t, built, "T"), typeIDOf(t, built, "U"), typeIDOf(t, built, "P")
	tKey := immutable.WrapKey([]any{"t1"})
	snap := snappartstest.Loaded(t, built, snapparts.Parts{
		Instances: []snapparts.Instance{
			{TypeID: uid, PrimaryKey: immutable.WrapKey([]any{"u1"}), Properties: immutable.WrapProperties(map[string]any{"id": "u1"})},
			{
				TypeID: tid, PrimaryKey: tKey,
				Properties: immutable.WrapProperties(map[string]any{
					"id": "t1", "n": json.Number("5"), "ns": []any{json.Number("1"), json.Number("9007199254740993"), json.Number("99999999999999999999")},
					"v": []any{json.Number("3"), json.Number("-4")},
				}),
				Composed: map[string][]snapparts.Instance{"PS": {{
					TypeID: pid, PrimaryKey: immutable.WrapKey([]any{"p1"}),
					Properties: immutable.WrapProperties(map[string]any{"name": "p1", "n": json.Number("7")}),
				}}},
			},
		},
		Edges: []snapparts.Edge{{
			Relation: "E", SourceType: tid, SourceKey: tKey, TargetKey: immutable.WrapKey([]any{"u1"}),
			Properties: immutable.WrapProperties(map[string]any{"w": json.Number("99999999999999999999")}),
		}},
		Duplicates: []snapparts.Instance{{
			TypeID: tid, PrimaryKey: tKey,
			Properties: immutable.WrapProperties(map[string]any{"id": "t1", "n": json.Number("8")}),
		}},
		Unresolved: []snapparts.Unresolved{{
			SourceType: tid, SourceKey: tKey, Relation: "M", TargetKey: immutable.WrapKey([]any{"u9"}),
			Reason: "target_missing", Properties: immutable.WrapProperties(map[string]any{"w": json.Number("6")}),
		}},
	})
	// The snapshot's own schema holds each as an integer, or the test reads nothing.
	root := slices.Collect(snap.InstancesOf(tid))[0]
	if got, _ := root.Properties().Get("n"); got.Unwrap() != int64(5) {
		t.Fatalf("under the snapshot's schema n is %#v, want int64(5)", got.Unwrap())
	}

	g, res := graph.NewFromSnapshot(bound, snap)
	if res.HasErrors() {
		t.Fatalf("NewFromSnapshot: %s", res)
	}
	imported := g.Snapshot()
	wantFloat := func(where string, props immutable.Properties, name string, want ...float64) {
		t.Helper()
		v, ok := props.Get(name)
		if !ok {
			t.Fatalf("%s: no property %q", where, name)
		}
		got := []any{v.Unwrap()}
		if s, ok := v.Unwrap().(immutable.Slice); ok {
			got = s.Clone()
		}
		if len(got) != len(want) {
			t.Fatalf("%s %s = %#v, want %v", where, name, got, want)
		}
		for i, w := range want {
			f, ok := got[i].(float64)
			if !ok || math.Float64bits(f) != math.Float64bits(w) {
				t.Errorf("%s %s[%d] = %#v, want float64(%v)", where, name, i, got[i], w)
			}
		}
	}
	inst := slices.Collect(imported.InstancesOf(typeIDOf(t, bound, "T")))[0]
	wantFloat("the root", inst.Properties(), "n", 5)
	wantFloat("the root", inst.Properties(), "ns", 1, 9007199254740992, 1e20)
	wantFloat("the root", inst.Properties(), "v", 3, -4)
	part := slices.Collect(inst.Composed("PS"))[0]
	wantFloat("the part", part.Properties(), "n", 7)
	edge := slices.Collect(imported.Edges())[0]
	wantFloat("the edge", edge.Properties(), "w", 1e20)
	rec := slices.Collect(imported.Unresolved())[0]
	wantFloat("the unresolved record", rec.Properties(), "w", 6)
	dup := slices.Collect(imported.Duplicates())[0]
	wantFloat("the duplicate", dup.Instance().Properties(), "n", 8)

	// Each value is the type a reload reads, so the import is its own round trip.
	snapshottest.AssertRoundTrip(t, imported, bound)
}

// An import under another schema copies no properties it rewrites nothing in:
// where every number at a Float is already a float64, each further instance
// costs what an instance with no Float costs. A copy costs at least one
// allocation an instance, and the race detector's runtime adds a few at random.
func TestNewFromSnapshot_CopiesNoPropertiesItLeavesAsTheyAre(t *testing.T) {
	const added = 100
	growth := func(kind string, v any) float64 {
		src := "schema \"entry\"\n\ntype T {\n\tid String primary\n\tn " + kind + "\n}\n"
		built, bound := loadEntry(t, src), loadEntry(t, src)
		allocs := func(n int) float64 {
			insts := make([]snapparts.Instance, n)
			for i := range insts {
				id := strconv.Itoa(i)
				insts[i] = snapparts.Instance{
					TypeID: typeIDOf(t, built, "T"), PrimaryKey: immutable.WrapKey([]any{id}),
					Properties: immutable.WrapProperties(map[string]any{"id": id, "n": v}),
				}
			}
			snap := snappartstest.Loaded(t, built, snapparts.Parts{Instances: insts})
			return testing.AllocsPerRun(5, func() { graph.NewFromSnapshot(bound, snap) })
		}
		return allocs(2*added) - allocs(added)
	}
	if float, str := growth("Float", 2.5), growth("String", "s"); float-str >= added {
		t.Errorf("%d more instances cost %v allocations at a Float, %v at a String; want fewer than one more an instance",
			added, float, str)
	}
}
