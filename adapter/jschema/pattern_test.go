package jschema

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"
)

// patternSamples are the strings every rewritten pattern is matched against,
// chosen to separate the constructs whose meaning differs between RE2 and
// ECMA-262: line terminators other than "\n", Unicode spaces, letters outside
// ASCII, case variants, and the Kelvin sign that folds to "k".
var patternSamples = []string{
	"", "a", "A", "ab", "AB", "aB", "abc", "abz", "ab]", "a]", ":", "-", "_", "1", "12", "2024",
	" ", "\t", "\n", "\r", "\v", "\f", "\u00a0", "\u2003", "\u2028", "\u2029",
	"a\nb", "a\rb", "a\u2028b", "a.b", "axb", "a{,2}", "aa", "aaa",
	"é", "É", "αβ", "中", "k", "K", "\u212a", "s", "\u017f",
	"a@b.c", "x@y", "http://x", "\x00", "\x7f", "a\\b", "a/b", "$", "^", "[", "{", "\ufffd",
	"xay", "xbcy", "xbcy\n", "xa", "bcy",
}

// The rewrite must mean what the source means: for every sample, the source
// compiled by Go's regexp and the rewrite compiled the same way agree.
func TestSharedPattern_MatchesAsTheSourceDoes(t *testing.T) {
	sources := []string{
		`^[a-z]+$`,
		`(?i)^ab$`,
		`^(?i:ab)c$`,
		`(?i)k`,
		`(?i)s`,
		`(?s)^a.b$`,
		`^a.b$`,
		`^.$`,
		`(?U)^a+`,
		`^[[:alpha:]]+$`,
		`^[[:^digit:]]$`,
		`^\pL+$`,
		`^\p{L}+$`,
		`^\p{Greek}+$`,
		`^\PL$`,
		`\Aab\z`,
		`^\Qa.b\E$`,
		`^\x{41}$`,
		`^\101$`,
		`^(?P<n>a)$`,
		`^\s$`,
		`^\S$`,
		`^a{,2}$`,
		`^a{$`,
		`^[]a]$`,
		`^\-$`,
		`^\_$`,
		`^\@$`,
		`^\a$`,
		`^[\d-z]$`,
		`^\d+$`,
		`^\w+$`,
		`^\W$`,
		`\bab\b`,
		`a\B`,
		`^(a|b)*?$`,
		`^(ab|cd)+$`,
		`x(a|b)y`,
		`x(?:a|bc)y`,
		`x(a|bc)y`,
		`\p{Cs}`,
		`^\P{Cs}$`,
		`^a{2,3}$`,
		`^a{2}$`,
		`^a{2,}$`,
		`^(?:)$`,
		`^[^\x00-\x{10FFFF}]$`,
		`[\x00-\x{10FFFF}]`,
		`^[^a]$`,
		`^[\x00-\x7f]+$`,
		`^\$\^\[\{/\\$`,
		`^.+@.+\..+$`,
		`^https?://.+$`,
		`(^a|b$)`,
		`^(a*)*$`,
	}
	for _, src := range sources {
		t.Run(src, func(t *testing.T) {
			shared, ok := sharedPattern(src)
			if !ok {
				t.Fatalf("sharedPattern(%q) reported no shared form", src)
			}
			want := regexp.MustCompile(src)
			got, err := regexp.Compile(shared)
			if err != nil {
				t.Fatalf("rewrite %q of %q does not compile under RE2: %v", shared, src, err)
			}
			for _, s := range patternSamples {
				if w, g := want.MatchString(s), got.MatchString(s); w != g {
					t.Errorf("%q: source %q matches %v, rewrite %q matches %v", s, src, w, shared, g)
				}
			}
		})
	}
}

// Each construct outside the syntax the two dialects share is rewritten, and
// the result uses only shared syntax.
func TestSharedPattern_WritesOnlySharedSyntax(t *testing.T) {
	cases := []struct{ src, want string }{
		{`^[a-z]+$`, `^[a-z]+$`},
		{`^\d{3}-\D$`, `^[0-9]{3}-[^0-9]$`},
		{`^\w+\W$`, `^[0-9A-Z_a-z]+[^0-9A-Z_a-z]$`},
		{`^[\^_]$`, `^[\^_]$`},
		{`^[]]$`, `^\]$`},
		{`^[\-\\\]\[]$`, `^[\-\[-\]]$`},
		{`^\^\$\.\|\?\*\+\(\)\[\]\{\}\/\\$`, `^\^\$\.\|\?\*\+\(\)\[\]\{\}\/\\$`},
		{`^[^\x{10FFFF}]$`, "^[^\U0010FFFF]$"},
		{`^[\x{FFFF}-\x{10000}]$`, "^[\uFFFF\U00010000]$"},
		{`(?i)^ab$`, `^[Aa][Bb]$`},
		{`(?i)k`, "[Kk\u212a]"},
		{`^a.b$`, `^a[^\n]b$`},
		{`(?s)a.b`, `a[\s\S]b`},
		{`^\s$`, `^[\x09\x0A\x0C\x0D ]$`},
		{`^[[:alpha:]]$`, `^[A-Za-z]$`},
		{`^\PL$`, ``},
		{`\Aab\z`, `^ab$`},
		{`^\Qa.b\E$`, `^a\.b$`},
		{`^\x{41}\101$`, `^AA$`},
		{`^(?P<n>a|bc)$`, `^(?:a|bc)$`},
		{`^(?P<n>a|b)$`, `^[ab]$`},
		{`^a{,2}$`, `^a\{,2\}$`},
		{`^[]a]$`, `^[\]a]$`},
		{`^\-\_\@$`, `^-_@$`},
		{`^\a\x00$`, `^\x07\x00$`},
		{`^[^a]$`, `^[^a]$`},
		{`^(ab)+$`, `^(?:ab)+$`},
		{`^(a)*?$`, `^a*?$`},
		{`^a{2,3}b{2}c{2,}$`, `^a{2,3}b{2}c{2,}$`},
		{`x(a|bc)y`, `x(?:a|bc)y`},
		{`x(ab)y`, `xaby`},
		{`^/$`, `^\/$`},
	}
	for _, tc := range cases {
		got, ok := sharedPattern(tc.src)
		if !ok {
			t.Errorf("sharedPattern(%q) reported no shared form", tc.src)
			continue
		}
		if tc.want != "" && got != tc.want {
			t.Errorf("sharedPattern(%q) = %q, want %q", tc.src, got, tc.want)
		}
		for _, banned := range []string{"(?i", "(?s", "(?m", "(?U", "(?P", "[:", `\p`, `\P`, `\A`, `\z`, `\Q`, `\x{`, `\s`, `\S`} {
			if strings.Contains(got, banned) && !strings.Contains(got, `[\s\S]`) && !strings.Contains(got, `[^\s\S]`) {
				t.Errorf("sharedPattern(%q) = %q holds %q, which ECMA-262 does not read as RE2 does", tc.src, got, banned)
			}
		}
	}
}

// A line anchor under the "m" flag has no form both dialects read, so the
// pattern is carried as description and not asserted.
func TestSharedPattern_LineAnchorsHaveNoSharedForm(t *testing.T) {
	for _, src := range []string{`(?m)^b$`, `(?m)^a`, `(?m)a$`, `(?m:^)x`} {
		if got, ok := sharedPattern(src); ok {
			t.Errorf("sharedPattern(%q) = %q, want no shared form", src, got)
		}
	}
	if got, ok := sharedPattern(`(?m)ab`); !ok || got != `ab` {
		t.Errorf("sharedPattern(`(?m)ab`) = %q, %v; the flag without an anchor has a shared form", got, ok)
	}
}

func TestPatternSchema(t *testing.T) {
	cases := []struct {
		patterns []string
		want     string
	}{
		{[]string{`(?i)^ab$`}, `{"type": "string", "pattern": "^[Aa][Bb]$"}`},
		{[]string{`^a`, `(?i)b$`}, `{"type": "string", "allOf": [{"pattern": "^a"}, {"pattern": "[Bb]$"}]}`},
		{[]string{`(?m)^a$`}, `{"type": "string", "description": "Pattern[\"(?m)^a$\"]"}`},
		{[]string{`^a`, `(?m)^b$`}, `{"type": "string", "pattern": "^a", "description": "Pattern[\"(?m)^b$\"]"}`},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprint(tc.patterns), func(t *testing.T) {
			if g, w := normalize(t, patternSchema(tc.patterns)), normalizeWant(t, tc.want); g != w {
				t.Errorf("got  %s\nwant %s", g, w)
			}
		})
	}
}

// A pattern outside the shared syntax reaches the document rewritten, so the
// emitted schema and yammm agree on every value.
func TestContractAlignment_PatternDialects(t *testing.T) {
	src := `schema "p"

type Doc {
	id String primary
	code Pattern["(?i)^ab[[:alpha:]]+$"]
	line Pattern["^a.b$"]
	blank Pattern["^\\s$"]
	greek Pattern["^\\p{Greek}+$"]
}
`
	s := loadFixture(t, src, "test://pattern_dialects.yammm")
	compiled := compileEmitted(t, s)
	values := map[string][]string{
		"code":  {"abc", "ABc", "abZ", "ab]", "ab", "xab"},
		"line":  {"axb", "a\nb", "a\rb", "a b"},
		"blank": {" ", "\t", "\v", " ", " "},
		"greek": {"αβ", "ab", "Ω"},
	}
	for prop, vs := range values {
		for _, v := range vs {
			data, err := json.Marshal(map[string][]map[string]string{"Doc": {{"id": "d", prop: v}}})
			if err != nil {
				t.Fatal(err)
			}
			yammmOK := len(yammmErrors(t, s, data)) == 0
			editorOK := validateEmitted(t, compiled, data) == nil
			if yammmOK != editorOK {
				t.Errorf("%s = %q: yammm accepts %v, the emitted schema accepts %v", prop, v, yammmOK, editorOK)
			}
		}
	}
}

// ECMA-262 under the "u" flag refuses "(?i)" and "[[:alpha:]]", and reads "."
// and "\s" otherwise than RE2, so none reaches the document as written.
func TestMarshal_EmitsPatternsInSharedSyntax(t *testing.T) {
	src := `schema "p"

type Doc {
	id String primary
	code Pattern["(?i)^ab[[:alpha:]]$"]
	line Pattern["^a.b$"]
	blank Pattern["^\\s$", "(?m)^x$"]
}
`
	out, err := Marshal(loadFixture(t, src, "test://pattern_syntax.yammm"))
	if err != nil {
		t.Fatal(err)
	}
	doc := decodeDoc(t, out)
	for prop, want := range map[string]string{
		"code":  "{\"pattern\":\"^[Aa][Bb][A-Za-z\u017f\u212a]$\",\"type\":\"string\"}",
		"line":  `{"pattern":"^a[^\\n]b$","type":"string"}`,
		"blank": `{"description":"Pattern[\"(?m)^x$\"]","pattern":"^[\\x09\\x0A\\x0C\\x0D ]$","type":"string"}`,
	} {
		if got := defFragment(t, doc, "$defs", "Doc", "properties", prop); got != want {
			t.Errorf("%s = %s\nwant %s", prop, got, want)
		}
	}
}
