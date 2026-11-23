package instance_test

import (
	"context"
	"testing"

	"github.com/simon-lentz/yammm/internal/diagtest"
	"github.com/simon-lentz/yammm/internal/yammmtest"

	"github.com/google/uuid"
	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/instance"
)

// TestValidate_CancellationInsideAComposedBatchIsReportedOnce pins one
// cancellation rule at every point that ends
// work. A cancellation raised INSIDE a composed batch is exactly one E_CONTEXT_CANCELLED on the root row, not one per
// nesting level.
func TestValidate_CancellationInsideAComposedBatchIsReportedOnce(t *testing.T) {
	t.Parallel()
	s := loadT(t, "schema \"p\"\n\npart type Line {\n\tid String primary\n}\n\ntype T {\n\tid String primary\n\t*-> LINES (many) Line\n}\n")
	raws := []instance.RawInstance{
		{Properties: map[string]any{"id": "a", "lines": []any{map[string]any{"id": "l"}}}},
		{Properties: map[string]any{"id": "b", "lines": []any{map[string]any{"id": "l"}}}},
		{Properties: map[string]any{"id": "c", "lines": "not a list of lines"}}, // a validated row would report this
	}
	v := instance.NewValidator(s)
	valids, res := diagtest.Issues(t, func() ([]instance.ValidInstance, error) {
		return v.Validate(checkInSecondRow(t, v, "T", raws), "T", raws)
	})
	if valids != nil {
		t.Errorf("a cancelled batch returned a slice of %d", len(valids))
	}
	if n := countCode(res, diag.E_CONTEXT_CANCELLED); n != 1 {
		t.Fatalf("%d E_CONTEXT_CANCELLED issues, want exactly 1: %v", n, res)
	}
	is := mustIssue(t, res, diag.E_CONTEXT_CANCELLED)
	if idx, ok := detail(is, diag.DetailKeyInstanceIndex); !ok || idx != "1" {
		t.Errorf("instance_index = %q, %v; want the row it stopped on, 1", idx, ok)
	}
	if res.Len() != 1 {
		t.Errorf("%d issues, want the cancellation alone; no further row may be validated: %v", res.Len(), res)
	}
}

// A cancelled row's partial diagnostics are not merged into a result returned
// with a nil slice: the caller is told the batch did not run, and is not also
// handed half of a row's errors.
func TestValidate_ACancelledRowsPartialDiagnosticsAreDropped(t *testing.T) {
	t.Parallel()
	s := loadT(t, "schema \"p\"\n\ntype T {\n\tid String primary\n\tn Integer\n}\n")
	raws := []instance.RawInstance{
		{Properties: map[string]any{"id": "a", "n": int64(1)}},
		{Properties: map[string]any{"id": "b", "n": "not a number"}},
	}
	v := instance.NewValidator(s)
	total := yammmtest.CountChecks(t.Context(), func(ctx context.Context) { _, _ = v.Validate(ctx, "T", raws) })

	for at := range total {
		valids, res := diagtest.Issues(t, func() ([]instance.ValidInstance, error) {
			return v.Validate(yammmtest.NewCheckContext(t.Context(), at+1), "T", raws)
		})
		if valids != nil {
			t.Errorf("check %d: a cancelled batch returned a slice of %d", at+1, len(valids))
		}
		if res.HasCode(diag.E_TYPE_MISMATCH) {
			t.Errorf("check %d: the cancelled row's partial E_TYPE_MISMATCH was merged into a nil-slice result: %v", at+1, codes(res))
		}
		if n := countCode(res, diag.E_CONTEXT_CANCELLED); n != 1 {
			t.Errorf("check %d: %d E_CONTEXT_CANCELLED issues, want exactly 1", at+1, n)
		}
	}
}

// TestListProperty_RefusesAUUIDAsSixteenIntegers pins that the one list
// reader takes slices, not arrays — a fixed-size
// array is how a scalar carrier is spelled in this module, and uuid.UUID is
// [16]byte. A UUID at a List<Integer> property is E_TYPE_MISMATCH.
func TestListProperty_RefusesAUUIDAsSixteenIntegers(t *testing.T) {
	t.Parallel()
	s := loadT(t, "schema \"p\"\n\ntype T {\n\tid String primary\n\tnums List<Integer>\n}\n")
	v := instance.NewValidator(s)
	_, res := diagtest.Issues(t, func() (instance.ValidInstance, error) {
		return v.ValidateOne(t.Context(), "T", instance.RawInstance{Properties: map[string]any{
			"id": "a", "nums": uuid.MustParse("123e4567-e89b-12d3-a456-426614174000"),
		}})
	})
	if !res.HasCode(diag.E_TYPE_MISMATCH) {
		t.Errorf("a uuid.UUID at a List<Integer> property validated clean: %v", codes(res))
	}
}
