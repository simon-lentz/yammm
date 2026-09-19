package csv

import (
	"slices"
	"strings"
	"testing"
)

// Every list of up to three elements, each a word of up to two symbols from an
// alphabet holding the separators' own bytes and the escape, under separators of one to four bytes, some
// multibyte and some holding the escape after their first byte; "—" shares the
// first byte of "→", which is escaped wherever it occurs.
// Splitting what the escape wrote must give the list back. The one list that
// cannot come back is a single empty element, whose text is the empty list's;
// the writer refuses it.
func TestListSeparator_SplittingInvertsEscapingForEveryList(t *testing.T) {
	t.Parallel()
	alphabet := []string{"a", "b", "|", `\`, "→", "—"}
	var words []string
	var grow func(prefix string, n int)
	grow = func(prefix string, n int) {
		words = append(words, prefix)
		if n == 0 {
			return
		}
		for _, c := range alphabet {
			grow(prefix+c, n-1)
		}
	}
	grow("", 2)

	var lists [][]string
	for _, a := range words {
		lists = append(lists, []string{a})
		for _, b := range words {
			lists = append(lists, []string{a, b})
			for _, c := range words {
				lists = append(lists, []string{a, b, c})
			}
		}
	}

	for _, sep := range []string{"|", "||", "|||", "a|", "|a", "ab", "aba", `|\`, `a\b`, "→", "→|"} {
		for _, list := range lists {
			escaped := make([]string, len(list))
			for i, e := range list {
				escaped[i] = escapeListElem(e, sep)
			}
			text := strings.Join(escaped, sep)
			got := splitListElems(text, sep)
			if text == "" {
				if len(list) != 1 || list[0] != "" {
					t.Fatalf("sep %q: %q wrote the empty text", sep, list)
				}
				continue
			}
			if !slices.Equal(got, list) {
				t.Fatalf("sep %q: %q wrote %q, which splits to %q", sep, list, text, got)
			}
		}
	}
}
