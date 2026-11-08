package snapshot_test

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/internal/instancetest"
	"github.com/simon-lentz/yammm/internal/snapparts"
	"github.com/simon-lentz/yammm/internal/snappartstest"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

// A negative zero at a Float or as a Vector element is written with its sign, so
// Load reads back the value it held, whether the validator read the literal -0 or
// a document spelled -0.0.
func TestMarshal_ANegativeZeroKeepsItsSignAtAFloat(t *testing.T) {
	t.Parallel()
	s, res := schema.LoadString(t.Context(), "schema \"nz\"\n\ntype Holder {\n\tid String primary\n\tf Float\n\tv Vector[2]\n}\n", "nz.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res)
	}
	holderT, _ := s.Type("Holder")
	props := map[string]any{"id": "h1", "f": json.Number("-0"), "v": []any{json.Number("-0"), json.Number("1")}}
	valid := instancetest.Valid(t, s, "Holder", props)
	if stored, _ := valid.Property("f"); !negativeZero(stored.Unwrap()) {
		t.Fatalf("the validator stores -0 at a Float as %#v; want a negative zero", stored.Unwrap())
	}
	g := graph.New(s)
	if r := g.Add(t.Context(), valid); r.HasErrors() {
		t.Fatalf("Add: %s", r)
	}
	for name, snap := range map[string]graph.Snapshot{
		"validated": g.Snapshot(),
		"loaded": snappartstest.Loaded(t, s, snapparts.Parts{Instances: []snapparts.Instance{{
			TypeID: holderT.ID(), PrimaryKey: immutable.WrapKey([]any{"h1"}), Properties: immutable.WrapProperties(map[string]any{
				"id": "h1", "f": math.Copysign(0, -1), "v": []any{math.Copysign(0, -1), float64(1)},
			}),
		}}}),
	} {
		data, mres := snapshot.Marshal(t.Context(), snap)
		if mres.HasErrors() {
			t.Fatalf("%s: Marshal: %s", name, mres)
		}
		back, lres := snapshot.Load(t.Context(), data, s)
		if lres.HasErrors() {
			t.Fatalf("%s: Load: %s", name, lres)
		}
		inst, ok := back.InstanceByKey(holderT.ID(), `["h1"]`)
		if !ok {
			t.Fatalf("%s: Load: no Holder h1", name)
		}
		if p, _ := inst.Property("f"); !negativeZero(p.Unwrap()) {
			t.Errorf("%s: f read back as %#v from %s", name, p.Unwrap(), data)
		}
		p, _ := inst.Property("v")
		switch v := p.Clone().(type) {
		case []any:
			if len(v) != 2 || !negativeZero(v[0]) {
				t.Errorf("%s: v read back as %#v from %s", name, v, data)
			}
		case []float64:
			if len(v) != 2 || !negativeZero(v[0]) {
				t.Errorf("%s: v read back as %#v from %s", name, v, data)
			}
		default:
			t.Errorf("%s: v read back as %#v from %s", name, v, data)
		}
	}
}

func negativeZero(v any) bool {
	f, ok := v.(float64)
	return ok && f == 0 && math.Signbit(f)
}
