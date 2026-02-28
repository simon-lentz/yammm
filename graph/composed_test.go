package graph

import (
	"context"
	"errors"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/schema/build"
)

// AddComposed Tests
//
// These tests verify the AddComposed method for streaming composed children
// after their parent has been added to the graph.

func TestAddComposed_OneCardinality_Success(t *testing.T) {
	// Add single child to (one) composition
	s := testSchemaWithOneComposition(t) // Parent -> Child (one)
	g := New(s)
	ctx := t.Context()

	// Add parent first
	parent := mustValidInstance(t, s, "Parent",
		[]any{"p1"}, map[string]any{"name": "Parent 1"})

	if _, err := g.Add(ctx, parent); err != nil {
		t.Fatalf("Add parent error: %v", err)
	}

	// Add child via AddComposed
	child := mustValidPartInstance(t, s, "Child",
		[]any{"c1"}, map[string]any{"name": "Child 1"})

	result, err := g.AddComposed(ctx, "Parent", FormatKey("p1"), "child", child)
	if err != nil {
		t.Fatalf("AddComposed error: %v", err)
	}
	if !result.OK() {
		t.Errorf("AddComposed should succeed: %s", result.String())
	}

	// Verify child is attached
	snap := g.Snapshot()
	parents := snap.InstancesOf("Parent")
	if len(parents) != 1 {
		t.Fatalf("Expected 1 parent, got %d", len(parents))
	}

	assertComposedCount(t, parents[0], "child", 1)
}

func TestAddComposed_OneCardinality_Duplicate(t *testing.T) {
	// Second child → E_DUPLICATE_COMPOSED_PK
	s := testSchemaWithOneComposition(t)
	g := New(s)
	ctx := t.Context()

	// Add parent
	parent := mustValidInstance(t, s, "Parent",
		[]any{"p1"}, map[string]any{"name": "Parent 1"})

	if _, err := g.Add(ctx, parent); err != nil {
		t.Fatalf("Add parent error: %v", err)
	}

	// Add first child
	child1 := mustValidPartInstance(t, s, "Child",
		[]any{"c1"}, map[string]any{"name": "Child 1"})

	if _, err := g.AddComposed(ctx, "Parent", FormatKey("p1"), "child", child1); err != nil {
		t.Fatalf("AddComposed child1 error: %v", err)
	}

	// Try to add second child (should fail for (one) cardinality)
	child2 := mustValidPartInstance(t, s, "Child",
		[]any{"c2"}, map[string]any{"name": "Child 2"})

	result, err := g.AddComposed(ctx, "Parent", FormatKey("p1"), "child", child2)
	if err != nil {
		t.Fatalf("AddComposed child2 error: %v", err)
	}

	if result.OK() {
		t.Error("AddComposed should fail for (one) cardinality with existing child")
	}

	hasCode := false
	for issue := range result.Issues() {
		if issue.Code() == diag.E_DUPLICATE_COMPOSED_PK {
			hasCode = true
			break
		}
	}
	if !hasCode {
		t.Error("Expected E_DUPLICATE_COMPOSED_PK diagnostic")
	}
}

func TestAddComposed_ManyWithPK_Success(t *testing.T) {
	// Multiple children with different PKs
	s := testSchemaWithComposition(t) // Parent -> Child (many)
	g := New(s)
	ctx := t.Context()

	// Add parent
	parent := mustValidInstance(t, s, "Parent",
		[]any{"p1"}, map[string]any{"name": "Parent 1"})

	if _, err := g.Add(ctx, parent); err != nil {
		t.Fatalf("Add parent error: %v", err)
	}

	// Add multiple children
	for _, id := range []string{"c1", "c2", "c3"} {
		child := mustValidPartInstance(t, s, "Child",
			[]any{id}, map[string]any{"name": "Child " + id})

		result, err := g.AddComposed(ctx, "Parent", FormatKey("p1"), "children", child)
		if err != nil {
			t.Fatalf("AddComposed %s error: %v", id, err)
		}
		if !result.OK() {
			t.Errorf("AddComposed %s should succeed: %s", id, result.String())
		}
	}

	// Verify all children attached
	snap := g.Snapshot()
	parents := snap.InstancesOf("Parent")
	assertComposedCount(t, parents[0], "children", 3)
}

func TestAddComposed_ManyWithPK_Duplicate(t *testing.T) {
	// Same PK → E_DUPLICATE_COMPOSED_PK
	s := testSchemaWithComposition(t)
	g := New(s)
	ctx := t.Context()

	// Add parent
	parent := mustValidInstance(t, s, "Parent",
		[]any{"p1"}, map[string]any{"name": "Parent 1"})

	if _, err := g.Add(ctx, parent); err != nil {
		t.Fatalf("Add parent error: %v", err)
	}

	// Add first child
	child1 := mustValidPartInstance(t, s, "Child",
		[]any{"c1"}, map[string]any{"name": "Child 1"})

	if _, err := g.AddComposed(ctx, "Parent", FormatKey("p1"), "children", child1); err != nil {
		t.Fatalf("AddComposed child1 error: %v", err)
	}

	// Try to add child with same PK
	child2 := mustValidPartInstance(t, s, "Child",
		[]any{"c1"}, map[string]any{"name": "Child 1 Duplicate"})

	result, err := g.AddComposed(ctx, "Parent", FormatKey("p1"), "children", child2)
	if err != nil {
		t.Fatalf("AddComposed child2 error: %v", err)
	}

	if result.OK() {
		t.Error("AddComposed should fail for duplicate child PK")
	}

	hasCode := false
	for issue := range result.Issues() {
		if issue.Code() == diag.E_DUPLICATE_COMPOSED_PK {
			hasCode = true
			break
		}
	}
	if !hasCode {
		t.Error("Expected E_DUPLICATE_COMPOSED_PK diagnostic")
	}
}

func TestAddComposed_ManyWithoutPK_Appends(t *testing.T) {
	// PK-less children always append (positional identity)
	s := testSchemaWithPKLessChild(t)
	g := New(s)
	ctx := t.Context()

	// Add container
	container := mustValidInstance(t, s, "Container",
		[]any{"box1"}, map[string]any{"name": "Box 1"})

	if _, err := g.Add(ctx, container); err != nil {
		t.Fatalf("Add container error: %v", err)
	}

	// Add multiple PK-less children - all should succeed
	for i := range 3 {
		item := mustValidPKLessInstance(t, s, "Item",
			map[string]any{"value": "item"})

		result, err := g.AddComposed(ctx, "Container", FormatKey("box1"), "items", item)
		if err != nil {
			t.Fatalf("AddComposed item %d error: %v", i, err)
		}
		if !result.OK() {
			t.Errorf("AddComposed item %d should succeed: %s", i, result.String())
		}
	}

	// Verify all items attached
	snap := g.Snapshot()
	containers := snap.InstancesOf("Container")
	assertComposedCount(t, containers[0], "items", 3)
}

func TestAddComposed_TypeMismatch(t *testing.T) {
	// Wrong child type → E_GRAPH_INVALID_COMPOSITION
	s := testSchemaWithComposition(t)
	g := New(s)
	ctx := t.Context()

	// Add parent
	parent := mustValidInstance(t, s, "Parent",
		[]any{"p1"}, map[string]any{"name": "Parent 1"})

	if _, err := g.Add(ctx, parent); err != nil {
		t.Fatalf("Add parent error: %v", err)
	}

	// Try to add Parent as child (wrong type)
	wrongChild := mustValidInstance(t, s, "Parent",
		[]any{"p2"}, map[string]any{"name": "Parent 2"})

	result, err := g.AddComposed(ctx, "Parent", FormatKey("p1"), "children", wrongChild)
	if err != nil {
		t.Fatalf("AddComposed error: %v", err)
	}

	if result.OK() {
		t.Error("AddComposed should fail for wrong child type")
	}

	hasCode := false
	for issue := range result.Issues() {
		if issue.Code() == diag.E_GRAPH_INVALID_COMPOSITION {
			hasCode = true
			break
		}
	}
	if !hasCode {
		t.Error("Expected E_GRAPH_INVALID_COMPOSITION diagnostic")
	}
}

func TestAddComposed_ParentNotFound(t *testing.T) {
	// Missing parent → E_GRAPH_PARENT_NOT_FOUND
	s := testSchemaWithComposition(t)
	g := New(s)
	ctx := t.Context()

	// Don't add parent - try to add child directly
	child := mustValidPartInstance(t, s, "Child",
		[]any{"c1"}, map[string]any{"name": "Child 1"})

	result, err := g.AddComposed(ctx, "Parent", FormatKey("missing"), "children", child)
	if err != nil {
		t.Fatalf("AddComposed error: %v", err)
	}

	if result.OK() {
		t.Error("AddComposed should fail for missing parent")
	}

	hasCode := false
	for issue := range result.Issues() {
		if issue.Code() == diag.E_GRAPH_PARENT_NOT_FOUND {
			hasCode = true
			break
		}
	}
	if !hasCode {
		t.Error("Expected E_GRAPH_PARENT_NOT_FOUND diagnostic")
	}
}

func TestAddComposed_ParentTypeNotFound(t *testing.T) {
	// Unknown parent type → E_GRAPH_TYPE_NOT_FOUND
	s := testSchemaWithComposition(t)
	g := New(s)
	ctx := t.Context()

	child := mustValidPartInstance(t, s, "Child",
		[]any{"c1"}, map[string]any{"name": "Child 1"})

	result, err := g.AddComposed(ctx, "NonExistentType", FormatKey("x"), "children", child)
	if err != nil {
		t.Fatalf("AddComposed error: %v", err)
	}

	if result.OK() {
		t.Error("AddComposed should fail for unknown parent type")
	}

	hasCode := false
	for issue := range result.Issues() {
		if issue.Code() == diag.E_GRAPH_TYPE_NOT_FOUND {
			hasCode = true
			break
		}
	}
	if !hasCode {
		t.Error("Expected E_GRAPH_TYPE_NOT_FOUND diagnostic")
	}
}

func TestAddComposed_NotComposition(t *testing.T) {
	// Relation is association → E_GRAPH_INVALID_COMPOSITION
	s := testSchemaWithAssociation(t) // Person -> Company (association, not composition)
	g := New(s)
	ctx := t.Context()

	// Add Person
	person := mustValidInstance(t, s, "Person",
		[]any{"alice"}, map[string]any{"name": "Alice"})

	if _, err := g.Add(ctx, person); err != nil {
		t.Fatalf("Add person error: %v", err)
	}

	// Try to add Company as composed child (employer is association, not composition)
	company := mustValidInstance(t, s, "Company",
		[]any{"acme"}, map[string]any{"name": "Acme"})

	result, err := g.AddComposed(ctx, "Person", FormatKey("alice"), "employer", company)
	if err != nil {
		t.Fatalf("AddComposed error: %v", err)
	}

	if result.OK() {
		t.Error("AddComposed should fail for association relation")
	}

	hasCode := false
	for issue := range result.Issues() {
		if issue.Code() == diag.E_GRAPH_INVALID_COMPOSITION {
			hasCode = true
			break
		}
	}
	if !hasCode {
		t.Error("Expected E_GRAPH_INVALID_COMPOSITION diagnostic")
	}
}

func TestAddComposed_AfterAdd_Mixed(t *testing.T) {
	// Inline + streamed children coexist
	// This tests that children added via AddComposed work alongside
	// children that were inline in the ValidInstance during Add()
	s := testSchemaWithComposition(t)
	g := New(s)
	ctx := t.Context()

	// Add parent (without inline children for now)
	parent := mustValidInstance(t, s, "Parent",
		[]any{"p1"}, map[string]any{"name": "Parent 1"})

	if _, err := g.Add(ctx, parent); err != nil {
		t.Fatalf("Add parent error: %v", err)
	}

	// Add child via AddComposed
	child1 := mustValidPartInstance(t, s, "Child",
		[]any{"c1"}, map[string]any{"name": "Child 1"})

	if _, err := g.AddComposed(ctx, "Parent", FormatKey("p1"), "children", child1); err != nil {
		t.Fatalf("AddComposed child1 error: %v", err)
	}

	// Add another child
	child2 := mustValidPartInstance(t, s, "Child",
		[]any{"c2"}, map[string]any{"name": "Child 2"})

	if _, err := g.AddComposed(ctx, "Parent", FormatKey("p1"), "children", child2); err != nil {
		t.Fatalf("AddComposed child2 error: %v", err)
	}

	// Verify both children are present
	snap := g.Snapshot()
	parents := snap.InstancesOf("Parent")
	assertComposedCount(t, parents[0], "children", 2)

	// Verify child details
	children := parents[0].Composed("children")
	if len(children) != 2 {
		t.Fatalf("Expected 2 children, got %d", len(children))
	}
}

func TestAddComposed_NilChild(t *testing.T) {
	// Nil child → ErrNilChild
	s := testSchemaWithComposition(t)
	g := New(s)
	ctx := t.Context()

	// Add parent
	parent := mustValidInstance(t, s, "Parent",
		[]any{"p1"}, map[string]any{"name": "Parent 1"})

	if _, err := g.Add(ctx, parent); err != nil {
		t.Fatalf("Add parent error: %v", err)
	}

	// Try to add nil child
	_, err := g.AddComposed(ctx, "Parent", FormatKey("p1"), "children", nil)
	if !errors.Is(err, ErrNilChild) {
		t.Errorf("AddComposed(nil) should return ErrNilChild, got %v", err)
	}
}

func TestAddComposed_SchemaMismatch(t *testing.T) {
	// Child validated against a different schema should fail
	s := testSchemaWithComposition(t)
	g := New(s)
	ctx := t.Context()

	// Add parent from graph's schema
	parent := mustValidInstance(t, s, "Parent",
		[]any{"p1"}, map[string]any{"name": "Parent 1"})

	if _, err := g.Add(ctx, parent); err != nil {
		t.Fatalf("Add parent error: %v", err)
	}

	// Create a completely different schema
	otherSchema, _ := build.NewBuilder().
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
	_, err := g.AddComposed(ctx, "Parent", FormatKey("p1"), "children", otherChild)
	if !errors.Is(err, ErrSchemaMismatch) {
		t.Errorf("AddComposed with mismatched schema should return ErrSchemaMismatch, got %v", err)
	}
}

func TestAddComposed_NilReceiver(t *testing.T) {
	// Nil graph → ErrNilGraph
	var g *Graph
	ctx := t.Context()

	// Create a valid schema and child to pass to AddComposed
	s := testSchemaWithComposition(t)
	child := mustValidPartInstance(t, s, "Child",
		[]any{"c1"}, map[string]any{"name": "Child 1"})

	_, err := g.AddComposed(ctx, "Parent", FormatKey("p1"), "children", child)
	if !errors.Is(err, ErrNilGraph) {
		t.Errorf("AddComposed on nil Graph should return ErrNilGraph, got %v", err)
	}
}

func TestAddComposed_ContextCancelled(t *testing.T) {
	// Cancelled context → context.Canceled
	s := testSchemaWithComposition(t)
	g := New(s)
	ctx, cancel := context.WithCancel(t.Context())
	cancel() // Cancel immediately

	child := mustValidPartInstance(t, s, "Child",
		[]any{"c1"}, map[string]any{"name": "Child 1"})

	_, err := g.AddComposed(ctx, "Parent", FormatKey("p1"), "children", child)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("AddComposed with canceled context should return context.Canceled, got %v", err)
	}
}

func TestAddComposed_ErrorDetails(t *testing.T) {
	// Verify diagnostic details (parent_type, pk, relation)
	s := testSchemaWithComposition(t)
	g := New(s)
	ctx := t.Context()

	// Don't add parent - trigger E_GRAPH_PARENT_NOT_FOUND
	child := mustValidPartInstance(t, s, "Child",
		[]any{"c1"}, map[string]any{"name": "Child 1"})

	result, err := g.AddComposed(ctx, "Parent", FormatKey("p1"), "children", child)
	if err != nil {
		t.Fatalf("AddComposed error: %v", err)
	}

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
	g := New(s)
	ctx := t.Context()

	// Add parent
	parent := mustValidInstance(t, s, "Parent",
		[]any{"p1"}, map[string]any{"name": "Parent 1"})
	_, err := g.Add(ctx, parent)
	if err != nil {
		t.Fatalf("Add parent error: %v", err)
	}

	// Add first child
	child1 := mustValidPartInstance(t, s, "Child",
		[]any{"c1"}, map[string]any{"name": "Child 1"})
	_, err = g.AddComposed(ctx, "Parent", FormatKey("p1"), "child", child1)
	if err != nil {
		t.Fatalf("AddComposed child1 error: %v", err)
	}

	// Try to add second child (should fail for (one) cardinality)
	child2 := mustValidPartInstance(t, s, "Child",
		[]any{"c2"}, map[string]any{"name": "Child 2"})
	result, err := g.AddComposed(ctx, "Parent", FormatKey("p1"), "child", child2)
	if err != nil {
		t.Fatalf("AddComposed child2 error: %v", err)
	}

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
	g := New(s)
	ctx := t.Context()

	// Add parent
	parent := mustValidInstance(t, s, "Parent",
		[]any{"p1"}, map[string]any{"name": "Parent 1"})
	_, err := g.Add(ctx, parent)
	if err != nil {
		t.Fatalf("Add parent error: %v", err)
	}

	// Add first child
	child1 := mustValidPartInstance(t, s, "Child",
		[]any{"c1"}, map[string]any{"name": "Child 1"})
	_, err = g.AddComposed(ctx, "Parent", FormatKey("p1"), "children", child1)
	if err != nil {
		t.Fatalf("AddComposed child1 error: %v", err)
	}

	// Try to add child with same PK
	child2 := mustValidPartInstance(t, s, "Child",
		[]any{"c1"}, map[string]any{"name": "Child 1 Duplicate"})
	result, err := g.AddComposed(ctx, "Parent", FormatKey("p1"), "children", child2)
	if err != nil {
		t.Fatalf("AddComposed child2 error: %v", err)
	}

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
	// Verify Instance and Conflict are different instances
	if dup.Instance.PrimaryKey().String() != dup.Conflict.PrimaryKey().String() {
		t.Errorf("Instance and Conflict should have same PK for this test")
	}
}
