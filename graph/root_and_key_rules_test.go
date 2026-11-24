package graph_test

import (
	"slices"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/diagtest"
	"github.com/simon-lentz/yammm/internal/instancetest"
	"github.com/simon-lentz/yammm/schema"
)

const carSchema = `schema "guards"

type Car {
	vin String primary

	*-> SPARE (one) Wheel
}

part type Wheel {
	position String required
}
`

func loadCarSchema(t *testing.T) schema.Schema {
	t.Helper()
	s, err := schema.LoadString(t.Context(), carSchema, "guards.yammm")
	if err != nil {
		t.Fatalf("load schema: %s", err)
	}
	return s
}

// A validator refuses a part type at a root, so the one route to Add is a child
// read out of [instance.ValidInstance.Composed].
func TestAdd_ComposedChildAtARoot_Keyed(t *testing.T) {
	t.Parallel()
	s := testSchemaWithComposition(t)
	parent := instancetest.Valid(t, s, "Parent", map[string]any{
		"id": "p1", "name": "n", "children": []any{map[string]any{"id": "c1", "name": "n"}},
	})
	result := diagtest.Failures(t, graph.New(s).Add(t.Context(), firstChild(t, parent)))
	if diagtest.Err(result) == nil {
		t.Fatal("a part-typed root passed Add")
	}
	assertHasCode(t, result, diag.E_GRAPH_INVALID_COMPOSITION)
}

// A keyless part type has no address for a root.
func TestAdd_ComposedChildAtARoot_Keyless(t *testing.T) {
	t.Parallel()
	s := loadCarSchema(t)
	car := instancetest.Valid(t, s, "Car", map[string]any{"vin": "v1", "spare": []any{map[string]any{"position": "left"}}})
	result := diagtest.Failures(t, graph.New(s).Add(t.Context(), firstChild(t, car)))
	if diagtest.Err(result) == nil {
		t.Fatal("a root of a type with no primary key passed Add")
	}
	assertHasCode(t, result, diag.E_GRAPH_MISSING_PK)
	if n := result.Len(); n != 1 {
		t.Errorf("%d issues, want one: a keyless part draws E_GRAPH_MISSING_PK alone: %s", n, result)
	}
}

// firstChild reads a validated child out of the one composition inst holds.
func firstChild(t *testing.T, inst instance.ValidInstance) instance.ValidInstance {
	t.Helper()
	for rel := range inst.Type().AllCompositions() {
		for child := range inst.Composed(rel.Name()) {
			return child
		}
	}
	t.Fatal("the instance holds no composition")
	return instance.ValidInstance{}
}

// TestComposedChild_TransitivelyImportedType_Resolves pins that a composed
// child is resolved by IDENTITY across the whole import closure, not by the
// name-oriented direct-imports-only rule. A schema where B composes a part type
// from C loads clean and validates clean from a graph bound to A, so the graph
// has to serve it.
func TestComposedChild_TransitivelyImportedType_Resolves(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	members := loadClosure(t, "ta.yammm", map[string]string{
		"tc.yammm": `schema "schema_c"

part type Part {
	id Timestamp primary
}
`,
		"tb.yammm": `schema "schema_b"

import "tc" as c

type Middle {
	id String primary
	*-> PARTS (many) c.Part
}
`,
		"ta.yammm": `schema "schema_a"

import "tb" as b

type Top {
	id String primary
}
`,
	})
	schemaA, schemaB := members["schema_a"], members["schema_b"]

	middleID := mustTypeID(t, schemaB, "Middle")
	middle := instancetest.Valid(t, schemaA, "b.Middle", map[string]any{"id": "m1", "parts": []any{
		map[string]any{"id": "2024-01-01T00:00:00Z"},
		map[string]any{"id": "2024-01-02T00:00:00Z"},
	}})

	g := graph.New(schemaA)
	if err := g.Add(ctx, middle); err != nil {
		t.Fatalf("a composed child of a transitively imported type was refused: %s", err)
	}
	insts := slices.Collect(g.Snapshot().InstancesOf(middleID))
	if len(insts) != 1 || insts[0].ComposedCount("PARTS") != 2 {
		t.Fatalf("the transitively imported children did not attach (%d instances)", len(insts))
	}
}

// TestComposedChild_KeylessSiblings_AreNotDuplicates pins that keyless children
// do not collide: each renders "[]", a key neither child holds.
func TestComposedChild_KeylessSiblings_AreNotDuplicates(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	const manySchema = `schema "guards"

type Car {
	vin String primary

	*-> WHEELS (many) Wheel
}

part type Wheel {
	position String required
}
`
	sm, err := schema.LoadString(ctx, manySchema, "guards_many.yammm")
	if err != nil {
		t.Fatalf("load: %s", err)
	}

	g := graph.New(sm)
	var wheels []map[string]any
	for _, position := range []string{"left", "right"} {
		wheels = append(wheels, map[string]any{"position": position})
	}
	if err := g.Add(ctx, parentWith(t, sm, "Car", []any{"v1"}, map[string]any{"vin": "v1"}, "WHEELS", wheels...)); err != nil {
		t.Fatalf("keyless siblings were rejected: %s", err)
	}

	car := slices.Collect(g.Snapshot().InstancesOf(mustTypeID(t, sm, "Car")))[0]
	if n := car.ComposedCount("WHEELS"); n != 2 {
		t.Errorf("keyless siblings attached = %d, want 2", n)
	}
}
