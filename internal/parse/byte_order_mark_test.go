package parse

import (
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/location"
)

const markedSource = "\uFEFFschema \"demo\"\n\ntype T {\n\tid String primary\n}\n"

// TestParse_SkipsOneLeadingByteOrderMark pins that a source starting with
// U+FEFF parses as the same text without it, with every span a byte offset
// into the marked source: line 1 shifts by the mark's three bytes and one
// column, and every later line by three bytes alone.
func TestParse_SkipsOneLeadingByteOrderMark(t *testing.T) {
	t.Parallel()
	src := location.NewSourceID("demo.yammm")
	marked, markedIssues := Parse([]byte(markedSource), src)
	plain, plainIssues := Parse([]byte(markedSource[len("\uFEFF"):]), src)
	if len(markedIssues) != 0 || len(plainIssues) != 0 {
		t.Fatalf("issues: marked %v, plain %v", markedIssues, plainIssues)
	}
	if marked.Name != "demo" || len(marked.Types) != 1 {
		t.Fatalf("marked file: name %q, %d types", marked.Name, len(marked.Types))
	}
	const mark = len("\uFEFF")
	if got, want := marked.NameSpan.Start, plain.NameSpan.Start; got.Byte != want.Byte+mark || got.Line != 1 || got.Column != want.Column+1 {
		t.Errorf("name span start = %+v, want byte %d line 1 column %d", got, want.Byte+mark, want.Column+1)
	}
	if got, want := marked.Types[0].Span.Start, plain.Types[0].Span.Start; got.Byte != want.Byte+mark || got.Line != want.Line || got.Column != want.Column {
		t.Errorf("type span start = %+v, want byte %d line %d column %d", got, want.Byte+mark, want.Line, want.Column)
	}
}

// TestParse_RefusesAByteOrderMarkPastTheStart pins that only the first byte
// order mark is skipped: a second one, or one after the header, is a syntax
// error at the mark.
func TestParse_RefusesAByteOrderMarkPastTheStart(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		src      string
		wantByte int
	}{
		"two marks":            {"\uFEFF\uFEFFschema \"demo\"\n", 3},
		"a mark on the header": {"schema \uFEFF\"demo\"\n", 7},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, issues := Parse([]byte(tc.src), location.NewSourceID("demo.yammm"))
			for _, iss := range issues {
				if iss.Code() == diag.E_SYNTAX && iss.Span().Start.Byte == tc.wantByte {
					return
				}
			}
			t.Fatalf("no E_SYNTAX at byte %d in %v", tc.wantByte, issues)
		})
	}
}

// TestLex_SkipsOneLeadingByteOrderMark pins the token stream's side: the mark
// is no token, and the first token's offsets count the mark's bytes.
func TestLex_SkipsOneLeadingByteOrderMark(t *testing.T) {
	t.Parallel()
	got := Lex("\uFEFFa b")
	want := []Token{
		{Kind: "LC_WORD", Value: "a", Start: 3, End: 4},
		{Kind: "WS", Value: " ", Start: 4, End: 5},
		{Kind: "LC_WORD", Value: "b", Start: 5, End: 6},
	}
	if len(got) != len(want) {
		t.Fatalf("Lex = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("token %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	_, toks, _ := LexAndParse("\uFEFFa b", location.SourceID{})
	if len(toks) != len(want) || toks[0] != want[0] {
		t.Errorf("LexAndParse tokens = %+v, want %+v", toks, want)
	}
}
