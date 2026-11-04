package graph

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/internal/value"
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
// Components come back as string, int64, float64, bool and nil. A number reads
// as the .ys reader reads a key component: an integer literal is int64, and a
// literal carrying '.', 'e' or 'E' is float64, whole or not. An integer literal
// no int64 holds, and a literal no finite float64 holds, is an error naming the
// component's index: an integer literal past the int64 range is one, which
// FormatKey writes for an unsigned integer
// above math.MaxInt64, a json.Number that spells one and a whole float64 or
// float32 of magnitude 2^63 to below 1e21, and a literal such as 1e999.
//
// The round-trip law holds over a valid UTF-8 string, int64, bool, nil and a
// finite float64 that is not whole: ParseKey(FormatKey(vs...)) returns vs, and
// FormatKey(ParseKey(s)...) returns s for any s that FormatKey produced from such
// values. FormatKey renders a whole float64 below 2^63 in magnitude as an integer
// literal, so float64(5) reads back as int64(5) at the same rendering, and a
// float64 negative zero as -0, which reads back as int64(0), rendered 0.
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
		read := value.Read(v)
		switch n := read.(type) {
		case []any, map[string]any:
			return nil, fmt.Errorf("graph.ParseKey: component %d is not a scalar: %T", i, read)
		case json.Number:
			if immutable.IsIntegerLiteral(n) {
				return nil, fmt.Errorf("graph.ParseKey: component %d is an integer literal no int64 holds: %v", i, diag.CutValue(string(n)))
			}
			return nil, fmt.Errorf("graph.ParseKey: component %d is a number literal no finite float64 holds: %v", i, diag.CutValue(string(n)))
		}
		out[i] = read
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
