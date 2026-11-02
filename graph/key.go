package graph

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/immutable"
)

// FormatKey renders primary key components as the JSON array string the
// graph's addresses use: ["value1", value2, ...].
//
// It renders the values AS GIVEN and canonicalizes nothing; the graph does,
// at every address it receives. The graph indexes an instance by the key it
// carries. For a Timestamp, Date or UUID key whose constraint renders the value,
// the key is the canonical text that value took at entry — so
// [Snapshot.InstanceByKey] canonicalizes an address that misses as spelled under
// the type's key constraints, and FormatKey over each text spelling of one
// instant at one offset addresses it, as does a time.Time at a Timestamp key with
// no layout. A time.Time at a Date key, or at a Timestamp key whose layout does not read
// RFC 3339, misses: pass the stored text there. [Instance.PrimaryKey] is the address itself.
//
// Examples:
//
//	FormatKey("ABC123")       -> `["ABC123"]`
//	FormatKey("us", 12345)    -> `["us",12345]`
//	FormatKey(42)             -> `[42]`
//
// It renders each value as [encoding/json] does, and the graph reads a key
// component only as a scalar.
//
// Calling FormatKey with no arguments, or with a nil slice to spread, renders
// `null`, since the variadic parameter is then a nil slice. An empty slice spread
// renders `[]`. [ParseKey] reads a rendered key back, not that case, and its doc
// names the components it does not read back as written.
//
// FormatKey panics if any value cannot be JSON-marshaled (e.g., channels,
// functions, cyclic structs). This is a programmer error: no key a graph holds
// holds such a component.
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
// float64, an int-shaped literal is int64. Two consequences follow:
//
//   - An int-shaped literal beyond the int64 range comes back as float64, with
//     the precision loss that implies. FormatKey writes one for any value
//     encoding/json renders as such a literal, as a whole float64 of magnitude
//     2^63 to below 1e21 or a uint64 above math.MaxInt64 is rendered.
//   - A literal that is valid JSON but has no finite Go value, such as 1e999,
//     is an error naming the component's index. FormatKey writes one only for a
//     value that encoding/json renders as such a literal, which no value of
//     its documented component types is.
//
// The round-trip law holds over these normalized components: a valid UTF-8
// string, int64, bool, nil and finite non-whole float64. ParseKey(FormatKey(vs...))
// returns vs, and FormatKey(ParseKey(s)...) returns s for any s that FormatKey
// produced from such values. Two carve-outs are documented rather than hidden:
// FormatKey renders float64(5) as the literal 5, which ParseKey reads back as
// int64(5) at the same rendering, and a float64 negative zero as -0, which
// ParseKey reads back as int64(0), rendered 0.
//
// The string must hold exactly one JSON array and nothing else; surrounding
// whitespace is tolerated, trailing content is not. A component that is itself
// an array or object is an error naming its index: FormatKey's documented
// domain is scalars.
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
			return nil, fmt.Errorf("graph.ParseKey: component %d is not a representable number: %v", i, diag.ValueText(fmt.Sprint(norm)))
		}
		out[i] = norm
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
		return nil, fmt.Errorf("graph: parsing key %s: %w", diag.QuoteValue(s), err)
	}
	if out == nil {
		return nil, fmt.Errorf("graph: parsing key %s: want a JSON array", diag.QuoteValue(s))
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("graph: parsing key %s: trailing content after the array", diag.QuoteValue(s))
	}
	return out, nil
}
