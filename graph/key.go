package graph

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/simon-lentz/yammm/immutable"
)

// FormatKey produces a canonical JSON array string for primary key lookup.
// The format is a JSON array: ["value1", value2, ...].
//
// This is the canonical form used for:
//   - Map key indexing within the graph
//   - Diagnostic messages showing duplicate keys
//   - InstanceByKey lookups
//
// Examples:
//
//	FormatKey("ABC123")       -> `["ABC123"]`
//	FormatKey("us", 12345)    -> `["us",12345]`
//	FormatKey(42)             -> `[42]`
//
// FormatKey panics if any value cannot be JSON-marshaled (e.g., channels,
// functions, cyclic structs). This is a programmer error—primary key values
// come from validated instances and are guaranteed JSON-marshalable.
func FormatKey(values ...any) string {
	data, err := json.Marshal(values)
	if err != nil {
		panic(fmt.Sprintf("graph.FormatKey: failed to marshal key values: %v", err))
	}
	return string(data)
}

// FormatComposedKey returns the canonical identity string for a composed child.
//
// The format is a JSON array: [ParentKeyArray, "CompositionName", ChildKeyOrIndex].
// This handles all edge cases including keys containing delimiters, quotes,
// brackets, or other special characters.
//
// Parameters:
//   - parentKeyValues: the parent's primary key values (same values passed to FormatKey)
//   - compositionName: the DSL relation name (e.g., "WHEELS")
//   - childKeyOrIndex: either:
//   - nil: for (one) cardinality compositions
//   - []any: for (many) with PK (the child's primary key values)
//   - int: for (many) without PK (0-based array index)
//
// Returns an error if:
//   - parentKeyValues is nil or empty
//   - compositionName is empty
//   - childKeyOrIndex is not nil, []any, or int
//   - childKeyOrIndex is []any but empty
//   - childKeyOrIndex is int but negative
//
// JSON marshaling failures cause a panic (same rationale as FormatKey).
//
// Examples:
//
//	FormatComposedKey([]any{"ABC123"}, "ADDRESS", nil)
//	  -> `[["ABC123"],"ADDRESS"]`, nil
//
//	FormatComposedKey([]any{"ABC123"}, "WHEELS", []any{"front-left"})
//	  -> `[["ABC123"],"WHEELS",["front-left"]]`, nil
//
//	FormatComposedKey([]any{"ABC123"}, "NOTES", 0)
//	  -> `[["ABC123"],"NOTES",0]`, nil
func FormatComposedKey(parentKeyValues []any, compositionName string, childKeyOrIndex any) (string, error) {
	if len(parentKeyValues) == 0 {
		return "", errors.New("parentKeyValues cannot be nil or empty")
	}
	if compositionName == "" {
		return "", errors.New("compositionName cannot be empty")
	}

	// Validate and construct the array based on childKeyOrIndex type
	var arr []any

	switch v := childKeyOrIndex.(type) {
	case nil:
		// (one) cardinality: [ParentKey, "CompositionName"]
		arr = []any{parentKeyValues, compositionName}

	case []any:
		if len(v) == 0 {
			return "", errors.New("childKeyOrIndex []any cannot be empty")
		}
		// (many) with PK: [ParentKey, "CompositionName", ChildKey]
		arr = []any{parentKeyValues, compositionName, v}

	case int:
		if v < 0 {
			return "", fmt.Errorf("childKeyOrIndex int cannot be negative: %d", v)
		}
		// (many) without PK: [ParentKey, "CompositionName", ArrayIndex]
		arr = []any{parentKeyValues, compositionName, v}

	default:
		return "", fmt.Errorf("childKeyOrIndex must be nil, []any, or int; got %T", childKeyOrIndex)
	}

	data, err := json.Marshal(arr)
	if err != nil {
		panic(fmt.Sprintf("graph.FormatComposedKey: failed to marshal: %v", err))
	}
	return string(data), nil
}

// ParseComposedKey parses a composed-child identity string back to components.
//
// Returns:
//   - parentKeyValues: the parent's primary key values (not the JSON string)
//   - compositionName: the DSL relation name
//   - childKeyOrIndex: []any (child key values), int (array index), or nil ((one) cardinality)
//   - err: non-nil if the input is malformed
//
// This enables diagnostic tooling, test assertions, and APIs that accept identity strings.
// The returned values are structured ([]any) and can be used directly with other graph APIs.
func ParseComposedKey(s string) (parentKeyValues []any, compositionName string, childKeyOrIndex any, err error) {
	var arr []any
	if err = json.Unmarshal([]byte(s), &arr); err != nil {
		return nil, "", nil, fmt.Errorf("invalid composed key format: %w", err)
	}

	if len(arr) < 2 || len(arr) > 3 {
		return nil, "", nil, fmt.Errorf("composed key must have 2 or 3 elements, got %d", len(arr))
	}

	// Parse parent key values (first element must be an array)
	parentKeyRaw, ok := arr[0].([]any)
	if !ok {
		return nil, "", nil, fmt.Errorf("first element must be an array (parent key values), got %T", arr[0])
	}
	if len(parentKeyRaw) == 0 {
		return nil, "", nil, errors.New("parent key values cannot be empty")
	}
	parentKeyValues = parentKeyRaw

	// Parse composition name (second element must be a string)
	compositionName, ok = arr[1].(string)
	if !ok {
		return nil, "", nil, fmt.Errorf("second element must be a string (composition name), got %T", arr[1])
	}
	if compositionName == "" {
		return nil, "", nil, errors.New("composition name cannot be empty")
	}

	// Parse child key or index (optional third element)
	if len(arr) == 2 {
		// (one) cardinality: no child identifier
		childKeyOrIndex = nil
	} else {
		switch v := arr[2].(type) {
		case []any:
			// Child key values (many with PK)
			if len(v) == 0 {
				return nil, "", nil, errors.New("child key values cannot be empty")
			}
			childKeyOrIndex = v
		case float64:
			// JSON numbers unmarshal as float64; convert to int for array index
			idx := int(v)
			if float64(idx) != v {
				return nil, "", nil, fmt.Errorf("array index must be an integer, got %v", v)
			}
			if idx < 0 {
				return nil, "", nil, fmt.Errorf("array index cannot be negative: %d", idx)
			}
			childKeyOrIndex = idx
		default:
			return nil, "", nil, fmt.Errorf("third element must be array (child key) or number (index), got %T", arr[2])
		}
	}

	return parentKeyValues, compositionName, childKeyOrIndex, nil
}

// keyToValues extracts []any from immutable.Key for use with FormatComposedKey.
//
// This helper converts the immutable key representation to the []any format
// expected by FormatComposedKey and FormatKey.
func keyToValues(k immutable.Key) []any {
	result := make([]any, k.Len())
	for i := range k.Len() {
		result[i] = k.Get(i).Unwrap()
	}
	return result
}
