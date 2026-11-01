package instance_test

import (
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/location/path"
)

func TestValidate_TwoFailedComposedChildrenAreNoDuplicateKey(t *testing.T) {
	t.Parallel()
	s := loadT(t, "schema \"p\"\n\npart type Kid {\n\tid String primary\n\tqty Integer\n}\n\ntype T {\n\tid String primary\n\t*-> KIDS (many) Kid\n}\n")
	_, res := instance.NewValidator(s).ValidateOne(t.Context(), "T", instance.RawInstance{Properties: map[string]any{
		"id": "t",
		"kids": []any{
			map[string]any{"id": "a", "qty": "not a number"},
			map[string]any{"id": "b", "qty": "not a number"},
		},
	}})
	if res.OK() {
		t.Fatal("two children that fail must fail the root")
	}
	if res.HasCode(instance.ErrDuplicateComposedPK) {
		t.Errorf("two failed children read as a duplicate key: %s", res)
	}
}

func TestValidate_AnEmptyBatchsRefusalNamesNoSource(t *testing.T) {
	t.Parallel()
	s := loadT(t, "schema \"p\"\n\nabstract type A {\n\tid String primary\n}\n")
	_, res := instance.NewValidator(s).Validate(t.Context(), "A", nil)
	if !res.HasCode(diag.E_ABSTRACT_TYPE) {
		t.Fatalf("codes = %s", res)
	}
	for issue := range res.Issues() {
		if issue.SourceName() != "" {
			t.Errorf("SourceName = %q, want none", issue.SourceName())
		}
	}
	_, res = instance.NewValidator(s).Validate(t.Context(), "A", []instance.RawInstance{{
		Properties: map[string]any{"id": "a"},
		Provenance: location.NewProvenance("rows.json", path.Root(), location.Span{}),
	}})
	for issue := range res.Issues() {
		if issue.SourceName() != "rows.json" {
			t.Errorf("a batch with a source: SourceName = %q", issue.SourceName())
		}
	}
}
