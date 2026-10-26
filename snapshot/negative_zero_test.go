package snapshot_test

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/instancetest"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

// A json.Number -0 at a Float or as a Vector element is written as the Float
// stores it, a negative zero, so Load reads back the value the validator stores
// for the literal.
func TestMarshal_ANegativeZeroLiteralKeepsItsSignAtAFloat(t *testing.T) {
	t.Parallel()
	s, res := schema.LoadString(t.Context(), "schema \"nz\"\n\ntype Holder {\n\tid String primary\n\tf Float\n\tv Vector[2]\n}\n", "nz.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res)
	}
	stored, err := instance.CanonicalValue(json.Number("-0"), schema.NewFloatConstraint())
	if f, ok := stored.(float64); err != nil || !ok || !math.Signbit(f) {
		t.Fatalf("the validator stores -0 at a Float as %#v, %v; want a negative zero", stored, err)
	}
	holderT, _ := s.Type("Holder")
	g := graph.New(s)
	if r := g.Add(t.Context(), instancetest.VI("Holder", instancetest.TypeID(holderT.ID()), instancetest.PK("h1"),
		instancetest.Props(map[string]any{"id": "h1", "f": json.Number("-0"), "v": []any{json.Number("-0"), json.Number("1")}}))); r.HasErrors() {
		t.Fatalf("Add: %s", r)
	}
	data, mres := snapshot.Marshal(t.Context(), g.Snapshot())
	if mres.HasErrors() {
		t.Fatalf("Marshal: %s", mres)
	}
	back, lres := snapshot.Load(t.Context(), data, s)
	if lres.HasErrors() {
		t.Fatalf("Load: %s", lres)
	}
	inst, ok := back.InstanceByKey(holderT.ID(), `["h1"]`)
	if !ok {
		t.Fatal("Load: no Holder h1")
	}
	if p, _ := inst.Property("f"); !negativeZero(p.Unwrap()) {
		t.Errorf("f read back as %#v from %s", p.Unwrap(), data)
	}
	p, _ := inst.Property("v")
	switch v := p.Clone().(type) {
	case []any:
		if len(v) != 2 || !negativeZero(v[0]) {
			t.Errorf("v read back as %#v from %s", v, data)
		}
	case []float64:
		if len(v) != 2 || !negativeZero(v[0]) {
			t.Errorf("v read back as %#v from %s", v, data)
		}
	default:
		t.Errorf("v read back as %#v from %s", v, data)
	}
}

func negativeZero(v any) bool {
	f, ok := v.(float64)
	return ok && f == 0 && math.Signbit(f)
}
