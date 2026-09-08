package graph_test

import (
	"testing"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
)

// TestFileLoadedSchema_NestedOneUnderAManyIsRefusedASecondOccupant pins the
// (one) slot's premise where it is nested rather than direct: a (one) part
// composed under a keyed (_:many) part, under a Timestamp-keyed root whose
// spelling canonicalizes. The corpus reached a (one) slot only at depth one,
// so the shape whose composed-key segment carries no discriminator below the
// first hop was in no fixture.
func TestFileLoadedSchema_NestedOneUnderAManyIsRefusedASecondOccupant(t *testing.T) {
	t.Parallel()
	s := loadFromDisk(t)
	runID := mustTypeID(t, s, "Run")

	mark := func(m string) *instance.ValidInstance {
		return instance.NewValidInstance("Mark", mustTypeID(t, s, "Mark"),
			immutable.WrapKey(nil), immutable.WrapProperties(map[string]any{"mark": m}), nil, nil, nil)
	}
	summary := func(label string, marks ...*instance.ValidInstance) *instance.ValidInstance {
		vals := make([]any, len(marks))
		for i, m := range marks {
			vals[i] = m
		}
		return instance.NewValidInstance("StepSummary", mustTypeID(t, s, "StepSummary"),
			immutable.WrapKey(nil), immutable.WrapProperties(map[string]any{"label": label}), nil,
			map[string]immutable.Value{"MARKS": immutable.Wrap(vals)}, nil)
	}
	step := func(id string, summaries ...*instance.ValidInstance) *instance.ValidInstance {
		vals := make([]any, len(summaries))
		for i, sm := range summaries {
			vals[i] = sm
		}
		return instance.NewValidInstance("Step", mustTypeID(t, s, "Step"),
			immutable.WrapKey([]any{id}), immutable.WrapProperties(map[string]any{"step_id": id}), nil,
			map[string]immutable.Value{"SUMMARY": immutable.Wrap(vals)}, nil)
	}
	run := func(steps ...*instance.ValidInstance) *instance.ValidInstance {
		vals := make([]any, len(steps))
		for i, st := range steps {
			vals[i] = st
		}
		return instance.NewValidInstance("Run", runID,
			immutable.WrapKey([]any{rawInstant}), immutable.WrapProperties(map[string]any{"at": rawInstant}), nil,
			map[string]immutable.Value{"STEPS": immutable.Wrap(vals)}, nil)
	}

	// One occupant of the nested (one) slot: accepted, and the root's raw
	// spelling is stored canonical.
	g := graph.New(s)
	if res := g.Add(t.Context(), run(step("s1", summary("L", mark("m1"), mark("m2"))))); !res.OK() {
		t.Fatalf("the nested (one) shape was refused: %s", res)
	}
	if got := g.Snapshot().InstancesOf(runID)[0].PrimaryKey().String(); got != graph.FormatKey(canonInstant) {
		t.Errorf("the root key is %s, want the canonical %s", got, graph.FormatKey(canonInstant))
	}

	// Two occupants of it: refused. Without the nested fixture this shape had
	// nowhere to be exercised below the first hop.
	res := graph.New(s).Add(t.Context(), run(step("s1", summary("L"), summary("M"))))
	if res.OK() {
		t.Fatal("two occupants of a nested (one) composition were accepted")
	}
}
