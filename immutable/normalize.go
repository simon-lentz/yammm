package immutable

import (
	"encoding/json"
	"strconv"
	"strings"
)

// NormalizeNumber converts a json.Number to the appropriate Go numeric type.
//
// The conversion applies the lexical rule [IsIntegerLiteral] states: a number
// string containing '.', 'e', or 'E' is treated as float64; otherwise as int64.
// This correctly classifies scientific notation like "1e2" as float64 (its
// JSON representation uses exponent notation, even though its mathematical
// value is an integer). Text that is no JSON number literal ([IsNumberLiteral]),
// and a float literal no finite float64 holds (e.g., "1e400"), is returned
// unchanged.
//
// Fallback chain for an integer literal:
//  1. strconv.ParseInt(s, 10, 64) — succeeds for values in int64 range
//  2. strconv.ParseFloat(s, 64) — fallback for values exceeding int64 range
//     (e.g., "99999999999999999999"); precision may be lost but the value is
//     representable
//  3. Returns the original json.Number unchanged if no float64 is finite for
//     it (e.g., a 400-digit integer)
//
// Classification is by lexical form, within the fallbacks above: a float
// indicator ('.', 'e', 'E') means float64, an int-shaped literal means int64 —
// the reader sees only the text, never a schema. A writer that wants a whole
// float to survive the round trip therefore has to emit the indicator itself,
// as snapshot.Marshal and adapter/json both do for every float in a property
// value, at any depth of its lists and string-keyed maps, a float behind a
// pointer or in a named type included. The Value typed accessors (Int(),
// Float()) read both representations transparently.
func NormalizeNumber(n json.Number) any {
	s := n.String()
	if !IsNumberLiteral(n) {
		return n
	}

	if !IsIntegerLiteral(n) {
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			return f
		}
		return n
	}

	// Integer path: try int64 first, float64 fallback for overflow.
	if i, err := strconv.ParseInt(s, 10, 64); err == nil {
		return i
	}
	// A JSON number literal parses to an infinity only beside ErrRange.
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f
	}
	return n
}

// IsIntegerLiteral reports whether n is spelled as an integer: it carries no
// float indicator ('.', 'e' or 'E'). It judges the spelling alone, not the
// syntax or the range. It is the lexical rule every reader of a data document
// applies: an Integer value is an integer literal, and a decimal or exponent
// literal is a float even when its value is whole.
func IsIntegerLiteral(n json.Number) bool {
	return !strings.ContainsAny(string(n), ".eE")
}

// IsNumberLiteral reports whether n is a JSON number literal (RFC 8259,
// section 6), -? (0 | [1-9][0-9]*) (.[0-9]+)? ([eE][+-]?[0-9]+)?, with nothing
// around it: the text encoding/json decodes as a number and writes as one. A
// json.Number holding any other text, which only a Go caller builds, is no
// number to any reader or writer of the module.
func IsNumberLiteral(n json.Number) bool {
	s := string(n)
	i := 0
	if i < len(s) && s[i] == '-' {
		i++
	}
	switch {
	case i < len(s) && s[i] == '0':
		i++
	case i < len(s) && s[i] >= '1' && s[i] <= '9':
		i = digitsFrom(s, i)
	default:
		return false
	}
	if i < len(s) && s[i] == '.' {
		j := digitsFrom(s, i+1)
		if j == i+1 {
			return false
		}
		i = j
	}
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		i++
		if i < len(s) && (s[i] == '+' || s[i] == '-') {
			i++
		}
		j := digitsFrom(s, i)
		if j == i {
			return false
		}
		i = j
	}
	return i == len(s)
}

// digitsFrom returns the index past the run of ASCII digits starting at i.
func digitsFrom(s string, i int) int {
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	return i
}

// NormalizeValue recursively normalizes json.Number values within arbitrary
// Go values. It walks map[string]any, []any, and scalar positions, applying
// NormalizeNumber to each json.Number encountered. It rewrites each
// map[string]any and []any in place and returns the same container, so a
// caller must own what it passes. Non-json.Number scalars are returned
// unchanged.
//
// NormalizeValue enforces a maximum recursion depth of 64 levels. If the
// depth limit is exceeded, NormalizeValue returns the value unnormalized at
// that level (json.Number values below the limit are normalized; values
// beyond the limit pass through as-is). This prevents stack overflow from
// maliciously crafted .ys files with deeply nested property values.
//
// Note: this depth limit (64) is distinct from the composed nesting depth
// limit of 32 used by snapshot.Load for structural nesting. Composed nesting
// is structural (schema-driven, practically never deep); property value
// nesting is JSON-driven (theoretically unbounded, practically shallow,
// defensively capped at a higher limit).
func NormalizeValue(v any) any {
	return normalizeValue(v, 0)
}

const normalizeMaxDepth = 64

func normalizeValue(v any, depth int) any {
	if depth > normalizeMaxDepth {
		return v
	}
	switch val := v.(type) {
	case json.Number:
		return NormalizeNumber(val)
	case map[string]any:
		for k, elem := range val {
			val[k] = normalizeValue(elem, depth+1)
		}
		return val
	case []any:
		for i, elem := range val {
			val[i] = normalizeValue(elem, depth+1)
		}
		return val
	default:
		return v
	}
}
