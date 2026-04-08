package immutable

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizeNumber(t *testing.T) {
	tests := []struct {
		name string
		num  json.Number
		want any
	}{
		// Integer strings
		{name: "positive integer", num: json.Number("42"), want: int64(42)},
		{name: "zero", num: json.Number("0"), want: int64(0)},
		{name: "negative integer", num: json.Number("-1"), want: int64(-1)},
		{name: "large positive", num: json.Number("1000000"), want: int64(1_000_000)},

		// Float strings with decimal point
		{name: "simple float", num: json.Number("3.14"), want: float64(3.14)},
		{name: "negative float", num: json.Number("-2.5"), want: float64(-2.5)},
		{name: "trailing zero", num: json.Number("1.0"), want: float64(1.0)},

		// Scientific notation — the bug fix
		{name: "scientific lowercase e", num: json.Number("1e2"), want: float64(100)},
		{name: "scientific uppercase E", num: json.Number("1E2"), want: float64(100)},
		{name: "scientific with decimal", num: json.Number("1.5e2"), want: float64(150)},
		{name: "scientific negative exponent", num: json.Number("1.5E-3"), want: float64(0.0015)},
		{name: "scientific negative base", num: json.Number("-2.5e1"), want: float64(-25)},
		{name: "scientific zero exponent", num: json.Number("5e0"), want: float64(5)},

		// int64 boundary
		{name: "max int64", num: json.Number("9223372036854775807"), want: int64(math.MaxInt64)},
		{name: "min int64", num: json.Number("-9223372036854775808"), want: int64(math.MinInt64)},

		// int64 overflow → float64 fallback
		{name: "int64 overflow positive", num: json.Number("9223372036854775808"), want: float64(9.223372036854776e18)},
		{name: "int64 overflow large", num: json.Number("99999999999999999999"), want: float64(1e20)},

		// Invalid → return original json.Number
		{name: "NaN", num: json.Number("NaN"), want: json.Number("NaN")},
		{name: "Infinity", num: json.Number("Infinity"), want: json.Number("Infinity")},
		{name: "empty string", num: json.Number(""), want: json.Number("")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeNumber(tt.num)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestNormalizeValue_Scalars(t *testing.T) {
	// json.Number values are normalized
	assert.Equal(t, int64(42), NormalizeValue(json.Number("42")))
	assert.Equal(t, float64(3.14), NormalizeValue(json.Number("3.14")))
	assert.Equal(t, float64(100), NormalizeValue(json.Number("1e2")))

	// Non-json.Number values pass through unchanged
	assert.Equal(t, "hello", NormalizeValue("hello"))
	assert.Equal(t, true, NormalizeValue(true))
	assert.Nil(t, NormalizeValue(nil))
	assert.Equal(t, int64(7), NormalizeValue(int64(7)))
}

func TestNormalizeValue_Map(t *testing.T) {
	m := map[string]any{
		"count": json.Number("5"),
		"rate":  json.Number("1.5e2"),
		"name":  "test",
	}
	got := NormalizeValue(m)
	expected := map[string]any{
		"count": int64(5),
		"rate":  float64(150),
		"name":  "test",
	}
	assert.Equal(t, expected, got)
}

func TestNormalizeValue_Slice(t *testing.T) {
	s := []any{json.Number("1"), json.Number("2.5"), "text"}
	got := NormalizeValue(s)
	expected := []any{int64(1), float64(2.5), "text"}
	assert.Equal(t, expected, got)
}

func TestNormalizeValue_Nested(t *testing.T) {
	v := map[string]any{
		"outer": map[string]any{
			"inner": []any{
				json.Number("42"),
				map[string]any{
					"deep": json.Number("3e2"),
				},
			},
		},
	}
	got := NormalizeValue(v)
	expected := map[string]any{
		"outer": map[string]any{
			"inner": []any{
				int64(42),
				map[string]any{
					"deep": float64(300),
				},
			},
		},
	}
	assert.Equal(t, expected, got)
}

func TestNormalizeValue_DepthGuard(t *testing.T) {
	// Build a structure deeper than normalizeMaxDepth (64)
	var deepValue any = json.Number("99")
	for i := range 65 {
		_ = i
		deepValue = map[string]any{"nested": deepValue}
	}

	// Should not panic
	got := NormalizeValue(deepValue)

	// Walk down to verify: the outermost 64 levels are traversed,
	// but the json.Number at depth 65 passes through as-is
	current := got
	for range 65 {
		m, ok := current.(map[string]any)
		if !ok {
			t.Fatal("expected map[string]any at traversal level")
		}
		current = m["nested"]
	}
	// At depth 65, the json.Number should be unchanged (not normalized)
	assert.Equal(t, json.Number("99"), current)
}

func TestNormalizeValue_DepthGuard_NormalizesWithinLimit(t *testing.T) {
	// Build a structure exactly at normalizeMaxDepth (64)
	var deepValue any = json.Number("42")
	for i := range 64 {
		_ = i
		deepValue = map[string]any{"n": deepValue}
	}

	got := NormalizeValue(deepValue)

	// Walk down 64 levels — the json.Number at depth 64 should be normalized
	current := got
	for range 64 {
		m, ok := current.(map[string]any)
		if !ok {
			t.Fatal("expected map[string]any at traversal level")
		}
		current = m["n"]
	}
	assert.Equal(t, int64(42), current)
}
