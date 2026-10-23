package parse

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/location"
)

// The diagnostic each kind of refused text draws at its own bytes.
const (
	nulRefused     = `E_SYNTAX NUL character (U+0000) in the source; a string writes it as \0`
	markRefused    = "E_SYNTAX byte order mark (U+FEFF) past the start of the source"
	invalidRefused = "E_SYNTAX invalid UTF-8 encoding"
)

// regexSite is one of the two places a schema compiles a regex.
type regexSite struct {
	// source returns a schema holding the regex whose text is pattern.
	source func(pattern string) string
	// compileError is the diagnostic the site draws for a pattern regexp
	// refuses with err.
	compileError func(pattern string, err error) string
	// valued describes the value the tree holds for the regex, or is empty
	// when it holds none.
	valued func(f *File) string
}

var regexSites = map[string]regexSite{
	"regex literal": {
		source: func(p string) string { return checkSource(`! "m" id =~ /` + p + `/`) },
		compileError: func(_ string, err error) string {
			return fmt.Sprintf("E_INVALID_INVARIANT invalid regexp literal: %v", err)
		},
		// An invariant whose regex has no value names the nil that stands in
		// for it, so no later check judges it.
		valued: oneLiteralWithNoValue,
	},
	"pattern constraint": {
		source: func(p string) string {
			return checkSource(`p Pattern["` + strings.ReplaceAll(p, `\`, `\\`) + `"]`)
		},
		compileError: func(pattern string, err error) string {
			return fmt.Sprintf("E_INVALID_CONSTRAINT invalid regex pattern %q: %v", pattern, err)
		},
		valued: func(f *File) string {
			if lit := f.Types[0].Properties[1].Constraint.PatternLits[0]; lit.Kept || lit.Regex != nil || lit.Text != "" {
				return fmt.Sprintf("Kept %v, Regex %v, Text %q", lit.Kept, lit.Regex, lit.Text)
			}
			return ""
		},
	},
}

// issueLines returns each issue as its code and message, sorted.
func issueLines(issues []diag.Issue) []string {
	out := make([]string, 0, len(issues))
	for _, iss := range issues {
		out = append(out, iss.Code().String()+" "+iss.Message())
	}
	slices.Sort(out)
	return out
}

// TestParse_ARefusedRegexIsJudgedAsWrittenOrNotAtAll pins what a regex draws
// when it holds text the source rules refuse, at both sites that compile one. A
// NUL and a byte order mark are characters, so the regex is compiled with them
// as written: regexp's verdict on that text, and on no other, says whether a
// compile diagnostic stands beside the source rule's, and its error is the
// message. A byte that is no part of a character gives the regex no text to
// compile, so the byte's diagnostic stands alone whatever surrounds it. Most
// class rows write the refused text where it is one end of a range, the place
// a stand-in character would decide the verdict.
func TestParse_ARefusedRegexIsJudgedAsWrittenOrNotAtAll(t *testing.T) {
	t.Parallel()
	patterns := []string{
		`ab`, `a(b)`, `a[bc]`, `a\d+`, `a|b`,
		`a(`, `a)`, `a[b`, `a**`, `a\p{Nope}`, `a(?P<n`, `a\8`,
	}
	// Each shape writes the refused text where "@" stands.
	shapes := []string{"[@-a]", "[b-@]", "[^@-a]", "@+b", "[@]", "(@)", "a|@*", "@{2}", "@(", "(?P<n>@", `\Q@\E(`}
	faults := map[string]struct{ bytes, refused string }{
		"nul":             {"\x00", nulRefused},
		"byte order mark": {"\uFEFF", markRefused},
		"invalid byte":    {"\xff", invalidRefused},
		"truncated rune":  {"\xe2\x82", invalidRefused},
		"lone trail byte": {"\xa9", invalidRefused},
	}
	for siteName, site := range regexSites {
		for faultName, fault := range faults {
			var written []string
			for _, pattern := range patterns {
				written = append(written, pattern[:1]+fault.bytes+pattern[1:])
			}
			for _, shape := range shapes {
				written = append(written, strings.ReplaceAll(shape, "@", fault.bytes))
			}
			for _, pattern := range written {
				t.Run(siteName+"/"+faultName+"/"+strconv.Quote(pattern), func(t *testing.T) {
					t.Parallel()
					want := []string{fault.refused}
					if utf8.ValidString(pattern) {
						if _, err := regexp.Compile(pattern); err != nil {
							want = append(want, site.compileError(pattern, err))
						}
					}
					slices.Sort(want)
					f, issues := Parse([]byte(site.source(pattern)), location.NewSourceID("s.yammm"))
					if got := issueLines(issues); !slices.Equal(got, want) {
						t.Errorf("issues:\n  %s\nwant:\n  %s", strings.Join(got, "\n  "), strings.Join(want, "\n  "))
					}
					if valued := site.valued(f); valued != "" {
						t.Errorf("the refused regex has a value: %s", valued)
					}
				})
			}
		}
	}
}

// TestParse_ARefusedRegexTableHoldsBothVerdicts guards the table above: among
// the texts a NUL or a mark is written into, regexp refuses some and compiles
// others, and a refused character at one end of a class range decides it.
func TestParse_ARefusedRegexTableHoldsBothVerdicts(t *testing.T) {
	t.Parallel()
	for pattern, compiles := range map[string]bool{
		"a\x00b": true, "a\x00(": false,
		"[\x00-a]": true, "[b-\x00]": false,
		"[\uFEFF-a]": false, "[b-\uFEFF]": true,
		// U+FFFD is above "a" and "b", so a stand-in for a byte would refuse
		// the first range and compile the second.
		"[\uFFFD-a]": false, "[b-\uFFFD]": true,
	} {
		if _, err := regexp.Compile(pattern); (err == nil) != compiles {
			t.Errorf("regexp.Compile(%q): %v, want compiles %v", pattern, err, compiles)
		}
	}
}

// TestParse_APatternWithNoTextIsCountedButNotCompiled pins the pattern limit
// and the surrogate rule beside refused text. The limit counts every pattern
// the source wrote. A pattern holding a NUL is compiled as written, so it is
// held to the surrogate rule; one holding a byte that is no part of a
// character is not compiled, so beside the limit nothing but the byte is
// reported for it. The last rows write an escape and a raw byte that make one
// character between them: the source holds the raw byte, so the pattern is not
// compiled.
func TestParse_APatternWithNoTextIsCountedButNotCompiled(t *testing.T) {
	t.Parallel()
	const overLimit = "E_INVALID_CONSTRAINT pattern constraint exceeds maximum of 2 patterns (got 3)"
	const overLimitFour = "E_INVALID_CONSTRAINT pattern constraint exceeds maximum of 2 patterns (got 4)"
	rows := map[string]struct {
		patterns string
		want     []string
	}{
		"a third pattern holding a NUL counts": {
			"\"a\", \"b\", \"c\x00\"", []string{nulRefused, overLimit},
		},
		"a third pattern holding a mark counts": {
			"\"a\", \"b\", \"c\uFEFF\"", []string{markRefused, overLimit},
		},
		"a third pattern holding a NUL that does not compile counts": {
			"\"a\", \"b\", \"(\x00\"",
			[]string{nulRefused, overLimit, "E_INVALID_CONSTRAINT invalid regex pattern \"(\\x00\": error parsing regexp: missing closing ): `(\x00`"},
		},
		"a third pattern holding an invalid byte counts": {
			"\"a\", \"b\", \"c\xff\"", []string{invalidRefused, overLimit},
		},
		"a first pattern holding an invalid byte counts": {
			"\"c\xff\", \"a\", \"b\"", []string{invalidRefused, overLimit},
		},
		"three patterns beside one holding an invalid byte": {
			"\"c\xff\", \"a\", \"b\", \"d\"", []string{invalidRefused, overLimitFour},
		},
		"a surrogate beside a NUL": {
			"\"\\\\x{D800}\x00\"",
			[]string{nulRefused, "E_INVALID_CONSTRAINT " + SurrogateMessage("\\x{D800}\x00", 0xD800)},
		},
		"a surrogate beside an invalid byte": {
			"\"\\\\x{D800}\xff\"", []string{invalidRefused},
		},
		"an escape and a raw byte that make one character": {
			"\"\\xc3\xa9(\"", []string{invalidRefused},
		},
		"an escape and a raw byte that make one character, third": {
			"\"a\", \"b\", \"\\xc3\xa9\"", []string{invalidRefused, overLimit},
		},
	}
	for name, row := range rows {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			want := slices.Clone(row.want)
			slices.Sort(want)
			f, issues := Parse([]byte(checkSource("p Pattern["+row.patterns+"]")), location.NewSourceID("s.yammm"))
			if got := issueLines(issues); !slices.Equal(got, want) {
				t.Errorf("issues:\n  %s\nwant:\n  %s", strings.Join(got, "\n  "), strings.Join(want, "\n  "))
			}
			for _, lit := range f.Types[0].Properties[1].Constraint.PatternLits {
				if _, refused := nextTextFault(lit.Raw, 0); refused && (lit.Kept || lit.Regex != nil || lit.Text != "") {
					t.Errorf("the refused pattern %q has a value: Kept %v, Regex %v, Text %q", lit.Raw, lit.Kept, lit.Regex, lit.Text)
				}
			}
		})
	}
}
