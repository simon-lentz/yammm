package format_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/format"
	"github.com/simon-lentz/yammm/internal/yammmtest"
)

const (
	corpusRoot    = "testdata"
	roundTripDir  = "testdata/roundtrip"
	goldenSuffix  = ".golden"
	fixtureSuffix = ".yammm"
)

// unpairedByDesign maps a corpus input with no golden sibling to the test that
// runs it instead. An entry is a claim that some driver reaches the file, so
// adding one to quiet [TestCorpusIsFullyReached] puts a fixture beyond every
// driver's reach.
var unpairedByDesign = map[string]string{
	"testdata/golden/unformatted.yammm": "irregular pair name; driven by TestTokenStream_GoldenFile",
}

// discoverPairs returns every input under testdata with a golden beside it.
// It fails on an empty result: a walk that matches nothing reports success
// having formatted no fixture.
func discoverPairs(t *testing.T) []string {
	t.Helper()
	var pairs []string
	for _, in := range discoverInputs(t) {
		if _, err := os.Stat(in + goldenSuffix); err == nil {
			pairs = append(pairs, in)
		}
	}
	if len(pairs) == 0 {
		t.Fatalf("no golden pairs discovered under %s; the corpus walk is not reaching the fixtures", corpusRoot)
	}
	return pairs
}

// discoverInputs returns every .yammm under testdata, recursively, in walk
// order. Goldens carry a further suffix and are not inputs.
func discoverInputs(t *testing.T) []string {
	t.Helper()
	var inputs []string
	err := filepath.WalkDir(corpusRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, fixtureSuffix) {
			return nil
		}
		inputs = append(inputs, filepath.ToSlash(path))
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", corpusRoot, err)
	}
	if len(inputs) == 0 {
		t.Fatalf("no .yammm inputs discovered under %s", corpusRoot)
	}
	return inputs
}

// TestTokenStream_CorpusPairs formats every discovered pair against its
// committed golden, which regenerates under -update. Each output is formatted
// again and must be a fixed point, so format-on-save never oscillates. The
// corpus is discovered rather than listed: a listed one cannot see a fixture
// added for a defect it does not already name.
func TestTokenStream_CorpusPairs(t *testing.T) {
	t.Parallel()

	pairs := discoverPairs(t)
	t.Logf("discovered %d golden pairs under %s", len(pairs), corpusRoot)

	for _, in := range pairs {
		t.Run(in, func(t *testing.T) {
			t.Parallel()

			src, err := os.ReadFile(in)
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}
			got, err := format.TokenStream(string(src))
			if err != nil {
				t.Fatalf("TokenStream returned an error: %v", err)
			}
			// yammmtest.Golden resolves names against testdata itself.
			yammmtest.Golden(t, strings.TrimPrefix(in, corpusRoot+"/"), []byte(got))

			second, err := format.TokenStream(got)
			if err != nil {
				t.Fatalf("TokenStream returned an error on its own output: %v", err)
			}
			if second != got {
				t.Errorf("formatting is not idempotent for %s", in)
			}
		})
	}
}

// TestCorpusIsFullyReached fails on a fixture no driver claims and on a golden
// whose input is gone. Either one reads as coverage and asserts nothing.
func TestCorpusIsFullyReached(t *testing.T) {
	t.Parallel()

	var orphans []string
	for _, in := range discoverInputs(t) {
		switch {
		case strings.HasPrefix(in, roundTripDir+"/"):
		case unpairedByDesign[in] != "":
		default:
			if _, err := os.Stat(in + goldenSuffix); err != nil {
				orphans = append(orphans, in)
			}
		}
	}
	if len(orphans) > 0 {
		t.Errorf("%d corpus input(s) reached by no driver — add a golden, move the file under %s, "+
			"or record it in unpairedByDesign with the test that runs it:\n  %s",
			len(orphans), roundTripDir, strings.Join(orphans, "\n  "))
	}

	for _, g := range discoverGoldens(t) {
		// formatted.yammm.golden is unformatted.yammm's golden under an
		// irregular name, so it has no same-stem input by construction.
		if g == "testdata/golden/formatted.yammm.golden" {
			continue
		}
		if _, err := os.Stat(strings.TrimSuffix(g, goldenSuffix)); err != nil {
			t.Errorf("golden %s has no input", g)
		}
	}
}

// discoverGoldens returns every .golden under testdata, in walk order.
func discoverGoldens(t *testing.T) []string {
	t.Helper()
	var goldens []string
	err := filepath.WalkDir(corpusRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, goldenSuffix) {
			goldens = append(goldens, filepath.ToSlash(path))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", corpusRoot, err)
	}
	return goldens
}
