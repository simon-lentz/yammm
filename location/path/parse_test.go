package path

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParse_ValidPaths(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "root only",
			input:    "$",
			expected: "$",
		},
		// Numeric PK values canonicalize on output: scientific notation and
		// trailing-zero forms parse, then render in the canonical decimal form.
		{
			name:     "scientific notation canonicalizes",
			input:    "$[value=1.5e10]",
			expected: "$[value=15000000000.0]",
		},
		{
			name:     "negative scientific canonicalizes",
			input:    "$[value=-2.5e-5]",
			expected: "$[value=-0.000025]",
		},
		{
			name:     "very small float",
			input:    "$[value=0.0000001]",
			expected: "$[value=0.0000001]",
		},
		{
			name:     "zero integer pk value",
			input:    "$[count=0]",
			expected: "$[count=0]",
		},
		{
			name:     "single key",
			input:    "$.name",
			expected: "$.name",
		},
		{
			name:     "nested keys",
			input:    "$.a.b.c",
			expected: "$.a.b.c",
		},
		{
			name:     "array index",
			input:    "$[0]",
			expected: "$[0]",
		},
		{
			name:     "key then index",
			input:    "$.items[0]",
			expected: "$.items[0]",
		},
		{
			name:     "index then key",
			input:    "$[0].name",
			expected: "$[0].name",
		},
		{
			name:     "bracket notation key",
			input:    `$["with spaces"]`,
			expected: `$["with spaces"]`,
		},
		{
			name:     "bracket notation key with dot",
			input:    `$["a.b.c"]`,
			expected: `$["a.b.c"]`,
		},
		{
			name:     "escaped quote in key",
			input:    `$["say \"hello\""]`,
			expected: `$["say \"hello\""]`,
		},
		{
			name:     "escaped backslash in key",
			input:    `$["path\\to\\file"]`,
			expected: `$["path\\to\\file"]`,
		},
		{
			name:     "newline escape",
			input:    `$["line1\nline2"]`,
			expected: `$["line1\nline2"]`,
		},
		{
			name:     "tab escape",
			input:    `$["col1\tcol2"]`,
			expected: `$["col1\tcol2"]`,
		},
		{
			name:     "carriage return escape",
			input:    `$["a\rb"]`,
			expected: `$["a\rb"]`,
		},
		{
			name:     "backspace escape",
			input:    `$["a\bb"]`,
			expected: `$["a\bb"]`,
		},
		{
			name:     "form feed escape",
			input:    `$["a\fb"]`,
			expected: `$["a\fb"]`,
		},
		{
			name:     "unicode escape",
			input:    `$["\u0041"]`,
			expected: `$.A`, // \u0041 = 'A', which is identifier-safe
		},
		// Typed PK values round-trip through their formatted form — the type
		// distinction is observable only in the rendered text (quoted string
		// vs bare int vs decimal float vs bool keyword).
		{
			name:     "PK integer",
			input:    "$[id=42]",
			expected: "$[id=42]",
		},
		{
			name:     "PK string",
			input:    `$[name="Alice"]`,
			expected: `$[name="Alice"]`,
		},
		{
			name:     "PK boolean true",
			input:    "$[active=true]",
			expected: "$[active=true]",
		},
		{
			name:     "PK boolean false",
			input:    "$[active=false]",
			expected: "$[active=false]",
		},
		{
			name:     "composite PK",
			input:    `$[region="us",studentId=12345]`,
			expected: `$[region="us",studentId=12345]`,
		},
		{
			name:     "PK with float",
			input:    "$[score=3.14]",
			expected: "$[score=3.14]",
		},
		{
			name:     "PK with negative integer",
			input:    "$[offset=-10]",
			expected: "$[offset=-10]",
		},
		{
			name:     "PK with negative float",
			input:    "$[delta=-0.5]",
			expected: "$[delta=-0.5]",
		},
		{
			name:     "complex path with PK",
			input:    `$.Company[id="acme"].employees[0].name`,
			expected: `$.Company[id="acme"].employees[0].name`,
		},
		{
			name:     "underscore key",
			input:    "$._private",
			expected: "$._private",
		},
		{
			name:     "key with digits",
			input:    "$.item123",
			expected: "$.item123",
		},
		{
			name:     "PK field with underscore",
			input:    "$[_id=1]",
			expected: "$[_id=1]",
		},
		{
			name:     "multiple indices",
			input:    "$[0][1][2]",
			expected: "$[0][1][2]",
		},
		{
			name:     "PK with exponent",
			input:    "$[x=1e10]",
			expected: "$[x=10000000000.0]", // parsed as float, rendered canonical
		},
		{
			name:     "max int64 array index",
			input:    "$[9223372036854775807]",
			expected: "$[9223372036854775807]",
		},
		// Structural depth and mixed-segment shapes.
		{
			name:     "deeply nested keys",
			input:    "$.a.b.c.d.e.f.g.h.i.j",
			expected: "$.a.b.c.d.e.f.g.h.i.j",
		},
		{
			name:     "many indices",
			input:    "$[0][1][2][3][4][5][6][7][8][9]",
			expected: "$[0][1][2][3][4][5][6][7][8][9]",
		},
		{
			name:     "mixed nesting",
			input:    "$.data[0].items[1].value[2].name",
			expected: "$.data[0].items[1].value[2].name",
		},
		{
			name:     "PK then navigation",
			input:    `$.User[id="abc123"].profile.settings.theme`,
			expected: `$.User[id="abc123"].profile.settings.theme`,
		},
		{
			name:     "multiple PKs",
			input:    `$.Order[id=1].items[sku="ABC"].variants[color="red"]`,
			expected: `$.Order[id=1].items[sku="ABC"].variants[color="red"]`,
		},
		// Unicode in quoted keys.
		{
			name:     "CJK characters",
			input:    `$["日本語"]`,
			expected: `$["日本語"]`,
		},
		{
			name:     "emoji",
			input:    `$["🎉"]`,
			expected: `$["🎉"]`,
		},
		{
			name:     "arabic",
			input:    `$["مرحبا"]`,
			expected: `$["مرحبا"]`,
		},
		{
			name:     "mixed unicode and ascii",
			input:    `$["hello世界"]`,
			expected: `$["hello世界"]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, err := Parse(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, b.String())
		})
	}
}

func TestParse_InvalidPaths(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		errMsg string
	}{
		{
			name:   "empty string",
			input:  "",
			errMsg: "empty path",
		},
		{
			name:   "missing dollar",
			input:  "name",
			errMsg: "path must start with '$'",
		},
		{
			name:   "dot without key",
			input:  "$.",
			errMsg: "unexpected end",
		},
		{
			name:   "empty bracket",
			input:  "$[]",
			errMsg: "unexpected character",
		},
		{
			name:   "negative array index",
			input:  "$[-1]",
			errMsg: "negative array index",
		},
		{
			name:   "negative array index large",
			input:  "$[-100]",
			errMsg: "negative array index",
		},
		{
			name:   "unclosed bracket",
			input:  "$[0",
			errMsg: "expected ']'",
		},
		{
			name:   "unclosed string",
			input:  `$["hello`,
			errMsg: "unterminated string",
		},
		{
			name:   "invalid escape",
			input:  `$["\x"]`,
			errMsg: "unknown escape",
		},
		{
			name:   "incomplete unicode escape",
			input:  `$["\u00"]`,
			errMsg: "invalid unicode escape",
		},
		{
			name:   "invalid unicode escape",
			input:  `$["\uGGGG"]`,
			errMsg: "invalid unicode",
		},
		{
			name:   "PK missing equals",
			input:  "$[id]",
			errMsg: "expected '='",
		},
		{
			name:   "PK missing value",
			input:  "$[id=]",
			errMsg: "unexpected character ']' in PK value",
		},
		{
			name:   "PK incomplete composite",
			input:  `$[a="x",]`,
			errMsg: "PK field name",
		},
		{
			name:   "unexpected character",
			input:  "$ .name",
			errMsg: "unexpected character",
		},
		{
			name:   "double dot",
			input:  "$..name",
			errMsg: "identifier must start",
		},
		{
			name:   "invalid PK value",
			input:  "$[id=@]",
			errMsg: "unexpected character",
		},
		{
			name:   "decimal point without digits",
			input:  "$[x=.]",
			errMsg: "expected digit",
		},
		{
			name:   "unexpected character after dollar",
			input:  "$a",
			errMsg: "unexpected character",
		},
		{
			name:   "quoted key missing close bracket",
			input:  `$["key"`,
			errMsg: "expected ']' after string key",
		},
		{
			name:   "bracket with unexpected character",
			input:  "$[@]",
			errMsg: "unexpected character",
		},
		{
			name:   "int64-min array index",
			input:  "$[-9223372036854775808]",
			errMsg: "negative array index",
		},
		{
			name:   "array index overflows int",
			input:  "$[92233720368547758070]",
			errMsg: "invalid integer",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(tt.input)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errMsg)
		})
	}
}

func TestParse_RoundTrip(t *testing.T) {
	// Test that parsing the output of String() produces the same result
	paths := []Builder{
		Root(),
		Root().Key("name"),
		Root().Key("a").Key("b").Key("c"),
		Root().Index(0),
		Root().Index(0).Index(1),
		Root().Key("items").Index(0),
		Root().Key("Person").PK(PKField{Name: "id", Value: int64(42)}),
		Root().Key("Person").PK(PKField{Name: "name", Value: "Alice"}),
		Root().Key("Enrollment").PK(
			PKField{Name: "region", Value: "us"},
			PKField{Name: "studentId", Value: int64(12345)},
		),
		Root().Key("data").Key("users").Index(0).Key("profile"),
	}

	for _, original := range paths {
		t.Run(original.String(), func(t *testing.T) {
			str := original.String()
			parsed, err := Parse(str)
			require.NoError(t, err)
			assert.Equal(t, str, parsed.String())
		})
	}
}

func TestParseIdentifier(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		pos      int
		expected string
		newPos   int
		wantErr  bool
	}{
		{
			name:     "simple",
			input:    "name.other",
			pos:      0,
			expected: "name",
			newPos:   4,
		},
		{
			name:     "with underscore",
			input:    "_private",
			pos:      0,
			expected: "_private",
			newPos:   8,
		},
		{
			name:     "with digits",
			input:    "item123",
			pos:      0,
			expected: "item123",
			newPos:   7,
		},
		{
			name:    "starts with digit",
			input:   "123abc",
			pos:     0,
			wantErr: true,
		},
		{
			name:    "empty",
			input:   "",
			pos:     0,
			wantErr: true,
		},
		{
			name:     "from middle",
			input:    "$.name",
			pos:      2,
			expected: "name",
			newPos:   6,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, newPos, err := parseIdentifier(tt.input, tt.pos)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.expected, id)
			assert.Equal(t, tt.newPos, newPos)
		})
	}
}

func TestParseQuotedString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		pos      int
		expected string
		newPos   int
		wantErr  bool
	}{
		{
			name:     "simple",
			input:    `"hello"`,
			pos:      0,
			expected: "hello",
			newPos:   7,
		},
		{
			name:     "with escaped quote",
			input:    `"say \"hi\""`,
			pos:      0,
			expected: `say "hi"`,
			newPos:   12,
		},
		{
			name:     "with escaped backslash",
			input:    `"path\\file"`,
			pos:      0,
			expected: `path\file`,
			newPos:   12,
		},
		{
			name:     "with newline",
			input:    `"line1\nline2"`,
			pos:      0,
			expected: "line1\nline2",
			newPos:   14,
		},
		{
			name:     "with unicode",
			input:    `"\u0041\u0042"`,
			pos:      0,
			expected: "AB",
			newPos:   14,
		},
		{
			name:    "unterminated",
			input:   `"hello`,
			pos:     0,
			wantErr: true,
		},
		{
			name:    "invalid escape",
			input:   `"\x"`,
			pos:     0,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			str, newPos, err := parseQuotedString(tt.input, tt.pos)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.expected, str)
			assert.Equal(t, tt.newPos, newPos)
		})
	}
}

func TestParseNumber(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		pos      int
		expected string
		newPos   int
		wantErr  bool
	}{
		{
			name:     "integer",
			input:    "42]",
			pos:      0,
			expected: "42",
			newPos:   2,
		},
		{
			name:     "negative",
			input:    "-42]",
			pos:      0,
			expected: "-42",
			newPos:   3,
		},
		{
			name:     "zero",
			input:    "0]",
			pos:      0,
			expected: "0",
			newPos:   1,
		},
		{
			name:     "float",
			input:    "3.14]",
			pos:      0,
			expected: "3.14",
			newPos:   4,
		},
		{
			name:     "exponent",
			input:    "1e10]",
			pos:      0,
			expected: "1e10",
			newPos:   4,
		},
		{
			name:     "negative exponent",
			input:    "1e-10]",
			pos:      0,
			expected: "1e-10",
			newPos:   5,
		},
		{
			name:     "full float",
			input:    "-3.14e-2]",
			pos:      0,
			expected: "-3.14e-2",
			newPos:   8,
		},
		{
			name:    "just minus",
			input:   "-]",
			pos:     0,
			wantErr: true,
		},
		{
			name:    "decimal point only",
			input:   ".]",
			pos:     0,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			num, newPos, err := parseNumber(tt.input, tt.pos)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.expected, num)
			assert.Equal(t, tt.newPos, newPos)
		})
	}
}

func TestContainsBeforeClose(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		char     byte
		expected bool
	}{
		{
			name:     "found before close",
			input:    "id=42]",
			char:     '=',
			expected: true,
		},
		{
			name:     "not found before close",
			input:    "42]",
			char:     '=',
			expected: false,
		},
		{
			name:     "close first",
			input:    "]id=42",
			char:     '=',
			expected: false,
		},
		{
			name:     "no close bracket",
			input:    "id=42",
			char:     '=',
			expected: true,
		},
		{
			name:     "empty string",
			input:    "",
			char:     '=',
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := containsBeforeClose(tt.input, tt.char)
			assert.Equal(t, tt.expected, result)
		})
	}
}
