package markdown

import (
	"bytes"
	"strconv"
	"testing"
)

func TestMultiplicity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		optional, many bool
		want           string
	}{
		{"required single", false, false, "one"},
		{"required many", false, true, "one:many"},
		{"optional many", true, true, "many"},
		{"optional single", true, false, "_"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := multiplicity(tt.optional, tt.many); got != tt.want {
				t.Errorf("multiplicity(%v, %v) = %q, want %q", tt.optional, tt.many, got, tt.want)
			}
		})
	}
}

func TestSlug(t *testing.T) {
	t.Parallel()

	tests := []struct {
		heading string
		want    string
	}{
		{"Person", "person"},
		{"common.Region", "commonregion"},
		{"Fuel_Type", "fuel_type"},
		{"Schema Common (imported as common)", "schema-common-imported-as-common"},
		{"Class Diagram", "class-diagram"},
		{"Data Types", "data-types"},
		{"Cafe\u0301 decomposed", "cafe\u0301-decomposed"},
		{"\u0928\u092e\u0938\u094d\u0924\u0947", "\u0928\u092e\u0938\u094d\u0924\u0947"},
		{"\u0130stanbul", "i\u0307stanbul"},
		{"\u24b6 circled", "\u24d0-circled"},
		{"\u16ee rune", "\u16ee-rune"},
		{"a\u203fb", "a\u203fb"},
		{"x\u00b2 squared", "x-squared"},
		{"a\u200cb\u200dc", "abc"},
		{"a (b) c.d!?", "a-b-cd"},
		{"a-b c", "a-b-c"},
		{"B-", "b-"},
	}
	for _, tt := range tests {
		t.Run(tt.heading, func(t *testing.T) {
			t.Parallel()
			if got := slug(tt.heading); got != tt.want {
				t.Errorf("slug(%q) = %q, want %q", tt.heading, got, tt.want)
			}
		})
	}
}

func TestEscapeCell(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "no special characters", "no special characters"},
		{"empty", "", ""},
		{"pipe", "a|b", `a\|b`},
		{"newline", "line1\nline2", "line1<br>line2"},
		{"crlf", "line1\r\nline2", "line1<br>line2"},
		{"bare cr", "line1\rline2", "line1<br>line2"},
		{"pipe and newline", "a|b\nc", `a\|b<br>c`},
		{"indentation around fold dropped", "line1: \n\tline2 indented", "line1:<br>line2 indented"},
		{"lone backslash doubled", `a\b`, `a\\b`},
		{"backslash before pipe keeps odd parity", `a\|b`, `a\\\|b`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := escapeCell(tt.in); got != tt.want {
				t.Errorf("escapeCell(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestCodeCell(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "String[1, 100]", "`String[1, 100]`"},
		{"empty", "", ""},
		{"pipe inside code span", `Enum["a|b"]`, "`Enum[\"a\\|b\"]`"},
		{"backtick falls back to code tag", "a`b", "<code>a&#96;b</code>"},
		{"backtick with html metacharacters", "a`<&>", "<code>a&#96;&lt;&amp;&gt;</code>"},
		{"backtick with pipe", "a`|b", "<code>a&#96;\\|b</code>"},
		{"backslash before pipe takes the code tag", `a\|b`, "<code>a&#92;\\|b</code>"},
		{"backslash stays single in a code span", `Pattern["^\\d+$"]`, "`Pattern[\"^\\\\d+$\"]`"},
		{"inline syntax in a code tag is entities", `a\|*_~!`, "<code>a&#92;\\|&#42;&#95;&#126;&#33;</code>"},
		{"a line break takes the code tag", "a\nb", "<code>a<br>b</code>"},
		{"a CRLF is one break", "a\r\nb", "<code>a<br>b</code>"},
		{"a lone CR is a break", "a\rb", "<code>a<br>b</code>"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := codeCell(tt.in); got != tt.want {
				t.Errorf("codeCell(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestEscapeInline pins every character escapeInline escapes, and the
// underscore rule: an underscore between two letters or digits opens no
// emphasis and is left alone; any other underscore is escaped.
func TestEscapeInline(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in, want string
	}{
		{"plain", "plain"},
		{"a_b", "a_b"},
		{"1_2", "1_2"},
		{"é_é", "é_é"},
		{"_x", `\_x`},
		{"x_", `x\_`},
		{"_", `\_`},
		{"x _a_ y", `x \_a\_ y`},
		{"a__b", `a\_\_b`},
		{`x\(y`, `x\\(y`},
		{"a `d`", "a \\`d\\`"},
		{"*[]<>&~|!#", `\*\[\]\<\>\&\~\|\!\#`},
		{"a (b) c.d", "a (b) c.d"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()
			if got := escapeInline(tt.in); got != tt.want {
				t.Errorf("escapeInline(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestPrintableName pins that a schema name's control characters take their
// Go escapes and every other character is kept.
func TestPrintableName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in, want string
	}{
		{"plain name", "plain name"},
		{"a\nb", `a\nb`},
		{"a\r\tb", `a\r\tb`},
		{"a\x00b", `a\x00b`},
		{"a\u0085b", `a\u0085b`},
		{`a\"b`, `a\"b`},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			t.Parallel()
			if got := printableName(tt.in); got != tt.want {
				t.Errorf("printableName(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestAnchorAllocator pins GitHub's allocation: a repeated slug takes the next
// free suffix, skipping a suffixed form an earlier heading holds as its own
// slug.
func TestAnchorAllocator(t *testing.T) {
	t.Parallel()

	var a anchorAllocator
	for i, tt := range []struct{ heading, want string }{
		{"X", "x"},
		{"X 1", "x-1"},
		{"X", "x-2"},
		{"X", "x-3"},
		{"Y", "y"},
	} {
		if got := a.allocate(tt.heading); got != tt.want {
			t.Errorf("allocation %d of %q = %q, want %q", i, tt.heading, got, tt.want)
		}
	}
}

// TestUniqueMermaidID pins that a taken id takes the first free _N suffix of
// its own base.
func TestUniqueMermaidID(t *testing.T) {
	t.Parallel()

	taken := map[string]bool{}
	for i, tt := range []struct{ base, want string }{
		{"A_B", "A_B"},
		{"A_B_2", "A_B_2"},
		{"A_B", "A_B_3"},
		{"A_B", "A_B_4"},
	} {
		if got := uniqueMermaidID(tt.base, taken); got != tt.want {
			t.Errorf("id %d from %q = %q, want %q", i, tt.base, got, tt.want)
		}
	}
}

func TestMermaidID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want string
	}{
		{"Person", "Person"},
		{"common.Region", "common_Region"},
		{"Fuel_Type", "Fuel_Type"},
		{"a.b.c", "a_b_c"},
		{"a.B9", "a_B9"},
		{"a.Z0z", "a_Z0z"},
		{"Person (R\u00e9gion)", "Person__R_gion_"},
		{"Person (\u0663\U0001D49C)", "Person_____"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()
			if got := mermaidID(tt.in); got != tt.want {
				t.Errorf("mermaidID(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestWriteTableHeader(t *testing.T) {
	t.Parallel()

	var b bytes.Buffer
	var g generator
	g.newTable(&b, "Property", "Type", "Modifiers", "Description")
	want := "| Property | Type | Modifiers | Description |\n| --- | --- | --- | --- |\n"
	if got := b.String(); got != want {
		t.Errorf("newTable = %q, want %q", got, want)
	}
}

func TestWriteTableRow(t *testing.T) {
	t.Parallel()

	var b bytes.Buffer
	writeTableRow(&b, "`vin`", "`String`", "primary", "")
	want := "| `vin` | `String` | primary |  |\n"
	if got := b.String(); got != want {
		t.Errorf("writeTableRow = %q, want %q", got, want)
	}
}

func TestWriteFence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		lang string
		body string
		want string
	}{
		{"simple", "yammm", "age >= 18", "```yammm\nage >= 18\n```\n"},
		{"trailing newline not doubled", "yammm", "age >= 18\n", "```yammm\nage >= 18\n```\n"},
		{"multi-line", "yammm", "age >= 18\n&& age <= 150", "```yammm\nage >= 18\n&& age <= 150\n```\n"},
		{"embedded fence lengthens the outer fence", "", "```\ninner\n```", "````\n```\ninner\n```\n````\n"},
		{"short backtick run keeps minimum fence", "", "a `code` span", "```\na `code` span\n```\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var b bytes.Buffer
			writeFence(&b, tt.lang, tt.body)
			if got := b.String(); got != tt.want {
				t.Errorf("writeFence(%q, %q) = %q, want %q", tt.lang, tt.body, got, tt.want)
			}
		})
	}
}

// TestMermaidText pins the characters a diagram label writes as entity codes:
// the quote that ends a class label, the colon and semicolon that end an edge
// label, the percent sign that opens a comment or directive, the number sign
// that opens an entity, the characters a rendered label reads as HTML, and the
// first white space of a direction statement.
func TestMermaidText(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct{ in, want string }{
		{"common.Region", "common.Region"},
		{"WHEELS (one:many)", "WHEELS (one#58;many)"},
		{`a"b`, "a#quot;b"},
		{"a;b", "a#59;b"},
		{"%%{init}%%", "#37;#37;{init}#37;#37;"},
		{"#quot;", "#35;quot#59;"},
		{"<b>&amp;", "#60;b#62;#38;amp#59;"},
		{"R\u00e9gion (x)", "R\u00e9gion (x)"},
		{"P (a direction LR)", "P (a direction#32;LR)"},
		{"xdirection\u00a0TB direction", "xdirection#160;TB direction"},
		{"directions LR", "directions LR"},
		{"P (direction String)", "P (direction String)"},
		{"P (direction lr)", "P (direction lr)"},
		{"P (direction  LR)", "P (direction#32; LR)"},
		{"direction RL, direction BT", "direction#32;RL, direction#32;BT"},
	} {
		if got := mermaidText(tt.in); got != tt.want {
			t.Errorf("mermaidText(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// TestMermaidTextEscapesEveryDirectionSpace pins that each character
// JavaScript's \s matches, which is what Mermaid's lexer reads between
// "direction" and a direction keyword, is written as its entity code there.
// The list is ECMAScript's WhiteSpace and LineTerminator sets, written out
// here rather than read from jsSpace.
func TestMermaidTextEscapesEveryDirectionSpace(t *testing.T) {
	t.Parallel()

	for _, r := range []rune{'\u0009', '\u000a', '\u000b', '\u000c', '\u000d', '\u0020', '\u00a0', '\u1680', '\u2000', '\u2001', '\u2002', '\u2003', '\u2004', '\u2005', '\u2006', '\u2007', '\u2008', '\u2009', '\u200a', '\u2028', '\u2029', '\u202f', '\u205f', '\u3000', '\ufeff'} {
		in := "P (direction" + string(r) + "TB)"
		want := "P (direction#" + strconv.Itoa(int(r)) + ";TB)"
		if got := mermaidText(in); got != want {
			t.Errorf("mermaidText(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestCommonPrefix pins the shared leading bytes of two indents, empty when
// they differ from the first byte.
func TestCommonPrefix(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct{ a, b, want string }{
		{"\t\t", "  ", ""},
		{"\t\t", "\t ", "\t"},
		{"\t\t\t", "\t\t", "\t\t"},
		{"\t", "\t\t", "\t"},
	} {
		if got := commonPrefix(tt.a, tt.b); got != tt.want {
			t.Errorf("commonPrefix(%q, %q) = %q, want %q", tt.a, tt.b, got, tt.want)
		}
	}
}
