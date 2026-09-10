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

// discoverPairs returns every corpus input that owes a golden — which is every
// input that is neither a round-trip fixture nor recorded in unpairedByDesign.
// Membership does not depend on the golden existing, so a new fixture is run
// (and, under -update, given its golden) rather than skipped for lacking one.
// It fails on an empty result: a walk that matches nothing reports success
// having formatted no fixture.
func discoverPairs(t *testing.T) []string {
	t.Helper()
	var pairs []string
	for _, in := range discoverInputs(t) {
		if strings.HasPrefix(in, roundTripDir+"/") || unpairedByDesign[in] != "" {
			continue
		}
		pairs = append(pairs, in)
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
			// A golden that records the layout the formatter does not yet
			// produce is compared by hand, so the mismatch can be pinned.
			if defect, pinned := pendingRepairs[in]; pinned {
				want, err := os.ReadFile(in + goldenSuffix)
				if err != nil {
					t.Fatalf("read golden: %v", err)
				}
				if got == string(want) {
					t.Errorf("the pinned defect (%s) no longer reproduces; delete its pendingRepairs entry", defect)
				}
				return
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

// TestCorpusIsFullyReached fails on a golden whose input is gone, which reads
// as coverage and asserts nothing. The other direction — an input no driver
// claims — cannot arise: discoverPairs claims everything that is not a
// round-trip fixture or recorded unpaired.
func TestCorpusIsFullyReached(t *testing.T) {
	t.Parallel()

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
