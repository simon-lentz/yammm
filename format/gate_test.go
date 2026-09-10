package format_test

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/format"
	"github.com/simon-lentz/yammm/schema"
)

// The variants perturb a corpus input in the ways the formatter has been
// measured to mishandle: operators written without spaces, a trailing comment
// carrying brackets and a quote, a blank line inside a block comment, and a
// trailing comment that ends in a brace.
var (
	variantTightOps   = regexp.MustCompile(`[ \t]*(&&|\|\|)[ \t]*`)
	variantLineEnd    = regexp.MustCompile(`(?m)([^\n])$`)
	variantBlockStart = regexp.MustCompile(`/\*`)
)

// variantsOf returns src and its perturbations, named.
func variantsOf(src string) map[string]string {
	return map[string]string{
		"original":             src,
		"tight-operators":      variantTightOps.ReplaceAllString(src, "$1"),
		"trailing-comment":     variantLineEnd.ReplaceAllString(src, "$1 // c ] { \"q\""),
		"blank-in-block":       variantBlockStart.ReplaceAllString(src, "/*\n\n"),
		"comment-ending-brace": variantLineEnd.ReplaceAllString(src, "$1 // c {"),
	}
}

// parses reports whether src has no syntax error, which is the whole of what
// TokenStream requires of its input.
func parses(src string) bool {
	_, err := format.TokenStream(src)
	return err == nil || !strings.HasPrefix(err.Error(), "parse failed")
}

// repositorySchemas returns every .yammm in the repository outside this
// package's own fixtures, read from the module root above this package. Dot
// directories and node_modules hold no tracked schema.
func repositorySchemas(tb testing.TB) []string {
	tb.Helper()
	var srcs []string
	err := filepath.WalkDir("..", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && path != ".." && (strings.HasPrefix(d.Name(), ".") || d.Name() == "node_modules" || path == filepath.Join("..", "format", "testdata")) {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, ".yammm") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		srcs = append(srcs, string(b))
		return nil
	})
	if err != nil {
		tb.Fatalf("walking the repository: %v", err)
	}
	if len(srcs) < 100 {
		tb.Fatalf("found %d schemas; the walk is not reaching the repository", len(srcs))
	}
	return srcs
}

// formatOutcome runs one input through the formatter and returns the first
// property it fails, or empty: the output holds the input's tokens and
// comments, is a fixed point, and obeys the documented layout.
func formatOutcome(src string) string {
	out, err := format.TokenStream(src)
	if err != nil {
		return "TokenStream: " + err.Error()
	}
	if err := format.Preserves(src, out); err != nil {
		return "output does not preserve the source: " + err.Error()
	}
	again, err := format.TokenStream(out)
	if err != nil {
		return "TokenStream on its own output: " + err.Error()
	}
	if again != out {
		return "not idempotent"
	}
	if v := format.LayoutViolations(out); len(v) > 0 {
		return "layout: " + strings.Join(v, "; ")
	}
	return ""
}

// TestTokenStream_CorpusVariants runs every corpus input and its variants
// through the formatter's three properties. A variant that no longer parses is
// skipped: the formatter owes nothing to a source that has a syntax error.
func TestTokenStream_CorpusVariants(t *testing.T) {
	t.Parallel()

	inputs := discoverInputs(t)
	checked := 0
	for _, path := range inputs {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		variants := variantsOf(string(src))
		names := make([]string, 0, len(variants))
		for name := range variants {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			v := variants[name]
			if !parses(v) {
				continue
			}
			checked++
			key := path + "#" + name
			t.Run(key, func(t *testing.T) {
				t.Parallel()
				checkRepairState(t, key, formatOutcome(v))
			})
		}
	}
	if checked < 100 {
		t.Fatalf("checked %d inputs; the corpus walk is not reaching the fixtures", checked)
	}
}

// FuzzTokenStream is the formatter's property under fuzzing: for any input
// that parses, formatting keeps its tokens and comments, is a fixed point,
// and — when the input loads — keeps its structural hash. The seeds are the
// repository's schemas; this package's fixtures, built to reproduce defects,
// are the corpus test's.
func FuzzTokenStream(f *testing.F) {
	for _, src := range repositorySchemas(f) {
		f.Add(src)
	}
	f.Fuzz(func(t *testing.T, src string) {
		if !parses(src) {
			return
		}
		if failure := formatOutcome(src); failure != "" {
			t.Fatal(failure)
		}
		s, result := schema.LoadString(context.Background(), src, "fuzz.yammm")
		if result.Err() != nil || s == nil {
			return
		}
		out, _ := format.TokenStream(src)
		after, ok := loadHash(t, out)
		if !ok {
			t.Fatalf("a loading schema no longer loads after formatting:\n%s", out)
		}
		if before := schema.StructuralHash(s); after != before {
			t.Fatalf("formatting changed the schema's meaning: %s -> %s\n%s", before, after, out)
		}
	})
}
