package parse

import (
	"maps"
	"os"
	"slices"
	"strings"
	"testing"
)

// A package outside this one reads testdata/reserved_words.txt for the whole
// vocabulary, which [IsReserved] answers only a name at a time.
func TestReservedWords_NamesFileListsTheVocabulary(t *testing.T) {
	data, err := os.ReadFile("testdata/reserved_words.txt")
	if err != nil {
		t.Fatal(err)
	}
	want := slices.Sorted(maps.Keys(reservedLC))
	want = slices.Sorted(slices.Values(append(want, slices.Collect(maps.Keys(datatypeKeywords))...)))
	if got := strings.Fields(string(data)); !slices.Equal(got, want) {
		t.Errorf("reserved_words.txt holds %v, the vocabulary %v", got, want)
	}
}
