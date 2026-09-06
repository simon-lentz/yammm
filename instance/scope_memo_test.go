package instance

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/schema"
)

// T13 (A-315): one scope per instance, built once and shared upward. A chain
// of composed instances with an invariant at every level costs linear work
// in its depth, because a parent's scope holds each child's memoised scope
// rather than rebuilding the whole subtree per level. Allocations stand in
// for time: they are deterministic, and the quadratic shape shows in them.
func TestScope_IsMemoisedAndLinearInDepth(t *testing.T) {
	const n = 6
	allocs := func(depth int) float64 {
		s, raw := chainSchemaAndInstance(t, depth)
		v := NewValidator(s)
		if _, res := v.ValidateOne(t.Context(), "L0", RawInstance{Properties: raw}); res.Err() != nil {
			t.Fatalf("depth %d: %v", depth, res.Err())
		}
		return testing.AllocsPerRun(20, func() {
			_, _ = v.ValidateOne(t.Context(), "L0", RawInstance{Properties: raw})
		})
	}
	a, b := allocs(n), allocs(2*n)
	if ratio := b / a; ratio > 3 {
		t.Errorf("allocations at depth %d are %.0f, at depth %d %.0f: ratio %.2f, want linear (about 2)", n, a, 2*n, b, ratio)
	}
}

// The parent's scope holds the child's memo itself, not a copy: the entry
// under the composition's field name and the child's own memo share one
// entry map.
func TestScope_ParentHoldsTheChildsMemo(t *testing.T) {
	s, raw := chainSchemaAndInstance(t, 2)
	v := NewValidator(s)
	root, res := v.ValidateOne(t.Context(), "L0", RawInstance{Properties: raw})
	if res.Err() != nil {
		t.Fatal(res.Err())
	}
	var child *ValidInstance
	for name, val := range root.Compositions() {
		if name == "NEXT" {
			child = composedChildren(val.Unwrap())[0]
		}
	}
	if child == nil {
		t.Fatal("no NEXT composition on the root")
	}
	childMemo := v.scopeOf(child)
	entry, ok := v.scopeOf(root).Get("next")
	if !ok {
		t.Fatal("the root's scope has no entry under the composition's field name")
	}
	parentHeld := entry.Unwrap()
	if entriesPointer(parentHeld) != entriesPointer(childMemo) {
		t.Error("the parent's scope holds a copy of the child's scope, not its memo")
	}
	if entriesPointer(v.scopeOf(child)) != entriesPointer(childMemo) {
		t.Error("scopeOf built the child's scope twice")
	}
}

func entriesPointer(m any) uintptr {
	rv := reflect.ValueOf(m)
	if rv.Kind() != reflect.Struct {
		return 0
	}
	return rv.FieldByName("entries").Pointer()
}

// chainSchemaAndInstance builds L0 -> NEXT (one) L1 -> ... -> L<depth>, each
// level with an invariant that reads its child, and one instance of it.
func chainSchemaAndInstance(t *testing.T, depth int) (*schema.Schema, map[string]any) {
	t.Helper()
	var sb strings.Builder
	sb.WriteString("schema \"chain\"\n\n")
	for i := depth; i >= 0; i-- {
		kind := "part type"
		if i == 0 {
			kind = "type"
		}
		fmt.Fprintf(&sb, "%s L%d {\n\tid String primary\n\tn Integer\n", kind, i)
		if i < depth {
			fmt.Fprintf(&sb, "\t*-> NEXT (one) L%d\n\t! \"m\" NEXT.n >= 0\n", i+1)
		}
		sb.WriteString("}\n\n")
	}
	s, res := schema.LoadString(t.Context(), sb.String(), "chain.yammm")
	if res.Err() != nil {
		t.Fatal(res.Err())
	}
	var build func(i int) map[string]any
	build = func(i int) map[string]any {
		m := map[string]any{"id": fmt.Sprintf("k%d", i), "n": int64(i)}
		if i < depth {
			m["next"] = []any{build(i + 1)}
		}
		return m
	}
	return s, build(0)
}
