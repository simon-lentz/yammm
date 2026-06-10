package snapshot_test

import (
	"context"
	"fmt"
	"runtime"
	"slices"
	"sync"
	"testing"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

// benchInputs caches the generated ~20 MB Marshal output plus the
// loaded schema so every benchmark invocation measures the primitive
// under test rather than the setup cost.
type benchInputs struct {
	schema  *schema.Schema
	data    []byte
	newMeta map[string]string
}

var (
	benchInputsOnce sync.Once
	benchInputsVal  benchInputs
)

// loadBenchInputs materializes a realistic ~20 MB snapshot once and
// caches it. The target size is approximate — scaled via instance count
// to land between 15 MB and 25 MB on the in-tree test schema shape;
// the benchmarks measure the update primitive, not the marshal setup.
func loadBenchInputs(tb testing.TB) benchInputs {
	tb.Helper()
	benchInputsOnce.Do(func() {
		s, sres := schema.NewBuilder().
			WithName("test").
			WithSourceID(location.MustNewSourceID("test://bench.yammm")).
			AddType("Person").
			WithPrimaryKey("id", schema.NewStringConstraint()).
			WithProperty("name", schema.NewStringConstraint()).
			WithProperty("email", schema.NewStringConstraint()).
			WithProperty("role", schema.NewStringConstraint()).
			Done().
			Build()
		if sres.HasErrors() {
			tb.Fatalf("bench schema: %v", sres.Err())
		}

		typ, _ := s.Type("Person")
		g := graph.New(s)
		ctx := context.Background()
		// ~100_000 instances × ~200 bytes each ≈ 20 MB.
		const n = 100_000
		for i := range n {
			inst := instance.NewValidInstance(
				"Person", typ.ID(),
				immutable.WrapKey([]any{fmt.Sprintf("p%06d", i)}),
				immutable.WrapProperties(map[string]any{
					"name":  fmt.Sprintf("Person %06d", i),
					"email": fmt.Sprintf("person%06d@example.org", i),
					"role":  "contributor",
				}),
				nil, nil, nil,
			)
			g.Add(ctx, inst)
		}
		snap := g.Snapshot()
		data, res := snapshot.Marshal(ctx, snap)
		if res.HasErrors() {
			tb.Fatalf("bench marshal: %v", res.Err())
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
		out, res := snapshot.UpdateMetadata(context.Background(), in.data, in.newMeta)
		if res.HasErrors() {
			b.Fatalf("UpdateMetadata: %v", res.Err())
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
		loaded, loadRes := snapshot.Load(ctx, in.data, in.schema)
		if loadRes.HasErrors() {
			b.Fatalf("Load: %v", loadRes.Err())
		}
		out, marshalRes := snapshot.Marshal(ctx, loaded, snapshot.WithMetadata(in.newMeta))
		if marshalRes.HasErrors() {
			b.Fatalf("Marshal: %v", marshalRes.Err())
		}
		if len(out) == 0 {
			b.Fatal("Marshal returned empty output")
		}
	}
}

// BenchmarkUpdateMetadataRatio emits the per-run speedup via
// b.ReportMetric("x-speedup") so operators and reviewers see the
// absolute ratio in standard go test -bench output alongside the
// absolute timings. Non-gated; the 3× floor gate is
// TestUpdateMetadataRatioFloor.
func BenchmarkUpdateMetadataRatio(b *testing.B) {
	in := loadBenchInputs(b)
	ctx := context.Background()

	// Single-sample timing for each path. Run both back-to-back so they
	// experience the same warm cache state.
	fast := testing.Benchmark(func(b *testing.B) {
		b.Helper()
		for b.Loop() {
			_, res := snapshot.UpdateMetadata(ctx, in.data, in.newMeta)
			if res.HasErrors() {
				b.Fatalf("UpdateMetadata: %v", res.Err())
			}
		}
	})
	slow := testing.Benchmark(func(b *testing.B) {
		b.Helper()
		for b.Loop() {
			loaded, loadRes := snapshot.Load(ctx, in.data, in.schema)
			if loadRes.HasErrors() {
				b.Fatalf("Load: %v", loadRes.Err())
			}
			_, marshalRes := snapshot.Marshal(ctx, loaded, snapshot.WithMetadata(in.newMeta))
			if marshalRes.HasErrors() {
				b.Fatalf("Marshal: %v", marshalRes.Err())
			}
		}
	})

	ratio := float64(slow.NsPerOp()) / float64(fast.NsPerOp())
	b.ReportMetric(ratio, "x-speedup")
	b.ReportMetric(float64(fast.NsPerOp()), "ns/op-fast")
	b.ReportMetric(float64(slow.NsPerOp()), "ns/op-slow")
}

// TestUpdateMetadataRatioFloor asserts UpdateMetadata is at least 3×
// faster than Load + Marshal on a 20 MB input. Median of 5 samples per
// path; runtime.GC between samples to damp CI-runner noise.
//
// Threshold rationale: 3× is the floor below which the
// primitive stops being interesting. Target on M2-class hardware is
// ~8-10×, on typical CI runners ~6-8×; a drop below 3× signals a
// regression that operator-facing documentation (the ~10% cost claim)
// no longer holds.
//
// Skipped under `go test -short` since the 20 MB setup is ~2 seconds
// and the median-of-5 loop dominates short-test runtime.
func TestUpdateMetadataRatioFloor(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping ratio-floor benchmark gate under -short")
	}
	in := loadBenchInputs(t)
	ctx := context.Background()

	fastSamples := make([]int64, 0, 5)
	slowSamples := make([]int64, 0, 5)
	for range 5 {
		runtime.GC()
		fast := testing.Benchmark(func(b *testing.B) {
			b.Helper()
			for b.Loop() {
				_, res := snapshot.UpdateMetadata(ctx, in.data, in.newMeta)
				if res.HasErrors() {
					b.Fatalf("UpdateMetadata: %v", res.Err())
				}
			}
		})
		fastSamples = append(fastSamples, fast.NsPerOp())

		runtime.GC()
		slow := testing.Benchmark(func(b *testing.B) {
			b.Helper()
			for b.Loop() {
				loaded, loadRes := snapshot.Load(ctx, in.data, in.schema)
				if loadRes.HasErrors() {
					b.Fatalf("Load: %v", loadRes.Err())
				}
				_, marshalRes := snapshot.Marshal(ctx, loaded, snapshot.WithMetadata(in.newMeta))
				if marshalRes.HasErrors() {
					b.Fatalf("Marshal: %v", marshalRes.Err())
				}
			}
		})
		slowSamples = append(slowSamples, slow.NsPerOp())
	}

	fastMedian := median(fastSamples)
	slowMedian := median(slowSamples)
	ratio := float64(slowMedian) / float64(fastMedian)
	t.Logf("UpdateMetadata median: %d ns/op; Load+Marshal median: %d ns/op; ratio: %.2fx",
		fastMedian, slowMedian, ratio)
	const floor = 3.0
	if ratio < floor {
		t.Errorf("UpdateMetadata speedup ratio %.2fx is below the %.2fx floor; "+
			"fast=%d ns/op, slow=%d ns/op",
			ratio, floor, fastMedian, slowMedian)
	}
}

// median returns the median of a sorted-in-place copy of xs. Pure
// stdlib — avoids a math library dependency for a one-line use case.
func median(xs []int64) int64 {
	s := slices.Clone(xs)
	slices.Sort(s)
	return s[len(s)/2]
}
