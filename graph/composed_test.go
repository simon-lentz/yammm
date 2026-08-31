package graph_test

import (
	"context"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// AddComposed Tests
//
// These tests verify the AddComposed method for streaming composed children
// after their parent has been added to the graph.

func TestAddComposed_OneCardinality_Success(t *testing.T) {
	// Add single child to (one) composition
	s := testSchemaWithOneComposition(t) // Parent -> Child (one)
	g := graph.New(s)
	ctx := t.Context()

	// Add parent first
	parent := mustValidInstance(t, s, "Parent",
		[]any{"p1"}, map[string]any{"name": "Parent 1"})

	g.Add(ctx, parent)

	// Add child via AddComposed
	child := mustValidPartInstance(t, s, "Child",
		[]any{"c1"}, map[string]any{"name": "Child 1"})

	result := g.AddComposed(ctx, mustTypeID(t, s, "Parent"), graph.FormatKey("p1"), "child", child)
	if err := result.Err(); err != nil {
		t.Errorf("AddComposed should succeed: %v", err)
	}

	// Verify child is attached
	snap := g.Snapshot()
	parents := snap.InstancesOf(mustTypeID(t, s, "Parent"))
	if len(parents) != 1 {
		t.Fatalf("Expected 1 parent, got %d", len(parents))
	}

	assertComposedCount(t, parents[0], "child", 1)
}

func TestAddComposed_OneCardinality_Duplicate(t *testing.T) {
	// Second child → E_DUPLICATE_COMPOSED_PK
	s := testSchemaWithOneComposition(t)
	g := graph.New(s)
	ctx := t.Context()

	// Add parent
	parent := mustValidInstance(t, s, "Parent",
		[]any{"p1"}, map[string]any{"name": "Parent 1"})

	g.Add(ctx, parent)

	// Add first child
	child1 := mustValidPartInstance(t, s, "Child",
		[]any{"c1"}, map[string]any{"name": "Child 1"})

	g.AddComposed(ctx, mustTypeID(t, s, "Parent"), graph.FormatKey("p1"), "child", child1)

	// Try to add second child (should fail for (one) cardinality)
	child2 := mustValidPartInstance(t, s, "Child",
		[]any{"c2"}, map[string]any{"name": "Child 2"})

	result := g.AddComposed(ctx, mustTypeID(t, s, "Parent"), graph.FormatKey("p1"), "child", child2)

	if result.OK() {
		t.Error("AddComposed should fail for (one) cardinality with existing child")
	}

	assertHasCode(t, result, diag.E_DUPLICATE_COMPOSED_PK)
}

func TestAddComposed_ManyWithPK_Success(t *testing.T) {
	// Multiple children with different PKs
	s := testSchemaWithComposition(t) // Parent -> Child (many)
	g := graph.New(s)
	ctx := t.Context()

	// Add parent
	parent := mustValidInstance(t, s, "Parent",
		[]any{"p1"}, map[string]any{"name": "Parent 1"})

	g.Add(ctx, parent)

	// Add multiple children
	for _, id := range []string{"c1", "c2", "c3"} {
		child := mustValidPartInstance(t, s, "Child",
			[]any{id}, map[string]any{"name": "Child " + id})

		result := g.AddComposed(ctx, mustTypeID(t, s, "Parent"), graph.FormatKey("p1"), "children", child)
		if err := result.Err(); err != nil {
			t.Errorf("AddComposed %s should succeed: %v", id, err)
		}
	}

	// Verify all children attached
	snap := g.Snapshot()
	parents := snap.InstancesOf(mustTypeID(t, s, "Parent"))
	assertComposedCount(t, parents[0], "children", 3)
}

func TestAddComposed_ManyWithPK_Duplicate(t *testing.T) {
	// Same PK → E_DUPLICATE_COMPOSED_PK
	s := testSchemaWithComposition(t)
	g := graph.New(s)
	ctx := t.Context()

	// Add parent
	parent := mustValidInstance(t, s, "Parent",
		[]any{"p1"}, map[string]any{"name": "Parent 1"})

	g.Add(ctx, parent)

	// Add first child
	child1 := mustValidPartInstance(t, s, "Child",
		[]any{"c1"}, map[string]any{"name": "Child 1"})

	g.AddComposed(ctx, mustTypeID(t, s, "Parent"), graph.FormatKey("p1"), "children", child1)

	// Try to add child with same PK
	child2 := mustValidPartInstance(t, s, "Child",
		[]any{"c1"}, map[string]any{"name": "Child 1 Duplicate"})

	result := g.AddComposed(ctx, mustTypeID(t, s, "Parent"), graph.FormatKey("p1"), "children", child2)

	if result.OK() {
		t.Error("AddComposed should fail for duplicate child PK")
	}

	assertHasCode(t, result, diag.E_DUPLICATE_COMPOSED_PK)
}

func TestAddComposed_ManyWithoutPK_Appends(t *testing.T) {
	// PK-less children always append (positional identity)
	s := testSchemaWithPKLessChild(t)
	g := graph.New(s)
	ctx := t.Context()

	// Add container
	container := mustValidInstance(t, s, "Container",
		[]any{"box1"}, map[string]any{"name": "Box 1"})

	g.Add(ctx, container)

	// Add multiple PK-less children - all should succeed
	for i := range 3 {
		item := mustValidPKLessInstance(t, s, "Item",
			map[string]any{"value": "item"})

		result := g.AddComposed(ctx, mustTypeID(t, s, "Container"), graph.FormatKey("box1"), "items", item)
		if err := result.Err(); err != nil {
			t.Errorf("AddComposed item %d should succeed: %v", i, err)
		}
	}

	// Verify all items attached
	snap := g.Snapshot()
	containers := snap.InstancesOf(mustTypeID(t, s, "Container"))
	assertComposedCount(t, containers[0], "items", 3)
}

func TestAddComposed_TypeMismatch(t *testing.T) {
	// Wrong child type → E_GRAPH_INVALID_COMPOSITION
	s := testSchemaWithComposition(t)
	g := graph.New(s)
	ctx := t.Context()

	// Add parent
	parent := mustValidInstance(t, s, "Parent",
		[]any{"p1"}, map[string]any{"name": "Parent 1"})

	g.Add(ctx, parent)

	// Try to add Parent as child (wrong type)
	wrongChild := mustValidInstance(t, s, "Parent",
		[]any{"p2"}, map[string]any{"name": "Parent 2"})

	result := g.AddComposed(ctx, mustTypeID(t, s, "Parent"), graph.FormatKey("p1"), "children", wrongChild)

	if result.OK() {
		t.Error("AddComposed should fail for wrong child type")
	}

	assertHasCode(t, result, diag.E_GRAPH_INVALID_COMPOSITION)
}

func TestAddComposed_ParentNotFound(t *testing.T) {
	// Missing parent → E_GRAPH_PARENT_NOT_FOUND
	s := testSchemaWithComposition(t)
	g := graph.New(s)
	ctx := t.Context()

	// Don't add parent - try to add child directly
	child := mustValidPartInstance(t, s, "Child",
		[]any{"c1"}, map[string]any{"name": "Child 1"})

	result := g.AddComposed(ctx, mustTypeID(t, s, "Parent"), graph.FormatKey("missing"), "children", child)

	if result.OK() {
		t.Error("AddComposed should fail for missing parent")
	}

	assertHasCode(t, result, diag.E_GRAPH_PARENT_NOT_FOUND)
}

// TestAddComposed_ParentTypeNotFound drives an identity no schema in this
// graph's closure declares. A rendered name could not express this case
// unambiguously, which is why the parameter is an identity.
func TestAddComposed_ParentTypeNotFound(t *testing.T) {
	s := testSchemaWithComposition(t)
	g := graph.New(s)
	ctx := t.Context()

	other, res := schema.NewBuilder().
		WithName("elsewhere").
		WithSourceID(location.MustNewSourceID("test://elsewhere-parent.yammm")).
		AddType("Parent").
		WithPrimaryKey("id", schema.StringConstraint{}).
		Done().
		Build()
	if res.HasErrors() {
		t.Fatalf("build second schema: %s", res.String())
	}

	child := mustValidPartInstance(t, s, "Child",
		[]any{"c1"}, map[string]any{"name": "Child 1"})

	result := g.AddComposed(ctx, mustTypeID(t, other, "Parent"), graph.FormatKey("x"), "children", child)
	if result.OK() {
		t.Error("AddComposed should fail for a parent identity outside the closure")
	}
	// The identity resolves before the instance does, so a wrong type and a
	// wrong key stay distinguishable.
	assertHasCode(t, result, diag.E_GRAPH_TYPE_NOT_FOUND)
}

// TestAddComposed_ZeroParentTypeID pins the zero identity a caller reaches by
// forgetting to set one: TypeByID reports false for it, so it is named as a
// type problem rather than reported as a missing parent.
func TestAddComposed_ZeroParentTypeID(t *testing.T) {
	s := testSchemaWithComposition(t)
	g := graph.New(s)
	ctx := t.Context()

	child := mustValidPartInstance(t, s, "Child", []any{"c1"}, map[string]any{"name": "Child 1"})
	result := g.AddComposed(ctx, schema.TypeID{}, graph.FormatKey("p1"), "children", child)
	if result.OK() {
		t.Error("a zero parent TypeID was accepted")
	}
	assertHasCode(t, result, diag.E_GRAPH_TYPE_NOT_FOUND)
}

// TestAddComposed_ParentInstanceNotFound is the other arm: the type resolves
// and no instance carries the key.
func TestAddComposed_ParentInstanceNotFound(t *testing.T) {
	s := testSchemaWithComposition(t)
	g := graph.New(s)
	ctx := t.Context()

	child := mustValidPartInstance(t, s, "Child", []any{"c1"}, map[string]any{"name": "Child 1"})
	result := g.AddComposed(ctx, mustTypeID(t, s, "Parent"), graph.FormatKey("nobody"), "children", child)
	if result.OK() {
		t.Error("AddComposed should fail when no parent carries the key")
	}
	assertHasCode(t, result, diag.E_GRAPH_PARENT_NOT_FOUND)
}

func TestAddComposed_NotComposition(t *testing.T) {
	// Relation is association → E_GRAPH_INVALID_COMPOSITION
	s := testSchemaWithAssociation(t) // Person -> Company (association, not composition)
	g := graph.New(s)
	ctx := t.Context()

	// Add Person
	person := mustValidInstance(t, s, "Person",
		[]any{"alice"}, map[string]any{"name": "Alice"})

	g.Add(ctx, person)

	// Try to add Company as composed child (employer is association, not composition)
	company := mustValidInstance(t, s, "Company",
		[]any{"acme"}, map[string]any{"name": "Acme"})

	result := g.AddComposed(ctx, mustTypeID(t, s, "Person"), graph.FormatKey("alice"), "employer", company)

	if result.OK() {
		t.Error("AddComposed should fail for association relation")
	}

	assertHasCode(t, result, diag.E_GRAPH_INVALID_COMPOSITION)
}

func TestAddComposed_AfterAdd_Mixed(t *testing.T) {
	// Inline + streamed children coexist
	// This tests that children added via AddComposed work alongside
	// children that were inline in the ValidInstance during Add()
	s := testSchemaWithComposition(t)
	g := graph.New(s)
	ctx := t.Context()

	// Add parent (without inline children for now)
	parent := mustValidInstance(t, s, "Parent",
		[]any{"p1"}, map[string]any{"name": "Parent 1"})

	g.Add(ctx, parent)

	// Add child via AddComposed
	child1 := mustValidPartInstance(t, s, "Child",
		[]any{"c1"}, map[string]any{"name": "Child 1"})

	g.AddComposed(ctx, mustTypeID(t, s, "Parent"), graph.FormatKey("p1"), "children", child1)

	// Add another child
	child2 := mustValidPartInstance(t, s, "Child",
		[]any{"c2"}, map[string]any{"name": "Child 2"})

	g.AddComposed(ctx, mustTypeID(t, s, "Parent"), graph.FormatKey("p1"), "children", child2)

	// Verify both children are present
	snap := g.Snapshot()
	parents := snap.InstancesOf(mustTypeID(t, s, "Parent"))
	assertComposedCount(t, parents[0], "children", 2)

	// Verify child details
	children := parents[0].Composed("children")
	if len(children) != 2 {
		t.Fatalf("Expected 2 children, got %d", len(children))
	}
}

func TestAddComposed_NilChild(t *testing.T) {
	// Nil child → panic
	s := testSchemaWithComposition(t)
	g := graph.New(s)
	ctx := t.Context()

	// Add parent
	parent := mustValidInstance(t, s, "Parent",
		[]any{"p1"}, map[string]any{"name": "Parent 1"})

	g.Add(ctx, parent)

	// Try to add nil child
	defer func() {
		if r := recover(); r == nil {
			t.Error("AddComposed(nil child) should panic")
		}
	}()
	g.AddComposed(ctx, mustTypeID(t, s, "Parent"), graph.FormatKey("p1"), "children", nil)
}

func TestAddComposed_SchemaMismatch(t *testing.T) {
	// Child validated against a different schema should panic
	s := testSchemaWithComposition(t)
	g := graph.New(s)
	ctx := t.Context()

	// Add parent from graph's schema
	parent := mustValidInstance(t, s, "Parent",
		[]any{"p1"}, map[string]any{"name": "Parent 1"})

	g.Add(ctx, parent)

	// Create a completely different schema
	otherSchema, _ := schema.NewBuilder().
		WithName("other").
		WithSourceID(location.MustNewSourceID("test://other.yammm")).
		AddType("OtherChild").
		AsPart().
		WithPrimaryKey("id", schema.StringConstraint{}).
		Done().
		Build()

	otherChildType, _ := otherSchema.Type("OtherChild")
	otherChild := instance.NewValidInstance(
		"OtherChild",
		otherChildType.ID(), // TypeID points to different schema
		immutable.WrapKey([]any{"oc1"}),
		immutable.WrapProperties(map[string]any{}),
		nil, nil, nil,
	)

	// Try to add child from different schema
	defer func() {
		if r := recover(); r == nil {
			t.Error("AddComposed with mismatched schema should panic")
		}
	}()
	g.AddComposed(ctx, mustTypeID(t, s, "Parent"), graph.FormatKey("p1"), "children", otherChild)
}

func TestAddComposed_NilReceiver(t *testing.T) {
	// Nil graph → panic
	var g *graph.Graph
	ctx := t.Context()

	// Create a valid schema and child to pass to AddComposed
	s := testSchemaWithComposition(t)
	child := mustValidPartInstance(t, s, "Child",
		[]any{"c1"}, map[string]any{"name": "Child 1"})

	defer func() {
		if r := recover(); r == nil {
			t.Error("AddComposed on nil Graph should panic")
		}
	}()
	g.AddComposed(ctx, mustTypeID(t, s, "Parent"), graph.FormatKey("p1"), "children", child)
}

func TestAddComposed_ContextCancelled(t *testing.T) {
	// Cancelled context → Fatal diagnostic in result
	s := testSchemaWithComposition(t)
	g := graph.New(s)
	ctx, cancel := context.WithCancel(t.Context())
	cancel() // Cancel immediately

	child := mustValidPartInstance(t, s, "Child",
		[]any{"c1"}, map[string]any{"name": "Child 1"})

	result := g.AddComposed(ctx, mustTypeID(t, s, "Parent"), graph.FormatKey("p1"), "children", child)
	if result.OK() {
		t.Error("AddComposed with canceled context should produce a non-OK result")
	}

	hasFatal := false
	for issue := range result.Issues() {
		if issue.Severity() == diag.Fatal {
			hasFatal = true
			break
		}
	}
	if !hasFatal {
		t.Error("Expected Fatal diagnostic for canceled context")
	}
}

func TestAddComposed_ErrorDetails(t *testing.T) {
	// Verify diagnostic details (parent_type, pk, relation)
	s := testSchemaWithComposition(t)
	g := graph.New(s)
	ctx := t.Context()

	// Don't add parent - trigger E_GRAPH_PARENT_NOT_FOUND
	child := mustValidPartInstance(t, s, "Child",
		[]any{"c1"}, map[string]any{"name": "Child 1"})

	result := g.AddComposed(ctx, mustTypeID(t, s, "Parent"), graph.FormatKey("p1"), "children", child)

	// Check issue has expected details
	found := false
	for issue := range result.Issues() {
		if issue.Code() == diag.E_GRAPH_PARENT_NOT_FOUND {
			found = true
			// Verify details are present
			details := issue.Details()
			if len(details) == 0 {
				t.Error("Issue should have details")
				continue
			}

			hasType := false
			hasPK := false
			for _, d := range details {
				if d.Key == "type" {
					hasType = true
				}
				if d.Key == "pk" {
					hasPK = true
				}
			}
			if !hasType {
				t.Error("Issue should have 'type' detail")
			}
			if !hasPK {
				t.Error("Issue should have 'pk' detail")
			}
			break
		}
	}
	if !found {
		t.Error("Expected E_GRAPH_PARENT_NOT_FOUND issue not found")
	}
}

// Test that composed duplicates are recorded in Result.Duplicates()

func TestResult_Duplicates_IncludesComposedDuplicates_OneCardinality(t *testing.T) {
	// Verify that (one) cardinality violations are recorded in Result.Duplicates()
	s := testSchemaWithOneComposition(t)
	g := graph.New(s)
	ctx := t.Context()

	// Add parent
	parent := mustValidInstance(t, s, "Parent",
		[]any{"p1"}, map[string]any{"name": "Parent 1"})
	g.Add(ctx, parent)

	// Add first child
	child1 := mustValidPartInstance(t, s, "Child",
		[]any{"c1"}, map[string]any{"name": "Child 1"})
	g.AddComposed(ctx, mustTypeID(t, s, "Parent"), graph.FormatKey("p1"), "child", child1)

	// Try to add second child (should fail for (one) cardinality)
	child2 := mustValidPartInstance(t, s, "Child",
		[]any{"c2"}, map[string]any{"name": "Child 2"})
	result := g.AddComposed(ctx, mustTypeID(t, s, "Parent"), graph.FormatKey("p1"), "child", child2)

	if result.OK() {
		t.Fatal("AddComposed should fail for (one) cardinality with existing child")
	}

	// Verify Result.Duplicates() includes the composed duplicate
	snap := g.Snapshot()
	dups := snap.Duplicates()

	if len(dups) != 1 {
		t.Fatalf("Expected 1 duplicate, got %d", len(dups))
	}

	dup := dups[0]
	if dup.Diagnostic.Code() != diag.E_DUPLICATE_COMPOSED_PK {
		t.Errorf("Expected E_DUPLICATE_COMPOSED_PK, got %s", dup.Diagnostic.Code())
	}
	if dup.Instance == nil {
		t.Error("Duplicate.Instance should not be nil")
	}
	if dup.Conflict == nil {
		t.Error("Duplicate.Conflict should not be nil")
	}
}

func TestResult_Duplicates_IncludesComposedDuplicates_ManyWithPK(t *testing.T) {
	// Verify that (many) with duplicate PK is recorded in Result.Duplicates()
	s := testSchemaWithComposition(t)
	g := graph.New(s)
	ctx := t.Context()

	// Add parent
	parent := mustValidInstance(t, s, "Parent",
		[]any{"p1"}, map[string]any{"name": "Parent 1"})
	g.Add(ctx, parent)

	// Add first child
	child1 := mustValidPartInstance(t, s, "Child",
		[]any{"c1"}, map[string]any{"name": "Child 1"})
	g.AddComposed(ctx, mustTypeID(t, s, "Parent"), graph.FormatKey("p1"), "children", child1)

	// Try to add child with same PK
	child2 := mustValidPartInstance(t, s, "Child",
		[]any{"c1"}, map[string]any{"name": "Child 1 Duplicate"})
	result := g.AddComposed(ctx, mustTypeID(t, s, "Parent"), graph.FormatKey("p1"), "children", child2)

	if result.OK() {
		t.Fatal("AddComposed should fail for duplicate child PK")
	}

	// Verify Result.Duplicates() includes the composed duplicate
	snap := g.Snapshot()
	dups := snap.Duplicates()

	if len(dups) != 1 {
		t.Fatalf("Expected 1 duplicate, got %d", len(dups))
	}

	dup := dups[0]
	if dup.Diagnostic.Code() != diag.E_DUPLICATE_COMPOSED_PK {
		t.Errorf("Expected E_DUPLICATE_COMPOSED_PK, got %s", dup.Diagnostic.Code())
	}
	if dup.Instance == nil {
		t.Error("Duplicate.Instance should not be nil")
	}
	if dup.Conflict == nil {
		t.Error("Duplicate.Conflict should not be nil")
	}
	// The collision is on the key, so both carry it; what separates them is
	// the payload, and a record that returned the attached child as its own
	// rejected instance would carry no information.
	if dup.Instance.PrimaryKey().String() != dup.Conflict.PrimaryKey().String() {
		t.Error("Instance and Conflict should have the same PK for this test")
	}
	if dup.Instance == dup.Conflict {
		t.Error("Instance and Conflict are the same object")
	}
	instName, _ := dup.Instance.Property("name")
	conflictName, _ := dup.Conflict.Property("name")
	if instName.Unwrap() != "Child 1 Duplicate" {
		t.Errorf("Duplicate.Instance is not the rejected child: name=%v", instName.Unwrap())
	}
	if conflictName.Unwrap() != "Child 1" {
		t.Errorf("Duplicate.Conflict is not the attached child: name=%v", conflictName.Unwrap())
	}
}

// TestAdd_InheritingPartTypeChild_PassesTheKeyRule pins the composed-child key
// rule against inheritance, the one shape this package's corpus never declared:
// the rule reads the type's primary keys, and an inherited key has to be in
// that set or every validated child of an extending part type is refused.
func TestAdd_InheritingPartTypeChild_PassesTheKeyRule(t *testing.T) {
	ctx := t.Context()
	const src = `schema "fleet"

part type Base {
	id String primary
}

part type Wheel extends Base {
	position String required
}

type Car {
	vin String primary

	*-> WHEELS (many) Wheel
}
`
	s, res := schema.LoadString(ctx, src, "test://inheriting_part.yammm")
	if res.HasErrors() {
		t.Fatalf("schema load: %s", res.String())
	}

	v := instance.NewValidator(s)
	inst, vres := v.ValidateOne(ctx, "Car", instance.RawInstance{Properties: map[string]any{
		"vin": "v1",
		"wheels": []any{
			map[string]any{"id": "w1", "position": "left"},
			map[string]any{"id": "w2", "position": "right"},
		},
	}})
	if !vres.OK() {
		t.Fatalf("validation: %s", vres.String())
	}

	g := graph.New(s)
	if r := g.Add(ctx, inst); !r.OK() {
		t.Fatalf("a validated child of an extending part type was refused: %s", r.String())
	}
	car := g.Snapshot().InstancesOf(inst.TypeID())[0]
	if n := car.ComposedCount("WHEELS"); n != 2 {
		t.Fatalf("children attached = %d, want 2", n)
	}
}
