package graph

import (
	"context"
	"testing"

	"github.com/simon-lentz/yammm/instance"
)

// Ownership Isolation Tests for Graph Package
//
// These tests verify that the Graph maintains proper ownership semantics
// for ValidInstance data. Since ValidInstance is immutable by design
// (via immutable.WrapPropertiesClone in the validator), these tests verify:
//
// 1. Each Snapshot returns independent, deep-copied instances (isolation)
// 2. Within a single snapshot, references are consistent
// 3. Composed children added via AddComposed are correctly accessible
// 4. Instance data is preserved through graph operations

// TestGraph_SnapshotIsolation verifies that each call to Snapshot() returns
// an independent, isolated copy of the graph's instances.
func TestGraph_SnapshotIsolation(t *testing.T) {
	s := testSchemaWithComposition(t)
	g := New(s)
	ctx := context.Background()

	// Add parent
	parent := mustValidInstance(t, s, "Parent",
		[]any{"p1"}, map[string]any{"name": "Parent 1"})

	if _, err := g.Add(ctx, parent); err != nil {
		t.Fatalf("Add parent error: %v", err)
	}

	// Add children via AddComposed
	child1 := mustValidPartInstance(t, s, "Child",
		[]any{"c1"}, map[string]any{"name": "Child 1"})
	child2 := mustValidPartInstance(t, s, "Child",
		[]any{"c2"}, map[string]any{"name": "Child 2"})

	if _, err := g.AddComposed(ctx, "Parent", FormatKey("p1"), "children", child1); err != nil {
		t.Fatalf("AddComposed child1 error: %v", err)
	}
	if _, err := g.AddComposed(ctx, "Parent", FormatKey("p1"), "children", child2); err != nil {
		t.Fatalf("AddComposed child2 error: %v", err)
	}

	// Get multiple snapshots
	snap1 := g.Snapshot()
	snap2 := g.Snapshot()

	// Verify both snapshots have the parent
	parents1 := snap1.InstancesOf("Parent")
	parents2 := snap2.InstancesOf("Parent")

	if len(parents1) != 1 || len(parents2) != 1 {
		t.Fatalf("Expected 1 parent in each snapshot, got %d and %d", len(parents1), len(parents2))
	}

	// Different snapshots should have DIFFERENT instance pointers (isolation)
	if parents1[0] == parents2[0] {
		t.Error("Different snapshots should NOT share instance pointers")
	}

	// But they should have equivalent data
	if parents1[0].PrimaryKey().String() != parents2[0].PrimaryKey().String() {
		t.Error("Instances should have equivalent primary keys")
	}
	if parents1[0].TypeName() != parents2[0].TypeName() {
		t.Error("Instances should have equivalent type names")
	}

	// Within the same snapshot, multiple calls return the same pointer
	parents1Again := snap1.InstancesOf("Parent")
	if parents1[0] != parents1Again[0] {
		t.Error("Same snapshot should return same instance pointer")
	}

	// Verify parent data is preserved
	nameVal, ok := parents1[0].Property("name")
	if !ok {
		t.Fatal("Expected name property on parent")
	}
	name, ok := nameVal.String()
	if !ok || name != "Parent 1" {
		t.Errorf("Expected parent name 'Parent 1', got %q", name)
	}

	// Verify composed children are accessible
	children := parents1[0].Composed("children")
	if children == nil {
		t.Fatal("Expected children composition on parent")
	}
	if len(children) != 2 {
		t.Errorf("Expected 2 children, got %d", len(children))
	}
}

// TestGraph_ComposedChildAccess verifies that composed children added via
// AddComposed are correctly accessible with their original data preserved.
func TestGraph_ComposedChildAccess(t *testing.T) {
	s := testSchemaWithComposition(t)
	g := New(s)
	ctx := context.Background()

	// Add parent
	parent := mustValidInstance(t, s, "Parent",
		[]any{"p1"}, map[string]any{"name": "Parent 1"})

	if _, err := g.Add(ctx, parent); err != nil {
		t.Fatalf("Add parent error: %v", err)
	}

	// Add children with specific data
	child1 := mustValidPartInstance(t, s, "Child",
		[]any{"c1"}, map[string]any{"name": "First Child"})
	child2 := mustValidPartInstance(t, s, "Child",
		[]any{"c2"}, map[string]any{"name": "Second Child"})
	child3 := mustValidPartInstance(t, s, "Child",
		[]any{"c3"}, map[string]any{"name": "Third Child"})

	for _, child := range []*instance.ValidInstance{child1, child2, child3} {
		result, err := g.AddComposed(ctx, "Parent", FormatKey("p1"), "children", child)
		if err != nil {
			t.Fatalf("AddComposed error: %v", err)
		}
		if !result.OK() {
			t.Errorf("AddComposed should succeed: %s", result.String())
		}
	}

	// Access composed children via snapshot
	snap := g.Snapshot()
	parents := snap.InstancesOf("Parent")
	if len(parents) != 1 {
		t.Fatalf("Expected 1 parent, got %d", len(parents))
	}

	children := parents[0].Composed("children")
	if children == nil {
		t.Fatal("Expected children composition")
	}
	if len(children) != 3 {
		t.Fatalf("Expected 3 children, got %d", len(children))
	}

	// Verify each child has correct data preserved
	expectedNames := map[string]string{
		"c1": "First Child",
		"c2": "Second Child",
		"c3": "Third Child",
	}

	for _, child := range children {
		pkStr := child.PrimaryKey().String()
		// Extract the key value from JSON format like ["c1"]
		// The PK string is in JSON array format

		nameVal, ok := child.Property("name")
		if !ok {
			t.Errorf("Child %s missing name property", pkStr)
			continue
		}
		name, ok := nameVal.String()
		if !ok {
			t.Errorf("Child %s name is not a string", pkStr)
			continue
		}

		// Find expected name by checking the PK string
		var expectedName string
		for key, expected := range expectedNames {
			if pkStr == FormatKey(key) {
				expectedName = expected
				break
			}
		}

		if name != expectedName {
			t.Errorf("Child %s: expected name %q, got %q", pkStr, expectedName, name)
		}
	}
}

// TestSnapshot_Isolation_FromAddComposed verifies that a snapshot taken before
// AddComposed is isolated from composed children added afterwards.
// This is the core immutability guarantee.
func TestSnapshot_Isolation_FromAddComposed(t *testing.T) {
	s := testSchemaWithComposition(t)
	g := New(s)
	ctx := context.Background()

	// Add parent
	parent := mustValidInstance(t, s, "Parent",
		[]any{"p1"}, map[string]any{"name": "Parent 1"})
	if _, err := g.Add(ctx, parent); err != nil {
		t.Fatalf("Add parent error: %v", err)
	}

	// Take snapshot BEFORE adding composed child
	snap1 := g.Snapshot()

	// Add composed child AFTER snapshot
	child := mustValidPartInstance(t, s, "Child",
		[]any{"c1"}, map[string]any{"name": "Child 1"})
	if _, err := g.AddComposed(ctx, "Parent", FormatKey("p1"), "children", child); err != nil {
		t.Fatalf("AddComposed error: %v", err)
	}

	// Take second snapshot
	snap2 := g.Snapshot()

	// snap1 should show ZERO children (immutable snapshot)
	parents1 := snap1.InstancesOf("Parent")
	if len(parents1) != 1 {
		t.Fatalf("Expected 1 parent in snap1, got %d", len(parents1))
	}
	children1 := parents1[0].Composed("children")
	if len(children1) != 0 {
		t.Errorf("snap1 should have 0 children, got %d (snapshot not isolated!)", len(children1))
	}

	// snap2 should show ONE child
	parents2 := snap2.InstancesOf("Parent")
	if len(parents2) != 1 {
		t.Fatalf("Expected 1 parent in snap2, got %d", len(parents2))
	}
	children2 := parents2[0].Composed("children")
	if len(children2) != 1 {
		t.Errorf("snap2 should have 1 child, got %d", len(children2))
	}

	// Verify the child data in snap2
	if len(children2) > 0 {
		nameVal, ok := children2[0].Property("name")
		if !ok {
			t.Error("Expected name property on child")
		} else if name, ok := nameVal.String(); !ok || name != "Child 1" {
			t.Errorf("Expected child name 'Child 1', got %q", name)
		}
	}
}

// TestGraph_InstanceReferencePreservation verifies that within a single snapshot,
// the same Instance reference is returned when accessing the same instance multiple times.
// Different snapshots should return independent copies.
func TestGraph_InstanceReferencePreservation(t *testing.T) {
	s := testSchema(t)
	g := New(s)
	ctx := context.Background()

	// Add an instance
	person := mustValidInstance(t, s, "Person",
		[]any{"alice"}, map[string]any{"name": "Alice"})

	if _, err := g.Add(ctx, person); err != nil {
		t.Fatalf("Add error: %v", err)
	}

	// Access the instance multiple times within the SAME snapshot
	snap := g.Snapshot()
	instances1 := snap.InstancesOf("Person")
	instances2 := snap.InstancesOf("Person")

	if len(instances1) != 1 || len(instances2) != 1 {
		t.Fatalf("Expected 1 instance each time, got %d and %d", len(instances1), len(instances2))
	}

	// Within same snapshot, should be the same reference
	if instances1[0] != instances2[0] {
		t.Error("Expected same instance reference from multiple InstancesOf calls on same snapshot")
	}

	// Different snapshot should return different reference
	snap2 := g.Snapshot()
	instances3 := snap2.InstancesOf("Person")
	if instances1[0] == instances3[0] {
		t.Error("Different snapshots should NOT share instance pointers")
	}

	// But data should be equivalent
	if instances1[0].PrimaryKey().String() != instances3[0].PrimaryKey().String() {
		t.Error("Instances should have equivalent primary keys")
	}

	// Verify data is preserved
	nameVal, ok := instances1[0].Property("name")
	if !ok {
		t.Fatal("Expected name property")
	}
	name, ok := nameVal.String()
	if !ok || name != "Alice" {
		t.Errorf("Expected name 'Alice', got %q", name)
	}
}

// TestSnapshot_EdgeInstanceConsistency verifies that Edge.Source() and Edge.Target()
// are the same pointers as returned by Result.InstanceByKey() within the same snapshot.
func TestSnapshot_EdgeInstanceConsistency(t *testing.T) {
	s := testSchemaWithAssociation(t)
	g := New(s)
	ctx := context.Background()

	// Add company
	company := mustValidInstance(t, s, "Company",
		[]any{"acme"}, map[string]any{"name": "ACME Corp"})
	if _, err := g.Add(ctx, company); err != nil {
		t.Fatalf("Add company error: %v", err)
	}

	// Add person with reference to company
	person := mustValidInstanceWithEdge(t, s, "Person",
		[]any{"alice"}, map[string]any{"name": "Alice"},
		"employer", [][]any{{"acme"}})
	if _, err := g.Add(ctx, person); err != nil {
		t.Fatalf("Add person error: %v", err)
	}

	snap := g.Snapshot()

	// Get instances via direct lookup
	company1, ok := snap.InstanceByKey("Company", FormatKey("acme"))
	if !ok {
		t.Fatal("Expected to find Company by key")
	}
	person1, ok := snap.InstanceByKey("Person", FormatKey("alice"))
	if !ok {
		t.Fatal("Expected to find Person by key")
	}

	// Get instances via edges
	edges := snap.Edges()
	if len(edges) != 1 {
		t.Fatalf("Expected 1 edge, got %d", len(edges))
	}

	edge := edges[0]

	// Edge.Source() and Edge.Target() should be the same pointers
	// as Result.InstanceByKey() returns (within same snapshot)
	if edge.Source() != person1 {
		t.Error("Edge source should be same pointer as InstanceByKey result")
	}
	if edge.Target() != company1 {
		t.Error("Edge target should be same pointer as InstanceByKey result")
	}

	// Also verify edges from a different snapshot have different pointers
	snap2 := g.Snapshot()
	edges2 := snap2.Edges()
	if len(edges2) != 1 {
		t.Fatalf("Expected 1 edge in snap2, got %d", len(edges2))
	}

	if edges[0].Source() == edges2[0].Source() {
		t.Error("Different snapshots should NOT share edge source pointers")
	}
	if edges[0].Target() == edges2[0].Target() {
		t.Error("Different snapshots should NOT share edge target pointers")
	}
}
