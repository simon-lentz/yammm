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
)

// ParseObject parses JSON data structured as one top-level key per type name,
// each holding an array of instances — {"Person": [...], "Company": [...]}.
// Returns a map of type name -> slice of RawInstance.
//
//nolint:revive // ctx and source reserved for future use (cancellation, provenance)
func (a *Adapter) ParseObject(ctx context.Context, source location.SourceID, data []byte) (map[string][]instance.RawInstance, diag.Result) {
	collector := diag.NewCollectorUnlimited()
	result := make(map[string][]instance.RawInstance)

	// Preprocess with jsonc: comments and trailing commas are tolerated.
	processedData := jsonc.ToJSON(data)

	// Parse as map[string]json.RawMessage to preserve nested structure
	dec := json.NewDecoder(bytes.NewReader(processedData))
	dec.UseNumber()

	// Read opening brace
	tok, err := dec.Token()
	if err != nil {
		collector.Collect(*parseError("invalid JSON", err.Error()))
		return nil, collector.Result()
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '{' {
		collector.Collect(*parseError("expected object at root", "expected object"))
		return nil, collector.Result()
	}

	// Read each type name -> array pair
	for dec.More() {
		// Read type name
		keyTok, err := dec.Token()
		if err != nil {
			collector.Collect(*parseError("error reading key", err.Error()))
			return result, collector.Result()
		}
		typeName, ok := keyTok.(string)
		if !ok {
			collector.Collect(*parseError("expected string key", "expected string"))
			continue
		}

		// Validate type name
		if err := typetag.Validate(typeName); err != nil {
			collector.Collect(*typeTagError(typeName, err))
			// Skip the value
			var skip any
			if err := dec.Decode(&skip); err != nil {
				collector.Collect(*parseError("error skipping value", err.Error()))
			}
			continue
		}

		// Read the array of instances
		instances, parseIssues := parseArray(dec)
		for i := range parseIssues {
			collector.Collect(parseIssues[i])
		}
		if len(instances) > 0 {
			result[typeName] = instances
		}
	}

	// Read closing brace
	if _, err := dec.Token(); err != nil {
		collector.Collect(*parseError("error reading closing brace", err.Error()))
	}

	// Check for trailing content after root object
	if tok, err := dec.Token(); err == nil {
		collector.Collect(*parseError("unexpected content after root object", fmt.Sprintf("found %v", tok)))
	}

	return result, collector.Result()
}

// parseArray is an internal helper that parses an array from a decoder.
// Returns parsed instances and any issues encountered. Collects all errors
// instead of failing fast, allowing maximum information recovery.
func parseArray(dec *json.Decoder) ([]instance.RawInstance, []diag.Issue) {
	var issues []diag.Issue

	// Read opening bracket
	tok, err := dec.Token()
	if err != nil {
		issues = append(issues, *parseError("error reading array", err.Error()))
		return nil, issues
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '[' {
		issues = append(issues, *parseError("expected array", "expected array"))
		// Skip the remainder of the value to keep decoder synchronized
		skipValue(dec, tok)
		return nil, issues
	}

	var result []instance.RawInstance

	for dec.More() {
		var obj map[string]any
		if err := dec.Decode(&obj); err != nil {
			issues = append(issues, *parseError("error reading array element", err.Error()))

			// For syntax errors, the decoder cannot recover - stop parsing
			if syntaxErr, _ := errors.AsType[*json.SyntaxError](err); syntaxErr != nil || errors.Is(err, io.ErrUnexpectedEOF) {
				return result, issues
			}
			continue
		}

		// Reject null values - json.Decode into map yields nil without error
		if obj == nil {
			issues = append(issues, *parseError("expected object", "got null"))
			continue
		}

		// Normalize json.Number to numeric types
		normalizeNumbers(obj)

		result = append(result, instance.RawInstance{
			Properties: obj,
		})
	}

	// Read closing bracket
	if _, err := dec.Token(); err != nil {
		issues = append(issues, *parseError("error reading closing bracket", err.Error()))
	}

	return result, issues
}

// parseError creates an E_ADAPTER_PARSE issue.
// msg is the human-readable message; detail is the machine-oriented parse detail.
func parseError(msg, detail string) *diag.Issue {
	issue := diag.NewIssue(diag.Error, diag.E_ADAPTER_PARSE, msg).
		WithDetail(diag.DetailKeyFormat, "json").
		WithDetail(diag.DetailKeyDetail, detail).
		Build()
	return &issue
}

// typeTagError creates an E_INVALID_TYPE_TAG issue for type name validation errors.
func typeTagError(typeName string, err error) *diag.Issue {
	msg := fmt.Sprintf("invalid type name %q: %s", typeName, err.Error())
	issue := diag.NewIssue(diag.Error, diag.E_INVALID_TYPE_TAG, msg).
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
