package instance_test

import (
	"maps"
	"testing"

	"github.com/simon-lentz/yammm/instance"
)

const refusedSchema = `schema "rv"
type Company {
	cid String primary
	d Date primary
}
part type Line {
	sku String primary
	n Integer
}
type Person {
	id String primary
	born Date required
	nick String
	--> WORKS_AT (_:one) Company { since Integer required }
	--> KNOWS (_:many) Company { w Integer }
	*-> LINES (_:many) Line
}
`

func refusedPerson(extra map[string]any) map[string]any {
	props := map[string]any{"id": "p1", "born": "2020-01-01"}
	maps.Copy(props, extra)
	return props
}

func company(extra map[string]any) map[string]any {
	obj := map[string]any{"_target_cid": "c1", "_target_d": "2020-01-01", "since": 5}
	maps.Copy(obj, extra)
	return obj
}

// A Refused value at a property, a foreign-key field, an edge property of a
// (one) or a (many) association, or a composed child's property refuses the
// instance and draws no issue: its producer reported it.
func TestValidateOne_ARefusedValueRefusesTheInstanceAndDrawsNoIssue(t *testing.T) {
	t.Parallel()
	v := instance.NewValidator(loadSrc(t, refusedSchema))
	refused := instance.Refused{Value: "xyz"}
	for name, props := range map[string]map[string]any{
		"a required property":      refusedPerson(map[string]any{"born": refused}),
		"an optional property":     refusedPerson(map[string]any{"nick": refused}),
		"a foreign-key field":      refusedPerson(map[string]any{"works_at": company(map[string]any{"_target_d": refused})}),
		"a required edge property": refusedPerson(map[string]any{"works_at": company(map[string]any{"since": refused})}),
		"a many edge's second target": refusedPerson(map[string]any{"works_at": company(nil), "knows": []any{
			map[string]any{"_target_cid": "c1", "_target_d": "2020-01-01"},
			map[string]any{"_target_cid": "c2", "_target_d": "2020-01-01", "w": refused},
		}}),
		"a many edge's first target": refusedPerson(map[string]any{"works_at": company(nil), "knows": []any{
			map[string]any{"_target_cid": "c1", "_target_d": "2020-01-01", "w": refused},
			map[string]any{"_target_cid": "c2", "_target_d": "2020-01-01"},
		}}),
		"a foreign-key field before another": refusedPerson(map[string]any{"works_at": company(map[string]any{"_target_cid": refused})}),
		"a composed child's property":        refusedPerson(map[string]any{"works_at": company(nil), "lines": []any{map[string]any{"sku": "a"}, map[string]any{"sku": "b", "n": refused}}}),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			valid, res := v.ValidateOne(t.Context(), "Person", instance.RawInstance{Properties: props})
			if valid != nil || res.Len() != 0 {
				t.Errorf("instance %v, %s; want nil and no issue", valid, res)
			}
		})
	}
}

// The batch entries hold a refused instance as nil beside the others, and a
// refused composed child is nil in ValidateForComposition's result, with no
// issue for either.
func TestValidate_ARefusedInstanceIsANilEntryWithNoIssue(t *testing.T) {
	t.Parallel()
	v := instance.NewValidator(loadSrc(t, refusedSchema))
	ok := refusedPerson(map[string]any{"works_at": company(nil)})
	valids, res := v.Validate(t.Context(), "Person", []instance.RawInstance{
		{Properties: ok},
		{Properties: refusedPerson(map[string]any{"nick": instance.Refused{Value: 7}})},
	})
	if len(valids) != 2 || valids[0] == nil || valids[1] != nil || res.Len() != 0 {
		t.Errorf("valids %v, %s; want the second entry nil and no issue", valids, res)
	}
	children, res := v.ValidateForComposition(t.Context(), "Person", "LINES", []instance.RawInstance{
		{Properties: map[string]any{"sku": "a"}},
		{Properties: map[string]any{"sku": instance.Refused{Value: "b"}}},
	})
	if len(children) != 2 || children[0] == nil || children[1] != nil || res.Len() != 0 {
		t.Errorf("children %v, %s; want the second nil and no issue", children, res)
	}
}

// A refused value gates the passes after its own as a member error does, and
// the other members of its pass are still judged.
func TestValidateOne_ARefusedValueGatesAsAMemberError(t *testing.T) {
	t.Parallel()
	v := instance.NewValidator(loadSrc(t, refusedSchema))
	_, res := v.ValidateOne(t.Context(), "Person", instance.RawInstance{Properties: map[string]any{
		"id": "p1", "nick": instance.Refused{Value: "x"}, "works_at": map[string]any{"_target_cid": "c1"},
	}})
	if res.Len() != 1 || !res.HasCode(instance.ErrMissingRequired) {
		t.Errorf("%s; want the missing born property alone, the edge pass gated", res)
	}
}

// A refused property leaves the properties after it in its pass judged.
func TestValidateOne_ARefusedValueLeavesTheRestOfItsPassJudged(t *testing.T) {
	t.Parallel()
	v := instance.NewValidator(loadSrc(t, refusedSchema))
	_, res := v.ValidateOne(t.Context(), "Person", instance.RawInstance{Properties: map[string]any{
		"id": "p1", "born": instance.Refused{Value: "x"}, "nick": 5,
	}})
	if res.Len() != 1 || !res.HasCode(instance.ErrTypeMismatch) {
		t.Errorf("%s; want the nick property's type mismatch alone", res)
	}
}

// A Refused value is judged only where it stands for a member's whole value.
// Inside a list it is a value of no form, and a pointer to one is no Refused.
func TestValidateOne_ARefusedValueElsewhereIsAValueOfNoForm(t *testing.T) {
	t.Parallel()
	v := instance.NewValidator(loadSrc(t, `schema "nf"
type P { id String primary
	tags List<String> }
`))
	for name, val := range map[string]any{
		"a list element": []any{"a", instance.Refused{Value: "b"}},
		"a pointer":      &instance.Refused{Value: "b"},
	} {
		_, res := v.ValidateOne(t.Context(), "P", instance.RawInstance{Properties: map[string]any{"id": "p", "tags": val}})
		if !res.HasCode(instance.ErrTypeMismatch) {
			t.Errorf("%s: %s; want a type mismatch", name, res)
		}
	}
}

// A refused key component has no primary key to report.
func TestPrimaryKeyOf_ARefusedComponentHasNoKey(t *testing.T) {
	t.Parallel()
	v := instance.NewValidator(loadSrc(t, refusedSchema))
	if _, _, ok := v.PrimaryKeyOf("Person", instance.RawInstance{Properties: map[string]any{"id": instance.Refused{Value: "p"}}}); ok {
		t.Error("a refused key component reported a key")
	}
}

// ErrRefusedValue's text names its package.
func TestErrRefusedValue_TextNamesThePackage(t *testing.T) {
	t.Parallel()
	if got, want := instance.ErrRefusedValue.Error(), "instance: the instance holds a refused value"; got != want {
		t.Errorf("%q, want %q", got, want)
	}
}
