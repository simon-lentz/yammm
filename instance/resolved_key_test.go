package instance_test

import (
	"slices"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/value"
	"github.com/simon-lentz/yammm/schema"
)

const resolvedKeySchema = `schema "resolved"

type N {
	id String[1, 4] primary
	at Timestamp primary
}

type T {
	id String primary
	--> TO_N (_:many) N {
		w Integer[0, 5]
	}
}
`

// A foreign-key component the reader resolved at its address is the key's
// component as given: the validator judges it no further, refuses no target
// for it, and still judges the edge's own properties.
func TestValidate_AResolvedForeignKeyIsTheKeysComponentUnjudged(t *testing.T) {
	t.Parallel()
	s, res := schema.LoadString(t.Context(), resolvedKeySchema, "resolved.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res)
	}
	v := instance.NewValidator(s)
	target := func(w any) map[string]any {
		return map[string]any{
			"_target_id": value.Resolved{Value: "toolong"},
			"_target_at": value.Resolved{Value: "not a time"},
			"w":          w,
		}
	}

	vi, vr := v.ValidateOne(t.Context(), "T", instance.RawInstance{Properties: map[string]any{
		"id": "t1", "to_n": []any{target(int64(1))},
	}})
	if vr.HasErrors() {
		t.Fatalf("a resolved key was judged: %s", vr)
	}
	edge, ok := vi.Edge("TO_N")
	if !ok {
		t.Fatal("no edge under TO_N")
	}
	targets := slices.Collect(edge.Targets())
	if len(targets) != 1 {
		t.Fatalf("targets = %v, want one", targets)
	}
	if got, want := targets[0].TargetKey().String(), `["toolong","not a time"]`; got != want {
		t.Errorf("target key = %s, want %s", got, want)
	}

	_, vr = v.ValidateOne(t.Context(), "T", instance.RawInstance{Properties: map[string]any{
		"id": "t1", "to_n": []any{target(int64(9))},
	}})
	if n := vr.CodeCounts(diag.Error)[diag.E_CONSTRAINT_FAIL]; n != 1 || len(vr.CodeCounts(diag.Error)) != 1 {
		t.Errorf("an edge property out of its bounds beside a resolved key: %s; want its one E_CONSTRAINT_FAIL", vr)
	}
}
