package json

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/tidwall/jsonc"

	"github.com/simon-lentz/yammm/adapter/internal/typetag"
	"github.com/simon-lentz/yammm/location"
)

// prefixVerdict is what a fresh decoder makes of a prefix of a document when
// it performs ParseObject's reads over it.
type prefixVerdict int

const (
	prefixComplete prefixVerdict = iota // every read succeeds
	prefixEnded                         // a read runs out of input
	prefixFailed                        // a read meets a byte no continuation repairs
)

// readPrefix performs ParseObject's sequence of reads over b with a fresh
// decoder; it shares no placement code with the parser.
func readPrefix(b []byte) prefixVerdict {
	dec := json.NewDecoder(bytes.NewReader(b))
	verdict := func(err error) prefixVerdict {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return prefixEnded
		}
		return prefixFailed
	}
	// more stands in for Decoder.More, as encoding/json's own implementation
	// decides it: the next byte past white space is neither a closing delimiter
	// nor the end of b. Where encoding/json runs on its v2 implementation, the
	// default from Go 1.27, a read after More at the end of the input fails as
	// a syntax error and not as running out of input.
	more := func() (bool, bool) {
		rest := bytes.TrimLeft(b[dec.InputOffset():], " \t\r\n")
		if len(rest) == 0 {
			return false, false
		}
		return rest[0] != '}' && rest[0] != ']', true
	}
	nesting := func(tok json.Token) int {
		switch tok {
		case json.Delim('{'), json.Delim('['):
			return 1
		case json.Delim('}'), json.Delim(']'):
			return -1
		}
		return 0
	}

	tok, err := dec.Token()
	if err != nil {
		return verdict(err)
	}
	if tok != json.Delim('{') {
		return prefixComplete
	}
	for {
		next, ok := more()
		if !ok {
			return prefixEnded
		}
		if !next {
			break
		}
		key, err := dec.Token()
		if err != nil {
			return verdict(err)
		}
		if name, _ := key.(string); typetag.Validate(name) != nil {
			var skip json.RawMessage
			if err := dec.Decode(&skip); err != nil {
				return verdict(err)
			}
			continue
		}
		open, err := dec.Token()
		if err != nil {
			return verdict(err)
		}
		if open != json.Delim('[') {
			for depth := nesting(open); depth > 0; {
				tok, err := dec.Token()
				if err != nil {
					return verdict(err)
				}
				depth += nesting(tok)
			}
			continue
		}
		for {
			next, ok := more()
			if !ok {
				return prefixEnded
			}
			if !next {
				break
			}
			var obj map[string]any
			if err := dec.Decode(&obj); err != nil {
				if _, ok := errors.AsType[*json.UnmarshalTypeError](err); ok {
					continue
				}
				return verdict(err)
			}
		}
		if _, err := dec.Token(); err != nil {
			return verdict(err)
		}
	}
	if _, err := dec.Token(); err != nil {
		return verdict(err)
	}
	return prefixComplete
}

// decodePrefix judges b by decoding it whole as one value, as either
// implementation of encoding/json refuses a byte no continuation repairs.
func decodePrefix(b []byte) prefixVerdict {
	var v json.RawMessage
	err := json.NewDecoder(bytes.NewReader(b)).Decode(&v)
	switch {
	case err == nil:
		return prefixComplete
	case errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF):
		return prefixEnded
	}
	return prefixFailed
}

// prefixFault returns the last byte of the shortest prefix of jsonc's rewrite
// of data that judge fails, trying each length from from on, or len(data)
// when every prefix only ends. It reports false when the whole rewrite reads.
func prefixFault(t *testing.T, data []byte, from int, judge func([]byte) prefixVerdict) (int, bool) {
	t.Helper()
	trimmed := bytes.TrimPrefix(data, byteOrderMark)
	bom := len(data) - len(trimmed)
	buf := jsonc.ToJSON(trimmed)
	// A failed prefix stays failed however it is extended, so a scan may
	// start at any length that does not fail.
	if judge(buf[:from]) == prefixFailed {
		t.Fatalf("the scan's first prefix, %d bytes, already fails", from)
	}
	for n := from + 1; n <= len(buf); n++ {
		if judge(buf[:n]) == prefixFailed {
			return bom + n - 1, true
		}
	}
	if judge(buf) == prefixEnded {
		return len(data), true
	}
	return 0, false
}

// rewriteIsExact reports whether judging prefixes of jsonc's rewrite of data
// surely judges the document itself. It refuses a rewrite holding a "/", a
// blanked comma, or a comma after "[" or "{", each even inside a string: past
// an open comment, a stray "/" or a comma jsonc may blank, what a prefix can
// still become depends on bytes after it, which the rewrite has already read.
// A root whose first byte begins any value but an object is left out too: the
// parse refuses it by its shape unless its first token is malformed, a case
// the exact-byte table's root rows pin.
func rewriteIsExact(data []byte) bool {
	buf := jsonc.ToJSON(bytes.TrimPrefix(data, byteOrderMark))
	src := bytes.TrimPrefix(data, byteOrderMark)
	if len(buf) != len(src) || bytes.IndexByte(buf, '/') >= 0 {
		return false
	}
	if root := bytes.TrimLeft(buf, " \t\r\n"); len(root) > 0 && bytes.IndexByte([]byte(`["-0123456789tfn`), root[0]) >= 0 {
		return false
	}
	var last byte
	for i, c := range buf {
		if src[i] == ',' && c != ',' || c == ',' && (last == '[' || last == '{') {
			return false
		}
		if c != ' ' && c != '\t' && c != '\r' && c != '\n' {
			last = c
		}
	}
	return true
}

// assertPlacementAgrees checks one document: the parse reports a decoder fault
// exactly where the prefix scan finds one, and none after it, or no decoder
// fault at all when the scan finds none.
func assertPlacementAgrees(t *testing.T, doc []byte, from int, judge func([]byte) prefixVerdict) {
	t.Helper()
	want, faulty := prefixFault(t, doc, from, judge)
	_, result := New().ParseObject(t.Context(), location.MustNewSourceID("test://data/scan.json"), doc)
	var faults []int
	for issue := range result.Issues() {
		if isDecoderFault(issue) {
			faults = append(faults, issue.Span().Start.Byte)
		}
		if faulty && issue.HasSpan() && issue.Span().Start.Byte > want {
			t.Errorf("%q: %q at byte %d, past the fault at %d", doc, issue.Message(), issue.Span().Start.Byte, want)
		}
	}
	switch {
	case !faulty && len(faults) > 0:
		t.Errorf("%q: the scan finds no fault, the parse reports one at %v:%s", doc, faults, describe(result))
	case faulty && !slices.Contains(faults, want):
		t.Errorf("%q: the scan finds the fault at byte %d, the parse reports %v:%s", doc, want, faults, describe(result))
	}
}

// TestParseObject_FaultPlacementAgreesWithAPrefixScan holds the placement to a
// second implementation of the same contract. The parser reads the document's
// own bytes by a grammar of its own; the scan here decodes every prefix of
// jsonc's rewrite from its start with encoding/json, one after another. The
// documents are a set of valid ones, each single-byte deletion of them, and
// each insertion and replacement of a byte from a fixed alphabet, less those
// whose rewrite is not exact, which TestParseObject_PlacesAFaultAtTheOffendingByte
// covers by class.
func TestParseObject_FaultPlacementAgreesWithAPrefixScan(t *testing.T) {
	t.Parallel()
	seeds := []struct {
		doc   string
		exact int // the documents rewriteIsExact keeps, so the filter cannot empty the test
	}{
		{`{"A": [{"x": 1}, {"y": [2, {"z": "s"}]}], "B": []}`, 1348},
		{"{\n  \"A\": [\n    {\"k\": \"v\"},\n    {}\n  ]\n}\n", 1060},
		{`{"a b": [{"x": 1}], "A": {"x": [1]}, "A": [null, 5]}`, 1408},
		{"\uFEFF// c\n{\"A\": [/* é */ {\"x\": true}]}", 866},
		{`{"A": [{"s": "\"}{\u00e9"}], "B": [{"n": -1.5e3}]}`, 1387},
		{"{\"A\":\t[{\"f\": false,\r\n\"n\": [0, -0.5e-3, 1E5], \"s\": \"\\b\\u00FF\\t\"}]}", 1784},
	}
	alphabet := []byte{',', ':', '{', '}', '[', ']', '"', '\\', 'x', '1', ' ', '\n', '/', 0x01, 0xff}
	for _, seed := range seeds {
		t.Run(seed.doc, func(t *testing.T) {
			t.Parallel()
			src := []byte(seed.doc)
			docs := [][]byte{src}
			for i := range len(src) + 1 {
				if i < len(src) {
					docs = append(docs, slices.Delete(slices.Clone(src), i, i+1))
				}
				for _, b := range alphabet {
					docs = append(docs, slices.Insert(slices.Clone(src), i, b))
					if i < len(src) && src[i] != b {
						replaced := slices.Clone(src)
						replaced[i] = b
						docs = append(docs, replaced)
					}
				}
			}
			exact := 0
			for _, doc := range docs {
				if rewriteIsExact(doc) {
					exact++
					assertPlacementAgrees(t, doc, 0, decodePrefix)
				}
			}
			if exact != seed.exact {
				t.Errorf("%d of %d documents have an exact rewrite, want %d", exact, len(docs), seed.exact)
			}
		})
	}
}

// TestParseObject_FaultPlacementAgreesAtTheDepthLimit holds the placement to
// the prefix scan where the decoder refuses nesting past its limit. The limit
// counts from a different point in different implementations of
// encoding/json, so the scan judges each prefix by ParseObject's own sequence
// of reads, which counts as the parse does. Each document ends after its run of
// brackets: one the decoder does not refuse ends too soon, and is placed at
// its end.
func TestParseObject_FaultPlacementAgreesAtTheDepthLimit(t *testing.T) {
	t.Parallel()
	for _, n := range []int{9997, 9998, 9999, 10000, 10001, 10002} {
		for _, shape := range []struct{ name, open string }{
			{"inside an element", `{"A": [{"d": `},
			{"inside a refused name's value", `{"a b": `},
			{"inside a value that is not an array", `{"A": {"d": `},
			{"inside a later element", `{"A": [{}, {"d": `},
			{"inside an element that is an array", `{"A": [`},
			{"under a name valid once its escape is read", `{"\u0041": [`},
		} {
			t.Run(fmt.Sprintf("%s nested %d deep", shape.name, n), func(t *testing.T) {
				t.Parallel()
				doc := shape.open + strings.Repeat("[", n)
				// Every prefix shorter than the run of brackets reads, so the scan
				// starts at bracket 9,970, short of every shape's limit; the lowest
				// is 9,998 brackets, inside an element under the v2 implementation.
				assertPlacementAgrees(t, []byte(doc), len(shape.open)+9970, readPrefix)
			})
		}
	}
}
