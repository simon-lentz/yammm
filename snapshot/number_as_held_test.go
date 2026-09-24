package snapshot_test

import (
	"bytes"
	"slices"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
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
	g := graph.New(s)
	inst := instance.NewValidInstance("T", ty.ID(), immutable.WrapKey([]any{"a"}),
		immutable.WrapProperties(map[string]any{"id": "a", "n": float64(6), "ns": []any{int64(1), float64(2)}}),
		nil, nil, nil)
	if r := g.Add(ctx, inst); r.HasErrors() {
		t.Fatalf("add: %s", r)
	}
	data, mres := snapshot.Marshal(ctx, g.Snapshot())
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
