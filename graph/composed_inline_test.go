package graph_test

import (
	"errors"
	"testing"

	"github.com/simon-lentz/yammm/diag"
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

// TestValidateOne_InlineComposition_OneCardinalityRefused pins the instance
// layer's own enforcement: a composition always arrives as an array, so
// nothing about the shape settles multiplicity and the validator has to.
func TestValidateOne_InlineComposition_OneCardinalityRefused(t *testing.T) {
	ctx := t.Context()
	s, err := schema.LoadString(ctx, toOneCompositionSchema, "test://to_one_composition.yammm")
	if err != nil {
		t.Fatalf("schema load: %v", err)
	}

	v := instance.NewValidator(s)
	_, validateErr := v.ValidateOne(ctx, "Car", instance.RawInstance{Properties: map[string]any{
		"vin": "v1",
		"spare": []any{
			map[string]any{"position": "left"},
			map[string]any{"position": "right"},
		},
	}})
	if validateErr == nil {
		t.Fatal("two children under a (one) composition passed validation")
	}
	if !errors.Is(validateErr, diag.E_DUPLICATE_COMPOSED_PK) {
		t.Fatalf("want E_DUPLICATE_COMPOSED_PK, got %s", validateErr)
	}
}
