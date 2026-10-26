package graph

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/internal/value"
	"github.com/simon-lentz/yammm/schema"
)

// FormatKey renders primary key components as the JSON array string the
// graph's addresses use: ["value1", value2, ...].
//
// It renders the values AS GIVEN and canonicalizes nothing; the graph does,
// at every address it receives. The graph indexes an instance by the key it
// carries, and for a Timestamp, Date or UUID key that is the canonical text
// the value was rewritten into at entry — so [Graph.AddComposed] and
// [Snapshot.InstanceByKey] canonicalize the address they are handed under the
// type's key constraints before the lookup, and FormatKey over any spelling
// of the instant addresses it. [Instance.PrimaryKey] is the address itself.
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
// The round-trip law holds over normalized components — a string that is
// valid UTF-8, int64, bool, nil, and non-whole float64: ParseKey(FormatKey(vs...))
// returns vs, and FormatKey(ParseKey(s)...) returns s for any s that FormatKey
// produced from such values. Two carve-outs are documented rather than hidden:
// FormatKey renders float64(5) as the literal 5, which ParseKey reads back as
// int64(5) at the same rendering, and a float64 negative zero as -0, which
// ParseKey reads back as int64(0), rendered 0. The snapshot wire has the same
// asymmetry, for the same reason.
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
			return nil, fmt.Errorf("graph.ParseKey: component %d is not a representable number: %v", i, diag.ValueText(fmt.Sprint(norm)))
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

// unreadableComponent returns the index of the first component of k that
// [ParseKey] cannot read back at the same rendering, or -1: one of no form every
// writer writes ([value.Scalar]: a struct, an array, a list or a map among
// them, as a json.Number that is no JSON number literal is), a string that is
// not valid UTF-8, each invalid byte of which the rendering writes as U+FFFD, a
// number no finite float64 holds, or a number ParseKey reads back at another
// rendering, as a float64 negative zero reads back as 0, whose address a
// reload would move. A component whose rendering differs from its form's is
// refused too: the key's address renders the component through its
// type's methods, and every writer writes the form. So is a time.Time or a
// uuid.UUID whose key property's constraint does not render it, which no writer
// writes; a keyless part's key has no key property, so a time or a UUID there
// is refused. A string, a bool and an int64 settle without rendering. k has typ's key arity when typ declares a key.
func unreadableComponent(typ *schema.Type, k immutable.Key) int {
	pks := typ.PrimaryKeysSlice()
	for i := range k.Len() {
		var c schema.Constraint
		if i < len(pks) {
			c = pks[i].Constraint()
		}
		raw := k.Get(i).Unwrap()
		switch u := raw.(type) {
		case string:
			if !utf8.ValidString(u) {
				return i
			}
			continue
		case bool, int64, nil:
			continue
		}
		held, ok := value.Scalar(raw)
		if !ok {
			return i
		}
		switch u := held.(type) {
		case string:
			if !utf8.ValidString(u) {
				return i
			}
		case time.Time:
			if !keyRenders(c, schema.KindTimestamp, schema.KindDate) {
				return i
			}
		case uuid.UUID:
			if !keyRenders(c, schema.KindUUID) {
				return i
			}
		}
		b, err := json.Marshal(held)
		if err != nil {
			return i
		}
		if r, err := json.Marshal(raw); err != nil || !bytes.Equal(r, b) {
			return i
		}
		// A component must render back unchanged, or a reload moves the address:
		// a uint64 or a json.Number past int64 reads back as the nearest float64,
		// which can render otherwise.
		rendered := "[" + string(b) + "]"
		if back, err := ParseKey(rendered); err != nil || FormatKey(back...) != rendered {
			return i
		}
	}
	return -1
}

// keyRenders reports whether c, resolved through its aliases, is of one of
// kinds: the kinds that render a time.Time or a uuid.UUID as text.
func keyRenders(c schema.Constraint, kinds ...schema.ConstraintKind) bool {
	return c != nil && slices.Contains(kinds, schema.ResolveAlias(c).Kind())
}
