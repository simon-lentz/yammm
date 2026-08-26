package instance_test

import (
	"math"
	"strings"
	"testing"
)

// A pointer-valued Vector element is checked for finiteness. The element kind
// gate always accepted a pointer, but the NaN/Inf guard read the value through
// an extractor that answered "not a float" for one and skipped the check
// silently, so a *float64 holding NaN passed validation.
//
// Mutation: reverting GetFloat64's reflect fallback turns this red — the guard
// stops firing and the instance validates.
func TestNormalization_PointerVectorElementIsCheckedForFiniteness(t *testing.T) {
	nan := math.NaN()
	ok, detail := invariantHolds(t, "\tv Vector[2]\n", `v -> Len == 2`,
		map[string]any{"v": []any{1.0, &nan}})
	if ok {
		t.Fatal("a *float64 holding NaN passed a Vector constraint")
	}
	if !strings.Contains(detail, "not finite") {
		t.Errorf("rejected, but not for finiteness: %s", detail)
	}
}

// The datatype-test operator and the property checker agree on a pointer. They
// did not: `=~ Integer` classified through a path that dereferenced while the
// property checker extracted through one that did not, so the same value passed
// in an expression and failed as a property.
//
// Mutation: reverting GetInt64's reflect fallback turns BOTH halves red — the
// property check fails first, so the invariant never runs. That is the shape of
// the original defect: the checker gates everything downstream of it.
func TestNormalization_PointerAgreesBetweenOperatorAndChecker(t *testing.T) {
	n := int64(5)
	props := map[string]any{"n": &n}

	if ok, detail := invariantHolds(t, "\tn Integer\n", `n =~ Integer`, props); !ok {
		t.Errorf("`n =~ Integer` rejected a *int64: %s", detail)
	}
	if ok, detail := invariantHolds(t, "\tn Integer\n", `n == 5`, props); !ok {
		t.Errorf("an Integer property rejected a *int64: %s", detail)
	}
}

// == nil and -> IsNil answer the same question about a List property. An
// absent property and an explicit null are nil; a present empty list is not,
// because an empty list is a list.
func TestNormalization_NilIdiomsAgreeOnAList(t *testing.T) {
	var typedNil []any
	for _, tc := range []struct {
		name  string
		props map[string]any
		want  bool
	}{
		{"absent", map[string]any{}, true},
		{"explicit null", map[string]any{"tags": nil}, true},
		{"typed nil slice", map[string]any{"tags": typedNil}, false},
		{"empty slice", map[string]any{"tags": []any{}}, false},
		{"populated", map[string]any{"tags": []any{"a"}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			eq, _ := invariantHolds(t, "\ttags List<String>\n", `tags == nil`, tc.props)
			isNil, _ := invariantHolds(t, "\ttags List<String>\n", `tags -> IsNil`, tc.props)
			if eq != isNil {
				t.Errorf("`== nil` = %v but `-> IsNil` = %v", eq, isNil)
			}
			if eq != tc.want {
				t.Errorf("nil = %v, want %v", eq, tc.want)
			}
		})
	}
}
