package jschema

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// renderToString is the test harness entry: render v at top level.
func renderToString(v val) string {
	var buf bytes.Buffer
	v.render(&buf, 0)
	return buf.String()
}

func TestRender_OrderedKeysPreserved(t *testing.T) {
	// Insertion order must survive rendering — never alphabetized. The keys
	// are chosen reverse-alphabetically so an accidental sort is caught.
	v := object(
		kv{"zeta", scalar(1)},
		kv{"alpha", scalar(2)},
		kv{"mid", scalar(3)},
	)
	got := renderToString(v)
	want := `{ "zeta": 1, "alpha": 2, "mid": 3 }`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRender_LayoutContract(t *testing.T) {
	// Layout rule: a value renders compact (single-line) iff its compact
	// form is <= compactWidthLimit; empty containers are always compact;
	// scalars are always inline.
	longRef := object(kv{"$ref", scalar("#/$defs/EDGE_Vehicle_registered_owner_PersonOfInterest")})
	cases := []struct {
		name string
		v    val
		want string
	}{
		{
			name: "small_object_compact",
			v:    object(kv{"type", scalar("string")}, kv{"format", scalar("uuid")}),
			want: `{ "type": "string", "format": "uuid" }`,
		},
		{
			name: "empty_object",
			v:    object(),
			want: `{}`,
		},
		{
			name: "empty_array",
			v:    array(),
			want: `[]`,
		},
		{
			name: "scalar_array_compact",
			v:    array(scalar("id"), scalar("name")),
			want: `["id", "name"]`,
		},
		{
			name: "nested_ref_object_compact",
			v: object(
				kv{"type", scalar("array")},
				kv{"items", object(kv{"$ref", scalar("#/$defs/Person")})},
			),
			want: `{ "type": "array", "items": { "$ref": "#/$defs/Person" } }`,
		},
		{
			// The width rule applies at every level: the outer object expands
			// AND the inner ref object (whose own compact form also exceeds
			// the limit) expands with it.
			name: "wide_object_expands_recursively",
			v: object(
				kv{"type", scalar("array")},
				kv{"items", longRef},
			),
			want: "{\n" +
				"  \"type\": \"array\",\n" +
				"  \"items\": {\n" +
				"    \"$ref\": \"#/$defs/EDGE_Vehicle_registered_owner_PersonOfInterest\"\n" +
				"  }\n" +
				"}",
		},
		{
			name: "expansion_recurses_with_indent",
			v: object(
				kv{"$defs", object(
					kv{"Person", object(
						kv{"type", scalar("object")},
						kv{"properties", object(
							kv{"name", object(kv{"type", scalar("string")}, kv{"minLength", scalar(int64(1))}, kv{"maxLength", scalar(int64(100))})},
						)},
						kv{"required", array(scalar("name"))},
						kv{"additionalProperties", scalar(false)},
					)},
				)},
			),
			want: "{\n" +
				"  \"$defs\": {\n" +
				"    \"Person\": {\n" +
				"      \"type\": \"object\",\n" +
				"      \"properties\": {\n" +
				"        \"name\": { \"type\": \"string\", \"minLength\": 1, \"maxLength\": 100 }\n" +
				"      },\n" +
				"      \"required\": [\"name\"],\n" +
				"      \"additionalProperties\": false\n" +
				"    }\n" +
				"  }\n" +
				"}",
		},
		{
			name: "wide_scalar_array_expands",
			v: array(
				scalar("first-long-enum-member-value"),
				scalar("second-long-enum-member-value"),
			),
			want: "[\n" +
				"  \"first-long-enum-member-value\",\n" +
				"  \"second-long-enum-member-value\"\n" +
				"]",
		},
		{
			name: "raw_passthrough",
			v:    object(kv{"const", raw(json.RawMessage(`42`))}),
			want: `{ "const": 42 }`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := renderToString(tc.v); got != tc.want {
				t.Errorf("got:\n%s\nwant:\n%s", got, tc.want)
			}
		})
	}
}

func TestRender_WidthBoundary(t *testing.T) {
	// Pin the exact threshold: `{ "k": "<s>" }` has compact length len(s)+11,
	// so a 49-char string sits exactly at compactWidthLimit (60) and a
	// 50-char string sits one past it.
	atLimit := object(kv{"k", scalar(strings.Repeat("x", compactWidthLimit-11))})
	if got := renderToString(atLimit); strings.Contains(got, "\n") {
		t.Errorf("value at the width limit should render compact, got:\n%s", got)
	}
	pastLimit := object(kv{"k", scalar(strings.Repeat("x", compactWidthLimit-10))})
	if got := renderToString(pastLimit); !strings.Contains(got, "\n") {
		t.Errorf("value past the width limit should render expanded, got:\n%s", got)
	}
}

func TestRender_EscapingContract(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string // rendered JSON string literal, including quotes
	}{
		{"quote_and_backslash", `say "hi" \o/`, `"say \"hi\" \\o/"`},
		// SetEscapeHTML(false): comparison operators in descriptions (e.g.
		// invariant text) must not become > noise in goldens.
		{"html_chars_literal", "endDate > startDate & a < b", `"endDate > startDate & a < b"`},
		{"unicode_passthrough", "OWNER (one) → Person", `"OWNER (one) → Person"`},
		{"control_char_escaped", "line1\nline2", `"line1\nline2"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := renderToString(scalar(tc.in)); got != tc.want {
				t.Errorf("got %s, want %s", got, tc.want)
			}
		})
	}
}

func TestRender_Deterministic(t *testing.T) {
	v := object(
		kv{"properties", object(kv{"a", scalar("x")}, kv{"b", array(scalar(int64(1)), scalar(int64(2)))})},
		kv{"required", array(scalar("a"))},
	)
	first := renderToString(v)
	for range 3 {
		if got := renderToString(v); got != first {
			t.Fatalf("non-deterministic rendering:\n%s\nvs:\n%s", first, got)
		}
	}
}

func TestRenderDocument_ValidJSONWithTrailingNewline(t *testing.T) {
	// renderDocument is what Marshal returns: parseable JSON ending in
	// exactly one newline (goldens must survive the end-of-file fixer).
	out := renderDocument(object(kv{"title", scalar("Fleet")}))
	if !bytes.HasSuffix(out, []byte("\n")) || bytes.HasSuffix(out, []byte("\n\n")) {
		t.Errorf("document must end in exactly one newline, got %q", out)
	}
	if !json.Valid(out) {
		t.Errorf("document is not valid JSON: %s", out)
	}
}

func TestScalar_PanicsOnUnmarshalableInput(t *testing.T) {
	// Scalars are generator-controlled (strings, int64s, bools, float64s);
	// anything else reaching scalar is a generator bug and must fail loudly.
	defer func() {
		if recover() == nil {
			t.Error("scalar(chan) should panic")
		}
	}()
	scalar(make(chan int))
}
