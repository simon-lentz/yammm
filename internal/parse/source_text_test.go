package parse

import (
	"fmt"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/location"
)

// TestParse_SourceRulesHoldInEveryToken pins that a byte that is not valid
// UTF-8, a byte order mark past the start and a NUL are E_SYNTAX at the run
// inside a comment, a doc comment, a string or a regex literal. Each case
// marks the refused run with "@" and "#", which the test replaces with the
// run's bytes.
func TestParse_SourceRulesHoldInEveryToken(t *testing.T) {
	t.Parallel()
	runs := map[string]struct{ bytes, why string }{
		"invalid byte":           {"\xff", "invalid UTF-8 encoding"},
		"truncated rune":         {"\xe2\x82", "invalid UTF-8 encoding"},
		"byte order mark":        {"\uFEFF", "byte order mark (U+FEFF) past the start of the source"},
		"nul":                    {"\x00", `NUL character (U+0000) in the source; a string writes it as \0`},
		"adjacent invalid bytes": {"\xff\xfe", "invalid UTF-8 encoding"},
	}
	places := map[string]string{
		"line comment": "schema \"s\"\n// a @# b\ntype T {\n\tid String primary\n}\n",
		"doc comment":  "schema \"s\"\n/* a @# b */\ntype T {\n\tid String primary\n}\n",
		"type doc":     "schema \"s\"\ntype T {\n\t/* a @# b */\n\tid String primary\n}\n",
		"string":       "schema \"s\"\ntype T {\n\tid String primary\n\tk Enum[\"a@#b\", \"c\"]\n}\n",
		"regex":        "schema \"s\"\ntype T {\n\tid String primary\n\t! \"m\" id =~ /a@#b/\n}\n",
		"schema name":  "schema \"s@#\"\ntype T {\n\tid String primary\n}\n",
	}
	for runName, run := range runs {
		for placeName, place := range places {
			t.Run(runName+" in a "+placeName, func(t *testing.T) {
				t.Parallel()
				at := strings.Index(place, "@#")
				src := strings.Replace(place, "@#", run.bytes, 1)
				_, issues := Parse([]byte(src), location.NewSourceID("s.yammm"))
				var found []diag.Issue
				for _, iss := range issues {
					if iss.Code() == diag.E_SYNTAX && iss.Message() == run.why {
						found = append(found, iss)
					}
				}
				if len(found) != 1 {
					t.Fatalf("want one E_SYNTAX %q, got %d in %v", run.why, len(found), issues)
				}
				span := found[0].Span()
				if span.Start.Byte != at || span.End.Byte != at+len(run.bytes) {
					t.Errorf("span = [%d, %d), want [%d, %d)", span.Start.Byte, span.End.Byte, at, at+len(run.bytes))
				}
				if found[0].Severity() != diag.Error {
					t.Errorf("severity = %v, want Error", found[0].Severity())
				}
			})
		}
	}
}

// TestParse_SourceRulesReportEachRun pins that one token holding two refused
// runs reports each, and that a character only the escapes may write — U+0000
// and U+FEFF — is legal as an escape.
func TestParse_SourceRulesReportEachRun(t *testing.T) {
	t.Parallel()
	src := "schema \"s\"\n// a \xff b \x00 c \xfe d\ntype T {\n\tid String primary\n\tk Enum[\"\\0\", \"\\uFEFF\"]\n}\n"
	_, issues := Parse([]byte(src), location.NewSourceID("s.yammm"))
	var got []string
	for _, iss := range issues {
		got = append(got, iss.Code().String()+" "+iss.Message())
	}
	want := []string{
		"E_SYNTAX invalid UTF-8 encoding",
		`E_SYNTAX NUL character (U+0000) in the source; a string writes it as \0`,
		"E_SYNTAX invalid UTF-8 encoding",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("issues:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// TestParse_AStringValueIsValidUTF8 pins that the value a literal's escapes
// write must be valid UTF-8 at every site that reports an unquote failure: a
// \xHH escape writes one byte, so "\xc3\xa9" writes é and "\xff" writes none.
func TestParse_AStringValueIsValidUTF8(t *testing.T) {
	t.Parallel()
	const cause = "unquote string: the value its escapes write is not valid UTF-8"
	refused := []struct {
		name, src string
		code      diag.Code
		message   string
	}{
		{"schema name", "schema \"g\\xff\"\ntype T {\n\tid String primary\n}\n", diag.E_SYNTAX, "invalid schema name: " + cause},
		{"import path", "schema \"s\"\nimport \"a\\xff\"\ntype T {\n\tid String primary\n}\n", diag.E_SYNTAX, "invalid import path: " + cause},
		{"enum value", checkSource(`e Enum["a\xff", "b", "c"]`), diag.E_SYNTAX, "invalid enum value: " + cause},
		{"pattern", checkSource(`p Pattern["a\xff"]`), diag.E_SYNTAX, "invalid pattern: " + cause},
		{"timestamp format", checkSource(`t Timestamp["2006\xff"]`), diag.E_SYNTAX, "invalid timestamp format: " + cause},
		{"invariant message", "schema \"s\"\ntype T {\n\tid String primary\n\t! \"m\\xff\" id == \"a\"\n}\n", diag.E_SYNTAX, "invalid invariant message: " + cause},
		{"expression literal", "schema \"s\"\ntype T {\n\tid String primary\n\t! \"m\" id == \"a\\xe2\\x82\"\n}\n", diag.E_INVALID_INVARIANT, "invalid string literal: the value its escapes write is not valid UTF-8"},
	}
	for _, tc := range refused {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, issues := Parse([]byte(tc.src), location.NewSourceID("s.yammm"))
			for _, iss := range issues {
				if iss.Code() == tc.code && iss.Message() == tc.message {
					return
				}
			}
			t.Errorf("no %s %q in %v", tc.code, tc.message, issues)
		})
	}

	f, issues := Parse([]byte("schema \"caf\\xc3\\xa9\"\ntype T {\n\tid String primary\n}\n"), location.NewSourceID("s.yammm"))
	if len(issues) != 0 || f.Name != "café" {
		t.Errorf("escapes forming UTF-8: name %q, issues %v; want café and none", f.Name, issues)
	}
	if _, err := unquote(`"\xff"`); err == nil || err.Error() != cause {
		t.Errorf(`unquote("\xff") = %v, want %q`, err, cause)
	}
}

// TestParse_APatternNamingASurrogateIsRefused pins that a pattern writing a
// surrogate code point with a \x{…} escape — alone, folded, in a class, in an
// alternation or as a range's end — is E_INVALID_CONSTRAINT and not kept, while
// a class spanning the block, the surrogate category, an escaped backslash and
// quoted text write none.
func TestParse_APatternNamingASurrogateIsRefused(t *testing.T) {
	t.Parallel()
	for pattern, want := range map[string]rune{
		`a\\x{D800}b`:              0xD800,
		`(?i)\\x{DFFF}`:            0xDFFF,
		`[\\x{DBFF}]`:              0xDBFF,
		`(x|\\x{DC00})`:            0xDC00,
		`[\\x{D000}-\\x{D900}]`:    0xD900,
		`\\x{00D800}`:              0xD800,
		`\\\\\\x{DABC}`:            0xDABC,
		`\\Q\\x{D800}\\E\\x{DE00}`: 0xDE00,
	} {
		t.Run(pattern, func(t *testing.T) {
			t.Parallel()
			f, issues := Parse([]byte(checkSource(`p Pattern["`+pattern+`"]`)), location.NewSourceID("s.yammm"))
			text, err := unquote(`"` + pattern + `"`)
			if err != nil {
				t.Fatal(err)
			}
			message := fmt.Sprintf("regex pattern %q names the surrogate code point U+%04X, which no string holds", text, want)
			if !hasMessage(issues, message) {
				t.Fatalf("no %q in %s", message, messages(issues))
			}
			for _, iss := range issues {
				if iss.Message() == message && iss.Code() != diag.E_INVALID_CONSTRAINT {
					t.Errorf("code = %s, want E_INVALID_CONSTRAINT", iss.Code())
				}
			}
			if lit := f.Types[0].Properties[1].Constraint.PatternLits[0]; lit.Kept || lit.Regex != nil {
				t.Errorf("the refused pattern was kept: %+v", lit)
			}
		})
	}
	for _, pattern := range []string{
		`[\\x{D000}-\\x{E000}]`, `[^a]`, `\\x{D7FF}\\x{E000}`, `\\p{Cs}`,
		`\\\\x{D800}`, `\\Q\\x{D800}\\E`, `\\x41`,
	} {
		t.Run(pattern, func(t *testing.T) {
			t.Parallel()
			_, issues := Parse([]byte(checkSource(`p Pattern["`+pattern+`"]`)), location.NewSourceID("s.yammm"))
			if len(issues) != 0 {
				t.Errorf("issues: %s", messages(issues))
			}
		})
	}
}

// TestParse_SourceRulesAcceptTheReplacementCharacter pins that U+FFFD written
// as itself is legal text, and that a run of invalid bytes stops before one.
func TestParse_SourceRulesAcceptTheReplacementCharacter(t *testing.T) {
	t.Parallel()
	if _, issues := Parse([]byte("schema \"s\uFFFD\"\n// \uFFFD\ntype T {\n\tid String primary\n}\n"), location.NewSourceID("s.yammm")); len(issues) != 0 {
		t.Errorf("issues: %s", messages(issues))
	}
	_, issues := Parse([]byte("schema \"s\"\n// \xff\uFFFD\ntype T {\n\tid String primary\n}\n"), location.NewSourceID("s.yammm"))
	if len(issues) != 1 || issues[0].Span().End.Byte-issues[0].Span().Start.Byte != 1 {
		t.Errorf("want one fault one byte wide before U+FFFD, got %v", issues)
	}
}

// TestSourceTextFault_JudgesTheFirstByte pins that a refused character at the
// start of a string is found.
func TestSourceTextFault_JudgesTheFirstByte(t *testing.T) {
	t.Parallel()
	for _, s := range []string{"\xff", "\x00a", "\uFEFFa"} {
		if _, found := SourceTextFault(s); !found {
			t.Errorf("SourceTextFault(%q) found nothing", s)
		}
	}
}

// TestParse_ARegexLiteralHoldingARefusedByteDrawsOneDiagnostic pins that an
// invariant's regex literal holding a byte the source rules refuse is reported
// at the byte alone, as a string literal is.
func TestParse_ARegexLiteralHoldingARefusedByteDrawsOneDiagnostic(t *testing.T) {
	t.Parallel()
	_, issues := Parse([]byte("schema \"s\"\ntype T {\n\tid String primary\n\t! \"m\" id =~ /a\xffb/\n}\n"), location.NewSourceID("s.yammm"))
	if len(issues) != 1 || issues[0].Code() != diag.E_SYNTAX || issues[0].Message() != "invalid UTF-8 encoding" {
		t.Errorf("want one E_SYNTAX at the byte, got %s", messages(issues))
	}
}

// TestSurrogateEscape_ReadsThePatternAsRE2Does pins the scan's edges: a compile
// error is reported before a surrogate, \Q quotes to the pattern's end when no
// \E closes it, a value wider than sixteen bits is no surrogate, and a \xHH
// escape does not hide a later \x{…} one.
func TestSurrogateEscape_ReadsThePatternAsRE2Does(t *testing.T) {
	t.Parallel()
	issuesOf := func(pattern string) []diag.Issue {
		_, issues := Parse([]byte(checkSource(`p Pattern["`+pattern+`"]`)), location.NewSourceID("s.yammm"))
		return issues
	}
	if got := issuesOf(`\\x{D800}(`); len(got) != 1 || !strings.Contains(got[0].Message(), "invalid regex pattern") {
		t.Errorf("a pattern that does not compile: %s", messages(got))
	}
	for _, accepted := range []string{`\\Qa\\x{D800}`, `\\x{1D800}`} {
		if got := issuesOf(accepted); len(got) != 0 {
			t.Errorf("%s: %s", accepted, messages(got))
		}
	}
	if got := issuesOf(`\\x41\\x{D800}`); len(got) != 1 || !strings.Contains(got[0].Message(), "U+D800") {
		t.Errorf("a surrogate after a two-digit escape: %s", messages(got))
	}
}
