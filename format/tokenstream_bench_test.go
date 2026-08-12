package format

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/internal/parse"
	"github.com/simon-lentz/yammm/location"
)

// minBenchCorpus keeps an emptied fixture directory from benchmarking
// nothing: the corpus is testdata's own top-level .yammm inputs, and fewer
// than this many means the walk is broken, not the workload small.
const minBenchCorpus = 20

// benchFixture is one corpus entry. A slice, not a map: Go randomises map
// iteration order, and the corpus benchmark walks the corpus inside its
// measured region, so a map would put that randomisation in the measurement.
type benchFixture struct {
	name string
	src  string
}

// benchCorpus loads every top-level testdata fixture, failing loudly on a
// fixture the formatter itself would reject. Paths come back sorted from
// Glob, so the walk order is stable across runs.
func benchCorpus(b *testing.B) []benchFixture {
	b.Helper()
	paths, err := filepath.Glob(filepath.Join("testdata", "*.yammm"))
	if err != nil {
		b.Fatalf("glob corpus: %v", err)
	}
	if len(paths) < minBenchCorpus {
		b.Fatalf("bench corpus holds %d fixtures, floor is %d", len(paths), minBenchCorpus)
	}
	corpus := make([]benchFixture, 0, len(paths))
	for _, p := range paths {
		src, err := os.ReadFile(p)
		if err != nil {
			b.Fatalf("read %s: %v", p, err)
		}
		if _, err := TokenStream(string(src)); err != nil {
			b.Fatalf("fixture %s does not format: %v", p, err)
		}
		corpus = append(corpus, benchFixture{name: filepath.Base(p), src: string(src)})
	}
	return corpus
}

// benchSynthetic returns a schema large enough that per-call overhead stops
// dominating: n types, each carrying invariant expressions, the one construct
// whose byte extents the parse view exists to provide.
func benchSynthetic(n int) string {
	var sb strings.Builder
	sb.WriteString("schema \"bench\"\n\n")
	for i := range n {
		fmt.Fprintf(&sb, "type T%03d {\n", i)
		sb.WriteString("\tid String primary\n")
		sb.WriteString("\tname String\n")
		sb.WriteString("\tcount Integer[0, _]\n")
		fmt.Fprintf(&sb, "\t! \"t%03d name\" name -> Len > 0 && name -> Len <= 100\n", i)
		fmt.Fprintf(&sb, "\t! \"t%03d count\" count >= 0 ? { count < 1000 : false }\n", i)
		sb.WriteString("}\n\n")
	}
	return sb.String()
}

func BenchmarkTokenStreamCorpus(b *testing.B) {
	corpus := benchCorpus(b)
	b.ReportAllocs()
	for b.Loop() {
		for _, f := range corpus {
			if _, err := TokenStream(f.src); err != nil {
				b.Fatalf("%s: %v", f.name, err)
			}
		}
	}
}

func BenchmarkTokenStreamSynthetic(b *testing.B) {
	src := benchSynthetic(200)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := TokenStream(src); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkLexOnlyCorpus is BenchmarkLexOnlySynthetic over the tracked
// corpus, so the corpus figure in docs/VERSIONING.md is derivable the same
// way the synthetic one is: the removed work is exactly this benchmark.
func BenchmarkLexOnlyCorpus(b *testing.B) {
	corpus := benchCorpus(b)
	b.ReportAllocs()
	for b.Loop() {
		for _, f := range corpus {
			parse.Lex(f.src)
		}
	}
}

// BenchmarkLexOnlySynthetic measures the avoidable share: TokenStream's
// second read of the source is exactly one parse.Lex.
func BenchmarkLexOnlySynthetic(b *testing.B) {
	src := benchSynthetic(200)
	b.ReportAllocs()
	for b.Loop() {
		parse.Lex(src)
	}
}

// BenchmarkParseOnlySynthetic bounds the unavoidable share for comparison.
func BenchmarkParseOnlySynthetic(b *testing.B) {
	src := benchSynthetic(200)
	b.ReportAllocs()
	for b.Loop() {
		parse.Parse([]byte(src), location.SourceID{})
	}
}
