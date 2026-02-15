package path

import (
	"testing"
)

// FuzzParse verifies that Parse does not panic on arbitrary input.
// The parser accepts user-provided path strings and must handle
// malformed, malicious, or random input gracefully by returning
// an error rather than panicking.
func FuzzParse(f *testing.F) {
	// Seed corpus with valid path patterns
	f.Add("$")
	f.Add("$.foo")
	f.Add("$.foo.bar")
	f.Add("$[0]")
	f.Add("$[42]")
	f.Add("$.items[0]")
	f.Add("$.items[0].name")
	f.Add(`$["complex key"]`)
	f.Add(`$["with spaces"]`)
	f.Add(`$["say \"hello\""]`)
	f.Add(`$["a.b.c"]`)

	// PK-based indices
	f.Add("$[id=123]")
	f.Add(`$[name="Alice"]`)
	f.Add(`$[a="x",b=1]`)
	f.Add("$[flag=true]")
	f.Add("$[enabled=false]")
	f.Add("$[rate=3.14]")
	f.Add("$[val=-42]")
	f.Add("$[big=1e10]")

	// Edge cases
	f.Add("")
	f.Add("$.")
	f.Add("$[")
	f.Add("$[]")
	f.Add("$[]]")
	f.Add("$[[")
	f.Add(`$["`)
	f.Add(`$["\"]`)
	f.Add("$[-1]")
	f.Add("$[999999999999999999999]")
	f.Add("$\\")
	f.Add("$\x00")
	f.Add("$\xff")

	// Unicode
	f.Add("$.日本語")
	f.Add(`$["emoji🎉"]`)
	f.Add("$.αβγ")

	f.Fuzz(func(t *testing.T, input string) {
		// Parse should never panic on any input
		_, _ = Parse(input)
	})
}

// FuzzExtractSegments verifies that extractSegments does not panic.
func FuzzExtractSegments(f *testing.F) {
	f.Add("$")
	f.Add("$.foo")
	f.Add("$[0]")
	f.Add("$[id=123]")
	f.Add("")

	f.Fuzz(func(t *testing.T, input string) {
		_, _ = extractSegments(input)
	})
}

// FuzzParseQuotedString verifies quoted string parsing robustness.
func FuzzParseQuotedString(f *testing.F) {
	f.Add(`"hello"`)
	f.Add(`"with \"escape\""`)
	f.Add(`"\n\r\t"`)
	f.Add(`"\u0041"`)
	f.Add(`"`)
	f.Add(`"unterminated`)
	f.Add(`"\`)
	f.Add(`"\x"`)
	f.Add(`"\u"`)
	f.Add(`"\u00"`)
	f.Add(`"\uZZZZ"`)
	f.Add(`""`)

	f.Fuzz(func(t *testing.T, input string) {
		_, _, _ = parseQuotedString(input, 0)
	})
}

// FuzzParsePKFields verifies PK field parsing robustness.
func FuzzParsePKFields(f *testing.F) {
	f.Add("id=123]")
	f.Add(`name="Alice"]`)
	f.Add("a=1,b=2]")
	f.Add("flag=true]")
	f.Add("")
	f.Add("=]")
	f.Add("x=]")
	f.Add("x=1")

	f.Fuzz(func(t *testing.T, input string) {
		_, _, _ = parsePKFields(input, 0)
	})
}
