package json

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/location"
)

// decoderFaults are the messages of the diagnostics a failed decoder read
// reports. Each stops the parse.
var decoderFaults = []string{
	"invalid JSON",
	"error reading key",
	"error skipping value",
	"error reading array",
	"error reading array element",
	"error reading closing bracket",
	"error reading closing brace",
}

// isDecoderFault reports whether issue is a failed decoder read. An element
// that is not an object shares a message with one and names a type error.
func isDecoderFault(issue diag.Issue) bool {
	if !slices.Contains(decoderFaults, issue.Message()) {
		return false
	}
	for _, d := range issue.Details() {
		if d.Key == diag.DetailKeyDetail && strings.HasPrefix(d.Value, "json: cannot unmarshal") {
			return false
		}
	}
	return true
}

// issueWithSpan reports whether result holds an E_ADAPTER_PARSE issue whose
// message is msg and whose span starts at byte, line and column.
func issueWithSpan(result diag.Result, msg string, byteOffset, line, column int) bool {
	for issue := range result.Issues() {
		start := issue.Span().Start
		if issue.Code() == diag.E_ADAPTER_PARSE && issue.Message() == msg &&
			start.Byte == byteOffset && start.Line == line && start.Column == column {
			return true
		}
	}
	return false
}

// TestParseObject_PlacesAFaultAtTheOffendingByte pins where each read site
// reports a fault the decoder cannot read past: at the first byte no
// continuation of the document can repair, with jsonc's comments and blanked
// commas, and at the end of the input when the document ends too soon. Most
// documents put that byte away from the start of the read that failed.
func TestParseObject_PlacesAFaultAtTheOffendingByte(t *testing.T) {
	t.Parallel()
	id := location.MustNewSourceID("test://data/fault.json")
	cases := []struct {
		name         string
		doc          string
		msg          string
		byteOffset   int
		line, column int
	}{
		// An element the decoder reads whole.
		{"a bare word inside an element", `{"A": [{oops}]}`, "error reading array element", 8, 1, 9},
		{"a missing comma between members", `{"A": [{"x": 1 "y": 2}]}`, "error reading array element", 15, 1, 16},
		{"a missing comma on a later line", "{\"A\": [\n{\n  \"a\": 1\n  \"b\": 2\n}]}", "error reading array element", 21, 4, 3},
		{"a doubled comma between elements", `{"A": [{},,{}]}`, "error reading array element", 10, 1, 11},
		// jsonc blanks a comma a closer follows, so "[," can still close.
		{"a comma before the first element", `{"A": [,{}]}`, "error reading array element", 8, 1, 9},
		{"a doubled comma across lines", "{\"A\": [{}, {}  ,\n  , {}]}", "error reading array element", 19, 2, 3},
		{"a control character inside a string", "{\"A\": [{\"s\": \"a\x01b\"}]}", "error reading array element", 15, 1, 16},
		{"a literal cut short", `{"A": [{"n": tru}]}`, "error reading array element", 16, 1, 17},
		{"a fault after invalid UTF-8", "{\"A\": [{\"k\": \"\xff\"}, {\"x\" 1}]}", "error reading array element", 24, 1, 25},
		{"a line break inside a string", "{\"A\": [\n  {\"k\": 1,\n   \"s\": \"x\n\"}]}", "error reading array element", 29, 3, 11},
		{"a document that ends inside a comment", `{"A": [1, /* c`, "error reading array element", 14, 1, 15},
		// jsonc writes "/*" at the very end back one byte longer than it read.
		{"a document that ends in a comment opener", `{"A": [1, /*`, "error reading array element", 12, 1, 13},
		{"a document that ends in a slash", `{"A": [1, /`, "error reading array element", 11, 1, 12},
		{"a slash that starts no comment", `{"A": [1, /x]}`, "error reading array element", 11, 1, 12},
		{"a comment inside a literal", `{"A": [tr/**/ue]}`, "error reading array element", 9, 1, 10},
		{"a doubled comma before a closing bracket", `{"A": [{},,]}`, "error reading array element", 10, 1, 11},
		{"a doubled comma before a closing brace", `{"A": [{"x": 1,,}]}`, "error reading array element", 15, 1, 16},
		{"a document that ends inside an element", `{"Person": [{"name":`, "error reading array element", 20, 1, 21},
		{"a digit after a leading zero", `{"A": [0x]}`, "error reading array element", 8, 1, 9},
		{"a fault after a negative exponent", `{"A": [1e-5x]}`, "error reading array element", 11, 1, 12},
		{"a fault after an upper-case exponent", `{"A": [1E5x]}`, "error reading array element", 10, 1, 11},
		{"a document that ends inside a literal", `{"A": [tru`, "error reading array element", 10, 1, 11},
		{"a fault after false", `{"A": [false x]}`, "error reading array element", 13, 1, 14},
		{"the last control character inside a string", "{\"A\": [\"\x1f\"]}", "error reading array element", 8, 1, 9},
		{"a fault after a solidus escape", `{"A": ["\/", x]}`, "error reading array element", 13, 1, 14},
		{"a fault after a backspace escape", `{"A": ["\b", x]}`, "error reading array element", 13, 1, 14},
		{"a fault after an upper-case hex escape", `{"A": ["\u00FF", x]}`, "error reading array element", 17, 1, 18},
		{"a fault after a carriage return", "{\"A\": [\r1x]}", "error reading array element", 9, 2, 2},
		{"a fault after a tab", "{\"A\": [\t1x]}", "error reading array element", 9, 1, 10},

		// The root value.
		{"a bare word at the root", `  x`, "invalid JSON", 2, 1, 3},
		{"a word with a fault at the root", `trux`, "invalid JSON", 3, 1, 4},
		{"a bare word after a comment", "// header\nx", "invalid JSON", 10, 2, 1},
		{"a second byte order mark", "\uFEFF\uFEFF{}", "invalid JSON", 3, 1, 2},
		{"an array root after blank lines", "\n\n  [1]", "expected object at root", 4, 3, 3},
		{"an array root after a comment", "// header\n[1]", "expected object at root", 10, 2, 1},
		{"an array root after white space", "  [\n]\n", "expected object at root", 2, 1, 3},

		// A key of the root object.
		{"a missing comma after an array", `{"A": [{"x": 1}] "B": []}`, "error reading key", 17, 1, 18},
		{"a comma before the first key", `{,"A": []}`, "error reading key", 2, 1, 3},
		{"a doubled comma in the root", `{"A": [],,}`, "error reading key", 9, 1, 10},
		{"a literal where a name belongs", `{null: []}`, "error reading key", 1, 1, 2},
		{"a word cut short where a name belongs", `{tru}`, "error reading key", 1, 1, 2},
		{"a number where a name belongs", `{-1: []}`, "error reading key", 1, 1, 2},
		{"a number where a later name belongs", `{"A": [], -"B": []}`, "error reading key", 10, 1, 11},
		{"a slash where a name belongs", `{/"A": []}`, "error reading key", 2, 1, 3},
		{"a document that ends after a comma", `{"A": [],`, "error reading key", 9, 1, 10},

		// The value of a refused type name, which the decoder reads whole.
		{"a fault inside a refused name's value", "{\"a b\": [\n  {},\n  {x}]}", "error skipping value", 19, 3, 4},

		// The value of a type name.
		{"a bare word as a value", `{"A": x, "B": []}`, "error reading array", 6, 1, 7},
		{"a line break inside a string value", "{\"A\": \"abc\nx\"}", "error reading array", 10, 1, 11},
		{"a fault inside a value that is not an array", `{"A": {"x": [}, "B": []}`, "error skipping value", 13, 1, 14},
		{"a word where a name belongs in a value that is not an array", `{"A": {t"x": 1}}`, "error skipping value", 7, 1, 8},
		{"a comma after a name", `{"A",}`, "error reading array", 4, 1, 5},

		// A comma jsonc blanks leaves the parse reading on; the fault after it.
		{"a fault after an array closed past a comma", `{"A": [,], "B": x}`, "error reading array", 16, 1, 17},
		{"a fault after an object closed past a comma", `{"A": {,}, "B": x}`, "error reading array", 16, 1, 17},
		{"a fault after a trailing comma in an object", `{"A": {"x": 1,}, "B": x}`, "error reading array", 22, 1, 23},
		{"a fault after a trailing comma in an array", `{"A": [1,], "B": x}`, "error reading array", 17, 1, 18},

		// The closing delimiters.
		{"an array closed by a brace", `{"A": [{"x": 1}}, "B": []}`, "error reading closing bracket", 15, 1, 16},
		{"an empty array closed by a brace", `{"A": [}`, "error reading closing bracket", 7, 1, 8},
		{"a root closed by a bracket", `{"A": [{"x": 1}]]`, "error reading closing brace", 16, 1, 17},
		{"an empty root closed by a bracket", `{]`, "error reading closing brace", 1, 1, 2},

		// Positions are measured in the bytes passed in.
		{"a fault after a comment holding a multibyte rune", `{"A": [/* naïve */ {oops}]}`, "error reading array element", 21, 1, 21},
		{"a fault after a byte order mark", "\uFEFF{\"A\": [{oops}]}", "error reading array element", 11, 1, 10},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			_, result := parseForProvenance(t, id, c.doc)
			if !issueWithSpan(result, c.msg, c.byteOffset, c.line, c.column) {
				t.Fatalf("no %q at byte %d (%d:%d); got%s", c.msg, c.byteOffset, c.line, c.column, describe(result))
			}
			// The parse stops at the fault: no diagnostic names a later byte.
			for issue := range result.Issues() {
				if at := issue.Span().Start.Byte; issue.HasSpan() && at > c.byteOffset {
					t.Errorf("%q at byte %d, past the fault at %d", issue.Message(), at, c.byteOffset)
				}
			}
		})
	}
}

// TestParseObject_AFaultStillNamesWhatTheDecoderSaid pins that the placement
// moves the span alone: the detail keeps the decoder's own words.
func TestParseObject_AFaultStillNamesWhatTheDecoderSaid(t *testing.T) {
	t.Parallel()
	_, result := parseForProvenance(t, location.MustNewSourceID("test://data/detail.json"), `{"A": [{oops}]}`)
	if got := detailAt(result, "error reading array element"); !strings.Contains(got, "invalid character 'o'") {
		t.Errorf("detail %q, want the decoder's message", got)
	}
}

// TestParseObject_AFaultIsReportedOnce pins that each document with one fault
// draws one diagnostic for it, whichever read site met it.
func TestParseObject_AFaultIsReportedOnce(t *testing.T) {
	t.Parallel()
	id := location.MustNewSourceID("test://data/once.json")
	for _, doc := range []string{
		`{"A": [{oops}]}`,
		`{"A": [{"x": 1}] "B": []}`,
		`{"A": {"x": [}, "B": []}`,
		`{"A": [{"x": 1}]]`,
		`  x`,
	} {
		_, result := parseForProvenance(t, id, doc)
		var n int
		for issue := range result.Issues() {
			if isDecoderFault(issue) {
				n++
			}
		}
		if n != 1 {
			t.Errorf("%s: %d decoder faults, want 1:%s", doc, n, describe(result))
		}
	}
}

// TestParseObject_ReadsWhatJsoncAccepts pins that a document jsonc turns into
// JSON draws no decoder fault, and that faultOffset reads it whole.
func TestParseObject_ReadsWhatJsoncAccepts(t *testing.T) {
	t.Parallel()
	id := location.MustNewSourceID("test://data/jsonc.json")
	for _, doc := range []string{
		`{"A": [,]}`,
		`{,}`,
		`{"A": [{}, ], }`,
		`{"A": [{"x": 1, /* c */ }] // end` + "\n}",
		"{\"A\": [\n// c, ]\n{}]}",
	} {
		_, result := parseForProvenance(t, id, doc)
		for issue := range result.Issues() {
			if isDecoderFault(issue) {
				t.Errorf("%q: %q at %v:%s", doc, issue.Message(), issue.Span().Start, describe(result))
			}
		}
		// The parse runs faultOffset only after a read fails, so it is asked here.
		if at, ok := faultOffset([]byte(doc)); ok {
			t.Errorf("%q: faultOffset refuses byte %d", doc, at)
		}
	}
}

// TestFaultOffset_ReadsATokenTheInputCutsShort pins that a document ending
// inside a token draws no byte: a continuation can still complete it. The
// grammar is asked directly, since the parse's message for such a document can
// differ between implementations of encoding/json, as {"A": [0 does.
func TestFaultOffset_ReadsATokenTheInputCutsShort(t *testing.T) {
	t.Parallel()
	for _, doc := range []string{
		`{"A": [0`, `{"A": [-`, `{"A": [1.`, `{"A": [1e`, `{"A": [1e-`, `{"A": [1E+`,
		`{"A": [tru`, `{"A": [fals`, `{"A": [nul`, `{"A": ["\u00`, `{"A": ["\`, `{"A": [/`,
	} {
		if at, ok := faultOffset([]byte(doc)); ok {
			t.Errorf("%q: faultOffset refuses byte %d", doc, at)
		}
	}
}

// TestParseObject_ReadStartSkipsEveryBlank pins that a span taken after a
// comma or a colon skips a tab and a carriage return as it skips a space.
func TestParseObject_ReadStartSkipsEveryBlank(t *testing.T) {
	t.Parallel()
	id := location.MustNewSourceID("test://data/blank.json")
	cases := []struct {
		name, doc, msg string
		byteOffset     int
	}{
		{"a tab after a colon", "{\"A\":\t5}", "expected array", 6},
		{"a carriage return after a comma", "{\"A\": [],\r\n\"a b\": []}", "invalid type name", 11},
	}
	for _, c := range cases {
		_, result := parseForProvenance(t, id, c.doc)
		var found bool
		for issue := range result.Issues() {
			if strings.HasPrefix(issue.Message(), c.msg) && issue.Span().Start.Byte == c.byteOffset {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: no %q at byte %d:%s", c.name, c.msg, c.byteOffset, describe(result))
		}
	}
	parsed, result := parseForProvenance(t, id, "{\"A\": [{},\r\n{}]}")
	if result.HasErrors() || len(parsed["A"]) != 2 {
		t.Fatalf("parse:%s", describe(result))
	}
	if got := parsed["A"][1].Provenance.Span().Start.Byte; got != 12 {
		t.Errorf("the second instance starts at byte %d, want 12", got)
	}
}

// BenchmarkParseObject_FaultPlacement measures what placing a fault costs: a
// document whose one element holds a long string and then a fault, beside the
// same document with the fault removed.
func BenchmarkParseObject_FaultPlacement(b *testing.B) {
	id := location.MustNewSourceID("test://data/bench.json")
	for _, size := range []int{1 << 10, 1 << 16, 1 << 20} {
		long := strings.Repeat("x", size)
		for _, c := range []struct{ name, doc string }{
			{"clean", `{"A": [{"s": "` + long + `", "t": 1}]}`},
			{"fault", `{"A": [{"s": "` + long + `" "t": 1}]}`},
		} {
			b.Run(fmt.Sprintf("%s/%dKiB", c.name, size>>10), func(b *testing.B) {
				data := []byte(c.doc)
				a := New()
				b.SetBytes(int64(len(data)))
				for b.Loop() {
					a.ParseObject(b.Context(), id, data)
				}
			})
		}
	}
}
