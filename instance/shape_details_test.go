package instance_test

import (
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/yammmtest"
)

// Every shape mismatch carries the expected and got shapes as details, so a
// structured consumer never has to parse the message.
func TestEdgeShapeMismatch_AlwaysCarriesExpectedAndGot(t *testing.T) {
	v := instance.NewValidator(loadSrc(t, personCompany))
	_, res := v.ValidateOne(t.Context(), "Person", instance.RawInstance{Properties: map[string]any{"id": "p", "works_at": 42}})
	is := mustIssue(t, res, instance.ErrEdgeShapeMismatch)
	yammmtest.Diff(t, []string{"object"}, detailValues(is, diag.DetailKeyExpected))
	yammmtest.Diff(t, []string{"number"}, detailValues(is, diag.DetailKeyGot))

	v = instance.NewValidator(loadSrc(t, docSrc))
	_, res = v.ValidateOne(t.Context(), "Doc", instance.RawInstance{Properties: map[string]any{"id": "d", "lines": "x"}})
	is = mustIssue(t, res, instance.ErrEdgeShapeMismatch)
	yammmtest.Diff(t, []string{"array"}, detailValues(is, diag.DetailKeyExpected))
	yammmtest.Diff(t, []string{"string"}, detailValues(is, diag.DetailKeyGot))
}

// A typed container is named by its kind, which is what a Go caller sent.
func TestEdgeShapeMismatch_NamesTypedContainers(t *testing.T) {
	v := instance.NewValidator(loadSrc(t, personCompany))
	_, res := v.ValidateOne(t.Context(), "Person", instance.RawInstance{Properties: map[string]any{"id": "p", "works_at": []string{"x"}}})
	yammmtest.Diff(t, []string{"array"}, detailValues(mustIssue(t, res, instance.ErrEdgeShapeMismatch), diag.DetailKeyGot))
}
