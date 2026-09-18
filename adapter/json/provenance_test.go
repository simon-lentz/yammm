package json

import (
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/location"
)

func parseForProvenance(t *testing.T, id location.SourceID, doc string) (map[string][]instance.RawInstance, diag.Result) {
	t.Helper()
	return New().ParseObject(t.Context(), id, []byte(doc))
}

// firstIssue returns the result's first issue in arrival order.
func firstIssue(result diag.Result) (diag.Issue, bool) {
	for issue := range result.Issues() {
		return issue, true
	}
	return diag.Issue{}, false
}

// TestParseObject_RecordsProvenancePerInstance pins the three parts of an
// instance's provenance: the document's identity, its path inside that
// document, and the span of its opening brace.
func TestParseObject_RecordsProvenancePerInstance(t *testing.T) {
	id := location.MustNewSourceID("test://data/people.json")
	doc := "{\n  \"Person\": [\n    {\"name\": \"ada\"},\n    {\"name\": \"grace\"}\n  ],\n  \"Company\": [\n    {\"name\": \"acme\"}\n  ]\n}\n"

	parsed, result := parseForProvenance(t, id, doc)
	if result.HasErrors() {
		t.Fatalf("parse: %s", result)
	}

	cases := []struct {
		typeName string
		index    int
		path     string
		line     int
		column   int
	}{
		{"Person", 0, "$.Person[0]", 3, 5},
		{"Person", 1, "$.Person[1]", 4, 5},
		{"Company", 0, "$.Company[0]", 7, 5},
	}
	for _, c := range cases {
		prov := parsed[c.typeName][c.index].Provenance
		if prov == nil {
			t.Fatalf("%s[%d]: no provenance", c.typeName, c.index)
		}
		if got := prov.SourceName(); got != id.String() {
			t.Errorf("%s[%d]: source name %q, want %q", c.typeName, c.index, got, id.String())
		}
		if got := prov.Path().String(); got != c.path {
			t.Errorf("%s[%d]: path %q, want %q", c.typeName, c.index, got, c.path)
		}
		span := prov.Span()
		if span.Source != id {
			t.Errorf("%s[%d]: span source %v, want %v", c.typeName, c.index, span.Source, id)
		}
		if span.Start.Line != c.line || span.Start.Column != c.column {
			t.Errorf("%s[%d]: span at %d:%d, want %d:%d",
				c.typeName, c.index, span.Start.Line, span.Start.Column, c.line, c.column)
		}
		if !span.IsPoint() {
			t.Errorf("%s[%d]: span is not a point: %+v", c.typeName, c.index, span)
		}
	}
}

// TestParseObject_ProvenanceStartsPastTheSeparator pins the offset correction
// every element after the first needs: the decoder reports the comma, not the
// element.
func TestParseObject_ProvenanceStartsPastTheSeparator(t *testing.T) {
	id := location.MustNewSourceID("test://data/sep.json")
	doc := `{"Person": [{"a": 1}, {"b": 2}]}`

	parsed, _ := parseForProvenance(t, id, doc)
	second := parsed["Person"][1].Provenance.Span().Start
	// Column 23 is the "{" of the second object; 22 is the comma before it.
	if second.Line != 1 || second.Column != 23 {
		t.Errorf("second element at %d:%d, want 1:23", second.Line, second.Column)
	}
}

// TestParseObject_ProvenanceCountsColumnsOverTheOriginalBytes pins that a
// comment is not measured as jsonc rewrites it: jsonc writes one space per
// comment BYTE, so a multibyte rune in a comment shifts every later column.
//
// The comment and the element share ONE LINE on purpose. With the element on a
// later line the two measurements agree — jsonc preserves line breaks — and the
// test cannot fail for the property it names.
func TestParseObject_ProvenanceCountsColumnsOverTheOriginalBytes(t *testing.T) {
	id := location.MustNewSourceID("test://data/comment.json")
	doc := `{"Person": [/* naïve */ {"a": 1}]}`

	parsed, result := parseForProvenance(t, id, doc)
	if result.HasErrors() {
		t.Fatalf("parse: %s", result)
	}
	start := parsed["Person"][0].Provenance.Span().Start
	// The comment is 13 runes and 14 bytes, so measuring over jsonc's buffer
	// would report column 26.
	if start.Line != 1 || start.Column != 25 {
		t.Errorf("element after a comment holding a multibyte rune: %d:%d, want 1:25", start.Line, start.Column)
	}
}

// TestParseObject_ProvenanceSkipsTheByteOrderMark pins that a leading mark
// shifts no position: it is trimmed before the decoder runs and its length is
// added back to every offset.
func TestParseObject_ProvenanceSkipsTheByteOrderMark(t *testing.T) {
	id := location.MustNewSourceID("test://data/bom.json")
	doc := string(rune(0xFEFF)) + `{"Person": [{"a": 1}]}`

	parsed, result := parseForProvenance(t, id, doc)
	if result.HasErrors() {
		t.Fatalf("parse: %s", result)
	}
	start := parsed["Person"][0].Provenance.Span().Start
	// The mark is one rune, so the "{" of the element is the 14th.
	if start.Line != 1 || start.Column != 14 {
		t.Errorf("element after a byte order mark: %d:%d, want 1:14", start.Line, start.Column)
	}
}

// TestParseObject_DiagnosticsCarryASpan pins that every parse diagnostic
// locates itself in the document the caller handed in.
func TestParseObject_DiagnosticsCarryASpan(t *testing.T) {
	id := location.MustNewSourceID("test://data/bad.json")
	cases := []struct {
		name string
		doc  string
		line int
	}{
		{"syntax error inside an element", "{\n  \"Person\": [\n    {oops}\n  ]\n}\n", 3},
		{"element is not an object", "{\n  \"Person\": [\n    42\n  ]\n}\n", 3},
		{"value is not an array", "{\n  \"Person\": 42\n}\n", 2},
		{"invalid type tag", "{\n  \"9bad\": []\n}\n", 2},
		{"root is not an object", "[\n]\n", 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, result := parseForProvenance(t, id, c.doc)
			first, ok := firstIssue(result)
			if !ok {
				t.Fatalf("no diagnostic")
			}
			if !first.HasSpan() {
				t.Fatalf("diagnostic %q carries no span", first.Message())
			}
			if first.Span().Source != id {
				t.Errorf("span source %v, want %v", first.Span().Source, id)
			}
			if got := first.Span().Start.Line; got != c.line {
				t.Errorf("span on line %d, want %d (%s)", got, c.line, first.Message())
			}
		})
	}
}

// TestParseObject_DiagnosticCodesAreStable pins the code each parse refusal
// reports, which no test asserted before.
func TestParseObject_DiagnosticCodesAreStable(t *testing.T) {
	id := location.MustNewSourceID("test://data/codes.json")
	cases := []struct {
		name string
		doc  string
		code diag.Code
	}{
		{"malformed document", "not json", diag.E_ADAPTER_PARSE},
		{"root is an array", "[]", diag.E_ADAPTER_PARSE},
		{"reserved type name", `{"type": []}`, diag.E_INVALID_TYPE_TAG},
		{"type name with a space", `{"a b": []}`, diag.E_INVALID_TYPE_TAG},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, result := parseForProvenance(t, id, c.doc)
			first, ok := firstIssue(result)
			if !ok {
				t.Fatalf("no diagnostic")
			}
			if got := first.Code(); got != c.code {
				t.Errorf("code %v, want %v (%s)", got, c.code, first.Message())
			}
		})
	}
}

// TestParseObject_PathIndexesTheDocumentNotTheResult pins the rule the package
// doc states: a failed element consumes its index, so the path keeps addressing
// the document.
func TestParseObject_PathIndexesTheDocumentNotTheResult(t *testing.T) {
	id := location.MustNewSourceID("test://data/gap.json")
	// The middle element is a number, which fails to decode into a map without
	// a syntax error, so the parser reports it and reads on.
	doc := `{"Person": [{"a": 1}, 42, {"b": 2}]}`

	parsed, result := parseForProvenance(t, id, doc)
	if _, ok := firstIssue(result); !ok {
		t.Fatalf("the middle element drew no diagnostic")
	}

	rows := parsed["Person"]
	if len(rows) != 2 {
		t.Fatalf("got %d instances, want 2", len(rows))
	}
	for i, want := range []string{"$.Person[0]", "$.Person[2]"} {
		if got := rows[i].Provenance.Path().String(); got != want {
			t.Errorf("instance %d: path %q, want %q", i, got, want)
		}
	}
}

// TestParseObject_DiagnosticSpansPinTheColumn pins the column of each parse
// refusal, not only its line. Every document here puts the right answer and a
// plausible wrong one on DIFFERENT lines or columns, which a line-only
// assertion cannot separate.
func TestParseObject_DiagnosticSpansPinTheColumn(t *testing.T) {
	id := location.MustNewSourceID("test://data/columns.json")
	cases := []struct {
		name   string
		doc    string
		line   int
		column int
	}{
		// The value, not the colon that precedes it on the line above.
		{"value is not an array", "{\n  \"Person\":\n    42\n}\n", 3, 5},
		// The element's start, not the end of the token the decoder rejected.
		{"element of the wrong type", "{\n  \"Person\": [\n    42\n  ]\n}\n", 3, 5},
		// The offending byte, not the one after it.
		{"syntax error inside an element", "{\"A\": [{oops}]}", 1, 9},
		// A syntax error whose offending byte ends a line must not name the next.
		{"syntax error at a line end", "{\"A\": \"abc\nx\"}", 1, 11},
		// The trailing token's start, not its end.
		{"trailing content", "{}\n12345\n", 2, 1},
		// The root refusal points at the first byte of the document.
		{"root is not an object", "  [\n]\n", 1, 1},
		// A null element is reported where the element is, not where its array
		// began.
		{"element is null", "{\n  \"Person\": [\n    null\n  ]\n}\n", 3, 5},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, result := parseForProvenance(t, id, c.doc)
			first, ok := firstIssue(result)
			if !ok {
				t.Fatalf("no diagnostic")
			}
			got := first.Span().Start
			if got.Line != c.line || got.Column != c.column {
				t.Errorf("span at %d:%d, want %d:%d (%s)", got.Line, got.Column, c.line, c.column, first.Message())
			}
		})
	}
}
