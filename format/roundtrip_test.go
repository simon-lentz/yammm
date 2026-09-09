package format_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/format"
	"github.com/simon-lentz/yammm/schema"
)

// TestTokenStream_CorpusRoundTrip pins what formatting does to a schema that
// loads clean: the output must load, mean the same thing, and be a fixed point.
// Every fixture here is a schema the formatter once corrupted, kept as a
// regression pin.
//
// A golden is the wrong instrument for these — it records whatever the
// formatter emits, so it passes on corrupt output and locks the corruption in.
// The oracle is the structural hash, which catches a value the formatter
// changed without breaking the parse.
func TestTokenStream_CorpusRoundTrip(t *testing.T) {
	t.Parallel()

	fixtures := discoverRoundTrip(t)
	t.Logf("discovered %d round-trip fixtures", len(fixtures))

	for _, path := range fixtures {
		t.Run(strings.TrimPrefix(path, roundTripDir+"/"), func(t *testing.T) {
			t.Parallel()

			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}
			before, ok := loadHash(t, string(src))
			if !ok {
				t.Fatalf("fixture does not load; a round-trip claim needs a clean input")
			}

			out, err := format.TokenStream(string(src))
			if err != nil {
				t.Fatalf("TokenStream returned an error: %v", err)
			}
			after, loaded := loadHash(t, out)
			if !loaded {
				t.Fatalf("formatting turned a loading schema into one that does not load:\n%s", out)
			}
			if after != before {
				t.Errorf("formatting changed the schema's meaning: hash %s -> %s\n%s", before, after, out)
			}

			second, err := format.TokenStream(out)
			if err != nil {
				t.Fatalf("TokenStream returned an error on its own output: %v", err)
			}
			if second != out {
				t.Errorf("formatting is not idempotent")
			}
		})
	}
}

// loadHash loads src and returns its structural hash, reporting whether the
// load produced no error.
func loadHash(t *testing.T, src string) (string, bool) {
	t.Helper()
	s, result := schema.LoadString(context.Background(), src, "roundtrip.yammm")
	if result.Err() != nil || s == nil {
		return "", false
	}
	return schema.StructuralHash(s), true
}

// discoverRoundTrip returns every fixture under the round-trip directory. It
// fails on an empty result, which would pass over nothing.
func discoverRoundTrip(t *testing.T) []string {
	t.Helper()
	var found []string
	for _, in := range discoverInputs(t) {
		if strings.HasPrefix(in, roundTripDir+"/") {
			found = append(found, in)
		}
	}
	if len(found) == 0 {
		t.Fatalf("no round-trip fixtures discovered under %s", roundTripDir)
	}
	return found
}
