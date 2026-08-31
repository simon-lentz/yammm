package graph_test

import (
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/instancetest"
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
	s, res := schema.LoadString(ctx, toOneCompositionSchema, "test://to_one_composition.yammm")
	if res.HasErrors() {
		t.Fatalf("schema load: %v", res.Err())
	}

	v := instance.NewValidator(s)
	_, vres := v.ValidateOne(ctx, "Car", instance.RawInstance{Properties: map[string]any{
		"vin": "v1",
		"spare": []any{
			map[string]any{"position": "left"},
			map[string]any{"position": "right"},
		},
	}})
	if vres.OK() {
		t.Fatal("two children under a (one) composition passed validation")
	}
	if !vres.HasCode(diag.E_EDGE_SHAPE_MISMATCH) {
		t.Fatalf("want E_EDGE_SHAPE_MISMATCH, got %s", vres.String())
	}
}

// TestAdd_InlineComposition_OneCardinalityViolated pins the graph's own guard
// on the same shape, which only a bypass caller can now build, and pins that
// the refusal leaves nothing behind.
func TestAdd_InlineComposition_OneCardinalityViolated(t *testing.T) {
	ctx := t.Context()
	s, res := schema.LoadString(ctx, toOneCompositionSchema, "test://to_one_composition.yammm")
	if res.HasErrors() {
		t.Fatalf("schema load: %v", res.Err())
	}

	wheel := func(position string) *instance.ValidInstance {
		return instancetest.VI(
			"Wheel",
			instancetest.TypeID(mustTypeID(t, s, "Wheel")),
			instancetest.Props(map[string]any{"position": position}),
		)
	}
	car := instancetest.VI(
		"Car",
		instancetest.TypeID(mustTypeID(t, s, "Car")),
		instancetest.PK("v1"),
		instancetest.Props(map[string]any{"vin": "v1"}),
		instancetest.Composed(map[string]immutable.Value{
			"SPARE": immutable.Wrap([]any{wheel("left"), wheel("right")}),
		}),
	)

	g := graph.New(s)
	result := g.Add(ctx, car)
	if result.OK() {
		t.Fatal("Add should reject 2 children under a (one) composition")
	}
	assertHasCode(t, result, diag.E_DUPLICATE_COMPOSED_PK)

	snap := g.Snapshot()
	if cars := snap.InstancesOf(mustTypeID(t, s, "Car")); len(cars) != 0 {
		t.Fatalf("a refused record installed %d cars", len(cars))
	}
	if dups := snap.Duplicates(); len(dups) != 0 {
		t.Fatalf("a refused record left %d Duplicate records", len(dups))
	}
}

// TestAdd_InlineComposition_NestedOneOverflowRefusesRoot pins that the guard
// reaches a slot at any depth, and that a violation there refuses the root.
func TestAdd_InlineComposition_NestedOneOverflowRefusesRoot(t *testing.T) {
	ctx := t.Context()
	const nested = `schema "fleet"

type Car {
    vin String primary

    *-> HOLDS (many) Box
}

part type Box {
    label String required

    *-> SPARE (one) Wheel
}

part type Wheel {
    position String required
}
`
	s, res := schema.LoadString(ctx, nested, "test://nested_one.yammm")
	if res.HasErrors() {
		t.Fatalf("schema load: %v", res.Err())
	}

	wheel := func(position string) *instance.ValidInstance {
		return instancetest.VI(
			"Wheel",
			instancetest.TypeID(mustTypeID(t, s, "Wheel")),
			instancetest.Props(map[string]any{"position": position}),
		)
	}
	box := instancetest.VI(
		"Box",
		instancetest.TypeID(mustTypeID(t, s, "Box")),
		instancetest.Props(map[string]any{"label": "b1"}),
		instancetest.Composed(map[string]immutable.Value{
			"SPARE": immutable.Wrap([]any{wheel("left"), wheel("right")}),
		}),
	)
	car := instancetest.VI(
		"Car",
		instancetest.TypeID(mustTypeID(t, s, "Car")),
		instancetest.PK("v1"),
		instancetest.Props(map[string]any{"vin": "v1"}),
		instancetest.Composed(map[string]immutable.Value{
			"HOLDS": immutable.Wrap([]any{box}),
		}),
	)

	g := graph.New(s)
	result := g.Add(ctx, car)
	if result.OK() {
		t.Fatal("Add should report the nested (one) overflow")
	}
	assertHasCode(t, result, diag.E_DUPLICATE_COMPOSED_PK)

	snap := g.Snapshot()
	if cars := snap.InstancesOf(mustTypeID(t, s, "Car")); len(cars) != 0 {
		t.Fatalf("a nested violation still installed %d roots", len(cars))
	}
	if dups := snap.Duplicates(); len(dups) != 0 {
		t.Fatalf("a refused record left %d Duplicate records", len(dups))
	}
}
