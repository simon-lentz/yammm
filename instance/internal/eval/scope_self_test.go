package eval_test

import (
	"testing"

	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance/internal/eval"
	"github.com/simon-lentz/yammm/schema/expr"
)

// PropertyScopeFromMap wraps the members once: $self reads the same wrapped
// values the property lookup reads, and building the scope costs the one
// wrap plus the scope's own bookkeeping — not a second recursive wrap.
func TestPropertyScopeFromMap_BindsSelfWithoutRewrapping(t *testing.T) {
	// Not parallel: AllocsPerRun refuses to run inside a parallel test.
	m := map[string]any{"x": int64(1), "nested": map[string]any{"y": "z"}, "xs": []any{int64(1), int64(2)}}
	scope := eval.PropertyScopeFromMap(m)

	self, ok := scope.Lookup("self")
	if !ok {
		t.Fatal("$self is not bound")
	}
	sm, ok := self.Map()
	if !ok {
		t.Fatalf("$self is %T, want a map", self.Unwrap())
	}
	// A wrapped map is not comparable, so sharing is read through a scalar
	// member: the same entries answer both lookups.
	viaSelf, _ := sm.Get("x")
	viaProp, _ := scope.LookupFold("x")
	if viaSelf != viaProp {
		t.Error("$self.x and the property lookup do not share one wrapped value")
	}
	nestedSelf, _ := sm.Get("nested")
	nestedProp, _ := scope.LookupFold("nested")
	ns, _ := nestedSelf.Map()
	np, _ := nestedProp.Map()
	ys, _ := ns.Get("y")
	yp, _ := np.Get("y")
	if ys != yp || ys.Unwrap() != "z" {
		t.Error("$self.nested.y and the property lookup do not share one wrapped value")
	}

	ev := eval.NewEvaluator()
	got, err := ev.Evaluate(expr.SExpr{expr.Op("."), expr.SExpr{expr.Op("$"), expr.NewLiteral("self")}, expr.NewLiteral("x")}, scope)
	if err != nil || got != int64(1) {
		t.Errorf("$self.x = %v, %v; want 1", got, err)
	}

	// On a 40-property instance the one wrap is ~405 allocations and the
	// scope's bookkeeping (sorted keys, folded index, the self binding, the
	// struct) under 20; a second recursive wrap adds ~400 again. The old
	// construction — WrapProperties beside Wrap of the raw map — is the
	// figure this must beat.
	big := map[string]any{}
	for i := range 40 {
		big["p"+string(rune('a'+i%26))+string(rune('a'+i/26))] = map[string]any{"x": int64(i), "y": "s", "z": []any{int64(1), int64(2), int64(3)}}
	}
	oneWrap := testing.AllocsPerRun(20, func() { _ = immutable.WrapMap(big, immutable.WithClone(true)) })
	twoWraps := testing.AllocsPerRun(20, func() {
		_ = immutable.WrapProperties(big, immutable.WithClone(true))
		_ = immutable.Wrap(big)
	})
	scoped := testing.AllocsPerRun(20, func() { _ = eval.PropertyScopeFromMap(big) })
	if scoped > oneWrap+20 || scoped >= twoWraps {
		t.Errorf("PropertyScopeFromMap allocates %v; one wrap is %v and the old two-wrap construction %v", scoped, oneWrap, twoWraps)
	}
}
