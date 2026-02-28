package graph

import (
	"fmt"
	"sync"
	"testing"

	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/schema/build"
)

// testConcurrentSchema creates a schema for concurrency testing.
func testConcurrentSchema(t *testing.T) *schema.Schema {
	t.Helper()

	s, result := build.NewBuilder().
		WithName("concurrent").
		WithSourceID(location.MustNewSourceID("test://concurrent.yammm")).
		AddType("Person").
		WithPrimaryKey("id", schema.StringConstraint{}).
		WithProperty("name", schema.StringConstraint{}).
		Done().
		AddType("Company").
		WithPrimaryKey("id", schema.StringConstraint{}).
		WithProperty("name", schema.StringConstraint{}).
		Done().
		Build()

	if result.HasErrors() {
		t.Fatalf("Failed to build test schema: %s", result.String())
	}
	return s
}

func TestGraph_Concurrent_Add(t *testing.T) {
	s := testConcurrentSchema(t)
	g := New(s)
	ctx := t.Context()

	personType, _ := s.Type("Person")

	const numGoroutines = 100
	const instancesPerGoroutine = 10

	var wg sync.WaitGroup

	for i := range numGoroutines {
		wg.Go(func() {
			for j := range instancesPerGoroutine {
				pk := fmt.Sprintf("person-%d-%d", i, j)
				inst := instance.NewValidInstance(
					"Person",
					personType.ID(),
					immutable.WrapKey([]any{pk}),
					immutable.WrapProperties(map[string]any{"name": pk}),
					nil, nil, nil,
				)

				_, err := g.Add(ctx, inst)
				if err != nil {
					t.Errorf("Add() error: %v", err)
				}
			}
		})
	}

	wg.Wait()

	// Verify all instances were added
	snap := g.Snapshot()
	instances := snap.InstancesOf("Person")
	expectedCount := numGoroutines * instancesPerGoroutine

	if len(instances) != expectedCount {
		t.Errorf("Expected %d instances, got %d", expectedCount, len(instances))
	}
}

func TestGraph_Concurrent_Add_WithDuplicates(t *testing.T) {
	s := testConcurrentSchema(t)
	g := New(s)
	ctx := t.Context()

	personType, _ := s.Type("Person")

	const numGoroutines = 50
	// All goroutines try to add the same instance
	const sharedKey = "shared-alice"

	var wg sync.WaitGroup

	var successCount int
	var mu sync.Mutex

	for i := range numGoroutines {
		wg.Go(func() {
			inst := instance.NewValidInstance(
				"Person",
				personType.ID(),
				immutable.WrapKey([]any{sharedKey}),
				immutable.WrapProperties(map[string]any{"name": fmt.Sprintf("Alice %d", i)}),
				nil, nil, nil,
			)

			result, err := g.Add(ctx, inst)
			if err != nil {
				t.Errorf("Add() error: %v", err)
				return
			}

			if result.OK() {
				mu.Lock()
				successCount++
				mu.Unlock()
			}
		})
	}

	wg.Wait()

	// Exactly one should succeed
	if successCount != 1 {
		t.Errorf("Expected exactly 1 success, got %d", successCount)
	}

	// Verify snapshot
	snap := g.Snapshot()
	instances := snap.InstancesOf("Person")
	if len(instances) != 1 {
		t.Errorf("Expected 1 instance, got %d", len(instances))
	}

	dups := snap.Duplicates()
	expectedDups := numGoroutines - 1
	if len(dups) != expectedDups {
		t.Errorf("Expected %d duplicates, got %d", expectedDups, len(dups))
	}
}

func TestGraph_Concurrent_Add_MultipleTypes(t *testing.T) {
	s := testConcurrentSchema(t)
	g := New(s)
	ctx := t.Context()

	personType, _ := s.Type("Person")
	companyType, _ := s.Type("Company")

	const numGoroutines = 50
	const instancesPerGoroutine = 10

	var wg sync.WaitGroup

	// Add Persons
	for i := range numGoroutines {
		wg.Go(func() {
			for j := range instancesPerGoroutine {
				pk := fmt.Sprintf("person-%d-%d", i, j)
				inst := instance.NewValidInstance(
					"Person",
					personType.ID(),
					immutable.WrapKey([]any{pk}),
					immutable.WrapProperties(map[string]any{"name": pk}),
					nil, nil, nil,
				)

				if _, err := g.Add(ctx, inst); err != nil {
					t.Errorf("Add Person error: %v", err)
				}
			}
		})
	}

	// Add Companies
	for i := range numGoroutines {
		wg.Go(func() {
			for j := range instancesPerGoroutine {
				pk := fmt.Sprintf("company-%d-%d", i, j)
				inst := instance.NewValidInstance(
					"Company",
					companyType.ID(),
					immutable.WrapKey([]any{pk}),
					immutable.WrapProperties(map[string]any{"name": pk}),
					nil, nil, nil,
				)

				if _, err := g.Add(ctx, inst); err != nil {
					t.Errorf("Add Company error: %v", err)
				}
			}
		})
	}

	wg.Wait()

	// Verify counts
	snap := g.Snapshot()
	persons := snap.InstancesOf("Person")
	companies := snap.InstancesOf("Company")
	expectedPerType := numGoroutines * instancesPerGoroutine

	if len(persons) != expectedPerType {
		t.Errorf("Expected %d Person instances, got %d", expectedPerType, len(persons))
	}
	if len(companies) != expectedPerType {
		t.Errorf("Expected %d Company instances, got %d", expectedPerType, len(companies))
	}

	types := snap.Types()
	if len(types) != 2 {
		t.Errorf("Expected 2 types, got %d", len(types))
	}
}

func TestGraph_Concurrent_Snapshot(t *testing.T) {
	s := testConcurrentSchema(t)
	g := New(s)
	ctx := t.Context()

	personType, _ := s.Type("Person")

	const numWriters = 10
	const numReaders = 20
	const instancesPerWriter = 50

	var wg sync.WaitGroup

	// Writers add instances
	for i := range numWriters {
		wg.Go(func() {
			for j := range instancesPerWriter {
				pk := fmt.Sprintf("person-%d-%d", i, j)
				inst := instance.NewValidInstance(
					"Person",
					personType.ID(),
					immutable.WrapKey([]any{pk}),
					immutable.WrapProperties(map[string]any{"name": pk}),
					nil, nil, nil,
				)

				if _, err := g.Add(ctx, inst); err != nil {
					t.Errorf("Add error: %v", err)
				}
			}
		})
	}

	// Readers take snapshots concurrently
	for range numReaders {
		wg.Go(func() {
			for range 10 {
				snap := g.Snapshot()
				// Verify snapshot is self-consistent
				types := snap.Types()
				for _, typeName := range types {
					instances := snap.InstancesOf(typeName)
					for _, inst := range instances {
						if inst.TypeName() != typeName {
							t.Errorf("Instance type mismatch: %q vs %q", inst.TypeName(), typeName)
						}
					}
				}
			}
		})
	}

	wg.Wait()

	// Final verification
	snap := g.Snapshot()
	instances := snap.InstancesOf("Person")
	expectedCount := numWriters * instancesPerWriter

	if len(instances) != expectedCount {
		t.Errorf("Expected %d instances, got %d", expectedCount, len(instances))
	}
}

func TestGraph_Concurrent_Check(t *testing.T) {
	s := testConcurrentSchema(t)
	g := New(s)
	ctx := t.Context()

	personType, _ := s.Type("Person")

	const numGoroutines = 20

	var wg sync.WaitGroup

	// Half check, half add
	for i := range numGoroutines {
		wg.Go(func() {
			pk := fmt.Sprintf("person-%d", i)
			inst := instance.NewValidInstance(
				"Person",
				personType.ID(),
				immutable.WrapKey([]any{pk}),
				immutable.WrapProperties(map[string]any{"name": pk}),
				nil, nil, nil,
			)

			if _, err := g.Add(ctx, inst); err != nil {
				t.Errorf("Add error: %v", err)
			}
		})

		wg.Go(func() {
			// Check should not error even during concurrent adds
			if _, err := g.Check(ctx); err != nil {
				t.Errorf("Check error: %v", err)
			}
		})
	}

	wg.Wait()
}

func TestGraph_Concurrent_DeterministicOrder(t *testing.T) {
	s := testConcurrentSchema(t)
	ctx := t.Context()

	personType, _ := s.Type("Person")

	// Run multiple times to test ordering consistency
	const runs = 5
	const numGoroutines = 20
	const instancesPerGoroutine = 5

	var previousOrder []string

	for run := range runs {
		g := New(s)

		var wg sync.WaitGroup

		for i := range numGoroutines {
			wg.Go(func() {
				for j := range instancesPerGoroutine {
					pk := fmt.Sprintf("person-%d-%d", i, j)
					inst := instance.NewValidInstance(
						"Person",
						personType.ID(),
						immutable.WrapKey([]any{pk}),
						immutable.WrapProperties(map[string]any{"name": pk}),
						nil, nil, nil,
					)

					if _, err := g.Add(ctx, inst); err != nil {
						t.Errorf("Add error: %v", err)
					}
				}
			})
		}

		wg.Wait()

		snap := g.Snapshot()
		instances := snap.InstancesOf("Person")

		var currentOrder []string
		for _, inst := range instances {
			currentOrder = append(currentOrder, inst.PrimaryKey().String())
		}

		if run > 0 {
			// All runs should produce the same order
			if len(currentOrder) != len(previousOrder) {
				t.Fatalf("Run %d: length mismatch: %d vs %d", run, len(currentOrder), len(previousOrder))
			}
			for i, pk := range currentOrder {
				if pk != previousOrder[i] {
					t.Errorf("Run %d: order mismatch at %d: %q vs %q", run, i, pk, previousOrder[i])
				}
			}
		}

		previousOrder = currentOrder
	}
}

func BenchmarkGraph_Add_Concurrent(b *testing.B) {
	s, result := build.NewBuilder().
		WithName("bench").
		WithSourceID(location.MustNewSourceID("test://bench.yammm")).
		AddType("Person").
		WithPrimaryKey("id", schema.StringConstraint{}).
		WithProperty("name", schema.StringConstraint{}).
		Done().
		Build()

	if result.HasErrors() {
		b.Fatalf("Failed to build schema: %s", result.String())
	}

	personType, _ := s.Type("Person")
	ctx := b.Context()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			g := New(s)
			for j := range 100 {
				pk := fmt.Sprintf("person-%d-%d", i, j)
				inst := instance.NewValidInstance(
					"Person",
					personType.ID(),
					immutable.WrapKey([]any{pk}),
					immutable.WrapProperties(map[string]any{"name": pk}),
					nil, nil, nil,
				)
				_, _ = g.Add(ctx, inst)
			}
			i++
		}
	})
}

// Integration Scenario Tests
//
// These tests verify complex real-world scenarios with multiple features combined.

func TestIntegration_ComplexMultiSchema(t *testing.T) {
	// 3-schema hierarchy with cross-references
	schemaA, schemaB, schemaC, _ := testTripleSchemaSetup(t)
	g := New(schemaA)
	ctx := t.Context()

	// Add instances at each level
	// Schema C: BaseType
	baseType, _ := schemaC.Type("BaseType")
	base := instance.NewValidInstance(
		"c.BaseType",
		baseType.ID(),
		immutable.WrapKey([]any{"base1"}),
		immutable.WrapProperties(map[string]any{"value": "base value"}),
		nil, nil, nil,
	)

	// Note: Schema A imports B which imports C
	// So A can access B types (b.MiddleType) but NOT C types directly
	// This test verifies the graph handles the imported B correctly

	middleType, _ := schemaB.Type("MiddleType")
	middle := instance.NewValidInstance(
		"b.MiddleType",
		middleType.ID(),
		immutable.WrapKey([]any{"mid1"}),
		immutable.WrapProperties(map[string]any{"name": "middle value"}),
		nil, nil, nil,
	)

	topType, _ := schemaA.Type("TopType")
	top := instance.NewValidInstance(
		"TopType",
		topType.ID(),
		immutable.WrapKey([]any{"top1"}),
		immutable.WrapProperties(map[string]any{"label": "top value"}),
		nil, nil, nil,
	)

	// Add in reverse order (forward refs)
	if _, err := g.Add(ctx, top); err != nil {
		t.Fatalf("Add top error: %v", err)
	}
	if _, err := g.Add(ctx, middle); err != nil {
		t.Fatalf("Add middle error: %v", err)
	}

	// Adding base from C should fail since A doesn't directly import C
	result, err := g.Add(ctx, base)
	if err != nil {
		t.Fatalf("Add base error: %v", err)
	}
	// This should fail with type not found
	if result.OK() {
		t.Error("Expected failure when adding transitive import type")
	}

	// Verify what was added successfully
	snap := g.Snapshot()
	if len(snap.InstancesOf("TopType")) != 1 {
		t.Error("TopType should be in graph")
	}
	if len(snap.InstancesOf("b.MiddleType")) != 1 {
		t.Error("b.MiddleType should be in graph")
	}
}

func TestIntegration_ForwardRefChain(t *testing.T) {
	// Chain of forward references that all resolve
	s := testSchemaWithChainedAssociations(t)
	g := New(s)
	ctx := t.Context()

	// Add in order: A → B → C (all forward refs)
	typeA := mustValidInstanceWithEdge(t, s, "TypeA",
		[]any{"a1"}, map[string]any{"name": "A1"},
		"refB", [][]any{{"b1"}})

	typeB := mustValidInstanceWithEdge(t, s, "TypeB",
		[]any{"b1"}, map[string]any{"name": "B1"},
		"refC", [][]any{{"c1"}})

	typeC := mustValidInstance(t, s, "TypeC",
		[]any{"c1"}, map[string]any{"name": "C1"})

	// Add in forward-ref order
	if _, err := g.Add(ctx, typeA); err != nil {
		t.Fatalf("Add A error: %v", err)
	}

	snap1 := g.Snapshot()
	if len(snap1.Unresolved()) == 0 {
		t.Error("Should have unresolved A→B")
	}

	if _, err := g.Add(ctx, typeB); err != nil {
		t.Fatalf("Add B error: %v", err)
	}

	snap2 := g.Snapshot()
	// A→B should be resolved, B→C still pending
	if len(snap2.Edges()) == 0 {
		t.Error("A→B edge should be resolved")
	}

	if _, err := g.Add(ctx, typeC); err != nil {
		t.Fatalf("Add C error: %v", err)
	}

	// All should be resolved
	snap3 := g.Snapshot()
	if len(snap3.Unresolved()) != 0 {
		t.Errorf("All edges should be resolved, have %d unresolved", len(snap3.Unresolved()))
	}
	if len(snap3.Edges()) != 2 {
		t.Errorf("Should have 2 edges (A→B, B→C), got %d", len(snap3.Edges()))
	}
}

func TestIntegration_MixedInlineStreamed(t *testing.T) {
	// Test mixing inline compositions with AddComposed
	s := testSchemaWithComposition(t)
	g := New(s)
	ctx := t.Context()

	// Add parent without inline children
	parent := mustValidInstance(t, s, "Parent",
		[]any{"p1"}, map[string]any{"name": "Parent 1"})

	if _, err := g.Add(ctx, parent); err != nil {
		t.Fatalf("Add parent error: %v", err)
	}

	// Stream in children one by one
	for i := range 5 {
		child := mustValidPartInstance(t, s, "Child",
			[]any{fmt.Sprintf("c%d", i)}, map[string]any{"name": fmt.Sprintf("Child %d", i)})

		result, err := g.AddComposed(ctx, "Parent", FormatKey("p1"), "children", child)
		if err != nil {
			t.Fatalf("AddComposed child %d error: %v", i, err)
		}
		if !result.OK() {
			t.Errorf("AddComposed child %d should succeed: %s", i, result.String())
		}
	}

	// Verify all children are attached
	snap := g.Snapshot()
	parents := snap.InstancesOf("Parent")
	if len(parents) != 1 {
		t.Fatalf("Expected 1 parent, got %d", len(parents))
	}

	assertComposedCount(t, parents[0], "children", 5)

	// Verify children are accessible
	children := parents[0].Composed("children")
	for i, child := range children {
		expected := fmt.Sprintf(`["c%d"]`, i)
		if child.PrimaryKey().String() != expected {
			t.Errorf("Child %d PK should be %s, got %s", i, expected, child.PrimaryKey().String())
		}
	}
}

func TestIntegration_ConcurrentAddCheck(t *testing.T) {
	// Concurrent Add + Check + Snapshot operations
	s := testSchemaWithAssociation(t)
	g := New(s)
	ctx := t.Context()

	personType, _ := s.Type("Person")
	companyType, _ := s.Type("Company")

	const numWorkers = 10
	const opsPerWorker = 20

	var wg sync.WaitGroup

	// Writers add instances
	for w := range numWorkers {
		wg.Go(func() {
			for i := range opsPerWorker {
				// Add person
				pk := fmt.Sprintf("person-%d-%d", w, i)
				person := instance.NewValidInstance(
					"Person", personType.ID(),
					immutable.WrapKey([]any{pk}),
					immutable.WrapProperties(map[string]any{"name": pk}),
					nil, nil, nil,
				)
				if _, err := g.Add(ctx, person); err != nil {
					t.Errorf("Add person error: %v", err)
				}

				// Add company
				ck := fmt.Sprintf("company-%d-%d", w, i)
				company := instance.NewValidInstance(
					"Company", companyType.ID(),
					immutable.WrapKey([]any{ck}),
					immutable.WrapProperties(map[string]any{"name": ck}),
					nil, nil, nil,
				)
				if _, err := g.Add(ctx, company); err != nil {
					t.Errorf("Add company error: %v", err)
				}
			}
		})
	}

	// Checkers run Check concurrently
	for range numWorkers {
		wg.Go(func() {
			for range opsPerWorker {
				if _, err := g.Check(ctx); err != nil {
					t.Errorf("Check error: %v", err)
				}
			}
		})
	}

	// Snapshotters take snapshots concurrently
	for range numWorkers {
		wg.Go(func() {
			for range opsPerWorker {
				snap := g.Snapshot()
				// Just verify it returns something valid
				_ = snap.Types()
				_ = snap.Edges()
			}
		})
	}

	wg.Wait()

	// Final verification
	snap := g.Snapshot()
	expectedPersons := numWorkers * opsPerWorker
	expectedCompanies := numWorkers * opsPerWorker

	persons := snap.InstancesOf("Person")
	companies := snap.InstancesOf("Company")

	if len(persons) != expectedPersons {
		t.Errorf("Expected %d persons, got %d", expectedPersons, len(persons))
	}
	if len(companies) != expectedCompanies {
		t.Errorf("Expected %d companies, got %d", expectedCompanies, len(companies))
	}
}

func TestGraph_Concurrent_ForwardReferences(t *testing.T) {
	// Multiple goroutines add instances with forward references to same target
	// Verifies that all pending edges are tracked and resolved atomically
	s := testSchemaWithAssociation(t)
	g := New(s)
	ctx := t.Context()

	const numWorkers = 50

	var wg sync.WaitGroup

	// All workers add Persons referencing same Company
	for i := range numWorkers {
		wg.Go(func() {
			person := mustValidInstanceWithEdge(t, s, "Person",
				[]any{fmt.Sprintf("person-%d", i)},
				map[string]any{"name": fmt.Sprintf("Person %d", i)},
				"employer", [][]any{{"shared-company"}})

			if _, err := g.Add(ctx, person); err != nil {
				t.Errorf("Add person-%d error: %v", i, err)
			}
		})
	}
	wg.Wait()

	// Verify all pending
	snap1 := g.Snapshot()
	if len(snap1.Unresolved()) != numWorkers {
		t.Errorf("Expected %d unresolved edges, got %d", numWorkers, len(snap1.Unresolved()))
	}

	// Add the target
	company := mustValidInstance(t, s, "Company",
		[]any{"shared-company"}, map[string]any{"name": "Shared Company"})
	if _, err := g.Add(ctx, company); err != nil {
		t.Fatalf("Add company error: %v", err)
	}

	// All should be resolved
	snap2 := g.Snapshot()
	if len(snap2.Unresolved()) != 0 {
		t.Errorf("Expected 0 unresolved edges, got %d", len(snap2.Unresolved()))
	}
	if len(snap2.Edges()) != numWorkers {
		t.Errorf("Expected %d edges, got %d", numWorkers, len(snap2.Edges()))
	}

	// Verify all sources have edges
	sources := make(map[string]bool)
	for _, edge := range snap2.Edges() {
		sources[edge.Source().PrimaryKey().String()] = true
	}
	if len(sources) != numWorkers {
		t.Errorf("Expected %d unique sources, got %d", numWorkers, len(sources))
	}
}

// TestConcurrent_SnapshotAndAddComposed_Race tests for data races between
// concurrent Snapshot() and AddComposed() calls.
//
// This test should be run with: go test -race ./graph -run TestConcurrent_SnapshotAndAddComposed_Race
//
// Before the deep-copy snapshot fix, this test would detect a race condition
// because AddComposed() mutated Instance.composed while Snapshot() readers
// were accessing the same Instance pointers.
func TestConcurrent_SnapshotAndAddComposed_Race(t *testing.T) {
	s := testSchemaWithComposition(t)
	g := New(s)
	ctx := t.Context()

	// Add parent
	parent := mustValidInstance(t, s, "Parent",
		[]any{"p1"}, map[string]any{"name": "Parent 1"})
	if _, err := g.Add(ctx, parent); err != nil {
		t.Fatalf("Add parent error: %v", err)
	}

	const numWriters = 10
	const numReaders = 20
	const opsPerWorker = 50

	var wg sync.WaitGroup

	// Writers call AddComposed
	for w := range numWriters {
		wg.Go(func() {
			for i := range opsPerWorker {
				child := mustValidPartInstance(t, s, "Child",
					[]any{fmt.Sprintf("c-%d-%d", w, i)},
					map[string]any{"name": fmt.Sprintf("Child %d-%d", w, i)})
				_, _ = g.AddComposed(ctx, "Parent", FormatKey("p1"), "children", child)
			}
		})
	}

	// Readers take snapshots and read composed children
	for range numReaders {
		wg.Go(func() {
			for range opsPerWorker {
				snap := g.Snapshot()
				parents := snap.InstancesOf("Parent")
				if len(parents) > 0 {
					// Access Composed() - this would race with addComposed() before the fix
					_ = parents[0].Composed("children")
					_ = parents[0].ComposedCount("children")
					_ = parents[0].ComposedRelations()
				}
			}
		})
	}

	wg.Wait()

	// If we get here without race detector errors, the test passes
	// Verify final state is consistent
	snap := g.Snapshot()
	parents := snap.InstancesOf("Parent")
	if len(parents) != 1 {
		t.Fatalf("Expected 1 parent, got %d", len(parents))
	}

	expectedChildren := numWriters * opsPerWorker
	actualChildren := parents[0].ComposedCount("children")
	if actualChildren != expectedChildren {
		t.Errorf("Expected %d children, got %d", expectedChildren, actualChildren)
	}
}
