package graph_test

import (
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
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

	// The record is refused entire: data the graph cannot name is not a
	// partial success, and a partial install disagrees with the error.
	snap := g.Snapshot()
	if insts := snap.InstancesOf(mustTypeID(t, s, "Person")); len(insts) != 0 {
		t.Fatal("a refused instance installed")
	}
	if len(snap.Edges()) != 0 {
		t.Fatal("edges under an undeclared name survived")
	}
	if len(snap.Unresolved()) != 0 {
		t.Fatal("a refused instance left unresolved records")
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
	if insts := g.Snapshot().InstancesOf(mustTypeID(t, s, "Car")); len(insts) != 0 {
		t.Fatal("a record carrying an undeclared composition name installed")
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
	if insts := g.Snapshot().InstancesOf(mustTypeID(t, s, "Car")); len(insts) != 0 {
		t.Fatal("a record carrying a wrong-typed child installed")
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

// TestAdd_BypassGuard_TypedNilComposedChild pins the shape that dereferenced
// rawly before the guard existed: a typed nil passes the type assertion, so
// the assertion's ok result cannot stand in for a nil check.
func TestAdd_BypassGuard_TypedNilComposedChild(t *testing.T) {
	t.Parallel()
	s := loadGuardSchema(t)
	g := graph.New(s)

	car := instancetest.VI(
		"Car",
		instancetest.TypeID(mustTypeID(t, s, "Car")),
		instancetest.PK("v1"),
		instancetest.Props(map[string]any{"vin": "v1"}),
		instancetest.Composed(map[string]immutable.Value{
			"SPARE": immutable.Wrap([]any{(*instance.ValidInstance)(nil)}),
		}),
	)

	result := g.Add(t.Context(), car)
	if result.OK() {
		t.Fatal("a typed-nil composed child passed Add")
	}
	assertHasCode(t, result, diag.E_GRAPH_INVALID_COMPOSITION)
	if insts := g.Snapshot().InstancesOf(mustTypeID(t, s, "Car")); len(insts) != 0 {
		t.Fatal("a refused record installed")
	}
}

// TestAdd_BypassGuard_InlineSiblingDuplicatePK pins the inline path against
// the streamed path's rule: two children of one (many) slot cannot share a
// primary key, which the writers would render as one composed key.
func TestAdd_BypassGuard_InlineSiblingDuplicatePK(t *testing.T) {
	t.Parallel()
	s := testSchemaWithComposition(t) // Parent -> (many) Child, Child has a PK
	childID := mustTypeID(t, s, "Child")

	child := func(name string) *instance.ValidInstance {
		return instancetest.VI(
			"Child",
			instancetest.TypeID(childID),
			instancetest.PK("c1"),
			instancetest.Props(map[string]any{"id": "c1", "name": name}),
		)
	}
	parent := instancetest.VI(
		"Parent",
		instancetest.TypeID(mustTypeID(t, s, "Parent")),
		instancetest.PK("p1"),
		instancetest.Props(map[string]any{"id": "p1"}),
		instancetest.Composed(map[string]immutable.Value{
			"children": immutable.Wrap([]any{child("first"), child("second")}),
		}),
	)

	g := graph.New(s)
	result := g.Add(t.Context(), parent)
	if result.OK() {
		t.Fatal("two siblings sharing a primary key passed Add")
	}
	assertHasCode(t, result, diag.E_DUPLICATE_COMPOSED_PK)
	if insts := g.Snapshot().InstancesOf(mustTypeID(t, s, "Parent")); len(insts) != 0 {
		t.Fatal("a refused record installed")
	}
}

// TestAdd_BypassGuard_KindDisagreementNamesTheDeclaredKind pins the message
// correction: a name declared as the other kind is not an undeclared name,
// and reporting it as one sends the reader to look for a schema bug.
func TestAdd_BypassGuard_KindDisagreementNamesTheDeclaredKind(t *testing.T) {
	t.Parallel()
	s := loadGuardSchema(t)
	g := graph.New(s)

	// SPARE is a composition; this files it as edge data instead.
	vi := instancetest.VI(
		"Car",
		instancetest.TypeID(mustTypeID(t, s, "Car")),
		instancetest.PK("v1"),
		instancetest.Props(map[string]any{"vin": "v1"}),
		instancetest.Edges(edgeData("SPARE", nil, []any{"w1"})),
	)

	result := g.Add(t.Context(), vi)
	if result.OK() {
		t.Fatal("a composition name filed as edge data passed Add")
	}
	assertHasCode(t, result, diag.E_GRAPH_UNKNOWN_RELATION)
	if !strings.Contains(result.String(), "is declared as a composition") {
		t.Fatalf("the message does not name the declared kind: %s", result.String())
	}
}

// TestAddComposed_BypassGuard_ChecksTheStreamedChildsSubtree pins that a
// streamed child runs the same structural guard a root does.
func TestAddComposed_BypassGuard_ChecksTheStreamedChildsSubtree(t *testing.T) {
	t.Parallel()
	s := testSchemaWithComposition(t)
	g := graph.New(s)
	ctx := t.Context()

	parent := instancetest.VI(
		"Parent",
		instancetest.TypeID(mustTypeID(t, s, "Parent")),
		instancetest.PK("p1"),
		instancetest.Props(map[string]any{"id": "p1"}),
	)
	if res := g.Add(ctx, parent); !res.OK() {
		t.Fatalf("parent add: %s", res.String())
	}

	// The child itself is well formed; its own edge slot is not.
	child := instancetest.VI(
		"Child",
		instancetest.TypeID(mustTypeID(t, s, "Child")),
		instancetest.PK("c1"),
		instancetest.Props(map[string]any{"id": "c1"}),
		instancetest.Edges(edgeData("bogus", nil, []any{"x"})),
	)
	result := g.AddComposed(ctx, mustTypeID(t, s, "Parent"), `["p1"]`, "children", child)
	if result.OK() {
		t.Fatal("a streamed child with an undeclared edge name attached")
	}
	assertHasCode(t, result, diag.E_GRAPH_UNKNOWN_RELATION)
	if n := g.Snapshot().InstancesOf(mustTypeID(t, s, "Parent"))[0].ComposedCount("children"); n != 0 {
		t.Fatalf("the refused child attached anyway (%d children)", n)
	}
}

// TestComposedChild_TransitivelyImportedType_Resolves pins that a composed
// child is resolved by IDENTITY across the whole import closure, not by the
// name-oriented direct-imports-only rule. A schema where B composes a part type
// from C loads clean and validates clean from a graph bound to A, so the graph
// has to serve it: resolving by name dropped the subtree silently and, once
// the child type went unresolved, disabled sibling duplicate detection with it.
func TestComposedChild_TransitivelyImportedType_Resolves(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	// C declares a keyed part type. B imports C and composes it. A imports B.
	schemaC, res := schema.NewBuilder().
		WithName("schema_c").
		WithSourceID(location.MustNewSourceID("test://tc.yammm")).
		AddType("Part").
		AsPart().
		WithPrimaryKey("id", schema.StringConstraint{}).
		Done().
		Build()
	if res.HasErrors() {
		t.Fatalf("schema C: %s", res.String())
	}
	reg := schema.NewRegistry()
	if err := reg.Register(schemaC); err != nil {
		t.Fatalf("register C: %v", err)
	}

	schemaB, res := schema.NewBuilder().
		WithName("schema_b").
		WithSourceID(location.MustNewSourceID("test://tb.yammm")).
		WithRegistry(reg).
		AddImport("schema_c", "c").
		AddType("Middle").
		WithPrimaryKey("id", schema.StringConstraint{}).
		WithComposition("parts", schema.NewTypeRef("c", "Part", location.Span{}), true, true).
		WithComposition("sole", schema.NewTypeRef("c", "Part", location.Span{}), true, false).
		Done().
		Build()
	if res.HasErrors() {
		t.Fatalf("schema B: %s", res.String())
	}
	if err := reg.Register(schemaB); err != nil {
		t.Fatalf("register B: %v", err)
	}

	schemaA, res := schema.NewBuilder().
		WithName("schema_a").
		WithSourceID(location.MustNewSourceID("test://ta.yammm")).
		WithRegistry(reg).
		AddImport("schema_b", "b").
		AddType("Top").
		WithPrimaryKey("id", schema.StringConstraint{}).
		Done().
		Build()
	if res.HasErrors() {
		t.Fatalf("schema A: %s", res.String())
	}

	partID := mustTypeID(t, schemaC, "Part")
	middleID := mustTypeID(t, schemaB, "Middle")
	part := func(id string) *instance.ValidInstance {
		return instancetest.VI(
			"c.Part",
			instancetest.TypeID(partID),
			instancetest.PK(id),
			instancetest.Props(map[string]any{"id": id}),
		)
	}
	middle := func(id string, kids ...any) *instance.ValidInstance {
		return instancetest.VI(
			"b.Middle",
			instancetest.TypeID(middleID),
			instancetest.PK(id),
			instancetest.Props(map[string]any{"id": id}),
			instancetest.Composed(map[string]immutable.Value{"parts": immutable.Wrap(kids)}),
		)
	}

	g := graph.New(schemaA)
	if r := g.Add(ctx, middle("m1", part("p1"), part("p2"))); !r.OK() {
		t.Fatalf("a composed child of a transitively imported type was refused: %s", r.String())
	}
	insts := g.Snapshot().InstancesOf(middleID)
	if len(insts) != 1 || insts[0].ComposedCount("parts") != 2 {
		t.Fatalf("the transitively imported children did not attach (%d instances)", len(insts))
	}

	// The same resolution is what makes sibling duplicate detection work: with
	// the child type unresolved the scan was silently skipped.
	r := g.Add(ctx, middle("m2", part("dup"), part("dup")))
	if r.OK() {
		t.Fatal("two siblings sharing a primary key were accepted")
	}
	assertHasCode(t, r, diag.E_DUPLICATE_COMPOSED_PK)

	// The (one) overflow's composed-key detail is rendered from the part
	// type too, so it needs the same identity resolution.
	overflow := instancetest.VI(
		"b.Middle",
		instancetest.TypeID(middleID),
		instancetest.PK("m3"),
		instancetest.Props(map[string]any{"id": "m3"}),
		instancetest.Composed(map[string]immutable.Value{
			"sole": immutable.Wrap([]any{part("s1"), part("s2")}),
		}),
	)
	r = g.Add(ctx, overflow)
	if r.OK() {
		t.Fatal("a (one) composition carrying two children was accepted")
	}
	want, err := graph.FormatComposedKey([]any{"m3"}, "sole", []any{"s2"})
	if err != nil {
		t.Fatalf("FormatComposedKey: %v", err)
	}
	var got string
	for issue := range r.Issues() {
		if issue.Code() != diag.E_DUPLICATE_COMPOSED_PK {
			continue
		}
		for _, d := range issue.Details() {
			if d.Key == diag.DetailKeyPrimaryKey {
				got = d.Value
			}
		}
	}
	if got != want {
		t.Errorf("overflow primary_key detail = %q, want %q", got, want)
	}
}

// TestComposedChild_WrongType_RefusesOnTheInlinePath is the adjacent case: an
// identity the relation's target does not match, refused before install.
func TestComposedChild_WrongType_RefusesOnTheInlinePath(t *testing.T) {
	t.Parallel()
	s := loadGuardSchema(t)
	g := graph.New(s)
	ctx := t.Context()

	// A Wheel carrying an identity no schema this graph can reach declares.
	foreign := instancetest.VI(
		"Wheel",
		instancetest.TypeID(mustTypeID(t, s, "Wheel")),
		instancetest.Props(map[string]any{"position": "left"}),
	)

	car := instancetest.VI(
		"Car",
		instancetest.TypeID(mustTypeID(t, s, "Car")),
		instancetest.PK("v1"),
		instancetest.Props(map[string]any{"vin": "v1"}),
		instancetest.Composed(map[string]immutable.Value{
			"SPARE": immutable.Wrap([]any{foreign}),
		}),
	)
	if res := g.Add(ctx, car); !res.OK() {
		t.Fatalf("the control case must succeed: %s", res.String())
	}

	// The same shape with an unresolvable identity: SPARE's declared target
	// resolves, so an unresolvable child can only arrive by bypass.
	other, res := schema.LoadString(ctx, `schema "elsewhere"

part type Wheel {
	position String required
}
`, "elsewhere.yammm")
	if res.HasErrors() {
		t.Fatalf("load second schema: %s", res.String())
	}
	stranger := instancetest.VI(
		"Wheel",
		instancetest.TypeID(mustTypeID(t, other, "Wheel")),
		instancetest.Props(map[string]any{"position": "left"}),
	)
	strandedCar := instancetest.VI(
		"Car",
		instancetest.TypeID(mustTypeID(t, s, "Car")),
		instancetest.PK("v2"),
		instancetest.Props(map[string]any{"vin": "v2"}),
		instancetest.Composed(map[string]immutable.Value{
			"SPARE": immutable.Wrap([]any{stranger}),
		}),
	)
	result := g.Add(ctx, strandedCar)
	if result.OK() {
		t.Fatal("a composed child of an unreachable type attached silently")
	}
	if !result.HasCode(diag.E_GRAPH_INVALID_COMPOSITION) && !result.HasCode(diag.E_GRAPH_TYPE_NOT_FOUND) {
		t.Fatalf("want an identity refusal, got %s", result.String())
	}
	// Both types render as "Wheel", so the message has to fall back to the
	// full identities rather than read `"Wheel" does not match "Wheel"`.
	if strings.Contains(result.String(), `"Wheel" does not match relation target "Wheel"`) {
		t.Errorf("the message cannot distinguish the two types: %s", result.String())
	}
	if !strings.Contains(result.String(), "elsewhere.yammm:Wheel") {
		t.Errorf("the message does not name the offending identity: %s", result.String())
	}
	if insts := g.Snapshot().InstancesOf(mustTypeID(t, s, "Car")); len(insts) != 1 {
		t.Fatalf("the refused record installed (%d cars, want just the control)", len(insts))
	}
}

// TestComposedChild_KeyRule_IsTheSameOnBothPaths pins the second asymmetry: a
// composed child's primary key is checked against its own properties on the
// inline and the streamed path alike, the rule Add already applied at a root.
func TestComposedChild_KeyRule_IsTheSameOnBothPaths(t *testing.T) {
	t.Parallel()
	s := testSchemaWithComposition(t) // Parent -> (many) Child, Child has a PK
	childID := mustTypeID(t, s, "Child")
	parentID := mustTypeID(t, s, "Parent")
	ctx := t.Context()

	forged := func() *instance.ValidInstance {
		// The key says c1, the property says c2.
		return instancetest.VI(
			"Child",
			instancetest.TypeID(childID),
			instancetest.PK("c1"),
			instancetest.Props(map[string]any{"id": "c2", "name": "forged"}),
		)
	}

	// An empty key on a keyed part type is the shape that made two keyless
	// siblings collide on "[]" before the rule reached the composed paths.
	keyless := func() *instance.ValidInstance {
		return instancetest.VI(
			"Child",
			instancetest.TypeID(childID),
			instancetest.PK(),
			instancetest.Props(map[string]any{"name": "keyless"}),
		)
	}

	inlineKeyless := graph.New(s)
	if r := inlineKeyless.Add(ctx, instancetest.VI(
		"Parent",
		instancetest.TypeID(parentID),
		instancetest.PK("p1"),
		instancetest.Props(map[string]any{"id": "p1"}),
		instancetest.Composed(map[string]immutable.Value{
			"children": immutable.Wrap([]any{keyless(), keyless()}),
		}),
	)); r.OK() {
		t.Fatal("inline: keyless children of a keyed part type were accepted")
	} else {
		assertHasCode(t, r, diag.E_GRAPH_INVALID_PK)
	}

	streamedKeyless := graph.New(s)
	if r := streamedKeyless.Add(ctx, instancetest.VI(
		"Parent",
		instancetest.TypeID(parentID),
		instancetest.PK("p1"),
		instancetest.Props(map[string]any{"id": "p1"}),
	)); !r.OK() {
		t.Fatalf("parent add: %s", r.String())
	}
	if r := streamedKeyless.AddComposed(ctx, mustTypeID(t, s, "Parent"), `["p1"]`, "children", keyless()); r.OK() {
		t.Fatal("streamed: a keyless child of a keyed part type was accepted")
	} else {
		assertHasCode(t, r, diag.E_GRAPH_INVALID_PK)
	}

	inline := graph.New(s)
	res := inline.Add(ctx, instancetest.VI(
		"Parent",
		instancetest.TypeID(parentID),
		instancetest.PK("p1"),
		instancetest.Props(map[string]any{"id": "p1"}),
		instancetest.Composed(map[string]immutable.Value{
			"children": immutable.Wrap([]any{forged()}),
		}),
	))
	if res.OK() {
		t.Fatal("inline: a forged composed-child key was accepted")
	}
	assertHasCode(t, res, diag.E_GRAPH_INVALID_PK)

	streamed := graph.New(s)
	if r := streamed.Add(ctx, instancetest.VI(
		"Parent",
		instancetest.TypeID(parentID),
		instancetest.PK("p1"),
		instancetest.Props(map[string]any{"id": "p1"}),
	)); !r.OK() {
		t.Fatalf("parent add: %s", r.String())
	}
	res = streamed.AddComposed(ctx, mustTypeID(t, s, "Parent"), `["p1"]`, "children", forged())
	if res.OK() {
		t.Fatal("streamed: a forged composed-child key was accepted")
	}
	assertHasCode(t, res, diag.E_GRAPH_INVALID_PK)
}

// TestComposedChild_KeylessSiblings_AreNotDuplicates pins the false positive
// the streamed scan carried: every keyless child renders "[]", so comparing
// them reported a collision on a key neither child holds.
func TestComposedChild_KeylessSiblings_AreNotDuplicates(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	// A (many) slot of keyless children, streamed one at a time.
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
	if r := g.Add(ctx, instancetest.VI(
		"Car",
		instancetest.TypeID(mustTypeID(t, sm, "Car")),
		instancetest.PK("v1"),
		instancetest.Props(map[string]any{"vin": "v1"}),
	)); !r.OK() {
		t.Fatalf("parent add: %s", r.String())
	}

	for _, position := range []string{"left", "right"} {
		child := instancetest.VI(
			"Wheel",
			instancetest.TypeID(mustTypeID(t, sm, "Wheel")),
			instancetest.Props(map[string]any{"position": position}),
		)
		if r := g.AddComposed(ctx, mustTypeID(t, sm, "Car"), `["v1"]`, "WHEELS", child); !r.OK() {
			t.Fatalf("keyless sibling %q was rejected: %s", position, r.String())
		}
	}

	car := g.Snapshot().InstancesOf(mustTypeID(t, sm, "Car"))[0]
	if n := car.ComposedCount("WHEELS"); n != 2 {
		t.Errorf("keyless siblings attached = %d, want 2", n)
	}
}
