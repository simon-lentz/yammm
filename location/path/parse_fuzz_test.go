package path

import (
	"math"
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
		b, err := Parse(input)
		if err != nil {
			return
		}
		// A path Parse accepts writes a form Parse reads back to itself.
		s := b.String()
		again, err := Parse(s)
		if err != nil {
			t.Fatalf("Parse(%q) succeeded, but its String %q does not parse: %v", input, s, err)
		}
		if again.String() != s {
			t.Fatalf("Parse(%q).String() = %q, which reparses as %q", input, s, again.String())
		}
	})
}

// FuzzBuilderRoundTrip holds every Builder built from these inputs to the
// round trip Parse(b.String()).String() == b.String(), for each value kind a
// PK field can hold that the grammar spells.
func FuzzBuilderRoundTrip(f *testing.F) {
	f.Add("name", uint16(0), "id", "Alice", int64(42), uint64(7), 1.5, true)
	f.Add("with \"quotes\" and \\", uint16(3), "_k", "\x01\xff", int64(-1), uint64(math.MaxUint64), math.Copysign(0, -1), false)
	f.Add("\xffkey", uint16(65535), "a1", "\u2028", int64(math.MinInt64), uint64(0), 1e300, true)

	f.Fuzz(func(t *testing.T, key string, idx uint16, name, sv string, iv int64, uv uint64, fv float64, bv bool) {
		if !isIdentifierSafe(name) || math.IsNaN(fv) || math.IsInf(fv, 0) {
			t.Skip("the grammar spells no such PK name or value")
		}
		b := Root().Key(key).Index(int(idx)).PK(
			PKField{Name: name, Value: sv},
			PKField{Name: name, Value: iv},
			PKField{Name: name, Value: uv},
			PKField{Name: name, Value: fv},
			PKField{Name: name, Value: bv},
		)
		s := b.String()
		parsed, err := Parse(s)
		if err != nil {
			t.Fatalf("Parse(%q): %v", s, err)
		}
		if got := parsed.String(); got != s {
			t.Fatalf("round trip changed %q to %q", s, got)
		}
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
