package format

import (
	"strings"
	"testing"
)

// TestTryCollapseEnum_FindsTheCodesEnumBracket pins that collapsing a
// multiline Enum locates the Enum[ that is code. An "Enum[" inside a literal
// earlier on the same line is data, and slicing the prefix at it truncates the
// declaration into output that does not parse.
func TestTryCollapseEnum_FindsTheCodesEnumBracket(t *testing.T) {
	const literal = `"Enum[z"`
	first := "\t@d(" + literal + ") status Enum["
	at := strings.Index(first, literal)

	ls := []line{
		{
			text:  first,
			class: lineContent,
			lex:   lexical{commentAt: noComment, literals: []span{{start: at, end: at + len(literal)}}},
		},
		contentLine("\t\t\"a\","),
		contentLine("\t] required"),
	}

	got := tryCollapseEnum(ls)
	if len(got) != 1 {
		t.Fatalf("expected one collapsed line, got %d: %q", len(got), textsOf(got))
	}
	want := "\t@d(" + literal + ") status Enum[\"a\"] required"
	if got[0].text != want {
		t.Errorf("prefix sliced at the literal's Enum[\n got: %q\nwant: %q", got[0].text, want)
	}
}
