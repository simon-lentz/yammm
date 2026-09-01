package graph

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

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
// Valid component types are those produced by JSON unmarshaling: string,
// int64, float64, bool, and nil.
//
// Calling FormatKey with no values renders `null`, not `[]`: a variadic
// parameter given no arguments is a nil slice, and so is one given a nil slice
// to spread. [ParseKey] is the inverse over rendered scalars, not over that
// case.
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

// ParseKey decodes a [FormatKey] string back into its component values.
//
// Components come back as the types a snapshot round trip produces: string,
// int64, float64, bool, and nil. Numbers are classified by lexical form, the
// same rule the .ys reader applies — a literal carrying '.', 'e', or 'E' is
// float64, an int-shaped literal is int64. Two consequences follow, and neither
// is reachable from FormatKey output:
//
//   - An int-shaped literal beyond the int64 range comes back as float64, with
//     the precision loss that implies.
//   - A literal that is valid JSON but has no finite Go value, such as 1e999,
//     is an error naming the component's index.
//
// The round-trip law holds over normalized components — string, int64, bool,
// nil, and non-whole float64: ParseKey(FormatKey(vs...)) returns vs, and
// FormatKey(ParseKey(s)...) returns s for any s that FormatKey produced from
// such values. One carve-out is documented rather than hidden: FormatKey renders
// float64(5) as the literal 5, and ParseKey reads that literal back as int64(5).
// The snapshot wire has the same asymmetry, for the same reason.
//
// The string must hold exactly one JSON array and nothing else; surrounding
// whitespace is tolerated, trailing content is not. A component that is itself
// an array or object is an error naming its index: FormatKey's documented
// domain is scalars.
//
// Use [ParseKeyStrings] when every component must be a string.
func ParseKey(s string) ([]any, error) {
	raw, err := decodeKeyArray(s)
	if err != nil {
		return nil, err
	}
	out := make([]any, len(raw))
	for i, v := range raw {
		norm := immutable.NormalizeValue(v)
		switch norm.(type) {
		case []any, map[string]any:
			return nil, fmt.Errorf("graph.ParseKey: component %d is not a scalar: %T", i, norm)
		case json.Number:
			// NormalizeNumber hands back the raw number when no finite Go value
			// represents it. Reporting it keeps the documented component set exact.
			return nil, fmt.Errorf("graph.ParseKey: component %d is not a representable number: %v", i, norm)
		}
		out[i] = norm
	}
	return out, nil
}

// ParseKeyStrings decodes a [FormatKey] string whose every component is a JSON
// string, which is the shape of a key over String-typed primary key properties.
//
// It shares [ParseKey]'s decoding and adds the string requirement: a component
// of any other type is an error naming its index.
func ParseKeyStrings(s string) ([]string, error) {
	raw, err := decodeKeyArray(s)
	if err != nil {
		return nil, err
	}
	out := make([]string, len(raw))
	for i, v := range raw {
		str, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("graph.ParseKeyStrings: component %d is %T, want string", i, immutable.NormalizeValue(v))
		}
		out[i] = str
	}
	return out, nil
}

// decodeKeyArray decodes s as exactly one JSON array, leaving numbers as
// json.Number.
//
// UseNumber is what keeps an integer component beyond 2^53 from being rewritten
// by a float64 round trip on its way back out. Decode stops at the first value,
// so trailing content needs its own probe: without it `["a"] garbage` would
// decode silently.
func decodeKeyArray(s string) ([]any, error) {
	dec := json.NewDecoder(strings.NewReader(s))
	dec.UseNumber()
	var out []any
	if err := dec.Decode(&out); err != nil {
		return nil, fmt.Errorf("graph: parsing key %q: %w", s, err)
	}
	if out == nil {
		return nil, fmt.Errorf("graph: parsing key %q: want a JSON array", s)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("graph: parsing key %q: trailing content after the array", s)
	}
	return out, nil
}
