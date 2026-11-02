package graph_test

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/instancetest"
	"github.com/simon-lentz/yammm/internal/yammmtest"
	"github.com/simon-lentz/yammm/schema"
)

// TestFileLoadedSchema_NestedOneUnderAManyIsRefusedASecondOccupant pins the
// (one) slot's premise where it is nested rather than direct: a (one) part
// composed under a keyed (_:many) part, under a Timestamp-keyed root whose
// spelling canonicalizes. The corpus reached a (one) slot only at depth one,
// so the shape whose composed-key segment carries no discriminator below the
// first hop was in no fixture.
func TestFileLoadedSchema_NestedOneUnderAManyIsRefusedASecondOccupant(t *testing.T) {
	t.Parallel()
	yammmtest.RequireNoModuleRoot(t, schema.FindModuleRoot)
	file := filepath.Join(t.TempDir(), "composed.yammm")
	s := loadFromDiskAt(t, file)
	runID := mustTypeID(t, s, "Run")

	run := func(summaries ...any) instance.ValidInstance {
		return instancetest.Valid(t, s, "Run", map[string]any{
			"at":    rawInstant,
			"steps": []any{map[string]any{"step_id": "s1", "summary": summaries}},
		})
	}
	marks := []any{map[string]any{"mark": "m1"}, map[string]any{"mark": "m2"}}

	// One occupant of the nested (one) slot: accepted, and the root's raw
	// spelling is stored canonical.
	g := graph.New(s)
	if res := g.Add(t.Context(), run(map[string]any{"label": "L", "marks": marks})); !res.OK() {
		t.Fatalf("the nested (one) shape was refused: %s", res)
	}
	if got := slices.Collect(g.Snapshot().InstancesOf(runID))[0].PrimaryKey().String(); got != graph.FormatKey(canonInstant) {
		t.Errorf("the root key is %s, want the canonical %s", got, graph.FormatKey(canonInstant))
	}

	// A second occupant of the nested (one) slot draws the validator's refusal.
	_, res := instance.NewValidator(s).ValidateOne(t.Context(), "Run", instance.RawInstance{Properties: map[string]any{
		"at": rawInstant,
		"steps": []any{map[string]any{"step_id": "s1", "summary": []any{
			map[string]any{"label": "L", "marks": marks},
			map[string]any{"label": "M", "marks": marks},
		}}},
	}})
	assertHasCode(t, res, diag.E_DUPLICATE_COMPOSED_PK)
}
