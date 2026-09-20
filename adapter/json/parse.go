package json

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"unicode/utf8"

	"github.com/tidwall/jsonc"

	"github.com/simon-lentz/yammm/adapter/internal/typetag"
	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/location/path"
)

// byteOrderMark is the UTF-8 encoding of U+FEFF. One leading mark is skipped,
// as the CSV adapter skips it; a second is a parse error.
var byteOrderMark = []byte("\uFEFF")

// ParseObject parses JSON data structured as one top-level key per type name,
// each holding an array of instances — {"Person": [...], "Company": [...]}.
// Returns a map of type name -> slice of RawInstance, with an entry for every
// key whose value is an array, an empty one included. Data can start with one
// UTF-8 byte order mark.
//
// Every instance carries a [location.Provenance] naming source, its path in
// the document ($.Person[0]) and the span of its opening brace, and every
// diagnostic carries a span in source. Positions count runes from the line
// start of the bytes passed in.
//
// ParseObject checks ctx once per top-level key, the unit it reads, and returns
// the types read so far beside a Fatal E_CONTEXT_CANCELLED.
func (a *Adapter) ParseObject(ctx context.Context, source location.SourceID, data []byte) (map[string][]instance.RawInstance, diag.Result) {
	collector := diag.NewCollectorUnlimited()
	result := make(map[string][]instance.RawInstance)

	// jsonc tolerates comments and trailing commas, writing one space per
	// comment byte, so an offset into the rewritten buffer is an offset into
	// data once the mark's length is added back — but a column must be counted
	// over data, where a multibyte rune in a comment still occupies one column.
	//
	// The mapping is not a length guarantee: recovering an unterminated trailing
	// block comment appends a byte, so the buffer can be one longer than data.
	// spanAt range-checks every offset against data and yields no span rather
	// than a wrong one, which is why the extra byte costs a position and not a
	// lie.
	trimmed := bytes.TrimPrefix(data, byteOrderMark)
	pc := &parseContext{
		source:    source,
		src:       trimmed,
		positions: newPositionTable(data),
		bomLen:    len(data) - len(trimmed),
	}
	pc.buf = jsonc.ToJSON(trimmed)

	// Read token by token, so each array element decodes, and is located, on
	// its own.
	dec := json.NewDecoder(bytes.NewReader(pc.buf))
	dec.UseNumber()

	// Read opening brace
	tok, err := dec.Token()
	if err != nil {
		collector.Collect(pc.parseError(pc.spanAt(0), "invalid JSON", err.Error()))
		return nil, collector.Result()
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '{' {
		collector.Collect(pc.parseError(pc.spanAt(0), "expected object at root", "expected object"))
		return nil, collector.Result()
	}

	// Every return below that skips the closing brace does so because the
	// decoder cannot read on: a later token would be read from the middle of
	// the value that failed, and would name a fault the document does not have.
	seen := make(map[string]struct{})
	for dec.More() {
		// Checked per top-level key, the unit of work this loop reads, as the
		// CSV parser checks per record. Fatal because HasFatal is documented to
		// mean the run did not finish.
		if err := ctx.Err(); err != nil {
			collector.Collect(diag.NewIssue(diag.Fatal, diag.E_CONTEXT_CANCELLED,
				fmt.Sprintf("json parse cancelled after %d type keys", len(result))).Build())
			return result, collector.Result()
		}

		keySpan := pc.spanAt(pc.significantFrom(int(dec.InputOffset())))

		// Read type name
		keyTok, err := dec.Token()
		if err != nil {
			collector.Collect(pc.parseError(keySpan, "error reading key", err.Error()))
			return result, collector.Result()
		}
		// The decoder returns a member name as a string or fails; the arm holds
		// that contract rather than trusting it.
		typeName, ok := keyTok.(string)
		if !ok {
			collector.Collect(pc.parseError(keySpan, "expected string key", "expected string"))
			return result, collector.Result()
		}

		if err := typetag.Validate(typeName); err != nil {
			collector.Collect(typeTagError(keySpan, typeName, err))
			if !pc.skipMember(dec, keySpan, collector) {
				return result, collector.Result()
			}
			continue
		}

		// A repeated key is reported and read: decoding into a map keeps the
		// last value, which drops a whole batch silently, and the instances of
		// both arrays are what the document states.
		if _, repeated := seen[typeName]; repeated {
			collector.Collect(pc.parseError(keySpan, fmt.Sprintf("repeated type key %q", typeName),
				"a type name is a key of the root object once"))
		}
		seen[typeName] = struct{}{}

		instances, readable := parseArray(dec, pc, typeName, collector)
		if instances != nil {
			if earlier, ok := result[typeName]; ok {
				instances = append(earlier, instances...)
			}
			result[typeName] = instances
		}
		if !readable {
			return result, collector.Result()
		}
	}

	// Read closing brace
	closeSpan := pc.spanAt(pc.significantFrom(int(dec.InputOffset())))
	if _, err := dec.Token(); err != nil {
		collector.Collect(pc.parseError(closeSpan, "error reading closing brace", err.Error()))
		return result, collector.Result()
	}

	// Only white space and closed comments may follow the root object. The
	// decoder reads what follows as a new value, so asking it for a token would
	// refuse a trailing value and accept a trailing fragment it cannot read. The
	// tail is read in the original bytes: jsonc blanks a comma before any
	// closing bracket, the root object's own tail included.
	if rest := trailingFrom(pc.src, int(dec.InputOffset())); rest < len(pc.src) {
		collector.Collect(pc.parseError(pc.spanAt(rest),
			"unexpected content after root object", "found "+describeByte(pc.src[rest:])))
	}

	return result, collector.Result()
}

// skipMember reads past the value of a member that is refused, reporting a
// value the decoder cannot read. It returns false when the decoder cannot read
// on.
func (pc *parseContext) skipMember(dec *json.Decoder, keySpan location.Span, collector *diag.Collector) bool {
	var skip json.RawMessage
	if err := dec.Decode(&skip); err != nil {
		collector.Collect(pc.parseError(keySpan, "error skipping value", err.Error()))
		return false
	}
	return true
}

// parseContext holds what a span costs to build: the document's identity, the
// buffer the decoder reports offsets into, the length of the byte order mark
// trimmed off it, and the converter from an offset in the original bytes to a
// position.
type parseContext struct {
	source    location.SourceID
	src       []byte // data with its byte order mark trimmed, as jsonc read it
	buf       []byte
	bomLen    int
	positions *positionTable

	// frames is repeatedMembers' stack, kept between elements so a scan
	// reuses what the last one grew.
	frames []memberFrame
}

// spanAt returns the point span at an offset into the decoder's buffer.
func (pc *parseContext) spanAt(bufOffset int) location.Span {
	byteOffset := bufOffset + pc.bomLen
	pos := pc.positions.positionAt(byteOffset)
	if !pos.IsKnown() {
		return location.Span{}
	}
	return location.PointWithByte(pc.source, pos.Line, pos.Column, byteOffset)
}

// significantFrom returns the offset of the next byte that is neither a
// separator nor white space.
//
// [json.Decoder.More] leaves the offset on the comma for every element after
// the first, and the offset after a member's key sits on its colon, so a span
// taken at either would start on the punctuation and not the value. jsonc has
// already rewritten a closed comment as white space, so this skips one too; an
// unterminated trailing comment it writes back verbatim halts the scan, which
// is the document's last byte either way.
func (pc *parseContext) significantFrom(bufOffset int) int {
	for bufOffset < len(pc.buf) {
		switch pc.buf[bufOffset] {
		case ',', ':', ' ', '\t', '\r', '\n':
			bufOffset++
		default:
			return bufOffset
		}
	}
	return bufOffset
}

// trailingFrom returns the offset of the first byte at or after off in src that
// is neither JSON white space nor inside a closed comment, or len(src). A line
// comment runs to a line feed and a block comment to its "*/", as jsonc reads
// them; an unterminated block comment is content, and starts at its "/*".
func trailingFrom(src []byte, off int) int {
	for off < len(src) {
		switch rest := src[off:]; {
		case rest[0] == ' ' || rest[0] == '\t' || rest[0] == '\r' || rest[0] == '\n':
			off++
		case bytes.HasPrefix(rest, []byte("//")):
			lf := bytes.IndexByte(rest, '\n')
			if lf < 0 {
				return len(src)
			}
			off += lf
		case bytes.HasPrefix(rest, []byte("/*")):
			end := bytes.Index(rest[2:], []byte("*/"))
			if end < 0 {
				return off
			}
			off += 2 + end + 2
		default:
			return off
		}
	}
	return off
}

// describeByte names the character that b starts with, or its first byte when
// that is not valid UTF-8.
func describeByte(b []byte) string {
	r, size := utf8.DecodeRune(b)
	if r == utf8.RuneError && size <= 1 {
		return fmt.Sprintf("byte 0x%02x", b[0])
	}
	return fmt.Sprintf("%q", r)
}

// parseArray reads the array of instances under typeName, collecting every
// fault it can read past. It returns the instances read, non-nil when the value
// opened as an array, and false when the decoder cannot read on.
//
// An instance's path indexes the array as the document writes it, so an element
// that failed to decode still consumes its index and the path agrees with the
// span beside it.
func parseArray(dec *json.Decoder, pc *parseContext, typeName string, collector *diag.Collector) ([]instance.RawInstance, bool) {
	// Read opening bracket
	arraySpan := pc.spanAt(pc.significantFrom(int(dec.InputOffset())))
	tok, err := dec.Token()
	if err != nil {
		collector.Collect(pc.parseError(arraySpan, "error reading array", err.Error()))
		return nil, false
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '[' {
		collector.Collect(pc.parseError(arraySpan, "expected array", "expected array"))
		// Skip the remainder of the value to keep decoder synchronized
		if err := skipValue(dec, tok); err != nil {
			collector.Collect(pc.parseError(arraySpan, "error skipping value", err.Error()))
			return nil, false
		}
		return nil, true
	}

	result := []instance.RawInstance{}

	for index := 0; dec.More(); index++ {
		elemStart := pc.significantFrom(int(dec.InputOffset()))
		elemSpan := pc.spanAt(elemStart)

		var obj map[string]any
		if err := dec.Decode(&obj); err != nil {
			collector.Collect(pc.parseError(elemSpan, "error reading array element", err.Error()))

			// Decode reads the whole value before it converts it, so only a
			// conversion failure leaves the decoder past the element.
			if typeErr, _ := errors.AsType[*json.UnmarshalTypeError](err); typeErr == nil {
				return result, false
			}
			continue
		}

		// Reject null values - json.Decode into map yields nil without error
		if obj == nil {
			collector.Collect(pc.parseError(elemSpan, "expected object", "got null"))
			continue
		}

		// A repeated member is reported and the instance is kept: the object
		// holds the value the decoder kept, which is the one every JSON reader
		// keeps, and a parser that dropped it would lose data the document
		// states over a fault the diagnostic already names.
		for _, r := range pc.repeatedMembers(elemStart, int(dec.InputOffset())) {
			first := pc.spanAt(r.first).Start
			collector.Collect(pc.parseError(pc.spanAt(r.at), fmt.Sprintf("repeated member %q", r.name),
				fmt.Sprintf("first at %d:%d; a member name is used once per object", first.Line, first.Column)))
		}

		// Normalize json.Number to numeric types
		normalizeNumbers(obj)

		result = append(result, instance.RawInstance{
			Properties: obj,
			Provenance: location.NewProvenance(
				pc.source.String(),
				path.Root().Key(typeName).Index(index),
				elemSpan,
			),
		})
	}

	// Read closing bracket
	bracketSpan := pc.spanAt(pc.significantFrom(int(dec.InputOffset())))
	if _, err := dec.Token(); err != nil {
		collector.Collect(pc.parseError(bracketSpan, "error reading closing bracket", err.Error()))
		return result, false
	}

	return result, true
}

// repeatedMember is a member name that repeats an earlier name of its object:
// the buffer offsets of the repeat and of the first occurrence.
type repeatedMember struct {
	name      string
	at, first int
}

// memberFrame is one open object or array of the value repeatedMembers scans.
// An object's names are compared in place while it has few, and through an
// index once it has more, so a small object allocates nothing and a large one
// is not scanned quadratically.
type memberFrame struct {
	object    bool
	expectKey bool
	names     []memberName
	index     map[string]int
}

// memberName is a member name as the decoder reads it, and its buffer offset.
type memberName struct {
	text []byte
	at   int
}

// indexedAbove is the name count past which a frame looks names up in a map.
const indexedAbove = 16

// repeatedMembers returns every member name in buf[start:end] that repeats an
// earlier name of the same object, at any depth. Decoding into a map keeps the
// last value of a repeated name, so the repeat is found on the bytes the value
// was decoded from, which the decoder has already checked are one JSON value.
// Names compare as the decoder decodes them, escapes resolved.
func (pc *parseContext) repeatedMembers(start, end int) []repeatedMember {
	var repeats []repeatedMember
	depth := 0
	for i := start; i < end; i++ {
		switch pc.buf[i] {
		case '{', '[':
			if depth == len(pc.frames) {
				pc.frames = append(pc.frames, memberFrame{})
			}
			f := &pc.frames[depth]
			f.object, f.expectKey = pc.buf[i] == '{', pc.buf[i] == '{'
			f.names = f.names[:0]
			clear(f.index)
			depth++
		case '}', ']':
			depth--
		case ',':
			if f := &pc.frames[depth-1]; f.object {
				f.expectKey = true
			}
		case '"':
			closing := stringEnd(pc.buf, i)
			if f := &pc.frames[depth-1]; f.object && f.expectKey {
				f.expectKey = false
				name := decodeName(pc.buf[i : closing+1])
				if first, ok := f.lookup(name); ok {
					repeats = append(repeats, repeatedMember{name: string(name), at: i, first: first})
				} else {
					f.add(name, i)
				}
			}
			i = closing
		}
	}
	return repeats
}

func (f *memberFrame) lookup(name []byte) (int, bool) {
	if len(f.names) > indexedAbove {
		at, ok := f.index[string(name)]
		return at, ok
	}
	for _, n := range f.names {
		if bytes.Equal(n.text, name) {
			return n.at, true
		}
	}
	return 0, false
}

func (f *memberFrame) add(name []byte, at int) {
	f.names = append(f.names, memberName{text: name, at: at})
	switch {
	case len(f.names) == indexedAbove+1:
		if f.index == nil {
			f.index = make(map[string]int, 2*indexedAbove)
		}
		for _, n := range f.names {
			f.index[string(n.text)] = n.at
		}
	case len(f.names) > indexedAbove+1:
		f.index[string(name)] = at
	}
}

// stringEnd returns the offset of the quote closing the string that opens at
// buf[open].
func stringEnd(buf []byte, open int) int {
	for i := open + 1; i < len(buf); i++ {
		switch buf[i] {
		case '\\':
			i++
		case '"':
			return i
		}
	}
	return len(buf) - 1
}

// decodeName returns the text of a quoted member name as the decoder reads it.
// A name holding no escape and only valid UTF-8 is the buffer's own bytes; the
// decoder replaces each byte of an invalid sequence with U+FFFD, so such a name
// is decoded as the decoder decodes it.
func decodeName(quoted []byte) []byte {
	if bytes.IndexByte(quoted, '\\') < 0 && utf8.Valid(quoted) {
		return quoted[1 : len(quoted)-1]
	}
	var name string
	if err := json.Unmarshal(quoted, &name); err != nil {
		return quoted
	}
	return []byte(name)
}

// parseError creates an E_ADAPTER_PARSE issue at span.
// msg is the human-readable message; detail is the machine-oriented parse detail.
func (pc *parseContext) parseError(span location.Span, msg, detail string) diag.Issue {
	return diag.NewIssue(diag.Error, diag.E_ADAPTER_PARSE, msg).
		WithSpan(span).
		WithDetail(diag.DetailKeyFormat, "json").
		WithDetail(diag.DetailKeyDetail, detail).
		Build()
}

// typeTagError creates an E_INVALID_TYPE_TAG issue for type name validation errors.
func typeTagError(span location.Span, typeName string, err error) diag.Issue {
	msg := fmt.Sprintf("invalid type name %q: %s", typeName, err.Error())
	return diag.NewIssue(diag.Error, diag.E_INVALID_TYPE_TAG, msg).
		WithSpan(span).
		WithDetail(diag.DetailKeyGot, typeName).
		WithDetail(diag.DetailKeyDetail, err.Error()).
		Build()
}

// skipValue consumes the remainder of a JSON value from the decoder after
// reading its first token, so the decoder stays synchronized when an unexpected
// value type is encountered. It returns the error that stopped the read.
func skipValue(dec *json.Decoder, firstTok json.Token) error {
	// A primitive (string, number, bool, null) is consumed whole by the Token
	// call that returned it, and so is a closing delimiter.
	if delim, ok := firstTok.(json.Delim); !ok || (delim != '{' && delim != '[') {
		return nil
	}
	for depth := 1; depth > 0; {
		tok, err := dec.Token()
		if err != nil {
			return fmt.Errorf("reading the value's remainder: %w", err)
		}
		if delim, ok := tok.(json.Delim); ok {
			switch delim {
			case '{', '[':
				depth++
			case '}', ']':
				depth--
			}
		}
	}
	return nil
}

// normalizeNumbers rewrites every json.Number in m through the module's
// canonical number rule.
func normalizeNumbers(m map[string]any) {
	for k, v := range m {
		m[k] = normalizeValue(v)
	}
}

// normalizeValue applies [immutable.NormalizeNumber] to each number and
// recurses into nested structures.
//
// The walk is not depth-capped because the decoder above it is:
// encoding/json refuses a document nested past 10,000 levels, and this runs
// only on what that decoder accepted. [immutable.NormalizeValue] would cap it
// at 64, which leaves a number below 10,000 levels and above 64 a json.Number
// with no diagnostic saying so.
func normalizeValue(v any) any {
	switch val := v.(type) {
	case json.Number:
		return immutable.NormalizeNumber(val)

	case map[string]any:
		normalizeNumbers(val)
		return val

	case []any:
		for i, elem := range val {
			val[i] = normalizeValue(elem)
		}
		return val

	default:
		return v
	}
}
