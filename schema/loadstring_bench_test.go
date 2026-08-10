package schema_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/simon-lentz/yammm/schema"
)

// The LoadString benchmarks measure the whole public loading path — parse,
// completion and validation — over a fixture set that lives beside them.
//
// The set is self-contained on purpose. A benchmark that walked the repository
// root would depend on five other packages' testdata and would have a root to
// resolve, which is the defect class that once made a corpus benchmark report a
// real number for a fraction of the intended work. A test binary's working
// directory is its own package directory, so the relative path below cannot
// resolve anywhere else.

// benchCorpusDir holds the fixtures both benchmarks read.
const benchCorpusDir = "testdata/bench"

// minBenchCorpus is the floor below which the directory cannot be the intended
// set, so an emptied or renamed directory fails loudly instead of benchmarking
// nothing.
const minBenchCorpus = 5

type benchInput struct {
	name string
	src  string
}

// benchCorpus reads the fixture set and loads each file once, failing the
// benchmark if any reports a diagnostic: every fixture must exercise the
// success path, or the number describes error handling instead of loading.
func benchCorpus(b *testing.B) []benchInput {
	b.Helper()

	entries, err := os.ReadDir(benchCorpusDir)
	if err != nil {
		b.Fatalf("read %s: %v", benchCorpusDir, err)
	}

	var out []benchInput
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".yammm" {
			continue
		}
		src, err := os.ReadFile(filepath.Join(benchCorpusDir, e.Name()))
		if err != nil {
			b.Fatalf("read %s: %v", e.Name(), err)
		}
		out = append(out, benchInput{name: e.Name(), src: string(src)})
	}

	if len(out) < minBenchCorpus {
		b.Fatalf("corpus of %d fixtures under %s is smaller than the floor of %d",
			len(out), benchCorpusDir, minBenchCorpus)
	}

	ctx := b.Context()
	for _, in := range out {
		if _, res := schema.LoadString(ctx, in.src, in.name); res.HasErrors() {
			b.Fatalf("fixture %s does not load cleanly: %v", in.name, res)
		}
	}
	return out
}

// benchLargest picks the largest fixture by source size at run time and names
// it, so the recorded figure says which file it describes.
func benchLargest(b *testing.B, ins []benchInput) benchInput {
	b.Helper()
	best := ins[0]
	for _, in := range ins[1:] {
		if len(in.src) > len(best.src) {
			best = in
		}
	}
	b.Logf("largest fixture: %s (%d bytes)", best.name, len(best.src))
	return best
}

// BenchmarkLoadStringCorpus loads every fixture once per iteration. Batch
// loading is what a pipeline consumer does, so this figure matters as much as
// the largest-fixture one.
func BenchmarkLoadStringCorpus(b *testing.B) {
	ins := benchCorpus(b)
	ctx := b.Context()
	b.ReportAllocs()
	for b.Loop() {
		for _, in := range ins {
			_, _ = schema.LoadString(ctx, in.src, in.name)
		}
	}
}

// BenchmarkLoadStringLargest loads the largest fixture, which is the figure a
// single-file consumer — an editor saving a schema — feels directly.
func BenchmarkLoadStringLargest(b *testing.B) {
	in := benchLargest(b, benchCorpus(b))
	ctx := b.Context()
	b.ReportAllocs()
	for b.Loop() {
		_, _ = schema.LoadString(ctx, in.src, in.name)
	}
}
