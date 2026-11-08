package snapshot_test

import (
	"bytes"
	"encoding/json"
	"slices"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/snapparts"
	"github.com/simon-lentz/yammm/internal/snappartstest"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

const asHeldSchema = `schema "held"

type T {
	id String primary
	n Integer
	ns List<Integer>
	at List<Timestamp>
}
`

// A float held under an Integer is written with its float indicator and read
// back as the float it is, which revalidation refuses. Without the indicator
// the document would say 6, and the load would hold the integer 6, which the
// snapshot never held.
func TestMarshal_AFloatHeldUnderAnIntegerIsWrittenAsHeld(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	s, res := schema.LoadString(ctx, asHeldSchema, "held.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res)
	}
	ty, _ := s.Type("T")
	snap := snappartstest.Loaded(t, s, snapparts.Parts{Instances: []snapparts.Instance{{
		TypeID: ty.ID(), PrimaryKey: immutable.WrapKey([]any{"a"}),
		Properties: immutable.WrapProperties(map[string]any{"id": "a", "n": float64(6), "ns": []any{int64(1), float64(2)}}),
	}}})
	data, mres := snapshot.Marshal(ctx, snap)
	if mres.HasErrors() {
		t.Fatalf("marshal: %s", mres)
	}
	for _, want := range []string{`"n":6.0`, `"ns":[1,2.0]`} {
		if !bytes.Contains(data, []byte(want)) {
			t.Errorf("the document does not hold %s:\n%s", want, data)
		}
	}

	loaded, lres := snapshot.Load(ctx, data, s)
	if lres.HasErrors() {
		t.Fatalf("load: %s", lres)
	}
	got, ok := loaded.InstanceByKey(ty.ID(), graph.FormatKey("a"))
	if !ok {
		t.Fatal("the instance did not load")
	}
	if n, _ := got.Property("n"); n.Unwrap() != float64(6) {
		t.Errorf("n loaded as %#v, want float64(6)", n.Unwrap())
	}

	_, rres := snapshot.Load(ctx, data, s, snapshot.WithRevalidation(diag.Error))
	if !rres.HasCode(diag.E_TYPE_MISMATCH) {
		t.Errorf("revalidation of a held float at an Integer drew %s, want E_TYPE_MISMATCH", rres)
	}
}

// A number no finite float64 holds stays the literal the document spells: Marshal
// writes it and Load reads it back as that literal.
func TestMarshal_ANumberNoFloatHoldsReadsBackAsTheLiteral(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	s, res := schema.LoadString(ctx, asHeldSchema, "held.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res)
	}
	ty, _ := s.Type("T")
	snap := snappartstest.Loaded(t, s, snapparts.Parts{Instances: []snapparts.Instance{{
		TypeID: ty.ID(), PrimaryKey: immutable.WrapKey([]any{"a"}),
		Properties: immutable.WrapProperties(map[string]any{"id": "a", "n": json.Number("1e400")}),
	}}})
	data, mres := snapshot.Marshal(ctx, snap)
	if mres.HasErrors() {
		t.Fatalf("marshal: %s", mres)
	}
	back, lres := snapshot.Load(ctx, data, s)
	if lres.HasErrors() {
		t.Fatalf("load: %s", lres)
	}
	inst, ok := back.InstanceByKey(ty.ID(), graph.FormatKey("a"))
	if !ok {
		t.Fatal("the instance did not load")
	}
	if n, _ := inst.Property("n"); n.Unwrap() != json.Number("1e400") {
		t.Errorf("1e400 read back as %#v", n.Unwrap())
	}
}

// The value-conformance check reads a property without changing it, so
// revalidation, which reads the same decoded document afterwards, judges what
// the document wrote whether or not conformance ran first.
func TestLoad_ValueConformanceLeavesRevalidationsInputAsWritten(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	s, res := schema.LoadString(ctx, asHeldSchema, "held.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res)
	}
	vi, vr := instance.NewValidator(s).ValidateOne(ctx, "T", instance.RawInstance{
		Properties: map[string]any{"id": "a", "n": int64(1), "at": []any{"2026-01-01T00:00:00Z"}},
	})
	if vr.HasErrors() {
		t.Fatalf("validate: %s", vr)
	}
	g := graph.New(s)
	if r := g.Add(ctx, vi); r.HasErrors() {
		t.Fatalf("add: %s", r)
	}
	clean, mres := snapshot.Marshal(ctx, g.Snapshot())
	if mres.HasErrors() {
		t.Fatalf("marshal: %s", mres)
	}
	const from = `"at":["2026-01-01T00:00:00Z"]`
	if !bytes.Contains(clean, []byte(from)) {
		t.Fatalf("the document does not hold %s:\n%s", from, clean)
	}
	data := bytes.Replace(clean, []byte(from), []byte(`"at":[5]`), 1)

	revalidation := func(opts ...snapshot.LoadOption) []string {
		opts = append(opts, snapshot.WithIntegrityCheck(false), snapshot.WithRevalidation(diag.Error))
		_, lres := snapshot.Load(ctx, data, s, opts...)
		var msgs []string
		for issue := range lres.Issues() {
			if issue.Code() == diag.E_TYPE_MISMATCH {
				msgs = append(msgs, issue.Message())
			}
		}
		return msgs
	}
	alone := revalidation()
	after := revalidation(snapshot.WithValueConformance(true))
	if len(alone) == 0 || !slices.Equal(alone, after) {
		t.Errorf("revalidation alone reported %q and after value conformance %q; want one report, the same", alone, after)
	}
}

const everyHeldSchema = `schema "held"

type T {
	id String primary
	n Integer
	s String
	ts Timestamp
	d Date
	u UUID
	tss List<Timestamp>
	v Vector[2]
	l List<Integer>
	f Float
}
`

// A float is written with its float indicator in every position that holds one,
// whatever kind the position declares, so the document never spells an integer
// the snapshot did not hold.
func TestMarshal_MarksEveryHeldFloat(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	s, res := schema.LoadString(ctx, everyHeldSchema, "held.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res)
	}
	ty, _ := s.Type("T")
	for _, c := range []struct {
		prop string
		held any
		want string
	}{
		{"n", float64(0.1), `"n":0.1}`},
		{"n", float64(6), `"n":6.0`},
		{"s", float64(6), `"s":6.0`},
		{"ts", float64(6), `"ts":6.0`},
		{"d", float64(6), `"d":6.0`},
		{"u", float64(6), `"u":6.0`},
		{"tss", []any{float64(6)}, `"tss":[6.0]`},
		{"v", float64(7), `"v":7.0`},
		{"l", float64(8), `"l":8.0`},
		{"n", []any{float64(6)}, `"n":[6.0]`},
		{"n", map[string]any{"k": float64(6)}, `"n":{"k":6.0}`},
		{"ts", []any{float64(6)}, `"ts":[6.0]`},
		{"f", []any{float64(6)}, `"f":[6.0]`},
		{"v", []any{[]any{float64(6)}, float64(1)}, `"v":[[6.0],1.0]`},
	} {
		snap := snappartstest.Loaded(t, s, snapparts.Parts{Instances: []snapparts.Instance{{
			TypeID: ty.ID(), PrimaryKey: immutable.WrapKey([]any{"a"}),
			Properties: immutable.WrapProperties(map[string]any{"id": "a", c.prop: c.held}),
		}}})
		data, mres := snapshot.Marshal(ctx, snap)
		if mres.HasErrors() || !bytes.Contains(data, []byte(c.want)) {
			t.Errorf("%s=%#v wrote %s (%s), want %s", c.prop, c.held, data, mres, c.want)
		}
	}
}
