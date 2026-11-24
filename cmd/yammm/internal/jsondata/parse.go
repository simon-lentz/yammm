package jsondata

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"unicode/utf8"

	"github.com/tidwall/jsonc"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/instance/path"
	"github.com/simon-lentz/yammm/internal/jsonstring"
	"github.com/simon-lentz/yammm/internal/value"
	"github.com/simon-lentz/yammm/location"
)

// byteOrderMark is the UTF-8 encoding of U+FEFF. One leading mark is skipped; a
// second is a parse error.
var byteOrderMark = []byte("\uFEFF")

// Element is one object of a type's instance array. Refused reports that it
// holds a string the Text section of the package doc refuses: it is an instance
// the document states and the parser refused, so a caller validates none of it.
// A caller reads its key from its Properties, edited as that section states, so
// a key component that holds such a string reads as no key.
type Element struct {
	instance.RawInstance
	Refused bool
}

// Batch is the elements stated under one type name, and Source, the first
// document that holds the name's array; a caller that merges documents' batches
// keeps the first Source. A caller anchors a diagnostic about the type name,
// which names no element, at Source.
type Batch struct {
	Source   location.SourceID
	Elements []Element
}

// Parse reads data as one top-level key per type name, each holding an array
// of instances, and returns the batches by type name. Every valid type name
// whose value is an array has an entry, an empty array included, unless a fault
// or a cancellation stops the parse first. The package doc states the
// diagnostics, the positions they carry, and what a cancellation returns.
func Parse(ctx context.Context, source location.SourceID, data []byte) (map[string]Batch, diag.Result) {
	return parse(ctx, source, data, newPositionTable(data))
}

// parse is [Parse] locating every diagnostic through positions, a table over
// data, so a test reads what the positions cost from the table afterwards.
func parse(ctx context.Context, source location.SourceID, data []byte, positions *positionTable) (map[string]Batch, diag.Result) {
	collector := diag.NewCollector(diag.NoLimit)
	result := make(map[string]Batch)

	// jsonc tolerates comments and trailing commas, writing one byte per input
	// byte, so an offset into the rewritten buffer is an offset into data once
	// the mark's length is added back — but a column must be counted over data,
	// where a multibyte rune in a comment still occupies one column.
	trimmed := bytes.TrimPrefix(data, byteOrderMark)
	pc := &parseContext{
		source:    source,
		src:       trimmed,
		positions: positions,
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
		collector.Collect(pc.parseError(pc.faultSpan(), "invalid JSON", err.Error()))
		return nil, collector.Result()
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '{' {
		collector.Collect(pc.parseError(pc.spanAt(pc.readStart(0)), "expected object at root", "expected object"))
		return nil, collector.Result()
	}

	// Every return below that skips the closing brace does so because the
	// decoder cannot read on: a later token would be read from the middle of
	// the value that failed, and would name a fault the document does not have.
	seen := make(map[string]struct{})
	keys := 0
	for dec.More() {
		// Checked per top-level key, the unit of work this loop reads.
		if err := ctx.Err(); err != nil {
			collector.Collect(diag.NewIssue(diag.Fatal, diag.E_CONTEXT_CANCELLED,
				fmt.Sprintf("json parse cancelled after %d top-level keys", keys)).Build())
			return result, collector.Result()
		}

		keyAt := pc.readStart(int(dec.InputOffset()))
		keySpan := pc.spanAt(keyAt)

		// Read type name
		keyTok, err := dec.Token()
		if err != nil {
			collector.Collect(pc.parseError(pc.faultSpan(), "error reading key", err.Error()))
			return result, collector.Result()
		}
		keys++
		// The decoder returns a member name as a string or fails; the arm holds
		// that contract rather than trusting it.
		typeName, ok := keyTok.(string)
		if !ok {
			collector.Collect(pc.parseError(keySpan, "expected string key", "expected string"))
			return result, collector.Result()
		}

		if fault, ok := pc.textFault(keyAt, stringEnd(pc.buf, keyAt)); ok {
			collector.Collect(fault)
			if !pc.skipMember(dec, collector) {
				return result, collector.Result()
			}
			continue
		}
		if fault := typeTagFault(typeName); fault != "" {
			collector.Collect(typeTagError(keySpan, typeName, fault))
			if !pc.skipMember(dec, collector) {
				return result, collector.Result()
			}
			continue
		}

		// A repeated key is reported and read: decoding into a map keeps the
		// last value, which drops a whole batch silently, and the instances of
		// both arrays are what the document states.
		if _, repeated := seen[typeName]; repeated {
			collector.Collect(pc.parseError(keySpan, "repeated type key "+diag.QuoteValue(typeName),
				"a type name is a key of the root object once"))
		}
		seen[typeName] = struct{}{}

		instances, readable := parseArray(dec, pc, typeName, collector)
		if instances != nil {
			if earlier, ok := result[typeName]; ok {
				instances = append(earlier.Elements, instances...)
			}
			result[typeName] = Batch{Source: source, Elements: instances}
		}
		if !readable {
			return result, collector.Result()
		}
	}

	// Read closing brace
	if _, err := dec.Token(); err != nil {
		collector.Collect(pc.parseError(pc.faultSpan(), "error reading closing brace", err.Error()))
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
func (pc *parseContext) skipMember(dec *json.Decoder, collector *diag.Collector) bool {
	var skip json.RawMessage
	if err := dec.Decode(&skip); err != nil {
		collector.Collect(pc.parseError(pc.faultSpan(), "error skipping value", err.Error()))
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

	// frames is scanElement's stack, kept between elements so a scan
	// reuses what the last one grew.
	frames []nameFrame
}

// spanAt returns the point span at an offset into the decoder's buffer.
func (pc *parseContext) spanAt(bufOffset int) location.Span {
	byteOffset := bufOffset + pc.bomLen
	pos := pc.positions.positionAt(byteOffset)
	if !pos.IsValid() {
		return location.Span{}
	}
	return location.Span{Source: pc.source, Start: pos, End: pos}
}

// readStart returns the offset of the first byte a successful read from
// bufOffset takes, past white space and the one comma or colon the decoder
// leaves its offset before; jsonc has already written every comment but an
// unclosed block comment as blank bytes.
func (pc *parseContext) readStart(bufOffset int) int {
	bufOffset = pc.skipSpace(bufOffset)
	if bufOffset < len(pc.buf) && (pc.buf[bufOffset] == ',' || pc.buf[bufOffset] == ':') {
		bufOffset = pc.skipSpace(bufOffset + 1)
	}
	return bufOffset
}

func (pc *parseContext) skipSpace(bufOffset int) int {
	for bufOffset < len(pc.buf) {
		switch pc.buf[bufOffset] {
		case ' ', '\t', '\r', '\n':
			bufOffset++
		default:
			return bufOffset
		}
	}
	return bufOffset
}

// faultSpan returns the span of the byte a failed decoder read is placed at:
// faultOffset's byte, or the end of the input when the input only ends early.
func (pc *parseContext) faultSpan() location.Span {
	if at, ok := faultOffset(pc.src); ok {
		return pc.spanAt(at)
	}
	return pc.spanAt(len(pc.src))
}

// trailingFrom returns the offset of the first byte at or after off in src that
// is neither JSON white space nor inside a closed comment, or len(src). A line
// comment runs to a carriage return or a line feed and a block comment to its
// "*/", as jsonc reads them; an unterminated block comment is content, and starts at its "/*".
func trailingFrom(src []byte, off int) int {
	for off < len(src) {
		switch rest := src[off:]; {
		case rest[0] == ' ' || rest[0] == '\t' || rest[0] == '\r' || rest[0] == '\n':
			off++
		case bytes.HasPrefix(rest, []byte("//")):
			eol := bytes.IndexAny(rest, "\r\n")
			if eol < 0 {
				return len(src)
			}
			off += eol
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
// fault it can read past. It returns the elements read, non-nil when the value
// opened as an array, and false when the decoder cannot read on.
//
// An instance's path indexes the array as the document writes it, so an element
// that failed to decode still consumes its index and the path agrees with the
// span beside it.
func parseArray(dec *json.Decoder, pc *parseContext, typeName string, collector *diag.Collector) ([]Element, bool) {
	// Read opening bracket
	valueStart := int(dec.InputOffset())
	tok, err := dec.Token()
	if err != nil {
		collector.Collect(pc.parseError(pc.faultSpan(), "error reading array", err.Error()))
		return nil, false
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '[' {
		collector.Collect(pc.parseError(pc.spanAt(pc.readStart(valueStart)), "expected array", "expected array"))
		// Skip the remainder of the value to keep decoder synchronized
		if err := skipValue(dec, tok); err != nil {
			collector.Collect(pc.parseError(pc.faultSpan(), "error skipping value", err.Error()))
			return nil, false
		}
		return nil, true
	}

	result := []Element{}

	for index := 0; dec.More(); index++ {
		elemStart := pc.readStart(int(dec.InputOffset()))
		elemSpan := pc.spanAt(elemStart)

		var obj map[string]any
		if err := dec.Decode(&obj); err != nil {
			// Decode reads the whole value before it converts it, so only a
			// conversion failure leaves the decoder past the element, whose
			// start the diagnostic names.
			if typeErr, _ := errors.AsType[*json.UnmarshalTypeError](err); typeErr == nil {
				collector.Collect(pc.parseError(pc.faultSpan(), "error reading array element", err.Error()))
				return result, false
			}
			collector.Collect(pc.parseError(elemSpan, "error reading array element", err.Error()))
			continue
		}

		// Reject null values - json.Decode into map yields nil without error
		if obj == nil {
			collector.Collect(pc.parseError(elemSpan, "expected object", "got null"))
			continue
		}

		// A repeated member is reported and the instance is kept: the object
		// holds the value encoding/json keeps, the last, and a parser that
		// dropped it would lose data the document states over a fault the
		// diagnostic already names.
		repeats, textFaults := pc.scanElement(elemStart, int(dec.InputOffset()))
		for _, r := range repeats {
			first := pc.spanAt(r.first).Start
			collector.Collect(pc.parseError(pc.spanAt(r.at), "repeated member "+diag.QuoteValue(r.name),
				fmt.Sprintf("first at %d:%d; a member name is used once per object", first.Line, first.Column)))
		}
		// A string that is not the text it reads as is reported and the element
		// is refused: the decoder read it as U+FFFD, a value the document does
		// not state. The element stays a stated instance, whose key a referrer
		// or a repeat still finds.
		for _, fault := range textFaults {
			collector.Collect(fault)
		}
		// Normalize json.Number to numeric types
		normalizeNumbers(obj)

		refused := len(textFaults) > 0
		if refused {
			for name, inName := range pc.faultedMembers(elemStart, int(dec.InputOffset())) {
				if inName {
					delete(obj, name)
				} else {
					obj[name] = nil
				}
			}
		}

		result = append(result, Element{
			RawInstance: instance.RawInstance{
				Properties: obj,
				Provenance: instance.NewProvenance(
					pc.source.String(),
					path.Root.Key(typeName).Index(index),
					elemSpan,
				),
			},
			Refused: refused,
		})
	}

	// Read closing bracket
	if _, err := dec.Token(); err != nil {
		collector.Collect(pc.parseError(pc.faultSpan(), "error reading closing bracket", err.Error()))
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

// nameFrame is one open object or array of the value scanElement scans.
// An object's names are compared in place while it has few, and through an
// index once it has more, so a small object makes no map and a large one is
// not scanned quadratically.
type nameFrame struct {
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

// scanElement reads the bytes of buf[start:end], which the decoder has already
// read as one JSON value, at every depth. It returns each member name that
// repeats an earlier name of the same object, since decoding into a map keeps
// the last value of a repeated name, and the [parseContext.textFault] of each
// string that has one. Names compare as the decoder decodes them, escapes
// resolved; a name with a text fault is compared with none.
func (pc *parseContext) scanElement(start, end int) (repeats []repeatedMember, textFaults []diag.Issue) {
	depth := 0
	for i := start; i < end; i++ {
		switch pc.buf[i] {
		case '{', '[':
			if depth == len(pc.frames) {
				pc.frames = append(pc.frames, nameFrame{})
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
			fault, faulted := pc.textFault(i, closing)
			if faulted {
				textFaults = append(textFaults, fault)
			}
			if f := &pc.frames[depth-1]; f.object && f.expectKey {
				f.expectKey = false
				if !faulted {
					pc.compareName(f, i, closing, &repeats)
				}
			}
			i = closing
		}
	}
	return repeats, textFaults
}

// faultedMembers maps the name, as the decoder keys the object, of each member
// of the element at buf[start:end] whose last occurrence, the one the decoder
// keeps, holds a [parseContext.textFault] to whether the fault is in its name;
// otherwise it is at some depth of its value. It reads each byte once, and
// only an element with a fault is read again.
func (pc *parseContext) faultedMembers(start, end int) map[string]bool {
	// fault is 0 for none, 1 in the value, 2 in the name.
	fault := make(map[string]int)
	name := ""
	depth := 0
	expectKey := false
	for i := start; i < end; i++ {
		switch pc.buf[i] {
		case '{', '[':
			depth++
			expectKey = depth == 1
		case '}', ']':
			depth--
		case ',':
			expectKey = depth == 1
		case '"':
			closing := stringEnd(pc.buf, i)
			_, faulted := pc.textFault(i, closing)
			switch {
			case expectKey:
				expectKey = false
				name = keyedName(pc.buf[i : closing+1])
				fault[name] = 0
				if faulted {
					fault[name] = 2
				}
			case faulted && fault[name] == 0:
				fault[name] = 1
			}
			i = closing
		}
	}
	inName := make(map[string]bool)
	for n, f := range fault {
		if f > 0 {
			inName[n] = f == 2
		}
	}
	return inName
}

// keyedName returns the quoted member name as the decoder keys a map with it,
// U+FFFD in place of each sequence a text fault names.
func keyedName(quoted []byte) string {
	var name string
	if err := json.Unmarshal(quoted, &name); err != nil {
		return string(quoted)
	}
	return name
}

// compareName records the name quoted at buf[open:closing+1] in f, or as a
// repeat when f already holds it.
func (pc *parseContext) compareName(f *nameFrame, open, closing int, repeats *[]repeatedMember) {
	name := decodeName(pc.buf[open : closing+1])
	if first, ok := f.lookup(name); ok {
		*repeats = append(*repeats, repeatedMember{name: string(name), at: open, first: first})
		return
	}
	f.add(name, open)
}

func (f *nameFrame) lookup(name []byte) (int, bool) {
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

func (f *nameFrame) add(name []byte, at int) {
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

// decodeName returns the text of a quoted member name that has no
// [parseContext.textFault] as the decoder reads it: a name holding no escape is
// the buffer's own bytes.
func decodeName(quoted []byte) []byte {
	if bytes.IndexByte(quoted, '\\') < 0 {
		return quoted[1 : len(quoted)-1]
	}
	var name string
	if err := json.Unmarshal(quoted, &name); err != nil {
		return quoted
	}
	return []byte(name)
}

// textFault returns an E_DATA_PARSE issue at the string buf[open:closing+1]
// when the decoder reads its text as other text: a byte that is not valid
// UTF-8, or a \u escape that is half of no surrogate pair, each of which
// encoding/json reads as U+FFFD.
func (pc *parseContext) textFault(open, closing int) (diag.Issue, bool) {
	body := pc.buf[open+1 : closing]
	if at := jsonstring.InvalidUTF8(body); at >= 0 {
		return pc.parseError(pc.spanAt(open), "string is not valid UTF-8",
			fmt.Sprintf("byte 0x%02x is not valid UTF-8; encoding/json would read it as U+FFFD", body[at])), true
	}
	if bytes.IndexByte(body, '\\') < 0 {
		return diag.Issue{}, false
	}
	if at := jsonstring.LoneSurrogate(body); at >= 0 {
		return pc.parseError(pc.spanAt(open), "string holds a lone surrogate escape",
			fmt.Sprintf("the escape %s is half of no surrogate pair; encoding/json would read it as U+FFFD", body[at:at+6])), true
	}
	return diag.Issue{}, false
}

// parseError creates an E_DATA_PARSE issue at span.
// msg is the human-readable message; detail is the machine-oriented parse detail.
func (pc *parseContext) parseError(span location.Span, msg, detail string) diag.Issue {
	return diag.NewIssue(diag.Error, diag.E_DATA_PARSE, msg).
		WithSpan(span).
		WithDetail(diag.DetailKeyFormat, "json").
		WithDetail(diag.DetailKeyDetail, detail).
		Build()
}

// typeTagError creates an E_INVALID_TYPE_TAG issue for type name validation errors.
func typeTagError(span location.Span, typeName, fault string) diag.Issue {
	msg := fmt.Sprintf("invalid type name %s: %s", diag.QuoteValue(typeName), fault)
	return diag.NewIssue(diag.Error, diag.E_INVALID_TYPE_TAG, msg).
		WithSpan(span).
		WithDetail(diag.DetailKeyGot, typeName).
		WithDetail(diag.DetailKeyDetail, fault).
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

// normalizeNumbers rewrites every json.Number in m by [normalizeValue]'s rule.
func normalizeNumbers(m map[string]any) {
	for k, v := range m {
		m[k] = normalizeValue(v)
	}
}

// normalizeValue reads each number by the value rule and recurses into nested
// structures: an integer literal an int64 holds is that int64, and a float
// literal a finite float64 holds is that float64. Every other number keeps its
// text as a json.Number, because the validator judges it at the property's kind:
//
//   - An integer literal outside the int64 range ([value.IsIntegerLiteral]).
//     An Integer refuses it, and a Float reads it as its nearest float64. Its
//     nearest float64 can be another integer: for a literal just below
//     math.MinInt64 it is -2^63, which is MinInt64.
//   - "-0". An int64 0 has no sign; at a Float the literal is the float64 -0.
//   - A literal no finite float64 holds, such as 1e400, which every kind refuses.
//
// The walk is not depth-capped because the decoder above it is:
// encoding/json refuses a document nested past 10,000 levels, and this runs
// only on what that decoder accepted, so every json.Number is a literal.
func normalizeValue(v any) any {
	switch val := v.(type) {
	case json.Number:
		if value.IsIntegerLiteral(val) {
			if i, err := strconv.ParseInt(string(val), 10, 64); err == nil && val != "-0" {
				return i
			}
			return val
		}
		if f, err := strconv.ParseFloat(string(val), 64); err == nil {
			return f
		}
		return val

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
