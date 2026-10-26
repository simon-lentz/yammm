package diag

import (
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

// Text of at most 32 bytes is rendered whole; longer text is cut to whole
// characters within 32 bytes, with its length in bytes beside the head.
func TestQuoteValue_CutsLongTextToWholeCharacters(t *testing.T) {
	t.Parallel()
	thirtyTwo := strings.Repeat("a", 32)
	rows := []struct{ in, quoted, plain string }{
		{"", `""`, ""},
		{"abc", `"abc"`, "abc"},
		{thirtyTwo, strconv.Quote(thirtyTwo), thirtyTwo},
		{thirtyTwo + "b", strconv.Quote(thirtyTwo) + "… (33 bytes)", thirtyTwo + "… (33 bytes)"},
		{strings.Repeat("a", 31) + "é", strconv.Quote(strings.Repeat("a", 31)) + "… (33 bytes)", strings.Repeat("a", 31) + "… (33 bytes)"},
		{"a\xffb", `"a\xffb"`, `"a\xffb"`},
		{strings.Repeat("\xff", 40), strconv.Quote(strings.Repeat("\xff", 32)) + "… (40 bytes)", strconv.Quote(strings.Repeat("\xff", 32)) + "… (40 bytes)"},
		{strings.Repeat("z", 100000), strconv.Quote(strings.Repeat("z", 32)) + "… (100000 bytes)", strings.Repeat("z", 32) + "… (100000 bytes)"},
	}
	for _, r := range rows {
		if got := QuoteValue(r.in); got != r.quoted {
			t.Errorf("QuoteValue(%.40q) = %q, want %q", r.in, got, r.quoted)
		}
		if got := ValueText(r.in); got != r.plain {
			t.Errorf("ValueText(%.40q) = %q, want %q", r.in, got, r.plain)
		}
		for _, got := range []string{QuoteValue(r.in), ValueText(r.in)} {
			if !utf8.ValidString(got) {
				t.Errorf("%.40q renders as %q, which is not valid UTF-8", r.in, got)
			}
		}
	}
}
