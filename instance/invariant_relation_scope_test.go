package instance_test

import (
	"maps"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
)

// relationInvariantSrc carries one association and one composition, both
// optional, so an instance can omit either and still be valid.
const relationInvariantSrc = `schema "x"

type Company { id String primary }

part type Item {
    sku String primary
    qty Integer
}

type Order {
    id String primary
    --> WORKS_AT (_:one) Company
    *-> ITEMS (_:many) Item
    ! "probe" %EXPR%
}
`

func relationInvariant(t *testing.T, expr string, props map[string]any) (bool, string) {
	t.Helper()
	src := strings.Replace(relationInvariantSrc, "%EXPR%", expr, 1)
	s, res := schema.LoadString(t.Context(), src, "x.yammm")
	if res.HasErrors() {
		t.Fatalf("an invariant naming a relation did not load: %s", res)
	}
	all := map[string]any{"id": "o1"}
	maps.Copy(all, props)
	_, r := instance.NewValidator(s).ValidateOne(t.Context(), "Order",
		instance.RawInstance{Properties: all})
	return r.OK(), r.String()
}

func populatedOrder() map[string]any {
	return map[string]any{
		"works_at": map[string]any{"_target_id": "c1"},
		"items": []any{
			map[string]any{"sku": "s1", "qty": int64(2)},
			map[string]any{"sku": "s2", "qty": int64(3)},
		},
	}
}

// An invariant may name a relation, and it reads the relation's real value.
// buildStaticScope has always admitted relation field names, so such an
// invariant loaded clean — but invariants were evaluated before edges and
// compositions were computed, against a scope holding node properties only. A
// relation therefore read nil, and an invariant over one REJECTED EVERY
// CONFORMING INSTANCE.
//
// Mutation: having invariantScope return props unchanged turns every subtest
// red. Moving the evaluateInvariants block back above the relation passes does
// not even compile — edges and composed do not exist yet, so the ordering this
// fix depends on is enforced by the language rather than by a test.
func TestInvariantRelationScope_ReadsRealValues(t *testing.T) {
	props := populatedOrder()

	for _, tc := range []struct {
		name string
		inv  string
		want bool
	}{
		{"association presence", `works_at != nil`, true},
		{"association key by DSL name", `WORKS_AT == "c1"`, true},
		{"association key by field name", `works_at == "c1"`, true},
		{"composition length", `ITEMS -> Len == 2`, true},
		{"composition length by field name", `items -> Len == 2`, true},
		{"composition is not nil", `ITEMS != nil`, true},
		{"a false claim still fails", `ITEMS -> Len == 99`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ok, detail := relationInvariant(t, tc.inv, props)
			if ok != tc.want {
				t.Errorf("%s: passed = %v, want %v — %s", tc.inv, ok, tc.want, detail)
			}
		})
	}
}

// A composition's children are part of the instance, so a lambda reads their
// own properties. This is the shape the plugin corpus teaches.
func TestInvariantRelationScope_CompositionLambdasReadChildProperties(t *testing.T) {
	props := populatedOrder()

	for _, tc := range []struct {
		name string
		inv  string
		want bool
	}{
		{"All over children", `ITEMS -> All |$i| { $i.qty > 0 }`, true},
		{"Map then Sum", `ITEMS -> Map |$i| { $i.qty } -> Sum == 5`, true},
		{"Filter then Len", `ITEMS -> Filter |$i| { $i.qty > 2 } -> Len == 1`, true},
		{"index then member", `ITEMS[0].qty == 2`, true},
		{"a child rule that fails", `ITEMS -> All |$i| { $i.qty > 2 }`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ok, detail := relationInvariant(t, tc.inv, props)
			if ok != tc.want {
				t.Errorf("%s: passed = %v, want %v — %s", tc.inv, ok, tc.want, detail)
			}
		})
	}
}

// An absent relation is nil, so the null-guard idiom SPEC documents works for a
// relation exactly as it does for a property.
func TestInvariantRelationScope_AbsentRelationIsNil(t *testing.T) {
	for _, tc := range []struct {
		name string
		inv  string
	}{
		{"absent association", `works_at == nil`},
		{"absent composition", `items == nil`},
		{"absent composition has no length", `ITEMS -> Len == 0`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if ok, detail := relationInvariant(t, tc.inv, map[string]any{}); !ok {
				t.Errorf("%s: %s", tc.inv, detail)
			}
		})
	}
}

// An association's target is a REFERENCE: the instance holds the foreign key,
// never the target's row, so reaching for a target property can never read a
// value. The static checker refuses it at load; the evaluator's own refusal
// is pinned by the contract table's refuse rows.
func TestInvariantRelationScope_AssociationTargetPropertiesAreNotAvailable(t *testing.T) {
	src := strings.Replace(relationInvariantSrc, "%EXPR%", `WORKS_AT -> Then |$c| { $c.id != "" }`, 1)
	_, res := schema.LoadString(t.Context(), src, "x.yammm")
	if !res.HasCode(diag.E_INVALID_INVARIANT) || !strings.Contains(res.Err().Error(), "association") {
		t.Errorf("want E_INVALID_INVARIANT naming the association; got %v", res.Err())
	}
}
