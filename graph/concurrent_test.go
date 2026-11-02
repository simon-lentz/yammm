package graph_test

import (
	"fmt"
	"slices"
	"sync"
	"testing"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/instancetest"
	"github.com/simon-lentz/yammm/schema"
)

// testConcurrentSchema creates a schema for concurrency testing.
func testConcurrentSchema(t *testing.T) schema.Schema {
	t.Helper()

	s, result := schema.LoadString(t.Context(), `schema "concurrent"

type Person {
	id String primary
	name String required
}

type Company {
	id String primary
	name String required
}
`, "concurrent.yammm")

	if result.HasErrors() {
		t.Fatalf("Failed to build test schema: %s", result.String())
	}
	return s
}

func TestGraph_Concurrent_Add(t *testing.T) {
	s := testConcurrentSchema(t)
	g := graph.New(s)
	ctx := t.Context()

	const numGoroutines = 100
	const instancesPerGoroutine = 10

	var wg sync.WaitGroup

	for i := range numGoroutines {
		wg.Go(func() {
			for j := range instancesPerGoroutine {
				pk := fmt.Sprintf("person-%d-%d", i, j)
				inst := instancetest.Valid(t, s, "Person", map[string]any{"id": pk, "name": pk})

				g.Add(ctx, inst)
			}
		})
	}

	wg.Wait()

	// Verify all instances were added
	snap := g.Snapshot()
	instances := slices.Collect(snap.InstancesOf(mustTypeID(t, s, "Person")))
	expectedCount := numGoroutines * instancesPerGoroutine

	if len(instances) != expectedCount {
		t.Errorf("Expected %d instances, got %d", expectedCount, len(instances))
	}
}

func TestGraph_Concurrent_Add_WithDuplicates(t *testing.T) {
	s := testConcurrentSchema(t)
	g := graph.New(s)
	ctx := t.Context()

	const numGoroutines = 50
	// All goroutines try to add the same instance
	const sharedKey = "shared-alice"

	var wg sync.WaitGroup

	var successCount int
	var mu sync.Mutex

	for i := range numGoroutines {
		wg.Go(func() {
			inst := instancetest.Valid(t, s, "Person", map[string]any{"id": sharedKey, "name": fmt.Sprintf("Alice %d", i)})

			result := g.Add(ctx, inst)

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
	instances := slices.Collect(snap.InstancesOf(mustTypeID(t, s, "Person")))
	if len(instances) != 1 {
		t.Errorf("Expected 1 instance, got %d", len(instances))
	}

	dups := slices.Collect(snap.Duplicates())
	expectedDups := numGoroutines - 1
	if len(dups) != expectedDups {
		t.Errorf("Expected %d duplicates, got %d", expectedDups, len(dups))
	}
}

func TestGraph_Concurrent_Add_MultipleTypes(t *testing.T) {
	s := testConcurrentSchema(t)
	g := graph.New(s)
	ctx := t.Context()

	const numGoroutines = 50
	const instancesPerGoroutine = 10

	var wg sync.WaitGroup

	// Add Persons
	for i := range numGoroutines {
		wg.Go(func() {
			for j := range instancesPerGoroutine {
				pk := fmt.Sprintf("person-%d-%d", i, j)
				inst := instancetest.Valid(t, s, "Person", map[string]any{"id": pk, "name": pk})

				g.Add(ctx, inst)
			}
		})
	}

	// Add Companies
	for i := range numGoroutines {
		wg.Go(func() {
			for j := range instancesPerGoroutine {
				pk := fmt.Sprintf("company-%d-%d", i, j)
				inst := instancetest.Valid(t, s, "Company", map[string]any{"id": pk, "name": pk})

				g.Add(ctx, inst)
			}
		})
	}

	wg.Wait()

	// Verify counts
	snap := g.Snapshot()
	persons := slices.Collect(snap.InstancesOf(mustTypeID(t, s, "Person")))
	companies := slices.Collect(snap.InstancesOf(mustTypeID(t, s, "Company")))
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
	g := graph.New(s)
	ctx := t.Context()

	const numWriters = 10
	const numReaders = 20
	const instancesPerWriter = 50

	var wg sync.WaitGroup

	// Writers add instances
	for i := range numWriters {
		wg.Go(func() {
			for j := range instancesPerWriter {
				pk := fmt.Sprintf("person-%d-%d", i, j)
				inst := instancetest.Valid(t, s, "Person", map[string]any{"id": pk, "name": pk})

				g.Add(ctx, inst)
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
				for _, typeID := range types {
					instances := slices.Collect(snap.InstancesOf(typeID))
					for _, inst := range instances {
						if inst.TypeID() != typeID {
							t.Errorf("Instance type mismatch: %q vs %q", inst.TypeID(), typeID)
						}
					}
				}
			}
		})
	}

	wg.Wait()

	// Final verification
	snap := g.Snapshot()
	instances := slices.Collect(snap.InstancesOf(mustTypeID(t, s, "Person")))
	expectedCount := numWriters * instancesPerWriter

	if len(instances) != expectedCount {
		t.Errorf("Expected %d instances, got %d", expectedCount, len(instances))
	}
}

func TestGraph_Concurrent_Check(t *testing.T) {
	s := testConcurrentSchema(t)
	g := graph.New(s)
	ctx := t.Context()

	const numGoroutines = 20

	var wg sync.WaitGroup

	// Half check, half add
	for i := range numGoroutines {
		wg.Go(func() {
			pk := fmt.Sprintf("person-%d", i)
			inst := instancetest.Valid(t, s, "Person", map[string]any{"id": pk, "name": pk})

			g.Add(ctx, inst)
		})

		wg.Go(func() {
			// Concurrent Check must never report a Fatal: the graph is
			// consistent at every point an Add can be observed from.
			if res := g.Check(ctx); res.HasFatal() {
				t.Errorf("concurrent Check returned Fatal: %s", res.String())
			}
		})
	}

	wg.Wait()

	// No Person declares a required association, so the settled graph checks
	// clean however the concurrent adds interleaved.
	if res := g.Check(ctx); !res.OK() {
		t.Errorf("Check after the concurrent phase: %s", res.String())
	}
}

func TestGraph_Concurrent_DeterministicOrder(t *testing.T) {
	s := testConcurrentSchema(t)
	ctx := t.Context()

	// Run multiple times to test ordering consistency
	const runs = 5
	const numGoroutines = 20
	const instancesPerGoroutine = 5

	var previousOrder []string

	for run := range runs {
		g := graph.New(s)

		var wg sync.WaitGroup

		for i := range numGoroutines {
			wg.Go(func() {
				for j := range instancesPerGoroutine {
					pk := fmt.Sprintf("person-%d-%d", i, j)
					inst := instancetest.Valid(t, s, "Person", map[string]any{"id": pk, "name": pk})

					g.Add(ctx, inst)
				}
			})
		}

		wg.Wait()

		snap := g.Snapshot()
		instances := slices.Collect(snap.InstancesOf(mustTypeID(t, s, "Person")))

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
	s, result := schema.LoadString(b.Context(), `schema "bench"

type Person {
	id String primary
	name String required
}
`, "bench.yammm")

	if result.HasErrors() {
		b.Fatalf("Failed to build schema: %s", result.String())
	}

	ctx := b.Context()

	instances := make([]instance.ValidInstance, 100)
	for j := range instances {
		pk := fmt.Sprintf("person-%d", j)
		instances[j] = instancetest.Valid(b, s, "Person", map[string]any{"id": pk, "name": pk})
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			g := graph.New(s)
			for _, inst := range instances {
				g.Add(ctx, inst)
			}
		}
	})
}

// Integration Scenario Tests
//
// These tests verify complex real-world scenarios with multiple features combined.

func TestIntegration_ComplexMultiSchema(t *testing.T) {
	// 3-schema hierarchy with cross-references
	schemaA, _, schemaC := testTripleSchemaSetup(t)
	g := graph.New(schemaA)
	ctx := t.Context()

	// Add instances at each level
	// Schema C: BaseType
	base := instancetest.Valid(t, schemaC, "BaseType", map[string]any{"id": "base1", "value": "base value"})

	// Note: Schema A imports B which imports C
	// So A can access B types (b.MiddleType) but NOT C types directly
	// This test verifies the graph handles the imported B correctly

	middle := instancetest.Valid(t, schemaA, "b.MiddleType", map[string]any{"id": "mid1", "name": "middle value"})

	top := instancetest.Valid(t, schemaA, "TopType", map[string]any{"id": "top1", "label": "top value"})

	// Add in reverse order (forward refs)
	g.Add(ctx, top)
	g.Add(ctx, middle)

	// Adding base from C should fail since A doesn't directly import C
	result := g.Add(ctx, base)
	// This should fail with type not found
	if result.OK() {
		t.Error("Expected failure when adding transitive import type")
	}

	// Verify what was added successfully
	snap := g.Snapshot()
	if len(slices.Collect(snap.InstancesOf(tagID(t, schemaA, "TopType")))) != 1 {
		t.Error("TopType should be in graph")
	}
	if len(slices.Collect(snap.InstancesOf(tagID(t, schemaA, "b.MiddleType")))) != 1 {
		t.Error("b.MiddleType should be in graph")
	}
}

func TestIntegration_ForwardRefChain(t *testing.T) {
	// Chain of forward references that all resolve
	s := testSchemaWithChainedAssociations(t)
	g := graph.New(s)
	ctx := t.Context()

	// Add in order: A → B → C (all forward refs)
	typeA := mustValidInstanceWithEdge(t, s, "TypeA",
		[]any{"a1"}, map[string]any{"name": "A1"},
		"REF_B", [][]any{{"b1"}})

	typeB := mustValidInstanceWithEdge(t, s, "TypeB",
		[]any{"b1"}, map[string]any{"name": "B1"},
		"REF_C", [][]any{{"c1"}})

	typeC := mustValidInstance(t, s, "TypeC",
		[]any{"c1"}, map[string]any{"name": "C1"})

	// Add in forward-ref order
	g.Add(ctx, typeA)

	snap1 := g.Snapshot()
	if len(slices.Collect(snap1.Unresolved())) == 0 {
		t.Error("Should have unresolved A→B")
	}

	g.Add(ctx, typeB)

	snap2 := g.Snapshot()
	// A→B should be resolved, B→C still pending
	if len(slices.Collect(snap2.Edges())) == 0 {
		t.Error("A→B edge should be resolved")
	}

	g.Add(ctx, typeC)

	// All should be resolved
	snap3 := g.Snapshot()
	if len(slices.Collect(snap3.Unresolved())) != 0 {
		t.Errorf("All edges should be resolved, have %d unresolved", len(slices.Collect(snap3.Unresolved())))
	}
	if len(slices.Collect(snap3.Edges())) != 2 {
		t.Errorf("Should have 2 edges (A→B, B→C), got %d", len(slices.Collect(snap3.Edges())))
	}
}

func TestIntegration_InlineChildrenKeepTheirOrder(t *testing.T) {
	s := testSchemaWithComposition(t)
	g := graph.New(s)
	ctx := t.Context()

	var kids []map[string]any
	for i := range 5 {
		kids = append(kids, partData(t, s, "Child",
			[]any{fmt.Sprintf("c%d", i)}, map[string]any{"name": fmt.Sprintf("Child %d", i)}))
	}
	parent := parentWith(t, s, "Parent", []any{"p1"}, map[string]any{"name": "Parent 1"}, "CHILDREN", kids...)
	if err := g.Add(ctx, parent).Err(); err != nil {
		t.Errorf("Add should succeed: %v", err)
	}

	// Verify all children are attached
	snap := g.Snapshot()
	parents := slices.Collect(snap.InstancesOf(mustTypeID(t, s, "Parent")))
	if len(parents) != 1 {
		t.Fatalf("Expected 1 parent, got %d", len(parents))
	}

	assertComposedCount(t, parents[0], "CHILDREN", 5)

	// Verify children are accessible
	children := slices.Collect(parents[0].Composed("CHILDREN"))
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
	g := graph.New(s)
	ctx := t.Context()

	const numWorkers = 10
	const opsPerWorker = 20

	var wg sync.WaitGroup

	// Writers add instances
	for w := range numWorkers {
		wg.Go(func() {
			for i := range opsPerWorker {
				// Add person
				pk := fmt.Sprintf("person-%d-%d", w, i)
				person := instancetest.Valid(t, s, "Person", map[string]any{"id": pk, "name": pk})
				g.Add(ctx, person)

				// Add company
				ck := fmt.Sprintf("company-%d-%d", w, i)
				company := instancetest.Valid(t, s, "Company", map[string]any{"id": ck, "name": ck})
				g.Add(ctx, company)
			}
		})
	}

	// Checkers run Check concurrently
	for range numWorkers {
		wg.Go(func() {
			for range opsPerWorker {
				g.Check(ctx)
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
				_ = slices.Collect(snap.Edges())
			}
		})
	}

	wg.Wait()

	// Final verification
	snap := g.Snapshot()
	expectedPersons := numWorkers * opsPerWorker
	expectedCompanies := numWorkers * opsPerWorker

	persons := slices.Collect(snap.InstancesOf(mustTypeID(t, s, "Person")))
	companies := slices.Collect(snap.InstancesOf(mustTypeID(t, s, "Company")))

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
	g := graph.New(s)
	ctx := t.Context()

	const numWorkers = 50

	var wg sync.WaitGroup

	// All workers add Persons referencing same Company
	for i := range numWorkers {
		wg.Go(func() {
			person := mustValidInstanceWithEdge(t, s, "Person",
				[]any{fmt.Sprintf("person-%d", i)},
				map[string]any{"name": fmt.Sprintf("Person %d", i)},
				"EMPLOYER", [][]any{{"shared-company"}})

			g.Add(ctx, person)
		})
	}
	wg.Wait()

	// Verify all pending
	snap1 := g.Snapshot()
	if len(slices.Collect(snap1.Unresolved())) != numWorkers {
		t.Errorf("Expected %d unresolved edges, got %d", numWorkers, len(slices.Collect(snap1.Unresolved())))
	}

	// Add the target
	company := mustValidInstance(t, s, "Company",
		[]any{"shared-company"}, map[string]any{"name": "Shared Company"})
	g.Add(ctx, company)

	// All should be resolved
	snap2 := g.Snapshot()
	if len(slices.Collect(snap2.Unresolved())) != 0 {
		t.Errorf("Expected 0 unresolved edges, got %d", len(slices.Collect(snap2.Unresolved())))
	}
	if len(slices.Collect(snap2.Edges())) != numWorkers {
		t.Errorf("Expected %d edges, got %d", numWorkers, len(slices.Collect(snap2.Edges())))
	}

	// Verify all sources have edges
	sources := make(map[string]bool)
	for edge := range snap2.Edges() {
		sources[edge.Source().PrimaryKey().String()] = true
	}
	if len(sources) != numWorkers {
		t.Errorf("Expected %d unique sources, got %d", numWorkers, len(sources))
	}
}
