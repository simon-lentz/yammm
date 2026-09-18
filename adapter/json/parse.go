package json

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/tidwall/jsonc"

	"github.com/simon-lentz/yammm/adapter/json/internal/typetag"
	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/location/path"
)

// byteOrderMark is the UTF-8 encoding of U+FEFF. One leading mark is skipped,
// as the CSV adapter skips it; a second is a parse error.
var byteOrderMark = []byte("\uFEFF")

// ParseObject parses JSON data structured as one top-level key per type name,
// each holding an array of instances — {"Person": [...], "Company": [...]}.
// Returns a map of type name -> slice of RawInstance. Data can start with one
// UTF-8 byte order mark.
//
// Every instance carries a [location.Provenance] naming source, its path in
// the document ($.Person[0]) and the span of its opening brace, and every
// diagnostic carries a span in source. Positions count runes from the line
// start of the bytes passed in.
//
//nolint:revive // ctx is reserved for a cancellation contract
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
		positions: newPositionTable(data),
		bomLen:    len(data) - len(trimmed),
	}
	pc.buf = jsonc.ToJSON(trimmed)

	// Parse as map[string]json.RawMessage to preserve nested structure
	dec := json.NewDecoder(bytes.NewReader(pc.buf))
	dec.UseNumber()

	// Read opening brace
	tok, err := dec.Token()
	if err != nil {
		collector.Collect(*pc.parseError(pc.errSpan(dec, err), "invalid JSON", err.Error()))
		return nil, collector.Result()
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '{' {
		collector.Collect(*pc.parseError(pc.spanAt(0), "expected object at root", "expected object"))
		return nil, collector.Result()
	}

	// Read each type name -> array pair
	for dec.More() {
		keySpan := pc.spanAt(pc.significantFrom(int(dec.InputOffset())))

		// Read type name
		keyTok, err := dec.Token()
		if err != nil {
			collector.Collect(*pc.parseError(pc.errSpan(dec, err), "error reading key", err.Error()))
			return result, collector.Result()
		}
		typeName, ok := keyTok.(string)
		if !ok {
			collector.Collect(*pc.parseError(keySpan, "expected string key", "expected string"))
			continue
		}

		// Validate type name
		if err := typetag.Validate(typeName); err != nil {
			collector.Collect(*typeTagError(keySpan, typeName, err))
			// Skip the value
			var skip any
			if err := dec.Decode(&skip); err != nil {
				collector.Collect(*pc.parseError(pc.errSpan(dec, err), "error skipping value", err.Error()))
			}
			continue
		}

		// Read the array of instances
		instances, parseIssues := parseArray(dec, pc, typeName)
		for i := range parseIssues {
			collector.Collect(parseIssues[i])
		}
		if len(instances) > 0 {
			result[typeName] = instances
		}
	}

	// Read closing brace
	if _, err := dec.Token(); err != nil {
		collector.Collect(*pc.parseError(pc.errSpan(dec, err), "error reading closing brace", err.Error()))
	}

	// Check for trailing content after root object. The offset is taken BEFORE
	// the token is read: afterwards it is the token's end.
	trailingSpan := pc.spanAt(pc.significantFrom(int(dec.InputOffset())))
	if tok, err := dec.Token(); err == nil {
		collector.Collect(*pc.parseError(trailingSpan,
			"unexpected content after root object", fmt.Sprintf("found %v", tok)))
	}

	return result, collector.Result()
}

// parseContext holds what a span costs to build: the document's identity, the
// buffer the decoder reports offsets into, the length of the byte order mark
// trimmed off it, and the converter from an offset in the original bytes to a
// position.
type parseContext struct {
	source    location.SourceID
	buf       []byte
	bomLen    int
	positions *positionTable
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

// errSpan locates a decoder error at the byte that caused it.
//
// [json.SyntaxError]'s Offset counts the bytes read BEFORE the error, so it sits
// one past the offending byte and can name the next line where that byte ends
// one. The span backs up by one. Without a reported offset the decoder's current
// position is the best answer there is.
func (pc *parseContext) errSpan(dec *json.Decoder, err error) location.Span {
	if syntaxErr, _ := errors.AsType[*json.SyntaxError](err); syntaxErr != nil {
		return pc.spanAt(max(int(syntaxErr.Offset)-1, 0))
	}
	return pc.spanAt(int(dec.InputOffset()))
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

// parseArray is an internal helper that parses an array from a decoder.
// Returns parsed instances and any issues encountered. Collects all errors
// instead of failing fast, allowing maximum information recovery.
//
// An instance's path indexes the array as the document writes it, so an element
// that failed to decode still consumes its index and the path agrees with the
// span beside it.
func parseArray(dec *json.Decoder, pc *parseContext, typeName string) ([]instance.RawInstance, []diag.Issue) {
	var issues []diag.Issue

	// Read opening bracket
	arraySpan := pc.spanAt(pc.significantFrom(int(dec.InputOffset())))
	tok, err := dec.Token()
	if err != nil {
		issues = append(issues, *pc.parseError(pc.errSpan(dec, err), "error reading array", err.Error()))
		return nil, issues
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '[' {
		issues = append(issues, *pc.parseError(arraySpan, "expected array", "expected array"))
		// Skip the remainder of the value to keep decoder synchronized
		skipValue(dec, tok)
		return nil, issues
	}

	var result []instance.RawInstance

	for index := 0; dec.More(); index++ {
		elemSpan := pc.spanAt(pc.significantFrom(int(dec.InputOffset())))

		var obj map[string]any
		if err := dec.Decode(&obj); err != nil {
			// A syntax error carries its own offset. Any other failure — an
			// element of the wrong JSON type — is reported at the element's
			// start, because the decoder's offset is then the END of the token
			// it rejected.
			span := elemSpan
			if syntaxErr, _ := errors.AsType[*json.SyntaxError](err); syntaxErr != nil {
				span = pc.errSpan(dec, err)
			}
			issues = append(issues, *pc.parseError(span, "error reading array element", err.Error()))

			// For syntax errors, the decoder cannot recover - stop parsing
			if syntaxErr, _ := errors.AsType[*json.SyntaxError](err); syntaxErr != nil || errors.Is(err, io.ErrUnexpectedEOF) {
				return result, issues
			}
			continue
		}

		// Reject null values - json.Decode into map yields nil without error
		if obj == nil {
			issues = append(issues, *pc.parseError(elemSpan, "expected object", "got null"))
			continue
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
	if _, err := dec.Token(); err != nil {
		issues = append(issues, *pc.parseError(pc.errSpan(dec, err), "error reading closing bracket", err.Error()))
	}

	return result, issues
}

// parseError creates an E_ADAPTER_PARSE issue at span.
// msg is the human-readable message; detail is the machine-oriented parse detail.
func (pc *parseContext) parseError(span location.Span, msg, detail string) *diag.Issue {
	issue := diag.NewIssue(diag.Error, diag.E_ADAPTER_PARSE, msg).
		WithSpan(span).
		WithDetail(diag.DetailKeyFormat, "json").
		WithDetail(diag.DetailKeyDetail, detail).
		Build()
	return &issue
}

// typeTagError creates an E_INVALID_TYPE_TAG issue for type name validation errors.
func typeTagError(span location.Span, typeName string, err error) *diag.Issue {
	msg := fmt.Sprintf("invalid type name %q: %s", typeName, err.Error())
	issue := diag.NewIssue(diag.Error, diag.E_INVALID_TYPE_TAG, msg).
		WithSpan(span).
		WithDetail(diag.DetailKeyGot, typeName).
		WithDetail(diag.DetailKeyDetail, err.Error()).
		Build()
	return &issue
}

// skipValue consumes the remainder of a JSON value from the decoder after
// reading its first token. This is used for error recovery when an unexpected
// value type is encountered, ensuring the decoder stays synchronized.
func skipValue(dec *json.Decoder, firstTok json.Token) {
	// For delimiters, we need to skip until the matching close
	if delim, ok := firstTok.(json.Delim); ok {
		switch delim {
		case '{', '[':
			skipUntilClose(dec)
		}
		// '}' and ']' are already consumed, no action needed
	}
	// For primitives (string, number, bool, null), the value was fully
	// consumed by the Token() call that returned firstTok
}

// skipUntilClose consumes tokens until the structure is balanced.
// Handles nested structures by tracking delimiter depth.
func skipUntilClose(dec *json.Decoder) {
	depth := 1
	for depth > 0 {
		tok, err := dec.Token()
		if err != nil {
			return // EOF or error, decoder is as synchronized as possible
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
}

// normalizeNumbers recursively converts json.Number values to int64 or float64.
func normalizeNumbers(m map[string]any) {
	for k, v := range m {
		m[k] = normalizeValue(v)
	}
}

// normalizeValue converts json.Number and recurses into nested structures.
func normalizeValue(v any) any {
	switch val := v.(type) {
	case json.Number:
		// Try int64 first
		if i, err := val.Int64(); err == nil {
			// Check if it was really an integer (no decimal point)
			if !strings.Contains(val.String(), ".") {
				return i
			}
		}
		// Fall back to float64
		if f, err := val.Float64(); err == nil {
			return f
		}
		// Return as string if conversion fails (shouldn't happen for valid JSON)
		return val.String()

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
