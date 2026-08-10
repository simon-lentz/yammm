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

// benchCorpus loads every top-level testdata fixture, failing loudly on a
// fixture the formatter itself would reject.
func benchCorpus(b *testing.B) map[string]string {
	b.Helper()
	paths, err := filepath.Glob(filepath.Join("testdata", "*.yammm"))
	if err != nil {
		b.Fatalf("glob corpus: %v", err)
	}
	if len(paths) < minBenchCorpus {
		b.Fatalf("bench corpus holds %d fixtures, floor is %d", len(paths), minBenchCorpus)
	}
	corpus := make(map[string]string, len(paths))
	for _, p := range paths {
		src, err := os.ReadFile(p)
		if err != nil {
			b.Fatalf("read %s: %v", p, err)
		}
		if _, err := TokenStream(string(src)); err != nil {
			b.Fatalf("fixture %s does not format: %v", p, err)
		}
		corpus[filepath.Base(p)] = string(src)
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
		for name, src := range corpus {
			if _, err := TokenStream(src); err != nil {
				b.Fatalf("%s: %v", name, err)
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
