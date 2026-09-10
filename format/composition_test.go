package format_test

import (
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/format"
)

// TestTokenStream_IsItsExportedPhasesComposed is the second implementation of
// the records phases 3 and 4 read: the pipeline must produce what the exported
// phases produce from a lex of the same text, on every corpus input and variant.
func TestTokenStream_IsItsExportedPhasesComposed(t *testing.T) {
	t.Parallel()

	checked := 0
	for _, path := range discoverInputs(t) {
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
			collapsed, err := format.Collapsed(v)
			if err != nil {
				continue
			}
			checked++
			t.Run(path+"#"+name, func(t *testing.T) {
				t.Parallel()
				got, err := format.Unchecked(v)
				if err != nil {
					t.Fatalf("the pipeline failed where phase 1 succeeded: %v", err)
				}
				want := format.Finalize(format.AlignColumns(format.WrapLongLines(collapsed)))
				if got != want {
					t.Errorf("phases 3 and 4 disagree with the exported phases:\n%s", firstDifference(got, want))
				}
			})
		}
	}
	if checked < 100 {
		t.Fatalf("checked %d inputs; the corpus walk is not reaching the fixtures", checked)
	}
}

// firstDifference names the first line at which the pipeline and the exported
// phases part.
func firstDifference(pipeline, exported string) string {
	a, b := strings.Split(pipeline, "\n"), strings.Split(exported, "\n")
	for i := range min(len(a), len(b)) {
		if a[i] != b[i] {
			return "line " + strconv.Itoa(i+1) + ":\n  pipeline: " + a[i] + "\n  exported: " + b[i]
		}
	}
	return "line counts differ: pipeline " + strconv.Itoa(len(a)) + ", exported " + strconv.Itoa(len(b))
}
