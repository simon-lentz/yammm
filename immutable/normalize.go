package immutable

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
)

// NormalizeNumber converts a json.Number to the appropriate Go numeric type.
//
// The conversion applies the lexical rule [IsIntegerLiteral] states: a number
// string containing '.', 'e', or 'E' is treated as float64; otherwise as int64.
// This correctly classifies scientific notation like "1e2" as float64 (its
// JSON representation uses exponent notation, even though its mathematical
// value is an integer). A float-form string that is malformed or has no finite
// float64 value (e.g., "1e400") is returned unchanged.
//
// Malformed means strconv's syntax, which is wider than JSON's, and both paths
// use it. Step 2 of the integer-form chain below therefore accepts more than an
// out-of-range integer: a hexadecimal float ("0x1p4" is 16) and '_' between
// digits ("1_000" is 1000) each reach it after ParseInt refuses them, and each
// yields a float64 rather than the int64 the form suggests. A leading '+' is
// read on either path. A hexadecimal literal whose exponent letter is 'e'
// ("0x1e4") takes the float path, which refuses it. encoding/json never
// produces such a Number, so only a caller that builds one meets them.
//
// Fallback chain for integer-form strings (no '.', 'e', 'E'):
//  1. strconv.ParseInt(s, 10, 64) — succeeds for values in int64 range
//  2. strconv.ParseFloat(s, 64) — fallback for values exceeding int64 range
//     (e.g., "99999999999999999999"); precision may be lost but the value is
//     representable
//  3. Returns the original json.Number unchanged if neither yields a finite
//     value: the string is malformed, or no float64 holds it (e.g., a
//     400-digit integer)
//
// Classification is by lexical form, within the fallbacks above: a float
// indicator ('.', 'e', 'E') means float64, an int-shaped literal means int64 —
// the reader sees only the text, never a schema. A writer that wants a whole
// float to survive the round trip therefore has to emit the indicator itself,
// as snapshot.Marshal and adapter/json both do for every float in a property
// value, at any depth of its lists and string-keyed maps. The Value
// typed accessors (Int(), Float()) read both representations transparently.
func NormalizeNumber(n json.Number) any {
	s := n.String()

	if !IsIntegerLiteral(n) {
		if f, err := strconv.ParseFloat(s, 64); err == nil && !math.IsNaN(f) && !math.IsInf(f, 0) {
			return f
		}
		return n // malformed or non-finite
	}

	// Integer path: try int64 first, float64 fallback for overflow.
	if i, err := strconv.ParseInt(s, 10, 64); err == nil {
		return i
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil && !math.IsNaN(f) && !math.IsInf(f, 0) {
		return f
	}
	return n // malformed or non-finite
}

// IsIntegerLiteral reports whether n is spelled as an integer: it carries no
// float indicator ('.', 'e' or 'E'). It judges the spelling alone, not the
// syntax or the range. It is the lexical rule every reader of a data document
// applies: an Integer value is an integer literal, and a decimal or exponent
// literal is a float even when its value is whole.
func IsIntegerLiteral(n json.Number) bool {
	return !strings.ContainsAny(string(n), ".eE")
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
