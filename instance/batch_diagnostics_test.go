package instance_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/yammmtest"
)

// instance_index correlates a diagnostic with the element of the batch the
// CALLER passed: the root's index through Validate, the child's through
// ValidateForComposition, and never both.
func TestValidate_StampsInstanceIndexOnce(t *testing.T) {
	v := instance.NewValidator(loadSrc(t, docSrc))
	raws := []instance.RawInstance{
		{Properties: map[string]any{"id": "d0", "lines": []any{map[string]any{"n": "a", "qty": 1}}}},
		{Properties: map[string]any{"id": "d1", "lines": []any{map[string]any{"n": "b", "qty": "x"}}}},
	}
	_, res := v.Validate(t.Context(), "Doc", raws)
	yammmtest.Diff(t, []string{"1"}, detailValues(mustIssue(t, res, instance.ErrTypeMismatch), diag.DetailKeyInstanceIndex))

	_, res = v.ValidateForComposition(t.Context(), "Doc", "LINES", []instance.RawInstance{
		{Properties: map[string]any{"n": "a", "qty": 1}},
		{Properties: map[string]any{"n": "b", "qty": "x"}},
	})
	yammmtest.Diff(t, []string{"1"}, detailValues(mustIssue(t, res, instance.ErrTypeMismatch), diag.DetailKeyInstanceIndex))
}

func manyUnknownFields(n int) map[string]any {
	props := map[string]any{"id": "p"}
	for i := range n {
		props[fmt.Sprintf("unk%03d", i)] = i
	}
	return props
}

const personOnly = `schema "p"

type Person {
    id String primary
}
`

// The per-instance cap's truncation facts reach the batch result, so a caller
// that gates on them is not told nothing was dropped when 50 errors were.
func TestValidate_PreservesTruncation(t *testing.T) {
	v := instance.NewValidator(loadSrc(t, personOnly))
	props := manyUnknownFields(150)
	_, one := v.ValidateOne(t.Context(), "Person", instance.RawInstance{Properties: props})
	_, batch := v.Validate(t.Context(), "Person", []instance.RawInstance{{Properties: props}})
	if !one.LimitReached() {
		t.Fatal("the single-instance result is not truncated; the test is vacuous")
	}
	if !batch.LimitReached() || batch.DroppedCount() != one.DroppedCount() {
		t.Errorf("batch: limitReached=%v dropped=%d; one: dropped=%d", batch.LimitReached(), batch.DroppedCount(), one.DroppedCount())
	}
	if got := batch.SeverityCounts().Errors; got != 150 {
		t.Errorf("batch errors seen = %d, want 150", got)
	}
}

// The set of diagnostics that survives the cap is a function of the input,
// not of map iteration order.
func TestUnknownFields_DeterministicUnderCap(t *testing.T) {
	v := instance.NewValidator(loadSrc(t, personOnly))
	props := manyUnknownFields(150)
	var first string
	for i := range 20 {
		_, res := v.ValidateOne(t.Context(), "Person", instance.RawInstance{Properties: props})
		got := res.String()
		if i == 0 {
			first = got
			continue
		}
		if got != first {
			t.Fatalf("run %d differs from run 0", i)
		}
	}
}

// Duplicate-key indices name positions in the caller's array, not in the
// slice that survives the shape filter.
func TestComposition_DuplicateKeyIndicesAreInputIndices(t *testing.T) {
	v := instance.NewValidator(loadSrc(t, docSrc))
	_, res := v.ValidateOne(t.Context(), "Doc", instance.RawInstance{Properties: map[string]any{
		"id": "d", "lines": []any{map[string]any{"n": "a", "qty": 1}, "not-an-object", map[string]any{"n": "a", "qty": 1}},
	}})
	is := mustIssue(t, res, instance.ErrDuplicateComposedPK)
	if !strings.Contains(is.Message(), "indices 0 and 2") {
		t.Errorf("message = %q", is.Message())
	}
	if want := `$.lines[n="a"]`; is.Path() != want {
		t.Errorf("path = %q, want %s", is.Path(), want)
	}
}

// A subtype's instance is validated against the inclusive member set: the
// parent's required property is required here too.
func TestValidateOne_InheritedPropertyIsRequired(t *testing.T) {
	v := instance.NewValidator(loadSrc(t, `schema "p"

abstract type Base {
    id String primary
    name String required
}

type Person extends Base {
    age Integer
}
`))
	_, res := v.ValidateOne(t.Context(), "Person", instance.RawInstance{Properties: map[string]any{"id": "p", "age": 3}})
	is := mustIssue(t, res, instance.ErrMissingRequired)
	yammmtest.Diff(t, []string{"name"}, detailValues(is, diag.DetailKeyPropertyName))
}
