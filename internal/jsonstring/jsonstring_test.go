package jsonstring

import "testing"

func TestInvalidUTF8_ReturnsTheFirstByteOfNoSequence(t *testing.T) {
	t.Parallel()
	for body, want := range map[string]int{
		"":                        -1,
		"ascii":                   -1,
		"\xc3\xa9 and \U0001F600": -1,
		"\uFFFD":                  -1,
		"a\xffb":                  1,
		"\xc3\xa9\xfe":            2,
		"\xed\xa0\x80":            0, // a surrogate encoded as UTF-8
		"ab\xe2\x82":              2, // a sequence cut short
		"\xc0\xaf":                0, // an overlong encoding
		"ok\xf4\x90\x80\x80":      2, // past U+10FFFF
		"\x80":                    0, // a continuation byte alone
		"\uFFFD\xff":              3, // U+FFFD written as itself, then a bad byte
	} {
		if got := InvalidUTF8([]byte(body)); got != want {
			t.Errorf("InvalidUTF8(%q) = %d, want %d", body, got, want)
		}
	}
}

func TestLoneSurrogate_ReturnsTheEscapeThatIsHalfOfNoPair(t *testing.T) {
	t.Parallel()
	for body, want := range map[string]int{
		``:                   -1,
		`\n\t\"`:             -1,
		`\u0041`:             -1,
		`\ud83d\ude00`:       -1,
		`\uD83D\uDE00`:       -1,
		`a\ud800`:            1,
		`\udc00`:             0,
		`\ud800x\udc00`:      0,
		`\ud800\ud800\udc00`: 0,
		`\ud83d\ude00\udc00`: 12,
		`\\ud800`:            -1, // an escaped backslash, then text
		`\ud800\u0041`:       0,
		`\ud900`:             0,
		`\ud900\udc00`:       -1,
		`\uDFFF`:             0,
		`\uDBFF\uDFFF`:       -1,
		`\udbff\udfff`:       -1,
	} {
		if got := LoneSurrogate([]byte(body)); got != want {
			t.Errorf("LoneSurrogate(%q) = %d, want %d", body, got, want)
		}
	}
}
