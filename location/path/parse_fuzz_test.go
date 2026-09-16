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

// FuzzBuilderRoundTrip builds a key, an index and a PK with a field of every
// kind formatPKValue writes: each integer width, float32, float64, a string, a
// bool and a value of another type. A Builder the grammar spells must satisfy
// Parse(b.String()).String() == b.String(). For a negative index, a PK name
// that is not an identifier or a NaN or infinite float, the Builder must panic
// with that refusal.
func FuzzBuilderRoundTrip(f *testing.F) {
	f.Add("name", 0, "id", "Alice",
		int8(1), int16(2), int32(3), int64(42), uint8(4), uint16(5), uint32(6), uint64(7),
		float32(0.1), 1.5, true)
	f.Add("with \"quotes\" and \\", 3, "_k", "\x01\xff",
		int8(math.MinInt8), int16(math.MinInt16), int32(math.MinInt32), int64(-1),
		uint8(math.MaxUint8), uint16(math.MaxUint16), uint32(math.MaxUint32), uint64(math.MaxUint64),
		float32(math.Copysign(0, -1)), math.Copysign(0, -1), false)
	f.Add("\xffkey", math.MaxInt32, "a1", "\u2028",
		int8(math.MaxInt8), int16(math.MaxInt16), int32(math.MaxInt32), int64(math.MinInt64),
		uint8(0), uint16(0), uint32(0), uint64(0),
		float32(math.MaxFloat32), 1e300, true)
	f.Add("k", -1, "id", "v", int8(1), int16(1), int32(1), int64(1), uint8(1), uint16(1), uint32(1), uint64(1), float32(1), 1.0, true)
	f.Add("k", 0, "my field", "v", int8(1), int16(1), int32(1), int64(1), uint8(1), uint16(1), uint32(1), uint64(1), float32(1), 1.0, true)
	f.Add("k", 0, "id", "v", int8(1), int16(1), int32(1), int64(1), uint8(1), uint16(1), uint32(1), uint64(1), float32(1), math.NaN(), true)
	f.Add("k", 0, "id", "v", int8(1), int16(1), int32(1), int64(1), uint8(1), uint16(1), uint32(1), uint64(1), float32(math.Inf(-1)), 1.0, true)

	f.Fuzz(func(t *testing.T, key string, idx int, name, sv string,
		i8 int8, i16 int16, i32 int32, iv int64, u8 uint8, u16 uint16, u32 uint32, uv uint64,
		f32 float32, fv float64, bv bool,
	) {
		build := func() Builder {
			return Root().Key(key).Index(idx).PK(
				PKField{Name: name, Value: sv},
				PKField{Name: name, Value: int(iv)},
				PKField{Name: name, Value: i8},
				PKField{Name: name, Value: i16},
				PKField{Name: name, Value: i32},
				PKField{Name: name, Value: iv},
				PKField{Name: name, Value: uint(uv)},
				PKField{Name: name, Value: u8},
				PKField{Name: name, Value: u16},
				PKField{Name: name, Value: u32},
				PKField{Name: name, Value: uv},
				PKField{Name: name, Value: f32},
				PKField{Name: name, Value: fv},
				PKField{Name: name, Value: bv},
				PKField{Name: name, Value: []string{sv}},
			)
		}

		// The switch follows build's call order: Index panics before PK, and PK
		// checks the first field's name before any float field's value.
		refusal := ""
		switch {
		case idx < 0:
			refusal = refusesNegativeIndex
		case !isIdentifierSafe(name):
			refusal = refusesPKName
		case math.IsNaN(float64(f32)) || math.IsInf(float64(f32), 0) || math.IsNaN(fv) || math.IsInf(fv, 0):
			refusal = refusesPKFloat
		}
		if refusal != "" {
			wantPanic(t, refusal, func() { _ = build() })
			return
		}

		b := build()
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
