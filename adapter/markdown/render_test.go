package markdown

import (
	"bytes"
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

// TestDescriptionCell pins that a doc comment in a table cell changes only
// where the table layer reads it: a line break folds to <br>, or to a space
// inside a code span or raw HTML, a hard-break backslash gives way to the
// <br>, and a pipe after an even backslash run gains one backslash, so it
// renders as the doc comment's.
func TestDescriptionCell(t *testing.T) {
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
		{"an author escape stays", `a\*b\*`, `a\*b\*`},
		{"a lone backslash stays", `a\b`, `a\b`},
		{"an escaped pipe stays escaped", `a\|b`, `a\|b`},
		{"a pipe after an escaped backslash is escaped", `a\\|b`, `a\\\|b`},
		{"a pipe after three backslashes stays", `a\\\|b`, `a\\\|b`},
		{"leading pipe", "|x", `\|x`},
		{"a pipe after an escape the run restarts at", `\*x|y`, `\*x\|y`},
		{"a line-end backslash is the break", "C:\\\nnext", "C:<br>next"},
		{"an escaped backslash at a line end stays", "C:\\\\\nnext", "C:\\\\<br>next"},
		{"a code span across a line folds to a space", "a `x\ny` b", "a `x y` b"},
		{"a code span's line-end backslash stays", "`x\\\ny`", "`x\\ y`"},
		{"raw HTML across a line folds to a space", "<a\nhref=\"#x\">y</a>", "<a href=\"#x\">y</a>"},
		{"an HTML block folds to spaces", "<div>\nx\n</div>", "<div> x </div>"},
		{"a pre element keeps its breaks", "<pre>\na\n  b\n</pre>", "<pre>a<br>  b<br></pre>"},
		{"a listing element keeps its breaks", "<listing>\na\nb\n</listing>", "<listing>a<br>b<br></listing>"},
		{"a break after a tag inside pre is kept", "<pre>a<b>\nc</b>\nd</pre>", "<pre>a<b><br>c</b><br>d</pre>"},
		{"a break after a pre element is white space", "<div><pre>a</pre>\nb\n</div>", "<div><pre>a</pre> b </div>"},
	}
	md := newParser()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := descriptionCell(md, tt.in); got != tt.want {
				t.Errorf("descriptionCell(%q) = %q, want %q", tt.in, got, tt.want)
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
		{"www.x", "www<!---->.x"},
		{"xWwW.y", "xWwW<!---->.y"},
		{"ww.x", "ww.x"},
		{"a:b@c", "a<!---->:b<!---->@c"},
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
		{"Redirection", "Redirec_tion"},
		{"redirection.Directions", "redirec_tion_Directions"},
		{"directiondirection", "direc_tiondirec_tion"},
		{"Direction", "Direction"},
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

func TestNewTable(t *testing.T) {
	t.Parallel()

	var g generator
	g.newTable(0, "Property", "Type", "Modifiers", "Description").row(tableCell{md: "`vin`"}, tableCell{md: "`String`"}, tableCell{md: "primary"}, tableCell{})
	want := "| Property | Type | Modifiers | Description |\n| --- | --- | --- | --- |\n| `vin` | `String` | primary |  |\n"
	if got := g.buf.String(); got != want {
		t.Errorf("table = %q, want %q", got, want)
	}
	var indented generator
	indented.newTable(bulletIndent, "A").row(tableCell{md: "x", author: true})
	want = "    | A |\n    | --- |\n    | x |\n"
	if got := indented.buf.String(); got != want {
		t.Errorf("indented table = %q, want %q", got, want)
	}
	if len(indented.authored) != 1 || indented.buf.String()[indented.authored[0].start:indented.authored[0].end] != "x" {
		t.Errorf("authored spans = %+v, want the one description cell", indented.authored)
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

// TestMermaidChars pins the characters an edge label or a member line writes
// as entity codes: the quote, the colon and semicolon that end an edge label,
// the percent sign that opens a comment or directive, the number sign that
// opens an entity, and the characters a rendered label reads as HTML.
func TestMermaidChars(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct{ in, want string }{
		{"WHEELS (one:many)", "WHEELS (one#58;many)"},
		{`a"b`, "a#quot;b"},
		{"a;b", "a#59;b"},
		{"%%{init}%%", "#37;#37;{init}#37;#37;"},
		{"#quot;", "#35;quot#59;"},
		{"<b>&amp;", "#60;b#62;#38;amp#59;"},
	} {
		if got := mermaidChars(tt.in); got != tt.want {
			t.Errorf("mermaidChars(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// TestMermaidLabel pins the class label's allowlist: ASCII letters and
// digits, a dot and an underscore between two ASCII letters or digits stay,
// and every other character is its decimal entity code.
func TestMermaidLabel(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct{ in, want string }{
		{"common.Region", "common.Region"},
		{"a_b.C_2", "a_b.C_2"},
		{"_a b_", "#95;a#32;b#95;"},
		{"a__b", "a#95;#95;b"},
		{"Person (sch)", "Person#32;#40;sch#41;"},
		{`*b* $x$ \d "q" #1; :;%<>&`, "#42;b#42;#32;#36;x#36;#32;#92;d#32;#34;q#34;#32;#35;1#59;#32;#58;#59;#37;#60;#62;#38;"},
		{"R\u00e9gion \u0663\U0001D49C", "R#233;gion#32;#1635;#119964;"},
		{"x\ufb02\u00b0y\u00b6\u00dfz", "x#64258;#176;y#182;#223;z"},
		{"direction LR", "direction#32;LR"},
	} {
		if got := mermaidLabel(tt.in); got != tt.want {
			t.Errorf("mermaidLabel(%q) = %q, want %q", tt.in, got, tt.want)
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
