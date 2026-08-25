package graph_test

import (
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/internal/instancetest"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// These guards bite only bypass callers: the validator enforces each rule
// for RawInstance input, so every fixture here is built with instancetest.

const bypassGuardSchema = `schema "guards"

type Car {
	vin String primary

	*-> SPARE (one) Wheel
}

part type Wheel {
	position String required
}
`

func loadGuardSchema(t *testing.T) *schema.Schema {
	t.Helper()
	s, res := schema.LoadString(t.Context(), bypassGuardSchema, "guards.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res.String())
	}
	return s
}

func TestAdd_BypassGuard_OneAssociationCardinality(t *testing.T) {
	t.Parallel()
	s := testSchemaWithAssociation(t) // employer is (one)
	g := graph.New(s)

	vi := instancetest.VI(
		"Person",
		instancetest.TypeID(mustTypeID(t, s, "Person")),
		instancetest.PK("p1"),
		instancetest.Props(map[string]any{"id": "p1"}),
		instancetest.Edges(edgeData("employer", nil, []any{"c1"}, []any{"c2"})),
	)
	result := g.Add(t.Context(), vi)
	if result.OK() {
		t.Fatal("two targets on a (one) association passed Add")
	}
	assertHasCode(t, result, diag.E_GRAPH_CARDINALITY)
	if insts := g.Snapshot().InstancesOf(mustTypeID(t, s, "Person")); len(insts) != 0 {
		t.Fatal("the rejected instance installed anyway")
	}
}

func TestAdd_BypassGuard_UnknownAssociationName(t *testing.T) {
	t.Parallel()
	s := testSchemaWithAssociation(t)
	g := graph.New(s)

	vi := instancetest.VI(
		"Person",
		instancetest.TypeID(mustTypeID(t, s, "Person")),
		instancetest.PK("p1"),
		instancetest.Props(map[string]any{"id": "p1"}),
		instancetest.Edges(edgeData("bogus", nil, []any{"c1"})),
	)
	result := g.Add(t.Context(), vi)
	if result.OK() {
		t.Fatal("an undeclared association name passed Add silently")
	}
	assertHasCode(t, result, diag.E_GRAPH_UNKNOWN_RELATION)

	// The instance installs; only the unnameable edges are dropped, loudly.
	snap := g.Snapshot()
	if insts := snap.InstancesOf(mustTypeID(t, s, "Person")); len(insts) != 1 {
		t.Fatal("instance with an undeclared edge name did not install")
	}
	// The required employer association is legitimately recorded absent;
	// only the undeclared name must leave no trace.
	if len(snap.Edges()) != 0 {
		t.Fatal("edges under an undeclared name survived")
	}
	for _, u := range snap.Unresolved() {
		if u.Relation == "bogus" {
			t.Fatal("an undeclared name produced an unresolved record")
		}
	}
}

func TestAdd_BypassGuard_UnknownCompositionName(t *testing.T) {
	t.Parallel()
	s := loadGuardSchema(t)
	g := graph.New(s)

	wheel := instancetest.VI(
		"Wheel",
		instancetest.TypeID(mustTypeID(t, s, "Wheel")),
		instancetest.Props(map[string]any{"position": "left"}),
	)
	car := instancetest.VI(
		"Car",
		instancetest.TypeID(mustTypeID(t, s, "Car")),
		instancetest.PK("v1"),
		instancetest.Props(map[string]any{"vin": "v1"}),
		instancetest.Composed(map[string]immutable.Value{"BOGUS": immutable.Wrap([]any{wheel})}),
	)
	result := g.Add(t.Context(), car)
	if result.OK() {
		t.Fatal("children under an undeclared composition name attached as (many)")
	}
	assertHasCode(t, result, diag.E_GRAPH_UNKNOWN_RELATION)
	insts := g.Snapshot().InstancesOf(mustTypeID(t, s, "Car"))
	if len(insts) != 1 || insts[0].ComposedCount("BOGUS") != 0 {
		t.Fatal("children under an undeclared name attached")
	}
}

func TestAdd_BypassGuard_InlineComposedChildTypeMismatch(t *testing.T) {
	t.Parallel()
	s := loadGuardSchema(t)
	g := graph.New(s)

	wrongChild := instancetest.VI(
		"Car",
		instancetest.TypeID(mustTypeID(t, s, "Car")),
		instancetest.PK("v2"),
		instancetest.Props(map[string]any{"vin": "v2"}),
	)
	car := instancetest.VI(
		"Car",
		instancetest.TypeID(mustTypeID(t, s, "Car")),
		instancetest.PK("v1"),
		instancetest.Props(map[string]any{"vin": "v1"}),
		instancetest.Composed(map[string]immutable.Value{"SPARE": immutable.Wrap([]any{wrongChild})}),
	)
	result := g.Add(t.Context(), car)
	if result.OK() {
		t.Fatal("a child of the wrong type attached on the inline path")
	}
	assertHasCode(t, result, diag.E_GRAPH_INVALID_COMPOSITION)
	insts := g.Snapshot().InstancesOf(mustTypeID(t, s, "Car"))
	if len(insts) != 1 || insts[0].ComposedCount("SPARE") != 0 {
		t.Fatal("the wrong-typed child attached")
	}
}

func TestAdd_BypassGuard_AbstractType(t *testing.T) {
	t.Parallel()
	s, res := schema.NewBuilder().
		WithName("guards").
		WithSourceID(location.MustNewSourceID("test://guards-abstract.yammm")).
		AddType("Ghost").
		AsAbstract().
		WithPrimaryKey("id", schema.StringConstraint{}).
		Done().
		Build()
	if res.HasErrors() {
		t.Fatalf("build schema: %s", res.String())
	}
	g := graph.New(s)

	vi := instancetest.VI(
		"Ghost",
		instancetest.TypeID(mustTypeID(t, s, "Ghost")),
		instancetest.PK("g1"),
		instancetest.Props(map[string]any{"id": "g1"}),
	)
	result := g.Add(t.Context(), vi)
	if result.OK() {
		t.Fatal("an abstract-typed instance passed Add")
	}
	assertHasCode(t, result, diag.E_GRAPH_ABSTRACT_TYPE)
}

func TestAdd_BypassGuard_EmptyKey(t *testing.T) {
	t.Parallel()
	s := loadGuardSchema(t)
	g := graph.New(s)

	vi := instancetest.VI(
		"Car",
		instancetest.TypeID(mustTypeID(t, s, "Car")),
		instancetest.Props(map[string]any{"vin": "v1"}),
	)
	result := g.Add(t.Context(), vi)
	if result.OK() {
		t.Fatal(`an empty key installed under the literal "[]"`)
	}
	assertHasCode(t, result, diag.E_GRAPH_INVALID_PK)
}

func TestAdd_BypassGuard_KeyPropertyMismatch(t *testing.T) {
	t.Parallel()
	s := loadGuardSchema(t)
	g := graph.New(s)

	vi := instancetest.VI(
		"Car",
		instancetest.TypeID(mustTypeID(t, s, "Car")),
		instancetest.PK("v1"),
		instancetest.Props(map[string]any{"vin": "v2"}),
	)
	result := g.Add(t.Context(), vi)
	if result.OK() {
		t.Fatal("a key disagreeing with its own key property passed Add")
	}
	assertHasCode(t, result, diag.E_GRAPH_INVALID_PK)
}

// TestAdd_BypassGuard_AbsentKeyPropertyTolerated pins the deliberate
// tolerance: the in-repo fixture corpus carries keys without materializing
// the property map, and that stays legal.
func TestAdd_BypassGuard_AbsentKeyPropertyTolerated(t *testing.T) {
	t.Parallel()
	s := loadGuardSchema(t)
	g := graph.New(s)

	vi := instancetest.VI(
		"Car",
		instancetest.TypeID(mustTypeID(t, s, "Car")),
		instancetest.PK("v1"),
		instancetest.Props(map[string]any{}),
	)
	if result := g.Add(t.Context(), vi); !result.OK() {
		t.Fatalf("a key without its property map was rejected: %s", result.String())
	}
}
