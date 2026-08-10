package parse

import (
	"testing"
)

// TestLex_FirstMatchOrdering states the outcome of every overlapping token
// shape the rule order exists to resolve. A reordering that looks harmless
// fails here.
func TestLex_FirstMatchOrdering(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []Token
	}{
		{
			name: "a valid float wins over trailing-junk reinterpretation",
			src:  "2.5e10",
			want: []Token{{Kind: "FLOAT", Value: "2.5e10", Start: 0, End: 6}},
		},
		{
			name: "a float with trailing junk falls through to the malformed rule",
			src:  "1.5e5x",
			want: []Token{{Kind: "INVALID_NUMBER", Value: "1.5e5x", Start: 0, End: 6}},
		},
		{
			name: "an integer with trailing junk is malformed, not integer plus word",
			src:  "0x10",
			want: []Token{{Kind: "INVALID_NUMBER", Value: "0x10", Start: 0, End: 4}},
		},
		{
			name: "a bare integer needs no boundary",
			src:  "42",
			want: []Token{{Kind: "INTEGER", Value: "42", Start: 0, End: 2}},
		},
		{
			name: "a slash pair on one line is a regex",
			src:  "/ab/",
			want: []Token{{Kind: "REGEXP", Value: "/ab/", Start: 0, End: 4}},
		},
		{
			name: "a lone slash is division",
			src:  "a / b",
			want: []Token{
				{Kind: "LC_WORD", Value: "a", Start: 0, End: 1},
				{Kind: "WS", Value: " ", Start: 1, End: 2},
				{Kind: "SLASH", Value: "/", Start: 2, End: 3},
				{Kind: "WS", Value: " ", Start: 3, End: 4},
				{Kind: "LC_WORD", Value: "b", Start: 4, End: 5},
			},
		},
		{
			name: "a line comment beats a regex",
			src:  "//x/",
			want: []Token{{Kind: "SL_COMMENT", Value: "//x/", Start: 0, End: 4}},
		},
		{
			name: "a block comment beats a regex",
			src:  "/*x*/",
			want: []Token{{Kind: "DOC_COMMENT", Value: "/*x*/", Start: 0, End: 5}},
		},
		{
			name: "multi-character operators beat their prefixes",
			src:  "--> *-> -> >= <= == =~ != !~ && || @@",
			want: opTokens(),
		},
		{
			name: "a variable beats a bare dollar",
			src:  "$a $",
			want: []Token{
				{Kind: "VARIABLE", Value: "$a", Start: 0, End: 2},
				{Kind: "WS", Value: " ", Start: 2, End: 3},
				{Kind: "DOLLAR", Value: "$", Start: 3, End: 4},
			},
		},
		{
			name: "keywords are ordinary words",
			src:  "schema String",
			want: []Token{
				{Kind: "LC_WORD", Value: "schema", Start: 0, End: 6},
				{Kind: "WS", Value: " ", Start: 6, End: 7},
				{Kind: "UC_WORD", Value: "String", Start: 7, End: 13},
			},
		},
		{
			name: "a keyword-shaped prefix stays one word",
			src:  "trueish",
			want: []Token{{Kind: "LC_WORD", Value: "trueish", Start: 0, End: 7}},
		},
		{
			name: "an invalid escape breaks the literal at its opening quote",
			src:  `"\q"`,
			want: []Token{
				{Kind: "ANY_OTHER", Value: `"`, Start: 0, End: 1},
				{Kind: "ANY_OTHER", Value: `\`, Start: 1, End: 2},
				{Kind: "LC_WORD", Value: "q", Start: 2, End: 3},
				{Kind: "ANY_OTHER", Value: `"`, Start: 3, End: 4},
			},
		},
		{
			name: "an escape the rule admits keeps the literal whole",
			src:  `"\x"`,
			want: []Token{{Kind: "STRING", Value: `"\x"`, Start: 0, End: 4}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assertTokens(t, Lex(tc.src), tc.want)
		})
	}
}

func opTokens() []Token {
	kinds := []string{"ASSOC", "COMP", "ARROW", "GTE", "LTE", "EQUAL", "MATCH", "NOTEQUAL", "NOTMATCH", "AND", "OR", "ATAT"}
	values := []string{"-->", "*->", "->", ">=", "<=", "==", "=~", "!=", "!~", "&&", "||", "@@"}
	var out []Token
	at := 0
	for i, v := range values {
		if i > 0 {
			out = append(out, Token{Kind: "WS", Value: " ", Start: at, End: at + 1})
			at++
		}
		out = append(out, Token{Kind: kinds[i], Value: v, Start: at, End: at + len(v)})
		at += len(v)
	}
	return out
}

// TestLex_IsTotal pins the property every rejection path depends on: no input
// fails to lex, so a diagnostic can always name a position.
func TestLex_IsTotal(t *testing.T) {
	for _, src := range []string{"¤", "\x00", "€uro", "\\", "\"unterminated", "🙂"} {
		toks := Lex(src)
		if len(toks) == 0 {
			t.Errorf("Lex(%q) produced no tokens", src)
			continue
		}
		if got := toks[len(toks)-1].End; got != len(src) {
			t.Errorf("Lex(%q) covered %d of %d bytes", src, got, len(src))
		}
	}
}

// TestLex_MultibyteOffsetsAreBytes pins that offsets count bytes, which is
// what every span downstream is measured in.
func TestLex_MultibyteOffsetsAreBytes(t *testing.T) {
	src := `"héllo" x`
	toks := Lex(src)
	if len(toks) != 3 {
		t.Fatalf("got %d tokens, want 3: %+v", len(toks), toks)
	}
	if toks[0].Kind != "STRING" || toks[0].End != 8 {
		t.Errorf("string token = %+v, want STRING ending at byte 8", toks[0])
	}
	if toks[2].Start != 9 {
		t.Errorf("trailing word starts at %d, want 9", toks[2].Start)
	}
}

// TestLex_KeepsWhitespaceAndComments pins the un-elided stream a formatter
// reads.
func TestLex_KeepsWhitespaceAndComments(t *testing.T) {
	toks := Lex("a // c\nb")
	kinds := make([]string, len(toks))
	for i, tok := range toks {
		kinds[i] = tok.Kind
	}
	want := []string{"LC_WORD", "WS", "SL_COMMENT", "WS", "LC_WORD"}
	if len(kinds) != len(want) {
		t.Fatalf("kinds = %v, want %v", kinds, want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Errorf("kind %d = %s, want %s", i, kinds[i], want[i])
		}
	}
}

func assertTokens(t *testing.T, got, want []Token) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d tokens, want %d\n got: %+v\nwant: %+v", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("token %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}
