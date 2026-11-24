package parse

import (
	"testing"
	"unicode/utf8"
)

// Quote's literal is text the source rules accept, unquote reads it back as the
// value for every rune, and the lexer reads each escape case as one STRING.
func TestQuote_UnquoteReadsTheValueBack(t *testing.T) {
	t.Parallel()
	escapes := []string{"", "m", `say "hi"`, `a\b`, "a\nb", "a\r\nb", "nul\x00", "mark\uFEFF", "tab\t", "it's", "é😀"}
	values := append([]string(nil), escapes...)
	for r := rune(0); r <= utf8.MaxRune; r++ {
		if utf8.ValidRune(r) {
			values = append(values, string(r))
		}
	}
	for _, v := range values {
		lit := Quote(v)
		if got, err := unquote(lit); err != nil || got != v {
			t.Fatalf("unquote(Quote(%q)) = %q, %v", v, got, err)
		}
		if f, refused := nextTextFault(lit, 0); refused {
			t.Fatalf("Quote(%q) = %s holds text the source rules refuse: %s", v, lit, f.why)
		}
	}
	for _, v := range escapes {
		if toks := Lex(Quote(v)); len(toks) != 1 || toks[0].Kind != "STRING" {
			t.Errorf("Lex(Quote(%q)) = %+v, want one STRING", v, toks)
		}
	}
}
