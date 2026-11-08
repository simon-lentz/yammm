package graph_test

import (
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/instancetest"
	"github.com/simon-lentz/yammm/internal/snapparts"
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
	s, res := schema.LoadString(t.Context(), carSchema, "guards.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res.String())
	}
	return s
}

// importCar imports one Car, holding the given key and vin, into a second load of
// the schema, which the key rule walks. A schema allows no number key, so a number
// key reaches the rule only through a rebuilt snapshot.
func importCar(t *testing.T, key, vin any) diag.Result {
	t.Helper()
	built := loadCarSchema(t)
	snap := rebuild(built, snapparts.Parts{Instances: []snapparts.Instance{{
		TypeID:     mustTypeID(t, built, "Car"),
		PrimaryKey: immutable.WrapKey([]any{key}),
		Properties: immutable.WrapProperties(map[string]any{"vin": vin}),
	}}})
	_, res := graph.NewFromSnapshot(loadCarSchema(t), snap)
	return res
}

// A validator refuses a part type at a root, so the one route to Add is a child
// read out of [instance.ValidInstance.Compositions].
func TestAdd_ComposedChildAtARoot_Keyed(t *testing.T) {
	t.Parallel()
	s := testSchemaWithComposition(t)
	parent := instancetest.Valid(t, s, "Parent", map[string]any{
		"id": "p1", "name": "n", "children": []any{map[string]any{"id": "c1", "name": "n"}},
	})
	result := graph.New(s).Add(t.Context(), firstChild(t, parent))
	if result.OK() {
		t.Fatal("a part-typed root passed Add")
	}
	assertHasCode(t, result, diag.E_GRAPH_INVALID_COMPOSITION)
}

// A keyless part type has no address for a root.
func TestAdd_ComposedChildAtARoot_Keyless(t *testing.T) {
	t.Parallel()
	s := loadCarSchema(t)
	car := instancetest.Valid(t, s, "Car", map[string]any{"vin": "v1", "spare": []any{map[string]any{"position": "left"}}})
	result := graph.New(s).Add(t.Context(), firstChild(t, car))
	if result.OK() {
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
	for _, composed := range inst.Compositions() {
		children, _ := composed.Slice()
		return children.Get(0).Unwrap().(instance.ValidInstance)
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
	if r := g.Add(ctx, middle); !r.OK() {
		t.Fatalf("a composed child of a transitively imported type was refused: %s", r.String())
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
	sm, res := schema.LoadString(ctx, manySchema, "guards_many.yammm")
	if res.HasErrors() {
		t.Fatalf("load: %s", res.String())
	}

	g := graph.New(sm)
	var wheels []map[string]any
	for _, position := range []string{"left", "right"} {
		wheels = append(wheels, map[string]any{"position": position})
	}
	if r := g.Add(ctx, parentWith(t, sm, "Car", []any{"v1"}, map[string]any{"vin": "v1"}, "WHEELS", wheels...)); !r.OK() {
		t.Fatalf("keyless siblings were rejected: %s", r.String())
	}

	car := slices.Collect(g.Snapshot().InstancesOf(mustTypeID(t, sm, "Car")))[0]
	if n := car.ComposedCount("WHEELS"); n != 2 {
		t.Errorf("keyless siblings attached = %d, want 2", n)
	}
}

// The import accepts a number key component ParseKey reads back at the same
// rendering, whatever its Go type: the rule is the rendering, not the type.
// float64(5) renders as the literal 5, which reads back as int64(5) and renders
// alike.
func TestNewFromSnapshot_AcceptsANumberKeyThatRendersBackUnchanged(t *testing.T) {
	t.Parallel()
	for _, v := range []any{float64(5), int64(5), int64(math.MaxInt64), float64(1.5)} {
		if res := importCar(t, v, v); res.HasErrors() {
			t.Errorf("key [%v] (%T): NewFromSnapshot = %s, want no refusal", v, v, res)
		}
	}
}

// The import refuses a key component that disagrees with its key property whatever
// Go type both hold.
func TestNewFromSnapshot_RefusesAKeyTheKeyRuleRefuses(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name      string
		key, prop any
		phrase    string
	}{
		{"two integers", int64(1), int64(2), "disagrees with property"},
		{"two bools", false, true, "disagrees with property"},
		{"a null key property", "v1", nil, "is absent or null"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			res := importCar(t, c.key, c.prop)
			if !res.HasCode(diag.E_GRAPH_INVALID_PK) || !strings.Contains(res.String(), c.phrase) {
				t.Errorf("NewFromSnapshot = %s, want E_GRAPH_INVALID_PK naming %q", res, c.phrase)
			}
		})
	}
}
