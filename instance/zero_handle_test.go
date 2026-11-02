package instance_test

import (
	"slices"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

func TestZeroValidRecords_ReportZeroAndHoldNothing(t *testing.T) {
	var valid instance.ValidInstance
	if !valid.IsZero() {
		t.Error("the zero ValidInstance must report zero")
	}
	if got := valid.TypeName(); got != "" {
		t.Errorf("TypeName = %q, want empty", got)
	}
	if !valid.Provenance().IsZero() {
		t.Error("Provenance must be zero")
	}
	if edge, ok := valid.Edge("R"); ok || !edge.IsZero() {
		t.Errorf("Edge = %v, %v, want the zero handle and false", edge, ok)
	}
	for range valid.Edges() {
		t.Error("a zero instance holds no edge")
	}
	for range valid.Compositions() {
		t.Error("a zero instance holds no composition")
	}

	var data instance.ValidEdgeData
	if !data.IsZero() || !data.IsEmpty() {
		t.Errorf("zero ValidEdgeData: IsZero = %v, IsEmpty = %v, want both true", data.IsZero(), data.IsEmpty())
	}
	if got := slices.Collect(data.Targets()); len(got) != 0 {
		t.Errorf("Targets = %v, want none", got)
	}
}

// A handle is a struct, so no zero handle reads as an absent value at a property.
func TestValidate_AZeroHandleAtAPropertyIsAMismatchAndNotAbsent(t *testing.T) {
	s, res := schema.LoadString(t.Context(), `schema "v"

type T {
	id String primary
	note String
	label String required
}
`, "v.yammm")
	if res.HasErrors() {
		t.Fatal(res)
	}
	for name, zero := range map[string]any{
		"schema.Type":            schema.Type{},
		"schema.Schema":          schema.Schema{},
		"location.Provenance":    location.Provenance{},
		"instance.ValidInstance": instance.ValidInstance{},
		"graph.Instance":         graph.Instance{},
	} {
		for _, property := range []string{"note", "label"} {
			props := map[string]any{"id": "a", "label": "l", property: zero}
			valid, res := instance.NewValidator(s).ValidateOne(t.Context(), "T", instance.RawInstance{Properties: props})
			if !valid.IsZero() || !slices.ContainsFunc(slices.Collect(res.Issues()), func(i diag.Issue) bool { return i.Code() == diag.E_TYPE_MISMATCH }) {
				t.Errorf("%s at %s: record %v, result %s; want no record and E_TYPE_MISMATCH", name, property, !valid.IsZero(), res)
			}
		}
	}
}
