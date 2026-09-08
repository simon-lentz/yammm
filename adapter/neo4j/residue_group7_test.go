package neo4j

import (
	"context"
	"testing"
)

// TestBatchNodeQueries_NestedComposedKeysUnderACanonicalRoot pins the composed
// key at depth two and depth three, under a root whose key canonicalizes.
//
// The corpus reached one level before this: a (one) slot directly under a root.
// Three shapes only appear deeper — a keyed (_:many) segment, a (one) segment
// nested under it, and a keyless (_:many) segment under that — and the root's
// canonical rendering is the writer-side second implementation for the rule
// that every address the graph holds is the spelling the instance carries.
func TestBatchNodeQueries_NestedComposedKeysUnderACanonicalRoot(t *testing.T) {
	t.Parallel()
	a, s, v, shape := setupWrite(t, "composed_nested.yammm")
	// The root's key is written in a NON-canonical spelling; every address
	// below must carry the canonical one.
	snap := buildGraphResult(t, s, v, map[string][]map[string]any{
		"Run": {{
			"at": "2020-01-02T03:04:05+00:00",
			"steps": []any{map[string]any{
				"step_id": "s1",
				"summary": []any{map[string]any{
					"label": "L",
					"marks": []any{map[string]any{"mark": "m1"}, map[string]any{"mark": "m2"}},
				}},
			}},
		}},
	})

	queries, err := a.BatchNodeQueries(context.Background(), snap, shape)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, q := range queries {
		if q.Kind != CompositionCreate {
			continue
		}
		for _, row := range q.Params["rows"].([]map[string]any) {
			got = append(got, row["props"].(map[string]any)[composedKeyProp].(string))
		}
	}

	// LITERALS, not a second call to the renderer under test. Depth one carries
	// the keyed child's own key; depth two carries the relation NAME ALONE,
	// because a (one) slot holds exactly one child — the premise the reader and
	// RebuildSnapshot now enforce; depth three indexes, the part being keyless.
	want := []string{
		`["nested_test__Run",["2020-01-02T03:04:05Z"],["STEPS",["s1"]]]`,
		`["nested_test__Run",["2020-01-02T03:04:05Z"],["STEPS",["s1"]],["SUMMARY"]]`,
		`["nested_test__Run",["2020-01-02T03:04:05Z"],["STEPS",["s1"]],["SUMMARY"],["MARKS",0]]`,
		`["nested_test__Run",["2020-01-02T03:04:05Z"],["STEPS",["s1"]],["SUMMARY"],["MARKS",1]]`,
	}
	if len(got) != len(want) {
		t.Fatalf("got %d composed keys, want %d: %v", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("composed key %d = %s\n              want %s", i, got[i], w)
		}
	}
}
