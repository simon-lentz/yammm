package instance_test

import (
	"errors"
	"maps"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/diagtest"
	"github.com/simon-lentz/yammm/schema"
)

// relationInvariantSrc carries a single and a many association and a many
// composition, all optional, so an instance can omit any and still be valid.
const relationInvariantSrc = `schema "x"

type Company { id String primary }

part type Item {
    sku String primary
    qty Integer
}

type Order {
    id String primary
    --> WORKS_AT (_:one) Company
    --> SUPPLIERS (_:many) Company
    *-> ITEMS (_:many) Item
    ! "probe" %EXPR%
}
`

func relationInvariant(t *testing.T, expr string, props map[string]any) (bool, string) {
	t.Helper()
	src := strings.Replace(relationInvariantSrc, "%EXPR%", expr, 1)
	s, err := schema.LoadString(t.Context(), src, "x.yammm")
	if err != nil {
		t.Fatalf("an invariant naming a relation did not load: %s", err)
	}
	all := map[string]any{"id": "o1"}
	maps.Copy(all, props)
	_, r := diagtest.Issues(t, func() (instance.ValidInstance, error) {
		return instance.NewValidator(s).ValidateOne(t.Context(), "Order",
			instance.RawInstance{Properties: all})
	})
	return diagtest.Err(r) == nil, r.String()
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
// The schema completer admits a relation read by its declared name, so such an
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
		{"association presence", `WORKS_AT != nil`, true},
		{"association key by DSL name", `WORKS_AT == "c1"`, true},
		{"association key by a variable", `$WORKS_AT == "c1"`, true},
		{"composition length", `ITEMS -> Len == 2`, true},
		{"composition length by a variable", `$ITEMS -> Len == 2`, true},
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

// An absent single-valued relation is nil, so the null-guard idiom SPEC
// documents works for it exactly as it does for a property.
func TestInvariantRelationScope_AbsentSingleRelationIsNil(t *testing.T) {
	for _, tc := range []struct {
		name string
		inv  string
	}{
		{"absent association", `WORKS_AT == nil`},
		{"absent association by a variable", `$WORKS_AT == nil`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if ok, detail := relationInvariant(t, tc.inv, map[string]any{}); !ok {
				t.Errorf("%s: %s", tc.inv, detail)
			}
		})
	}
}

// A many relation is a list whether the data left it out or wrote it empty, as
// a graph holds no record of either, so a root rebuilt from the graph reads as
// the record that wrote it.
func TestInvariantRelationScope_AManyRelationIsAList(t *testing.T) {
	for _, data := range []struct {
		name  string
		props map[string]any
	}{
		{"left out", map[string]any{}},
		{"written empty", map[string]any{"suppliers": []any{}, "items": []any{}}},
	} {
		for _, inv := range []string{
			`SUPPLIERS != nil`, `SUPPLIERS -> Len == 0`, `$SUPPLIERS != nil`,
			`ITEMS != nil`, `ITEMS -> Len == 0`, `$ITEMS != nil`,
			`(SUPPLIERS -> IsNil) == false`, `(ITEMS -> IsNil) == false`,
		} {
			t.Run(data.name+"/"+inv, func(t *testing.T) {
				if ok, detail := relationInvariant(t, inv, data.props); !ok {
					t.Errorf("%s: %s", inv, detail)
				}
			})
		}
	}
}

// A many association reads its target keys as Sort orders them, never in the
// order a listing wrote them: a graph holds the targets as a set.
func TestInvariantRelationScope_AManyAssociationReadsKeysAsSortOrdersThem(t *testing.T) {
	for _, ids := range [][]string{{"c2", "c1"}, {"Acme Corp", "Acme"}} {
		var listing []any
		for _, id := range ids {
			listing = append(listing, map[string]any{"_target_id": id})
		}
		props := map[string]any{"suppliers": listing}
		inv := `SUPPLIERS == (SUPPLIERS -> Sort) && SUPPLIERS[0] == "` + ids[1] + `"`
		if ok, detail := relationInvariant(t, inv, props); !ok {
			t.Errorf("listing %v: %s", ids, detail)
		}
	}
}

// An association's target is a REFERENCE: the instance holds the foreign key,
// never the target's row, so reaching for a target property can never read a
// value. The static checker refuses it at load; the evaluator's own refusal
// is pinned by the contract table's refuse rows.
func TestInvariantRelationScope_AssociationTargetPropertiesAreNotAvailable(t *testing.T) {
	src := strings.Replace(relationInvariantSrc, "%EXPR%", `WORKS_AT -> Then |$c| { $c.id != "" }`, 1)
	_, err := schema.LoadString(t.Context(), src, "x.yammm")
	if !errors.Is(err, diag.E_INVALID_INVARIANT) || !strings.Contains(err.Error(), "association") {
		t.Errorf("want E_INVALID_INVARIANT naming the association; got %v", err)
	}
}
