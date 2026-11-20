package jsondata

import (
	"context"
	"maps"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/internal/yammmtest/sourcetest"
)

// TestParse_AStringThatIsNotItsTextIsRefused pins the refusal of a string the
// decoder would read as U+FFFD: each is an E_DATA_PARSE at its opening quote,
// and the element that holds it is produced refused, beside the others, with
// each member whose name holds one removed and each whose value holds one
// holding nil.
func TestParse_AStringThatIsNotItsTextIsRefused(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, elem    string
		columns       []int
		message       string
		kept, refused []string
	}{
		{"a value with a byte that is not UTF-8", "{\"id\": \"a\xffb\"}", []int{9}, "string is not valid UTF-8", []string{"id"}, []string{"id"}},
		{"a value inside a list", "{\"id\": \"a\", \"tags\": [\"b\xfe\"]}", []int{23}, "string is not valid UTF-8", []string{"id", "tags"}, []string{"tags"}},
		{"a member name", "{\"id\": \"a\", \"p\xff\": 1}", []int{14}, "string is not valid UTF-8", []string{"id"}, nil},
		{"two names that read as one", "{\"p\xff\": 1, \"p\xfe\": 2}", []int{3, 12}, "string is not valid UTF-8", nil, nil},
		{"a surrogate encoded as UTF-8", "{\"id\": \"\xed\xa0\x80\"}", []int{9}, "string is not valid UTF-8", []string{"id"}, []string{"id"}},
		{"a lone high surrogate escape", `{"id": "a\ud800b"}`, []int{9}, "string holds a lone surrogate escape", []string{"id"}, []string{"id"}},
		{"a lone low surrogate escape", `{"id": "\udc00"}`, []int{9}, "string holds a lone surrogate escape", []string{"id"}, []string{"id"}},
		{"a high surrogate before another high", `{"id": "\ud800\ud800\udc00"}`, []int{9}, "string holds a lone surrogate escape", []string{"id"}, []string{"id"}},
		{"a high surrogate at the end", `{"id": "a\ud800"}`, []int{9}, "string holds a lone surrogate escape", []string{"id"}, []string{"id"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			// The element starts at column 2 of line 2, after one space.
			doc := "{\"T\": [{\"id\": \"before\"},\n " + c.elem + ", {\"id\": \"after\"}]}"
			parsed, result := Parse(context.Background(), sourcetest.ID("data.json"), []byte(doc))
			if got := len(allIssues(result)); got != len(c.columns) {
				t.Fatalf("got %d issues, want %d:%s", got, len(c.columns), describe(result))
			}
			for _, col := range c.columns {
				if !issueAt(result, c.message, 2, col) {
					t.Errorf("no %q at 2:%d:%s", c.message, col, describe(result))
				}
			}
			var paths []string
			var refusedAt []bool
			for _, e := range parsed["T"].Elements {
				paths = append(paths, e.Provenance.Path().String())
				refusedAt = append(refusedAt, e.Refused)
			}
			if want := []string{"$.T[0]", "$.T[1]", "$.T[2]"}; !slices.Equal(paths, want) {
				t.Fatalf("elements at %v, want %v", paths, want)
			}
			if want := []bool{false, true, false}; !slices.Equal(refusedAt, want) {
				t.Errorf("refused %v, want %v", refusedAt, want)
			}
			props := parsed["T"].Elements[1].Properties
			kept := slices.Sorted(maps.Keys(props))
			var refused []string
			for _, name := range kept {
				if props[name] == nil {
					refused = append(refused, name)
				}
			}
			if !slices.Equal(kept, c.kept) || !slices.Equal(refused, c.refused) {
				t.Errorf("members %v and refused %v, want %v and %v", kept, refused, c.kept, c.refused)
			}
		})
	}
}

// TestParse_ARefusedElementHoldsEachFaultedValueAtItsMember pins that a refused
// element keeps the name of each top-level member whose value holds a text
// fault at any depth with a nil value, drops a member whose name holds one,
// and keeps every other member as the decoder read it.
func TestParse_ARefusedElementHoldsEachFaultedValueAtItsMember(t *testing.T) {
	t.Parallel()
	doc := "{\"T\": [{\"id\": \"a\", \"m\": {\"_target_id\": \"x\xff\", \"w\": 1}, \"ok\": [1, {\"k\": \"v\"}], " +
		"\"lines\": [{\"sku\": \"s\"}, {\"sku\": \"t\", \"tags\": [\"u\", \"v\xfe\"]}], \"n\xff\": 2}]}"
	parsed, result := Parse(t.Context(), sourcetest.ID("data.json"), []byte(doc))
	if got := len(allIssues(result)); got != 3 || len(parsed["T"].Elements) != 1 || !parsed["T"].Elements[0].Refused {
		t.Fatalf("got %d issues and %v, want 3 and one refused element:%s", got, parsed["T"], describe(result))
	}
	want := map[string]any{
		"id":    "a",
		"m":     nil,
		"ok":    []any{int64(1), map[string]any{"k": "v"}},
		"lines": nil,
	}
	if got := parsed["T"].Elements[0].Properties; !reflect.DeepEqual(got, want) {
		t.Errorf("properties %#v\nwant %#v", got, want)
	}
}

// TestParse_ATextFaultEditsTheOccurrenceTheDecoderKeeps pins a text fault in a
// repeated member: both faults are reported, a faulted name is compared with
// none, and a member is edited only when its last occurrence, the one the
// decoder keeps, holds the fault.
func TestParse_ATextFaultEditsTheOccurrenceTheDecoderKeeps(t *testing.T) {
	t.Parallel()
	for name, c := range map[string]struct {
		elem    string
		repeats int
		want    map[string]any
	}{
		"the kept occurrence's value":         {"{\"id\": \"a\", \"id\": \"\xff\"}", 1, map[string]any{"id": nil}},
		"a dropped occurrence's value":        {"{\"id\": \"\xff\", \"id\": \"a\"}", 1, map[string]any{"id": "a"}},
		"one faulted name twice":              {"{\"id\": \"a\", \"p\xff\": \"x\", \"p\xff\": \"y\"}", 0, map[string]any{"id": "a"}},
		"a faulted name with a faulted value": {"{\"id\": \"a\", \"p\xff\": [\"\xfe\"]}", 0, map[string]any{"id": "a"}},
		"a faulted name, then its text":       {"{\"id\": \"a\", \"p\xff\": 1, \"p\\ufffd\": 2}", 0, map[string]any{"id": "a", "p\uFFFD": int64(2)}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			parsed, result := Parse(t.Context(), sourcetest.ID("data.json"), []byte("{\"T\": ["+c.elem+"]}"))
			repeats := 0
			for issue := range result.Issues() {
				if strings.HasPrefix(issue.Message(), "repeated member") {
					repeats++
				}
			}
			if repeats != c.repeats || !result.HasErrors() {
				t.Errorf("%d repeated members, want %d:%s", repeats, c.repeats, describe(result))
			}
			if len(parsed["T"].Elements) != 1 || !parsed["T"].Elements[0].Refused {
				t.Fatalf("elements %v, want one refused", parsed["T"])
			}
			if got := parsed["T"].Elements[0].Properties; !reflect.DeepEqual(got, c.want) {
				t.Errorf("properties %#v, want %#v", got, c.want)
			}
		})
	}
}

// TestParse_ATextFaultsDetailNamesWhatTheDecoderWouldRead pins each fault's
// detail: the byte or the escape, and that encoding/json would read U+FFFD.
func TestParse_ATextFaultsDetailNamesWhatTheDecoderWouldRead(t *testing.T) {
	t.Parallel()
	for doc, want := range map[string]string{
		"{\"T\": [{\"id\": \"a\xffb\"}]}": "byte 0xff is not valid UTF-8",
		`{"T": [{"id": "a\udc00"}]}`:      `the escape \udc00 is half of no surrogate pair`,
		// A string with both faults is named by its byte.
		"{\"T\": [{\"id\": \"\xff\\ud800\"}]}": "byte 0xff is not valid UTF-8",
	} {
		_, result := Parse(context.Background(), sourcetest.ID("data.json"), []byte(doc))
		got := detailAt(result, "string")
		if !strings.HasPrefix(got, want) || !strings.HasSuffix(got, "encoding/json would read it as U+FFFD") {
			t.Errorf("detail %q, want it to start %q and name U+FFFD", got, want)
		}
	}
}

// TestParse_ATypeNameThatIsNotItsTextIsRefused pins the refusal at a type name:
// one E_DATA_PARSE, no E_INVALID_TYPE_TAG beside it, its array skipped, and the
// next type read.
func TestParse_ATypeNameThatIsNotItsTextIsRefused(t *testing.T) {
	t.Parallel()
	for name, key := range map[string]string{
		"a byte that is not UTF-8": "\"T\xff\"",
		"a lone surrogate escape":  `"T\ud800"`,
		"a lone escape alone":      `"\udc00"`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc := "{" + key + ": [{\"id\": \"a\"}],\n \"U\": [{\"id\": \"b\"}]}"
			parsed, result := Parse(context.Background(), sourcetest.ID("data.json"), []byte(doc))
			issues := allIssues(result)
			if len(issues) != 1 || !issueAt(result, "string", 1, 2) {
				t.Fatalf("want one E_DATA_PARSE at 1:2:%s", describe(result))
			}
			if len(parsed) != 1 || len(parsed["U"].Elements) != 1 {
				t.Errorf("parsed %v, want U alone", parsed)
			}
		})
	}
}

// TestParse_AFaultedTypeNameBeforeAnUnreadableValueStops pins that a value the
// decoder cannot read after a faulted type name ends the parse there, as it does
// after a type name the tag rule refuses.
func TestParse_AFaultedTypeNameBeforeAnUnreadableValueStops(t *testing.T) {
	t.Parallel()
	_, result := Parse(t.Context(), sourcetest.ID("data.json"), []byte("{\"T\xff\": [}"))
	if got := len(allIssues(result)); got != 2 || !issueAt(result, "string is not valid UTF-8", 1, 2) ||
		detailAt(result, "error skipping value") == "" {
		t.Errorf("want the text fault and the skip's failure alone:%s", describe(result))
	}
}

// TestParse_AValidSurrogatePairIsRead pins that a pair of escapes, and the
// character U+FFFD written as itself, are text the document states.
func TestParse_AValidSurrogatePairIsRead(t *testing.T) {
	t.Parallel()
	doc := "{\"T\": [{\"id\": \"\\ud83d\\ude00\", \"r\": \"\uFFFD\", \"e\": \"\\uFFFD\"}]}"
	parsed, result := Parse(context.Background(), sourcetest.ID("data.json"), []byte(doc))
	if result.HasErrors() || len(parsed["T"].Elements) != 1 {
		t.Fatalf("parsed %d instances:%s", len(parsed["T"].Elements), describe(result))
	}
	props := parsed["T"].Elements[0].Properties
	if props["id"] != "\U0001F600" || props["r"] != "\uFFFD" || props["e"] != "\uFFFD" {
		t.Errorf("properties %#v, want the pair's character and U+FFFD twice", props)
	}
}

// TestParse_ARefusedElementCostsWhatItsCleanTwinCosts pins that refusing an
// element reads it once more, not once per level: a faulted element nested
// 4,000 deep allocates within twice what the same element without the fault
// does. It runs alone, since it reads the process's allocation total.
func TestParse_ARefusedElementCostsWhatItsCleanTwinCosts(t *testing.T) {
	nested := func(leaf string) []byte {
		const depth = 4000
		return []byte("{\"T\": [{\"id\": \"a\", \"v\": " + strings.Repeat("[", depth) + leaf +
			strings.Repeat("]", depth) + "}]}")
	}
	allocated := func(doc []byte) uint64 {
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		Parse(t.Context(), sourcetest.ID("data.json"), doc)
		runtime.ReadMemStats(&after)
		return after.TotalAlloc - before.TotalAlloc
	}
	clean, faulted := allocated(nested("\"x\"")), allocated(nested("\"\xff\""))
	if faulted > 2*clean {
		t.Errorf("a faulted element allocated %d bytes, its clean twin %d", faulted, clean)
	}
}
