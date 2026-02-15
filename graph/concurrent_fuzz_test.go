package graph

import (
	"context"
	"math/rand"
	"sync"
	"testing"

	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/schema/build"
)

// FuzzGraph_ConcurrentOperations tests that concurrent graph operations
// don't cause panics or data races. Uses random seeds to drive operation
// sequences across multiple goroutines.
func FuzzGraph_ConcurrentOperations(f *testing.F) {
	// Seed corpus with various random seeds
	f.Add(int64(0), 10, 5)
	f.Add(int64(42), 20, 10)
	f.Add(int64(12345), 50, 20)
	f.Add(int64(-1), 100, 50)

	f.Fuzz(func(t *testing.T, seed int64, numWorkers, opsPerWorker int) {
		// Constrain inputs to reasonable ranges
		if numWorkers < 1 {
			numWorkers = 1
		}
		if numWorkers > 100 {
			numWorkers = 100
		}
		if opsPerWorker < 1 {
			opsPerWorker = 1
		}
		if opsPerWorker > 50 {
			opsPerWorker = 50
		}

		// Create schema
		s := buildFuzzSchema(t)
		g := New(s)
		ctx := context.Background()

		// Run concurrent operations
		var wg sync.WaitGroup
		for w := range numWorkers {
			wg.Add(1)
			go func(workerID int) {
				defer wg.Done()
				// Each worker gets deterministic randomness based on seed and ID
				r := rand.New(rand.NewSource(seed + int64(workerID))) //nolint:gosec // fuzz test
				runFuzzOperations(t, g, s, ctx, r, workerID, opsPerWorker)
			}(w)
		}
		wg.Wait()

		// Verify final graph state is consistent
		verifyGraphConsistency(t, g)
	})
}

// buildFuzzSchema creates a simple schema for fuzz testing.
func buildFuzzSchema(t *testing.T) *schema.Schema {
	t.Helper()

	s, result := build.NewBuilder().
		WithName("fuzz").
		WithSourceID(location.MustNewSourceID("test://fuzz.yammm")).
		AddType("Person").
		WithPrimaryKey("id", schema.StringConstraint{}).
		WithProperty("name", schema.StringConstraint{}).
		WithRelation("friend", schema.LocalTypeRef("Person", location.Span{}), true, false).
		Done().
		Build()

	if result.HasErrors() {
		t.Fatalf("Failed to build fuzz schema: %s", result.String())
	}
	return s
}

// runFuzzOperations performs random graph operations.
func runFuzzOperations(t *testing.T, g *Graph, s *schema.Schema, ctx context.Context, r *rand.Rand, workerID, numOps int) {
	t.Helper()

	personType, ok := s.Type("Person")
	if !ok {
		t.Fatal("Person type not found")
	}

	for range numOps {
		op := r.Intn(4) // 0=Add, 1=Snapshot, 2=Check, 3=InstanceByKey

		switch op {
		case 0: // Add
			id := r.Intn(100) // Use limited ID space to create some duplicates
			pk := []any{formatID(workerID, id)}
			inst := instance.NewValidInstance(
				"Person",
				personType.ID(),
				immutable.WrapKey(pk),
				immutable.WrapProperties(map[string]any{"name": formatName(workerID, id)}),
				nil, nil, nil,
			)
			// Ignore errors/results - we just want to test for races/panics
			_, _ = g.Add(ctx, inst)

		case 1: // Snapshot
			snap := g.Snapshot()
			// Read from snapshot to ensure no concurrent modification issues
			_ = snap.Types()
			_ = snap.Edges()
			_ = snap.Unresolved()

		case 2: // Check
			_, _ = g.Check(ctx)

		case 3: // InstanceByKey
			id := r.Intn(100)
			snap := g.Snapshot()
			_, _ = snap.InstanceByKey("Person", FormatKey(formatID(workerID, id)))
		}
	}
}

func formatID(workerID, id int) string {
	return "p" + string(rune('A'+workerID%26)) + string(rune('0'+id%10))
}

func formatName(workerID, id int) string {
	return "Person-" + formatID(workerID, id)
}

// verifyGraphConsistency checks that the graph is in a valid state.
func verifyGraphConsistency(t *testing.T, g *Graph) {
	t.Helper()

	snap := g.Snapshot()

	// Basic consistency checks
	types := snap.Types()
	for _, typeName := range types {
		instances := snap.InstancesOf(typeName)
		// Each instance should be retrievable by key
		for _, inst := range instances {
			_, ok := snap.InstanceByKey(typeName, inst.PrimaryKey().String())
			if !ok {
				t.Errorf("Instance %s/%s not retrievable by key", typeName, inst.PrimaryKey())
			}
		}
	}

	// All edges should have valid source instances
	for _, edge := range snap.Edges() {
		srcType := edge.Source().TypeName()
		srcKey := edge.Source().PrimaryKey().String()
		_, ok := snap.InstanceByKey(srcType, srcKey)
		if !ok {
			t.Errorf("Edge source %s/%s not in graph", srcType, srcKey)
		}
	}
}

// FuzzGraph_AddSequence tests that adding instances in various orders
// produces consistent results.
func FuzzGraph_AddSequence(f *testing.F) {
	f.Add(int64(0))
	f.Add(int64(42))
	f.Add(int64(1234567890))

	f.Fuzz(func(t *testing.T, seed int64) {
		s := buildFuzzSchema(t)
		g := New(s)
		ctx := context.Background()

		personType, _ := s.Type("Person")
		r := rand.New(rand.NewSource(seed)) //nolint:gosec // fuzz test

		// Generate a sequence of 20 random instance IDs
		ids := make([]int, 20)
		for i := range ids {
			ids[i] = r.Intn(10) // Only 10 unique IDs to create overlaps
		}

		// Add instances in the random order
		for _, id := range ids {
			pk := []any{"p" + string(rune('0'+id))}
			inst := instance.NewValidInstance(
				"Person",
				personType.ID(),
				immutable.WrapKey(pk),
				immutable.WrapProperties(map[string]any{"name": "Person-" + string(rune('0'+id))}),
				nil, nil, nil,
			)
			_, _ = g.Add(ctx, inst)
		}

		// Count unique IDs we tried to add
		uniqueIDs := make(map[int]bool)
		for _, id := range ids {
			uniqueIDs[id] = true
		}

		// Graph should have exactly the unique instances (duplicates ignored)
		snap := g.Snapshot()
		instanceCount := len(snap.InstancesOf("Person"))
		if instanceCount != len(uniqueIDs) {
			t.Errorf("Expected %d instances, got %d", len(uniqueIDs), instanceCount)
		}
	})
}
