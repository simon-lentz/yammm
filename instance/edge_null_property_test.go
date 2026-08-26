package instance_test

import (
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
)

// nullEdgePropSchema declares one required and one optional edge property so a
// single fixture covers both halves of the null rule.
func nullEdgePropSchema(t *testing.T) *schema.Schema {
	t.Helper()
	s, result := schema.LoadString(t.Context(), `schema "test"
type Company {
    id String primary
}
type Person {
    id String primary
    --> EMPLOYER (_) Company {
        role String required
        note String
    }
}`, "test.yammm")
	if result.HasErrors() {
		t.Fatalf("schema: %s", result)
	}
	return s
}

// validateEmployer validates one Person carrying the given edge object.
func validateEmployer(t *testing.T, s *schema.Schema, employer map[string]any) (*instance.ValidInstance, string) {
	t.Helper()
	valid, result := instance.NewValidator(s).ValidateOne(t.Context(), "Person", instance.RawInstance{
		Properties: map[string]any{"id": "1", "employer": employer},
	})
	return valid, result.String()
}

// An explicit null does not satisfy a required edge property. The required
// check tested key presence while CheckValue and CoerceValue both accept nil,
// so writing the key with a nil value was enough to pass it — the one slot of
// the four where null was ACCEPTED rather than rejected or dropped.
//
// Mutation: removing the `fieldVal == nil` skip in validateEdgeTarget's
// property loop turns this red; the instance validates with a nil role.
func TestEdgeNullProperty_ExplicitNullDoesNotSatisfyRequired(t *testing.T) {
	valid, detail := validateEmployer(t, nullEdgePropSchema(t),
		map[string]any{"_target_id": "42", "role": nil})

	if valid != nil {
		t.Error("an explicit null satisfied a required edge property")
	}
	if !strings.Contains(detail, "missing required edge property") {
		t.Errorf("wrong diagnostic: %s", detail)
	}
}

// An explicit null and an absent key report identically, which is what "one
// rule for the slot" means.
func TestEdgeNullProperty_NullAndAbsentAgree(t *testing.T) {
	s := nullEdgePropSchema(t)

	_, withNull := validateEmployer(t, s, map[string]any{"_target_id": "42", "role": nil})
	_, withoutKey := validateEmployer(t, s, map[string]any{"_target_id": "42"})

	if withNull != withoutKey {
		t.Errorf("an explicit null and an absent key report differently:\n null: %s\n absent: %s", withNull, withoutKey)
	}
}

// An optional edge property set to null is dropped, not stored. Storing it
// would put a nil into the validated instance's properties, where every reader
// expects a value it can use.
func TestEdgeNullProperty_OptionalNullIsDropped(t *testing.T) {
	valid, detail := validateEmployer(t, nullEdgePropSchema(t),
		map[string]any{"_target_id": "42", "role": "engineer", "note": nil})

	if valid == nil {
		t.Fatalf("an optional null should validate: %s", detail)
	}
	edge, ok := valid.Edge("EMPLOYER")
	if !ok {
		t.Fatal("the EMPLOYER edge is missing")
	}
	if len(edge.Targets()) != 1 {
		t.Fatalf("got %d targets, want 1", len(edge.Targets()))
	}
	if _, present := edge.Targets()[0].Properties().Get("note"); present {
		t.Error("an optional edge property set to null was stored rather than dropped")
	}
}

// The FK slot keeps its own rule. A null FK component is a type mismatch, not a
// missing property: it is a structural key position, and the distinction is
// pinned by this package's goldens.
func TestEdgeNullProperty_FKSlotRuleIsUnchanged(t *testing.T) {
	valid, detail := validateEmployer(t, nullEdgePropSchema(t),
		map[string]any{"_target_id": nil, "role": "engineer"})

	if valid != nil {
		t.Fatal("a null FK component validated")
	}
	if !strings.Contains(detail, "got null") {
		t.Errorf("the FK slot no longer reports a type mismatch: %s", detail)
	}
	if strings.Contains(detail, "missing required edge property") {
		t.Errorf("the FK slot was folded into the required-property rule: %s", detail)
	}
}
