package json

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/tidwall/jsonc"

	"github.com/simon-lentz/yammm/adapter/json/internal/typetag"
	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/instance/path"
	"github.com/simon-lentz/yammm/location"
)

// ParseObject parses JSON data structured as {"TypeName": [...], "OtherType": [...]}.
//
// Each top-level key is a type name, and its value must be an array of instances.
// Returns a map of type name -> slice of RawInstance.
//
// Example input:
//
//	{
//	  "Person": [{"name": "Alice"}, {"name": "Bob"}],
//	  "Company": [{"title": "Acme Inc"}]
//	}
func (a *Adapter) ParseObject(source location.SourceID, data []byte) (map[string][]instance.RawInstance, diag.Result) {
	collector := diag.NewCollectorUnlimited()
	result := make(map[string][]instance.RawInstance)

	// Preprocess with jsonc if not strict
	processedData := data
	if !a.strictJSON {
		processedData = jsonc.ToJSON(data)
	}

	// Parse as map[string]json.RawMessage to preserve nested structure
	dec := json.NewDecoder(bytes.NewReader(processedData))
	dec.UseNumber()

	// Read opening brace
	tok, err := dec.Token()
	if err != nil {
		collector.Collect(*a.parseError(source, 0, "invalid JSON", err.Error()))
		return nil, collector.Result()
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '{' {
		collector.Collect(*a.parseError(source, 0, "expected object at root", "expected object"))
		return nil, collector.Result()
	}

	// Read each type name -> array pair
	for dec.More() {
		// Read type name
		keyTok, err := dec.Token()
		if err != nil {
			collector.Collect(*a.parseError(source, int(dec.InputOffset()), "error reading key", err.Error()))
			return result, collector.Result()
		}
		typeName, ok := keyTok.(string)
		if !ok {
			collector.Collect(*a.parseError(source, int(dec.InputOffset()), "expected string key", "expected string"))
			continue
		}

		// Validate type name
		if err := typetag.Validate(typeName); err != nil {
			collector.Collect(*a.typeTagError(source, int(dec.InputOffset()), typeName, err))
			// Skip the value
			var skip any
			if err := dec.Decode(&skip); err != nil {
				collector.Collect(*a.parseError(source, int(dec.InputOffset()), "error skipping value", err.Error()))
			}
			continue
		}

		// Read the array of instances
		instances, parseIssues := a.parseArray(dec, source, path.Root().Key(typeName))
		for i := range parseIssues {
			collector.Collect(parseIssues[i])
		}
		if len(instances) > 0 {
			result[typeName] = instances
		}
	}

	// Read closing brace
	if _, err := dec.Token(); err != nil {
		collector.Collect(*a.parseError(source, int(dec.InputOffset()), "error reading closing brace", err.Error()))
	}

	// Check for trailing content after root object
	if tok, err := dec.Token(); err == nil {
		collector.Collect(*a.parseError(source, int(dec.InputOffset()), "unexpected content after root object", fmt.Sprintf("found %v", tok)))
	}

	return result, collector.Result()
}

// ParseArray parses JSON data as an array of objects with $type fields.
//
// Each object must have a $type field (or the configured type field) specifying
// its type name. Returns a map of type name -> slice of RawInstance.
//
// Example input:
//
//	[
//	  {"$type": "Person", "name": "Alice"},
//	  {"$type": "Company", "title": "Acme Inc"}
//	]
func (a *Adapter) ParseArray(source location.SourceID, data []byte) (map[string][]instance.RawInstance, diag.Result) {
	collector := diag.NewCollectorUnlimited()
	result := make(map[string][]instance.RawInstance)

	// Preprocess with jsonc if not strict
	processedData := data
	if !a.strictJSON {
		processedData = jsonc.ToJSON(data)
	}

	dec := json.NewDecoder(bytes.NewReader(processedData))
	dec.UseNumber()

	// Read opening bracket
	tok, err := dec.Token()
	if err != nil {
		collector.Collect(*a.parseError(source, 0, "invalid JSON", err.Error()))
		return nil, collector.Result()
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '[' {
		collector.Collect(*a.parseError(source, 0, "expected array at root", "expected array"))
		return nil, collector.Result()
	}

	idx := 0
	for dec.More() {
		startOffset := int(dec.InputOffset())

		// Read the object
		var obj map[string]any
		if err := dec.Decode(&obj); err != nil {
			// Use syntax error offset for more precise error location
			errOffset := startOffset
			var syntaxErr *json.SyntaxError
			if errors.As(err, &syntaxErr) {
				errOffset = int(syntaxErr.Offset)
			}
			collector.Collect(*a.parseError(source, errOffset, "error reading array element", err.Error()))

			// For syntax errors, the decoder cannot recover - stop parsing
			if syntaxErr != nil || errors.Is(err, io.ErrUnexpectedEOF) {
				return result, collector.Result()
			}

			idx++
			continue
		}
		endOffset := int(dec.InputOffset())

		// Reject null values - json.Decode into map yields nil without error
		if obj == nil {
			collector.Collect(*a.parseError(source, startOffset, "expected object", "got null"))
			idx++
			continue
		}

		// Extract and validate type tag
		typeTagVal, hasType := obj[a.typeField]
		if !hasType {
			collector.Collect(*a.missingTypeTagError(source, startOffset, idx))
			idx++
			continue
		}

		typeName, ok := typeTagVal.(string)
		if !ok {
			collector.Collect(*a.invalidTypeTagError(source, startOffset, idx, "expected string", fmt.Sprintf("%T", typeTagVal)))
			idx++
			continue
		}

		if err := typetag.Validate(typeName); err != nil {
			collector.Collect(*a.typeTagError(source, startOffset, typeName, err))
			idx++
			continue
		}

		// Remove the type field from properties
		delete(obj, a.typeField)

		// Normalize json.Number to numeric types
		normalizeNumbers(obj)

		// Create RawInstance
		raw := instance.RawInstance{
			Properties: obj,
		}

		if a.trackLocations && a.registry != nil {
			prov := a.makeProvenance(source, path.Root().Index(idx), startOffset, endOffset)
			raw.Provenance = prov
		}

		result[typeName] = append(result[typeName], raw)
		idx++
	}

	// Read closing bracket
	if _, err := dec.Token(); err != nil {
		collector.Collect(*a.parseError(source, int(dec.InputOffset()), "error reading closing bracket", err.Error()))
	}

	// Check for trailing content after root array
	if tok, err := dec.Token(); err == nil {
		collector.Collect(*a.parseError(source, int(dec.InputOffset()), "unexpected content after root array", fmt.Sprintf("found %v", tok)))
	}

	return result, collector.Result()
}

// ParseTypedArray parses a JSON array where all objects are of the specified type.
//
// Objects do not need a $type field; the type is provided explicitly.
// Returns a slice of RawInstance.
//
// Example input (with typeName="Person"):
//
//	[{"name": "Alice"}, {"name": "Bob"}]
func (a *Adapter) ParseTypedArray(source location.SourceID, typeName string, data []byte) ([]instance.RawInstance, diag.Result) {
	collector := diag.NewCollectorUnlimited()

	// Validate type name
	if err := typetag.Validate(typeName); err != nil {
		collector.Collect(*a.typeTagError(source, 0, typeName, err))
		return nil, collector.Result()
	}

	// Preprocess with jsonc if not strict
	processedData := data
	if !a.strictJSON {
		processedData = jsonc.ToJSON(data)
	}

	dec := json.NewDecoder(bytes.NewReader(processedData))
	dec.UseNumber()

	result, parseIssues := a.parseArray(dec, source, path.Root())
	for i := range parseIssues {
		collector.Collect(parseIssues[i])
	}

	// Check for trailing content after root array
	if tok, err := dec.Token(); err == nil {
		collector.Collect(*a.parseError(source, int(dec.InputOffset()), "unexpected content after root array", fmt.Sprintf("found %v", tok)))
	}

	return result, collector.Result()
}

// ParseOne parses a single JSON object of the specified type.
//
// The object does not need a $type field; the type is provided explicitly.
//
// Example input (with typeName="Person"):
//
//	{"name": "Alice", "age": 30}
func (a *Adapter) ParseOne(source location.SourceID, typeName string, data []byte) (instance.RawInstance, diag.Result) {
	collector := diag.NewCollectorUnlimited()

	// Validate type name
	if err := typetag.Validate(typeName); err != nil {
		collector.Collect(*a.typeTagError(source, 0, typeName, err))
		return instance.RawInstance{}, collector.Result()
	}

	// Preprocess with jsonc if not strict
	processedData := data
	if !a.strictJSON {
		processedData = jsonc.ToJSON(data)
	}

	dec := json.NewDecoder(bytes.NewReader(processedData))
	dec.UseNumber()

	startOffset := int(dec.InputOffset())
	var obj map[string]any
	if err := dec.Decode(&obj); err != nil {
		collector.Collect(*a.parseError(source, startOffset, "invalid JSON", err.Error()))
		return instance.RawInstance{}, collector.Result()
	}
	endOffset := int(dec.InputOffset())

	// Reject null values - json.Decode into map yields nil without error
	if obj == nil {
		collector.Collect(*a.parseError(source, startOffset, "expected object", "got null"))
		return instance.RawInstance{}, collector.Result()
	}

	// Normalize json.Number to numeric types
	normalizeNumbers(obj)

	raw := instance.RawInstance{
		Properties: obj,
	}

	if a.trackLocations && a.registry != nil {
		prov := a.makeProvenance(source, path.Root(), startOffset, endOffset)
		raw.Provenance = prov
	}

	// Check for trailing content after root object
	if tok, err := dec.Token(); err == nil {
		collector.Collect(*a.parseError(source, int(dec.InputOffset()), "unexpected content after root object", fmt.Sprintf("found %v", tok)))
	}

	return raw, collector.Result()
}

// parseArray is an internal helper that parses an array from a decoder.
// Returns parsed instances and any issues encountered. Collects all errors
// instead of failing fast, allowing maximum information recovery.
func (a *Adapter) parseArray(dec *json.Decoder, source location.SourceID, basePath path.Builder) ([]instance.RawInstance, []diag.Issue) {
	var issues []diag.Issue

	// Read opening bracket
	tok, err := dec.Token()
	if err != nil {
		issues = append(issues, *a.parseError(source, int(dec.InputOffset()), "error reading array", err.Error()))
		return nil, issues
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '[' {
		issues = append(issues, *a.parseError(source, int(dec.InputOffset()), "expected array", "expected array"))
		// Skip the remainder of the value to keep decoder synchronized
		skipValue(dec, tok)
		return nil, issues
	}

	var result []instance.RawInstance
	idx := 0

	for dec.More() {
		startOffset := int(dec.InputOffset())

		var obj map[string]any
		if err := dec.Decode(&obj); err != nil {
			// Use syntax error offset for more precise error location
			errOffset := startOffset
			var syntaxErr *json.SyntaxError
			if errors.As(err, &syntaxErr) {
				errOffset = int(syntaxErr.Offset)
			}
			issues = append(issues, *a.parseError(source, errOffset, "error reading array element", err.Error()))

			// For syntax errors, the decoder cannot recover - stop parsing
			if syntaxErr != nil || errors.Is(err, io.ErrUnexpectedEOF) {
				return result, issues
			}

			idx++
			continue
		}
		endOffset := int(dec.InputOffset())

		// Reject null values - json.Decode into map yields nil without error
		if obj == nil {
			issues = append(issues, *a.parseError(source, startOffset, "expected object", "got null"))
			idx++
			continue
		}

		// Normalize json.Number to numeric types
		normalizeNumbers(obj)

		raw := instance.RawInstance{
			Properties: obj,
		}

		if a.trackLocations && a.registry != nil {
			prov := a.makeProvenance(source, basePath.Index(idx), startOffset, endOffset)
			raw.Provenance = prov
		}

		result = append(result, raw)
		idx++
	}

	// Read closing bracket
	if _, err := dec.Token(); err != nil {
		issues = append(issues, *a.parseError(source, int(dec.InputOffset()), "error reading closing bracket", err.Error()))
	}

	return result, issues
}

// makeProvenance creates a Provenance from byte offsets.
// If positions cannot be determined (IsZero), the span preserves only the source identity.
func (a *Adapter) makeProvenance(source location.SourceID, p path.Builder, startOffset, endOffset int) *instance.Provenance {
	startPos := a.registry.PositionAt(source, startOffset)
	endPos := a.registry.PositionAt(source, endOffset)

	// Guard: require both positions valid for a proper range span.
	// Unlike point spans in parseError which use a single position, a provenance
	// range needs both endpoints to be meaningful. If either fails, fall back
	// to source-only span rather than a malformed range.
	var span location.Span
	if !startPos.IsZero() && !endPos.IsZero() {
		span = location.Span{Source: source, Start: startPos, End: endPos}
	} else {
		// Preserve source identity for diagnostics even without precise positions
		span = location.Span{Source: source}
	}

	return instance.NewProvenance(source.String(), p, span)
}

// parseError creates an E_ADAPTER_PARSE issue.
// msg is the human-readable message; detail is the machine-oriented parse detail.
func (a *Adapter) parseError(source location.SourceID, offset int, msg, detail string) *diag.Issue {
	ib := diag.NewIssue(diag.Error, diag.E_ADAPTER_PARSE, msg).
		WithDetail(diag.DetailKeyFormat, "json").
		WithDetail(diag.DetailKeyDetail, detail)
	if a.trackLocations && a.registry != nil {
		pos := a.registry.PositionAt(source, offset)
		// Guard: only attach span if position is valid
		if !pos.IsZero() {
			span := location.Span{Source: source, Start: pos, End: pos}
			ib.WithSpan(span)
		}
	}
	issue := ib.Build()
	return &issue
}

// missingTypeTagError creates an E_MISSING_TYPE_TAG issue.
func (a *Adapter) missingTypeTagError(source location.SourceID, offset int, idx int) *diag.Issue {
	msg := fmt.Sprintf("missing %s field in array element [%d]", a.typeField, idx)
	ib := diag.NewIssue(diag.Error, diag.E_MISSING_TYPE_TAG, msg)
	if a.trackLocations && a.registry != nil {
		pos := a.registry.PositionAt(source, offset)
		// Guard: only attach span if position is valid
		if !pos.IsZero() {
			span := location.Span{Source: source, Start: pos, End: pos}
			ib.WithSpan(span)
		}
	}
	issue := ib.Build()
	return &issue
}

// invalidTypeTagError creates an E_INVALID_TYPE_TAG issue.
// detail is a canonical reason string; got is the observed value/type.
func (a *Adapter) invalidTypeTagError(source location.SourceID, offset int, idx int, detail, got string) *diag.Issue {
	msg := fmt.Sprintf("invalid %s in array element [%d]: %s, got %s", a.typeField, idx, detail, got)
	ib := diag.NewIssue(diag.Error, diag.E_INVALID_TYPE_TAG, msg).
		WithDetail(diag.DetailKeyDetail, detail).
		WithDetail(diag.DetailKeyGot, got)
	if a.trackLocations && a.registry != nil {
		pos := a.registry.PositionAt(source, offset)
		// Guard: only attach span if position is valid
		if !pos.IsZero() {
			span := location.Span{Source: source, Start: pos, End: pos}
			ib.WithSpan(span)
		}
	}
	issue := ib.Build()
	return &issue
}

// typeTagError creates an E_INVALID_TYPE_TAG issue for type name validation errors.
func (a *Adapter) typeTagError(source location.SourceID, offset int, typeName string, err error) *diag.Issue {
	msg := fmt.Sprintf("invalid type name %q: %s", typeName, err.Error())
	ib := diag.NewIssue(diag.Error, diag.E_INVALID_TYPE_TAG, msg).
		WithDetail(diag.DetailKeyGot, typeName).
		WithDetail(diag.DetailKeyDetail, err.Error())
	if a.trackLocations && a.registry != nil {
		pos := a.registry.PositionAt(source, offset)
		// Guard: only attach span if position is valid
		if !pos.IsZero() {
			span := location.Span{Source: source, Start: pos, End: pos}
			ib.WithSpan(span)
		}
	}
	issue := ib.Build()
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
