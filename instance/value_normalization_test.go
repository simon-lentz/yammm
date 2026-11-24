package instance_test

import "testing"

// == nil and -> IsNil answer the same question about a List property. An
// absent property, an explicit null and a nil slice are nil; a present empty
// list is not, because an empty list is a list.
func TestNormalization_NilIdiomsAgreeOnAList(t *testing.T) {
	var typedNil []any
	for _, tc := range []struct {
		name  string
		props map[string]any
		want  bool
	}{
		{"absent", map[string]any{}, true},
		{"explicit null", map[string]any{"tags": nil}, true},
		{"typed nil slice", map[string]any{"tags": typedNil}, true},
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
