package graph_test

import (
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
)

const toOneCompositionSchema = `schema "fleet"

type Car {
    vin String primary

    *-> SPARE (one) Wheel
}

part type Wheel {
    position String required
}
`

// TestAdd_InlineComposition_OneCardinalityViolated pins the inline-path
// enforcement of (one) composition cardinality: a RawInstance carrying two
// composed children under a (one) composition passes instance validation
// (validateComposition accepts any array length) and is rejected by
// [graph.Graph.Add] with E_DUPLICATE_COMPOSED_PK. The streaming path's
// equivalent is [TestAddComposed_OneCardinality_Duplicate]; this covers the
// nested-children-in-one-Add branch.
func TestAdd_InlineComposition_OneCardinalityViolated(t *testing.T) {
	ctx := t.Context()
	s, res := schema.LoadString(ctx, toOneCompositionSchema, "test://to_one_composition.yammm")
	if res.HasErrors() {
		t.Fatalf("schema load: %v", res.Err())
	}

	v := instance.NewValidator(s)
	raw := instance.RawInstance{Properties: map[string]any{
		"vin": "v1",
		"spare": []any{
			map[string]any{"position": "left"},
			map[string]any{"position": "right"},
		},
	}}

	inst, vres := v.ValidateOne(ctx, "Car", raw)
	if !vres.OK() {
		t.Fatalf("instance layer should accept the shape (cardinality is a graph-layer concern): %v", vres)
	}

	g := graph.New(s)
	result := g.Add(ctx, inst)
	if result.OK() {
		t.Fatal("Add should reject 2 children under a (one) composition")
	}
	assertHasCode(t, result, diag.E_DUPLICATE_COMPOSED_PK)
}
