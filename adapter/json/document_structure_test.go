package json

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

func allIssues(result diag.Result) []diag.Issue {
	return slices.Collect(result.Issues())
}

// issueAt reports whether result holds an Error E_ADAPTER_PARSE issue whose
// message contains text and whose span starts at line:column.
func issueAt(result diag.Result, text string, line, column int) bool {
	for issue := range result.Issues() {
		if issue.Severity() == diag.Error && issue.Code() == diag.E_ADAPTER_PARSE &&
			strings.Contains(issue.Message(), text) &&
			issue.Span().Start.Line == line && issue.Span().Start.Column == column {
			return true
		}
	}
	return false
}

// detailAt returns the parse detail of the Error E_ADAPTER_PARSE issue whose
// message contains text, or "" when no issue carries one.
func detailAt(result diag.Result, text string) string {
	for issue := range result.Issues() {
		if issue.Code() != diag.E_ADAPTER_PARSE || !strings.Contains(issue.Message(), text) {
			continue
		}
		for _, d := range issue.Details() {
			if d.Key == diag.DetailKeyDetail {
				return d.Value
			}
		}
	}
	return ""
}

func describe(result diag.Result) string {
	var b strings.Builder
	for issue := range result.Issues() {
		b.WriteString("\n  ")
		b.WriteString(issue.Code().String())
		b.WriteString(" ")
		b.WriteString(issue.Message())
		b.WriteString(" at ")
		b.WriteString(issue.Span().Start.String())
	}
	return b.String()
}

// TestParseObject_ARepeatedTypeKeyIsReportedAndRead pins that a second array
// under one type name is reported at its key and still read: decoding into a
// map keeps the last value, which drops the first array's whole batch, and
// every instance the document states is in the entry.
func TestParseObject_ARepeatedTypeKeyIsReportedAndRead(t *testing.T) {
	id := location.MustNewSourceID("test://data/repeated_key.json")
	cases := []struct {
		name   string
		doc    string
		line   int
		column int
		want   []string
	}{
		{"two arrays", "{\"Person\": [{\"id\": \"p1\"}],\n \"Person\": [{\"id\": \"p2\"}]}", 2, 2, []string{"p1", "p2"}},
		{"after a value that was not an array", "{\"Person\": 5,\n \"Person\": [{\"id\": \"p2\"}]}", 2, 2, []string{"p2"}},
		{"spelled with an escape", "{\"Person\": [{\"id\": \"p1\"}],\n \"P\\u0065rson\": [{\"id\": \"p2\"}]}", 2, 2, []string{"p1", "p2"}},
		{"after an empty array", "{\"Person\": [],\n \"Person\": [{\"id\": \"p2\"}]}", 2, 2, []string{"p2"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			parsed, result := parseForProvenance(t, id, c.doc)
			if !issueAt(result, `repeated type key "Person"`, c.line, c.column) {
				t.Fatalf("no refusal of the repeated key at %d:%d; got%s", c.line, c.column, describe(result))
			}
			rows, ok := parsed["Person"]
			if !ok {
				t.Fatalf("no entry for Person")
			}
			var ids []string
			for _, r := range rows {
				ids = append(ids, r.Properties["id"].(string))
			}
			if !slices.Equal(ids, c.want) {
				t.Errorf("Person holds %v, want %v", ids, c.want)
			}
		})
	}

	t.Run("the repeated array is read, and its own faults are reported", func(t *testing.T) {
		parsed, result := parseForProvenance(t, id, `{"Person": [{"id": "p1"}], "Person": [null, {"id": "p3"}]}`)
		// The repetition, and the null element inside the repeated array.
		if got := len(allIssues(result)); got != 2 {
			t.Fatalf("got %d issues, want 2:%s", got, describe(result))
		}
		if len(parsed["Person"]) != 2 {
			t.Fatalf("Person holds %d instances, want 2 (p1 and p3)", len(parsed["Person"]))
		}
		// The second array indexes its own elements, as the document writes it.
		if got := parsed["Person"][1].Provenance.Path().String(); got != "$.Person[1]" {
			t.Errorf("the second array's instance has path %q, want %q", got, "$.Person[1]")
		}
	})

	t.Run("distinct keys are not repetitions", func(t *testing.T) {
		parsed, result := parseForProvenance(t, id, `{"Person": [{"id": "p1"}], "person2": [], "Company": [{"id": "c1"}]}`)
		for issue := range result.Issues() {
			if strings.Contains(issue.Message(), "repeated") {
				t.Fatalf("unexpected repetition refusal:%s", describe(result))
			}
		}
		if len(parsed["Person"]) != 1 || len(parsed["Company"]) != 1 {
			t.Fatalf("got %v", parsed)
		}
	})
}

// TestParseObject_TrailingContentIsRefused pins that anything but white space
// after the root object is refused where it starts, whether or not it would
// read as a JSON value, and that the root object's instances are still parsed.
func TestParseObject_TrailingContentIsRefused(t *testing.T) {
	id := location.MustNewSourceID("test://data/trailing.json")
	const root = `{"A": [{"id": "1"}]}`
	// The root is 20 bytes, so a tail's first byte is column 21 of line 1.
	refused := []struct {
		name         string
		tail         string
		line, column int
	}{
		{"a bare word", " xyz", 1, 22},
		{"a closing brace", "}", 1, 21},
		{"a closing bracket", "]", 1, 21},
		{"markup", "\n<html>", 2, 1},
		{"a comma", ",", 1, 21},
		{"an unterminated block comment", " /*", 1, 22},
		{"a second object", ` {"B": []}`, 1, 22},
		{"a number", " 123", 1, 22},
		// jsonc blanks a comma before a closing bracket, after the root too.
		{"a comma before a closing bracket", ",]", 1, 21},
		{"a comma before a closing bracket on the next line", ",\n]", 1, 21},
		{"a comma after a comment", " /* c */,}", 1, 29},
		{"a byte that is not UTF-8", " \xff", 1, 22},
	}
	for _, c := range refused {
		t.Run(c.name, func(t *testing.T) {
			parsed, result := parseForProvenance(t, id, root+c.tail)
			if !issueAt(result, "unexpected content after root object", c.line, c.column) {
				t.Fatalf("no trailing-content refusal at %d:%d; got%s", c.line, c.column, describe(result))
			}
			if got := len(allIssues(result)); got != 1 {
				t.Errorf("got %d issues, want 1:%s", got, describe(result))
			}
			if len(parsed["A"]) != 1 {
				t.Errorf("A holds %d instances, want 1", len(parsed["A"]))
			}
		})
	}

	accepted := []struct {
		name string
		tail string
	}{
		{"nothing", ""},
		{"white space", " \t\r\n"},
		{"a line comment", " // done"},
		{"a block comment", " /* done */\n"},
		{"comments of both kinds", " /* a, b */ // c, ]\r\n/**/"},
	}
	for _, c := range accepted {
		t.Run("accepts "+c.name, func(t *testing.T) {
			_, result := parseForProvenance(t, id, root+c.tail)
			if !result.OK() {
				t.Fatalf("refused:%s", describe(result))
			}
		})
	}

	for _, c := range []struct{ tail, want string }{
		{" xyz", "found 'x'"},
		{" é", "found 'é'"},
		{",]", "found ','"},
		{" /*", "found '/'"},
		{" \xff", "found byte 0xff"},
	} {
		t.Run("names what it found in "+strconv.Quote(c.tail), func(t *testing.T) {
			_, result := parseForProvenance(t, id, root+c.tail)
			if got := detailAt(result, "unexpected content"); got != c.want {
				t.Errorf("detail %q, want %q", got, c.want)
			}
		})
	}
}

// TestParseObject_ARepeatedMemberIsReported pins that a member name repeated
// inside one object, at any depth of an instance, is reported at each repeat
// and the instance is still produced, holding the value the decoder kept.
func TestParseObject_ARepeatedMemberIsReported(t *testing.T) {
	id := location.MustNewSourceID("test://data/repeated_member.json")
	cases := []struct {
		name   string
		elem   string
		member string
		column int
	}{
		{"at the top of the instance", `{"id": "a", "id": "b"}`, "id", 14},
		{"inside a composed child", `{"id": "a", "kids": [{"n": 1, "n": 2}]}`, "n", 32},
		{"inside an edge object", `{"id": "a", "boss": {"_target_id": "x", "_target_id": "y"}}`, "_target_id", 42},
		{"spelled with an escape", `{"id": "a", "i\u0064": "b"}`, "id", 14},
		{"with equal values", `{"id": "a", "id": "a"}`, "id", 14},
		// The decoder reads each byte of an invalid sequence as U+FFFD.
		{"differing only in invalid UTF-8", "{\"x\xff\": 1, \"x\xfe\": 2}", "x\uFFFD", 12},
		{"invalid UTF-8 inside a composed child", "{\"id\": \"a\", \"kids\": [{\"n\xff\": 1, \"n\xfe\": 2}]}", "n\uFFFD", 33},
		{"invalid UTF-8 and the replacement it reads as", "{\"x\uFFFD\": 1, \"x\xff\": 2}", "x\uFFFD", 12},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// The element starts at column 2 of line 2, after one space.
			doc := "{\"Person\": [{\"id\": \"before\"},\n " + c.elem + ", {\"id\": \"after\"}]}"
			parsed, result := parseForProvenance(t, id, doc)
			if !issueAt(result, "repeated member \""+c.member+"\"", 2, c.column) {
				t.Fatalf("no refusal of the repeated member at 2:%d; got%s", c.column, describe(result))
			}
			rows := parsed["Person"]
			var paths []string
			for _, r := range rows {
				paths = append(paths, r.Provenance.Path().String())
			}
			if want := []string{"$.Person[0]", "$.Person[1]", "$.Person[2]"}; !slices.Equal(paths, want) {
				t.Errorf("instances at %v, want %v", paths, want)
			}
		})
	}

	t.Run("the detail names the first occurrence", func(t *testing.T) {
		doc := "{\"Person\": [\n  {\"a\": 1,\n   \"b\": 2,\n   \"a\": 3}]}"
		_, result := parseForProvenance(t, id, doc)
		if !issueAt(result, `repeated member "a"`, 4, 4) {
			t.Fatalf("no refusal at 4:4:%s", describe(result))
		}
		if got := detailAt(result, "repeated member"); !strings.HasPrefix(got, "first at 2:4;") {
			t.Errorf("detail %q, want it to name 2:4", got)
		}
	})

	t.Run("the instance holds the value the decoder kept", func(t *testing.T) {
		parsed, result := parseForProvenance(t, id, `{"Person": [{"id": "a", "id": "b"}]}`)
		if got := len(allIssues(result)); got != 1 {
			t.Fatalf("got %d issues, want 1:%s", got, describe(result))
		}
		rows := parsed["Person"]
		if len(rows) != 1 {
			t.Fatalf("got %d instances, want 1", len(rows))
		}
		// encoding/json keeps the last value of a repeated name.
		if got := rows[0].Properties["id"]; got != "b" {
			t.Errorf("id is %v, want \"b\"", got)
		}
	})

	t.Run("every repetition is reported", func(t *testing.T) {
		_, result := parseForProvenance(t, id, `{"Person": [{"a": 1, "a": 2, "b": 3, "b": 4, "a": 5}]}`)
		if got := len(allIssues(result)); got != 3 {
			t.Fatalf("got %d issues, want 3:%s", got, describe(result))
		}
	})

	// Past sixteen names an object's names are looked up through an index, so
	// a repeat is checked on both sides of that threshold and across it.
	wide := func(n int, extra ...string) string {
		var b strings.Builder
		b.WriteString("{")
		for i := range n {
			if i > 0 {
				b.WriteString(", ")
			}
			fmt.Fprintf(&b, `"m%d": %d`, i, i)
		}
		for _, e := range extra {
			fmt.Fprintf(&b, `, "%s": 0`, e)
		}
		b.WriteString("}")
		return b.String()
	}
	for _, c := range []struct {
		name string
		elem string
		want int
	}{
		{"a repeat of an early name in a wide object", wide(40, "m3"), 1},
		{"a repeat of a late name in a wide object", wide(40, "m39"), 1},
		{"a repeat that crosses the threshold", wide(16, "m0"), 1},
		{"a repeat of the name that crosses the threshold", wide(17, "m16"), 1},
		{"repeats before and after the threshold", wide(14, "m1", "x", "y", "z", "m2", "x"), 3},
		{"a wide object with no repeat", wide(40), 0},
		{"a wide object after a wide object", `{"a": ` + wide(40) + `, "b": ` + wide(40) + `}`, 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, result := parseForProvenance(t, id, `{"Person": [`+c.elem+`]}`)
			if got := len(allIssues(result)); got != c.want {
				t.Fatalf("got %d issues, want %d:%s", got, c.want, describe(result))
			}
		})
	}

	t.Run("the detail names the first occurrence past the index threshold", func(t *testing.T) {
		// Three occurrences of one name in an object wide enough to be
		// indexed: the third repeat names the first occurrence, not the second.
		doc := "{\"Person\": [\n " + wide(20, "m0", "m0") + "]}"
		_, result := parseForProvenance(t, id, doc)
		first := strings.Index(doc, `"m0"`)
		want := "first at 2:" + strconv.Itoa(first-strings.IndexByte(doc, '\n'))
		issues := allIssues(result)
		if len(issues) != 2 {
			t.Fatalf("got %d issues, want 2:%s", len(issues), describe(result))
		}
		// Every repeat names the FIRST occurrence, the second included.
		for _, issue := range issues {
			for _, d := range issue.Details() {
				if d.Key == diag.DetailKeyDetail && !strings.HasPrefix(d.Value, want+";") {
					t.Errorf("detail %q, want it to name %q", d.Value, want)
				}
			}
		}
	})

	// A repeat the scan reaches only by reading a string, an array or a value
	// whole.
	for _, c := range []struct{ name, elem string }{
		{"after a value holding an escaped quote", `{"a": "x\"", "b": 1, "a": 2}`},
		{"after a value holding a comma", `{"a": "x, y", "b": 1, "a": 2}`},
		{"after an array value", `{"a": [1, 2], "a": 3}`},
		{"after an array of objects", `{"a": [{"b": 1}], "a": 3}`},
		{"after a value holding a closing brace", `{"a": "}, {", "b": 1, "a": 2}`},
		{"after a value holding a closing bracket", `{"a": "[,]", "b": 1, "a": 2}`},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, result := parseForProvenance(t, id, `{"Person": [`+c.elem+`]}`)
			if got := len(allIssues(result)); got != 1 || !issueAt(result, `repeated member "a"`, 1, 1+strings.LastIndex(`{"Person": [`+c.elem, `"a"`)) {
				t.Fatalf("want one refusal of \"a\":%s", describe(result))
			}
		})
	}

	unique := []struct {
		name string
		elem string
	}{
		{"one name in two objects", `{"a": {"x": 1}, "b": {"x": 2}}`},
		{"one name in two array elements", `{"xs": [{"n": 1}, {"n": 2}]}`},
		{"a value that spells a member name", `{"xs": ["id", "id"], "id": "id"}`},
		{"a name holding an escaped quote", `{"a\"b": 1, "a": 2, "b": 3}`},
		{"a value holding an escaped quote and a colon", `{"id": "\"id\": ", "note": "x"}`},
		{"a name repeated after a nested object closes", `{"a": {"b": 1}, "b": 2}`},
		{"an array of strings repeating one", `{"xs": ["a", "b", "c", "b"]}`},
		// A string value is read whole: what it spells is not a member.
		{"a value spelling a repeated member", `{"a": "x, \"z\": 1, \"z\": 2", "b": 3}`},
	}
	for _, c := range unique {
		t.Run("accepts "+c.name, func(t *testing.T) {
			parsed, result := parseForProvenance(t, id, `{"Person": [`+c.elem+`]}`)
			if !result.OK() {
				t.Fatalf("refused:%s", describe(result))
			}
			if len(parsed["Person"]) != 1 {
				t.Fatalf("got %d instances, want 1", len(parsed["Person"]))
			}
		})
	}
}

// TestParseObject_AnEmptyArrayKeepsItsKey pins that a key whose value is an
// array is an entry of the result even when it yields no instance, so a caller
// ranging over the result sees every type the document names.
func TestParseObject_AnEmptyArrayKeepsItsKey(t *testing.T) {
	id := location.MustNewSourceID("test://data/empty.json")
	cases := []struct {
		name  string
		doc   string
		key   string
		ok    bool
		entry bool
	}{
		{"an empty array", `{"Person": []}`, "Person", true, true},
		{"an array of refused elements", `{"Person": [null, 42]}`, "Person", false, true},
		{"a value that is not an array", `{"Person": 5}`, "Person", false, false},
		{"an invalid type name", `{"person": []}`, "person", false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			parsed, result := parseForProvenance(t, id, c.doc)
			if result.OK() != c.ok {
				t.Fatalf("OK is %v, want %v:%s", result.OK(), c.ok, describe(result))
			}
			rows, entry := parsed[c.key]
			if entry != c.entry {
				t.Fatalf("entry present %v, want %v (%v)", entry, c.entry, parsed)
			}
			if len(rows) != 0 {
				t.Fatalf("got %d instances, want 0", len(rows))
			}
		})
	}
}

// TestParseObject_AnEmptyArrayOfAnUnknownTypeReachesTheValidator pins what the
// kept key gives a caller that validates every entry: a type name the schema
// does not declare is reported whatever its array holds.
func TestParseObject_AnEmptyArrayOfAnUnknownTypeReachesTheValidator(t *testing.T) {
	s, loadResult := schema.LoadString(t.Context(), `schema "s"
type Person {
	id String primary
}
`, "s.yammm")
	if !loadResult.OK() {
		t.Fatalf("load:%s", describe(loadResult))
	}
	parsed, result := parseForProvenance(t, location.MustNewSourceID("test://data/unknown.json"), `{"Person": [], "Nobody": []}`)
	if !result.OK() {
		t.Fatalf("parse refused:%s", describe(result))
	}
	v := instance.NewValidator(s)
	var codes []diag.Code
	for _, name := range []string{"Person", "Nobody"} {
		raws, ok := parsed[name]
		if !ok {
			t.Fatalf("no entry for %q", name)
		}
		_, vr := v.Validate(t.Context(), name, raws)
		for issue := range vr.Issues() {
			codes = append(codes, issue.Code())
		}
	}
	if len(codes) != 1 || codes[0] != diag.E_INSTANCE_TYPE_NOT_FOUND {
		t.Fatalf("validator reported %v, want one E_INSTANCE_TYPE_NOT_FOUND for Nobody", codes)
	}
}

// TestParseObject_AnUnreadableDocumentReportsOneFault pins that once the
// decoder cannot read on, parsing stops at the first fault: nothing after it
// is read, so no diagnostic names a read that never happened.
func TestParseObject_AnUnreadableDocumentReportsOneFault(t *testing.T) {
	id := location.MustNewSourceID("test://data/unreadable.json")
	cases := []struct {
		name     string
		doc      string
		messages []string
		kept     map[string]int
	}{
		{"truncated inside an element", `{"Person": [{"name":`, []string{"error reading array element"}, map[string]int{"Person": 0}},
		{"a syntax error inside an element", `{"A": [{"x": 1}, {bad}], "B": [{"id": "1"}]}`, []string{"error reading array element"}, map[string]int{"A": 1}},
		{"a missing comma between elements", `{"A": [{"x": 1} {"y": 2}], "B": [{"id": "1"}]}`, []string{"error reading array element"}, map[string]int{"A": 1}},
		{"a syntax error inside a value that is not an array", `{"A": {"x": [}, "B": [{"id": "1"}]}`, []string{"expected array", "error skipping value"}, map[string]int{}},
		{"a syntax error inside the value of an invalid type name", `{"a b": [{"x": 1}, {bad}], "B": [{"id": "1"}]}`, []string{"invalid type name", "error skipping value"}, map[string]int{}},
		{"a syntax error inside a repeated key's value", `{"A": [], "A": [{bad}], "B": [{"id": "1"}]}`, []string{"repeated type key", "error reading array element"}, map[string]int{"A": 0}},
		{"a missing comma after an array", `{"A": [{"x": 1}] "B": [{"id": "1"}]}`, []string{"error reading key"}, map[string]int{"A": 1}},
		{"a value that cannot open", `{"A": x, "B": [{"id": "1"}]}`, []string{"error reading array"}, map[string]int{}},
		{"an array closed by a brace", `{"A": [{"x": 1}}, "B": [{"id": "1"}]}`, []string{"error reading closing bracket"}, map[string]int{"A": 1}},
		{"a root closed by a bracket", `{"A": [{"x": 1}]]`, []string{"error reading closing brace"}, map[string]int{"A": 1}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			parsed, result := parseForProvenance(t, id, c.doc)
			issues := allIssues(result)
			if len(issues) != len(c.messages) {
				t.Fatalf("got %d issues, want %d:%s", len(issues), len(c.messages), describe(result))
			}
			for _, want := range c.messages {
				if !slices.ContainsFunc(issues, func(i diag.Issue) bool { return strings.Contains(i.Message(), want) }) {
					t.Errorf("no issue says %q:%s", want, describe(result))
				}
			}
			if _, ok := parsed["B"]; ok {
				t.Errorf("B was read after the decoder was lost")
			}
			for name, n := range c.kept {
				rows, ok := parsed[name]
				if !ok || len(rows) != n {
					t.Errorf("%s: entry %v with %d instances, want %d", name, ok, len(rows), n)
				}
			}
			if len(parsed) != len(c.kept) {
				t.Errorf("got entries %v, want %v", parsed, c.kept)
			}
		})
	}
}

// TestParseObject_AValueThatIsNotAnArrayIsSkippedWhole pins that the skip reads
// past every nested object and array, so the key after it is read from its own
// place.
func TestParseObject_AValueThatIsNotAnArrayIsSkippedWhole(t *testing.T) {
	id := location.MustNewSourceID("test://data/skip.json")
	for _, doc := range []string{
		`{"A": {"x": [1, [2, {"y": []}]]}, "B": [{"id": "1"}]}`,
		`{"A": {"x": {"y": {}}}, "B": [{"id": "1"}]}`,
	} {
		parsed, result := parseForProvenance(t, id, doc)
		if got := len(allIssues(result)); got != 1 || !issueAt(result, "expected array", 1, 7) {
			t.Fatalf("%s: want one refusal at 1:7:%s", doc, describe(result))
		}
		if len(parsed["B"]) != 1 {
			t.Fatalf("%s: B holds %d instances, want 1", doc, len(parsed["B"]))
		}
	}
}

// TestParseObject_ManyRepeatsOnOneLineCostLinearTime pins that locating a
// diagnostic does not rescan its line from the start. A minified document is
// one line, and each repeat is located twice; counting from the line start put
// 40,000 repeats at 14 s against 15 ms for the document without them.
func TestParseObject_ManyRepeatsOnOneLineCostLinearTime(t *testing.T) {
	const repeats = 100_000
	var b strings.Builder
	b.WriteString(`{"Person": [{"a": 0`)
	for range repeats {
		b.WriteString(`, "a": 0`)
	}
	b.WriteString(`}]}`)
	start := time.Now()
	_, result := parseForProvenance(t, location.MustNewSourceID("test://data/minified.json"), b.String())
	elapsed := time.Since(start)
	if got := len(allIssues(result)); got != repeats {
		t.Fatalf("got %d issues, want %d", got, repeats)
	}
	// Linear work here takes well under a second even under -race; the
	// quadratic scan takes minutes.
	if elapsed > 20*time.Second {
		t.Fatalf("parsing took %v", elapsed)
	}
}
