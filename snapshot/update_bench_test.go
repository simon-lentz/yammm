package snapshot_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

// benchInputs caches the generated ~15 MB Marshal output plus the
// loaded schema so every benchmark invocation measures the primitive
// under test rather than the setup cost.
type benchInputs struct {
	schema  schema.Schema
	data    []byte
	newMeta map[string]string
}

var (
	benchInputsOnce sync.Once
	benchInputsVal  benchInputs
)

// loadBenchInputs materializes a realistic snapshot of about 14.7 MB once
// and caches it. The instance count sets the size on the in-tree test
// schema shape; the benchmarks measure the update primitive, not the
// marshal setup.
func loadBenchInputs(tb testing.TB) benchInputs {
	tb.Helper()
	benchInputsOnce.Do(func() {
		s, err := schema.LoadString(tb.Context(), `schema "test"

type Person {
	id String primary
	name String required
	email String required
	role String required
}
`, "bench.yammm")
		if err != nil {
			tb.Fatalf("bench schema: %v", err)
		}

		v := instance.NewValidator(s)
		g := graph.New(s)
		ctx := context.Background()
		// 100_000 instances of about 147 bytes each: about 14.7 MB.
		const n = 100_000
		for i := range n {
			inst, err := v.ValidateOne(ctx, "Person", instance.RawInstance{Properties: map[string]any{
				"id":    fmt.Sprintf("p%06d", i),
				"name":  fmt.Sprintf("Person %06d", i),
				"email": fmt.Sprintf("person%06d@example.org", i),
				"role":  "contributor",
			}})
			if err != nil {
				tb.Fatalf("bench validate: %v", err)
			}
			if err := g.Add(ctx, inst); err != nil {
				tb.Fatalf("bench add: %v", err)
			}
		}
		snap := g.Snapshot()
		data, readErr := snapshot.Marshal(ctx, snap)
		if readErr != nil {
			tb.Fatalf("bench marshal: %v", readErr)
		}
		benchInputsVal = benchInputs{
			schema:  s,
			data:    data,
			newMeta: map[string]string{"phase": "link", "pipeline_completed": "true"},
		}
	})
	return benchInputsVal
}

// BenchmarkUpdateMetadata measures the metadata-rewrite fast path.
func BenchmarkUpdateMetadata(b *testing.B) {
	in := loadBenchInputs(b)
	b.ReportAllocs()
	b.SetBytes(int64(len(in.data)))
	b.ResetTimer()
	for b.Loop() {
		out, err := snapshot.UpdateMetadata(context.Background(), in.data, in.newMeta)
		if err != nil {
			b.Fatalf("UpdateMetadata: %v", err)
		}
		if len(out) == 0 {
			b.Fatal("UpdateMetadata returned empty output")
		}
	}
}

// BenchmarkLoadMarshalRoundTrip measures the slow-path equivalent:
// Load the snapshot then Marshal it back with the new metadata. This is
// the pre-v0.3.0 cost profile for SetSnapshotFlag / ForceComplete /
// RecordVerifyMissing.
func BenchmarkLoadMarshalRoundTrip(b *testing.B) {
	in := loadBenchInputs(b)
	ctx := context.Background()
	b.ReportAllocs()
	b.SetBytes(int64(len(in.data)))
	b.ResetTimer()
	for b.Loop() {
		loaded, err := snapshot.Unmarshal(ctx, in.data, in.schema)
		if err != nil {
			b.Fatalf("Load: %v", err)
		}
		out, readErr := snapshot.Marshal(ctx, loaded, snapshot.WithMetadata(in.newMeta))
		if readErr != nil {
			b.Fatalf("Marshal: %v", readErr)
		}
		if len(out) == 0 {
			b.Fatal("Marshal returned empty output")
		}
	}
}
