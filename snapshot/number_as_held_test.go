package snapshot_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/internal/diagtest"
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
	s, err := schema.LoadString(ctx, asHeldSchema, "held.yammm")
	if err != nil {
		t.Fatalf("load schema: %s", err)
	}
	ty, _ := s.Type("T")
	snap := snappartstest.Loaded(t, s, snapparts.Parts{Instances: []snapparts.Instance{{
		TypeID: ty.ID(), PrimaryKey: immutable.WrapKey([]any{"a"}),
		Properties: immutable.WrapProperties(map[string]any{"id": "a", "n": float64(6), "ns": []any{int64(1), float64(2)}}),
	}}})
	data, readErr := snapshot.Marshal(ctx, snap)
	if readErr != nil {
		t.Fatalf("marshal: %s", readErr)
	}
	for _, want := range []string{`"n":6.0`, `"ns":[1,2.0]`} {
		if !bytes.Contains(data, []byte(want)) {
			t.Errorf("the document does not hold %s:\n%s", want, data)
		}
	}

	loaded, readErr2 := snapshot.Unmarshal(ctx, data, s)
	if readErr2 != nil {
		t.Fatalf("load: %s", readErr2)
	}
	got, ok := loaded.InstanceByKey(ty.ID(), graph.FormatKey("a"))
	if !ok {
		t.Fatal("the instance did not load")
	}
	if n, _ := got.Property("n"); n.Any() != float64(6) {
		t.Errorf("n loaded as %#v, want float64(6)", n.Any())
	}

	_, rres := diagtest.Capture(t, func(warn func(diag.Issue)) (graph.Snapshot, error) {
		return snapshot.Unmarshal(ctx, data, s, snapshot.WithRevalidation(diag.Error), snapshot.WithWarnings(warn))
	})
	if !rres.HasCode(diag.E_TYPE_MISMATCH) {
		t.Errorf("revalidation of a held float at an Integer drew %s, want E_TYPE_MISMATCH", rres)
	}
}

// A number no finite float64 holds stays the literal the document spells: Marshal
// writes it and Load reads it back as that literal.
func TestMarshal_ANumberNoFloatHoldsReadsBackAsTheLiteral(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	s, err := schema.LoadString(ctx, asHeldSchema, "held.yammm")
	if err != nil {
		t.Fatalf("load schema: %s", err)
	}
	ty, _ := s.Type("T")
	snap := snappartstest.Loaded(t, s, snapparts.Parts{Instances: []snapparts.Instance{{
		TypeID: ty.ID(), PrimaryKey: immutable.WrapKey([]any{"a"}),
		Properties: immutable.WrapProperties(map[string]any{"id": "a", "n": json.Number("1e400")}),
	}}})
	data, readErr := snapshot.Marshal(ctx, snap)
	if readErr != nil {
		t.Fatalf("marshal: %s", readErr)
	}
	back, readErr2 := snapshot.Unmarshal(ctx, data, s)
	if readErr2 != nil {
		t.Fatalf("load: %s", readErr2)
	}
	inst, ok := back.InstanceByKey(ty.ID(), graph.FormatKey("a"))
	if !ok {
		t.Fatal("the instance did not load")
	}
	if n, _ := inst.Property("n"); n.Any() != json.Number("1e400") {
		t.Errorf("1e400 read back as %#v", n.Any())
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
	s, err := schema.LoadString(ctx, everyHeldSchema, "held.yammm")
	if err != nil {
		t.Fatalf("load schema: %s", err)
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
		data, err := snapshot.Marshal(ctx, snap)
		if err != nil || !bytes.Contains(data, []byte(c.want)) {
			t.Errorf("%s=%#v wrote %s (%s), want %s", c.prop, c.held, data, err, c.want)
		}
	}
}
