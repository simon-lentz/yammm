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
//     the precision loss that implies. FormatKey writes one for a whole float64
//     of magnitude 2^63 to below 1e21, on either sign, and for a uint64 above
//     math.MaxInt64.
//   - A literal that is valid JSON but has no finite Go value, such as 1e999,
//     is an error naming the component's index. FormatKey never writes one.
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

// unreadableComponent returns the index of the first component of k that
// [ParseKey] cannot read back as the value it is, or -1: one of no form every
// writer writes ([value.Scalar]: a struct, an array, a list or a map among
// them), a string that is not valid UTF-8, each invalid byte of which the
// rendering writes as U+FFFD, an empty json.Number, which renders as 0, or a
// number no finite float64 holds. A component whose rendering differs from its
// form's is refused too: the key's address renders the component through its
// type's methods, and every writer writes the form. So is a time.Time or a
// uuid.UUID whose key property's constraint does not render it, which no writer
// writes. A string, a bool, an int64 and a finite float64 settle without
// rendering. k has typ's key arity.
func unreadableComponent(typ *schema.Type, k immutable.Key) int {
	pks := typ.PrimaryKeysSlice()
	for i := range k.Len() {
		raw := k.Get(i).Unwrap()
		switch u := raw.(type) {
		case string:
			if !utf8.ValidString(u) {
				return i
			}
			continue
		case bool, int64, float64, nil:
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
		case json.Number:
			if u == "" {
				return i
			}
		case time.Time:
			if !keyRenders(pks[i].Constraint(), schema.KindTimestamp, schema.KindDate) {
				return i
			}
		case uuid.UUID:
			if !keyRenders(pks[i].Constraint(), schema.KindUUID) {
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
		if _, err := ParseKey("[" + string(b) + "]"); err != nil {
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
