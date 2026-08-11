package immutable

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
)

// NormalizeNumber converts a json.Number to the appropriate Go numeric type.
//
// The conversion applies a float-indicator heuristic: if the number string
// contains '.', 'e', or 'E', it is treated as float64; otherwise as int64.
// This correctly classifies scientific notation like "1e2" as float64 (its
// JSON representation uses exponent notation, even though its mathematical
// value is an integer).
//
// Fallback chain for integer-form strings (no '.', 'e', 'E'):
//  1. strconv.ParseInt(s, 10, 64) — succeeds for values in int64 range
//  2. strconv.ParseFloat(s, 64) — fallback for values exceeding int64 range
//     (e.g., "99999999999999999999"); precision may be lost but the value is
//     representable
//  3. Returns the original json.Number unchanged if both parsers fail
//     (malformed number string)
//
// Classification is by lexical form alone: a float indicator ('.', 'e', 'E')
// means float64, an int-shaped literal means int64 — the reader sees only the
// text, never a schema. A writer that wants a whole float to survive the round
// trip therefore has to emit the indicator itself, as snapshot.Marshal does for
// KindFloat values; a writer that does not (adapter/json) round-trips such a
// value as int64. The Value typed accessors (Int(), Float()) read both
// representations transparently.
func NormalizeNumber(n json.Number) any {
	s := n.String()

	// Float indicator: '.', 'e', or 'E' means float64.
	if strings.ContainsAny(s, ".eE") {
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

// NormalizeValue recursively normalizes json.Number values within arbitrary
// Go values. It walks map[string]any, []any, and scalar positions, applying
// NormalizeNumber to each json.Number encountered. Non-json.Number values
// are returned unchanged.
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
