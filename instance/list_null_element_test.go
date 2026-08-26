package instance_test

import (
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
)

func listElemSchema(t *testing.T) *schema.Schema {
	t.Helper()
	s, result := schema.LoadString(t.Context(), `schema "test"
type T {
    id String primary
    tags List<String>
    counts List<Integer>
}`, "test.yammm")
	if result.HasErrors() {
		t.Fatalf("schema: %s", result)
	}
	return s
}

// validateT validates one T and returns the instance and the rendered result.
func validateT(t *testing.T, s *schema.Schema, props map[string]any) (*instance.ValidInstance, string) {
	t.Helper()
	valid, result := instance.NewValidator(s).ValidateOne(t.Context(), "T", instance.RawInstance{Properties: props})
	return valid, result.String()
}

// A null list element is rejected. CheckValue short-circuits nil before any
// constraint runs, which is correct for an optional property and wrong for an
// element: no element position is optional, so ["a", null] validated and the
// resulting instance held a nil inside its list.
//
// Mutation: removing BOTH guards — checkList's and coerceList's — turns this
// red. Either one alone still rejects through the validator, because check runs
// before coerce; the two are separately pinned in the eval package, where each
// is the only defence for its own entry point.
func TestListNullElement_IsRejected(t *testing.T) {
	s := listElemSchema(t)

	for _, tc := range []struct {
		name  string
		props map[string]any
		want  string
	}{
		{"null among strings", map[string]any{"id": "1", "tags": []any{"a", nil}}, "expected string, got null"},
		{"null among integers", map[string]any{"id": "1", "counts": []any{int64(1), nil}}, "expected integer, got null"},
		{"null as the only element", map[string]any{"id": "1", "tags": []any{nil}}, "expected string, got null"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			valid, detail := validateT(t, s, tc.props)
			if valid != nil {
				t.Error("a null list element validated")
			}
			if !strings.Contains(detail, tc.want) {
				t.Errorf("diagnostic = %s, want it to name %q", detail, tc.want)
			}
		})
	}
}

// The diagnostic names the offending index, as every other element-position
// error in this package does.
func TestListNullElement_NamesTheIndex(t *testing.T) {
	_, detail := validateT(t, listElemSchema(t),
		map[string]any{"id": "1", "tags": []any{"a", "b", nil}})

	if !strings.Contains(detail, "element [2]") {
		t.Errorf("diagnostic does not name the index: %s", detail)
	}
}

// A whole-value null is still the optional-property case: the property is
// dropped, not rejected. The element rule must not swallow this distinction.
func TestListNullElement_WholeValueNullIsStillOptional(t *testing.T) {
	valid, detail := validateT(t, listElemSchema(t), map[string]any{"id": "1", "tags": nil})

	if valid == nil {
		t.Fatalf("an optional List set to null should validate: %s", detail)
	}
	if _, present := valid.Property("tags"); present {
		t.Error("an optional List set to null was stored rather than dropped")
	}
}

// An empty list is not a null element and stays valid.
func TestListNullElement_EmptyListIsValid(t *testing.T) {
	if valid, detail := validateT(t, listElemSchema(t),
		map[string]any{"id": "1", "tags": []any{}}); valid == nil {
		t.Errorf("an empty list should validate: %s", detail)
	}
}

// Vector already rejected a null element inline; the two collection kinds now
// answer the same way, which is the point of the rule.
func TestListNullElement_VectorAgreesWithList(t *testing.T) {
	s, result := schema.LoadString(t.Context(), `schema "test"
type T {
    id String primary
    v Vector[2]
}`, "test.yammm")
	if result.HasErrors() {
		t.Fatalf("schema: %s", result)
	}

	valid, detail := validateT(t, s, map[string]any{"id": "1", "v": []any{1.0, nil}})
	if valid != nil {
		t.Fatal("a null Vector element validated")
	}
	if !strings.Contains(detail, "element [1]") {
		t.Errorf("the Vector diagnostic does not name the index: %s", detail)
	}
}
