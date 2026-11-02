package instance_test

import (
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
)

// A graph does not judge again what a validator of its own load judged, so each
// shape here must stop at the validator, or at the schema load before it.
func TestValidateOne_RefusesWhatAGraphTakesAsJudged(t *testing.T) {
	t.Parallel()
	const (
		keyedT = "type T {\n\tid String primary\n}\n"
		partP  = "part type P {\n\tid String primary\n}\n"
		instP  = "part type P {\n\tid Timestamp primary\n}\n"
	)
	for _, c := range []struct {
		name, src, typeName string
		props               map[string]any
		want                diag.Code
	}{
		{
			"abstract root", "abstract type G {\n\tid String primary\n}\n", "G",
			map[string]any{"id": "g"},
			diag.E_ABSTRACT_TYPE,
		},
		{
			"part root", partP + "type H {\n\tid String primary\n\t*-> KIDS (many) P\n}\n", "P",
			map[string]any{"id": "p"},
			diag.E_PART_TYPE_DIRECT,
		},
		{
			"(one) association with two targets", keyedT + "type H {\n\tid String primary\n\t--> OWNER (one) T\n}\n", "H",
			map[string]any{"id": "h", "owner": []any{map[string]any{"_target_id": "a"}, map[string]any{"_target_id": "b"}}},
			diag.E_EDGE_SHAPE_MISMATCH,
		},
		{
			"(one) composition with two children", partP + "type H {\n\tid String primary\n\t*-> KID (one) P\n}\n", "H",
			map[string]any{"id": "h", "kid": []any{map[string]any{"id": "a"}, map[string]any{"id": "b"}}},
			diag.E_DUPLICATE_COMPOSED_PK,
		},
		{
			"siblings at two spellings of one instant", instP + "type H {\n\tid String primary\n\t*-> KIDS (many) P\n}\n", "H",
			map[string]any{"id": "h", "kids": []any{
				map[string]any{"id": "2024-01-01T00:00:00Z"},
				map[string]any{"id": "2024-01-01T00:00:00.000Z"},
			}},
			diag.E_DUPLICATE_COMPOSED_PK,
		},
		{
			"target key of the wrong arity", "type T {\n\ta String primary\n\tb String primary\n}\n\ntype H {\n\tid String primary\n\t--> OWNER (_:one) T\n}\n", "H",
			map[string]any{"id": "h", "owner": map[string]any{"_target_a": "a"}},
			diag.E_PARTIAL_COMPOSITE_FK,
		},
		{
			"key that is not valid UTF-8", "type H {\n\tid String primary\n}\n", "H",
			map[string]any{"id": "a\xff"},
			diag.E_TYPE_MISMATCH,
		},
		{
			"null key", "type H {\n\tid String primary\n}\n", "H",
			map[string]any{"id": nil},
			diag.E_MISSING_REQUIRED,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			s, res := schema.LoadString(t.Context(), "schema \"x\"\n\n"+c.src, "x.yammm")
			if res.HasErrors() {
				t.Fatalf("load: %s", res)
			}
			valid, res := instance.NewValidator(s).ValidateOne(t.Context(), c.typeName, instance.RawInstance{Properties: c.props})
			if !valid.IsZero() || !res.HasCode(c.want) {
				t.Errorf("ValidateOne: zero = %v, result = %s, want %s", valid.IsZero(), res, c.want)
			}
		})
	}
}

func TestLoad_RefusesACompositionTargetThatIsNoPart(t *testing.T) {
	t.Parallel()
	for name, target := range map[string]string{
		"abstract": "abstract type G {\n\tid String primary\n}\n",
		"concrete": "type G {\n\tid String primary\n}\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, res := schema.LoadString(t.Context(),
				"schema \"x\"\n\n"+target+"\ntype H {\n\tid String primary\n\t*-> KIDS (many) G\n}\n", "x.yammm")
			if !res.HasCode(diag.E_INVALID_COMPOSITION_TARGET) {
				t.Errorf("load = %s, want E_INVALID_COMPOSITION_TARGET", res)
			}
		})
	}
}

// A validator that allows unknown fields stores none of them, so a graph over its
// load meets only declared names.
func TestValidateOne_AllowUnknownFieldsStoresNoUndeclaredName(t *testing.T) {
	t.Parallel()
	s, res := schema.LoadString(t.Context(), "schema \"x\"\n\ntype T {\n\tid String primary\n}\n\ntype H {\n\tid String primary\n\t--> OWNER (_:one) T\n}\n", "x.yammm")
	if res.HasErrors() {
		t.Fatalf("load: %s", res)
	}
	valid, res := instance.NewValidator(s, instance.WithAllowUnknownFields(true)).ValidateOne(t.Context(), "H", instance.RawInstance{Properties: map[string]any{
		"id":    "h",
		"bogus": 1,
		"owner": map[string]any{"_target_id": "a", "bogus": 1},
	}})
	if valid.IsZero() {
		t.Fatalf("ValidateOne: %s", res)
	}
	if _, ok := valid.Property("bogus"); ok {
		t.Error("the instance holds an undeclared property")
	}
	edge, ok := valid.Edge("OWNER")
	if !ok {
		t.Fatal("the instance holds no OWNER edge")
	}
	for target := range edge.Targets() {
		if target.HasProperties() {
			t.Error("the edge target holds an undeclared property")
		}
	}
}
