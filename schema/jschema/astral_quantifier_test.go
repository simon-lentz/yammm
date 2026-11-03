package jschema

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"
)

// repeatsHalfACharacter reports whether an ECMA-262 engine without the "u"
// flag, which reads pattern as UTF-16 code units, reads a character above
// U+FFFF as less than a whole: inside a class, where it is two members, or
// bare before a quantifier, which then repeats its second unit alone.
func repeatsHalfACharacter(pattern string) bool {
	inClass, escaped := false, false
	prev := rune(-1)
	for _, r := range pattern {
		switch {
		case escaped:
			escaped = false
		case r == '\\':
			escaped = true
		case inClass:
			if r > 0xFFFF {
				return true
			}
			inClass = r != ']'
		case r == '[':
			inClass = true
		case r == '*' || r == '+' || r == '?' || r == '{':
			if prev > 0xFFFF {
				return true
			}
		}
		prev = r
	}
	return false
}

// TestMarshal_RepeatsAWholeCharacterAboveTheBMP pins, in the emitted
// document, that a character above U+FFFF before a quantifier is grouped, so
// an engine without the "u" flag repeats both of its code units, and that the
// pattern still matches as the source does.
func TestMarshal_RepeatsAWholeCharacterAboveTheBMP(t *testing.T) {
	t.Parallel()

	// The class of the last source holds the surrogate block, which the
	// rewrite drops, so it renders as one character.
	sources := []string{`^𝄞+$`, `^a𝄞*$`, `^[𝄞]?$`, `^(?i:𝄞){2}$`, `^𝄞{1,3}?$`, `^(𝄞)+$`, `^\x{10000}+$`, `^[\p{Cs}\x{1D11E}]+$`}
	samples := []string{"", "𝄞", "𝄞𝄞", "𝄞𝄞𝄞", "𝄞𝄞𝄞𝄞", "a", "a𝄞", "\U0001D11F", "\xed\xb4\x9e", "\U00010000", "\U00010000\U00010000"}
	for _, src := range sources {
		t.Run(src, func(t *testing.T) {
			t.Parallel()
			doc, err := Marshal(loadFixture(t, "schema \"p\"\n\ntype T {\n\tid String primary\n\tv Pattern[\""+strings.ReplaceAll(src, `\`, `\\`)+"\"]\n}\n", "p.yammm"))
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			var parsed struct {
				Defs map[string]struct {
					Properties map[string]struct {
						Pattern string `json:"pattern"`
					} `json:"properties"`
				} `json:"$defs"`
			}
			if err := json.Unmarshal(doc, &parsed); err != nil {
				t.Fatal(err)
			}
			got := parsed.Defs["T"].Properties["v"].Pattern
			if got == "" || !utf8.ValidString(got) {
				t.Fatalf("emitted pattern %q", got)
			}
			if repeatsHalfACharacter(got) {
				t.Errorf("emitted pattern %q repeats half of a character above U+FFFF without the u flag", got)
			}
			want, rewrite := regexp.MustCompile(src), regexp.MustCompile(got)
			for _, s := range samples {
				if w, g := want.MatchString(s), rewrite.MatchString(s); w != g {
					t.Errorf("%q: source %q matches %v, emitted %q matches %v", s, src, w, got, g)
				}
			}
		})
	}
}
