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
	writeTableHeader(&b, "Property", "Type", "Modifiers", "Description")
	want := "| Property | Type | Modifiers | Description |\n| --- | --- | --- | --- |\n"
	if got := b.String(); got != want {
		t.Errorf("writeTableHeader = %q, want %q", got, want)
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
