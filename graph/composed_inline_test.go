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

	// The overflow now leaves the same record the streamed path leaves:
	// one Duplicate, addressed to the root parent's slot, so the loss
	// survives a Marshal/Load round trip instead of vanishing with the
	// transient diagnostic.
	snap := g.Snapshot()
	dups := snap.Duplicates()
	if len(dups) != 1 {
		t.Fatalf("inline (one) overflow left %d Duplicate records, want 1", len(dups))
	}
	d := dups[0]
	if d.Relation != "SPARE" {
		t.Errorf("Duplicate.Relation = %q, want SPARE", d.Relation)
	}
	if d.Parent == nil || d.Parent.PrimaryKey().String() != `["v1"]` {
		t.Error("Duplicate.Parent does not address the root car")
	}
	pos := func(i *graph.Instance) string {
		v, _ := i.Property("position")
		s, _ := v.Unwrap().(string)
		return s
	}
	if d.Conflict == nil || pos(d.Conflict) != "left" {
		t.Error("Duplicate.Conflict is not the attached first child")
	}
	if pos(d.Instance) != "right" {
		t.Error("Duplicate.Instance is not the rejected second child")
	}
	cars := snap.InstancesOf(inst.TypeID())
	if len(cars) != 1 || cars[0].ComposedCount("SPARE") != 1 {
		t.Error("the first child did not stay attached alone")
	}
}

// TestAdd_InlineComposition_NestedOneOverflowStaysDiagnosticOnly pins the
// root-only rule: a Duplicate whose parent is not a root would marshal into
// a dangling reference, so a deeper slot keeps the transient diagnostic and
// records nothing.
func TestAdd_InlineComposition_NestedOneOverflowStaysDiagnosticOnly(t *testing.T) {
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

	v := instance.NewValidator(s)
	inst, vres := v.ValidateOne(ctx, "Car", instance.RawInstance{Properties: map[string]any{
		"vin": "v1",
		"holds": []any{map[string]any{
			"label": "b1",
			"spare": []any{
				map[string]any{"position": "left"},
				map[string]any{"position": "right"},
			},
		}},
	}})
	if !vres.OK() {
		t.Fatalf("instance layer should accept the shape: %v", vres)
	}

	g := graph.New(s)
	result := g.Add(ctx, inst)
	if result.OK() {
		t.Fatal("Add should report the nested (one) overflow")
	}
	assertHasCode(t, result, diag.E_DUPLICATE_COMPOSED_PK)
	if dups := g.Snapshot().Duplicates(); len(dups) != 0 {
		t.Fatalf("a nested slot recorded %d Duplicates; the wire cannot address its parent", len(dups))
	}
}
