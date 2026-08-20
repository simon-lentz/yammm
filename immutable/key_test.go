package immutable

import (
	"encoding/json"
	"math"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestKey_WrapKey(t *testing.T) {
	input := []any{"us", 12345}

	k := WrapKey(input)

	if k.Len() != 2 {
		t.Errorf("expected Len() to be 2, got %d", k.Len())
	}

	first := k.Get(0)
	wantString(t, first, "us")

	second := k.Get(1)
	wantInt(t, second, 12345)
}

func TestKey_WrapNil(t *testing.T) {
	k := WrapKey(nil)

	if k.Len() != 0 {
		t.Errorf("expected Len() to be 0 for nil, got %d", k.Len())
	}
}

func TestKey_WrapEmpty(t *testing.T) {
	k := WrapKey([]any{})

	if k.Len() != 0 {
		t.Errorf("expected Len() to be 0 for empty, got %d", k.Len())
	}
}

func TestKey_Get_Panic(t *testing.T) {
	k := WrapKey([]any{"a"})

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for out-of-bounds access")
		}
	}()

	_ = k.Get(5) // Should panic
}

func TestKey_String_CanonicalFormat(t *testing.T) {
	tests := []struct {
		name     string
		input    []any
		expected string
	}{
		{
			name:     "composite key",
			input:    []any{"us", 12345},
			expected: `["us",12345]`,
		},
		{
			name:     "single string",
			input:    []any{"id123"},
			expected: `["id123"]`,
		},
		{
			name:     "single int",
			input:    []any{42},
			expected: `[42]`,
		},
		{
			name:     "empty",
			input:    []any{},
			expected: `[]`,
		},
		{
			name:     "mixed types",
			input:    []any{"region", 100, "subkey"},
			expected: `["region",100,"subkey"]`,
		},
		{
			name:     "with float",
			input:    []any{3.14},
			expected: `[3.14]`,
		},
		{
			name:     "with bool",
			input:    []any{true},
			expected: `[true]`,
		},
		{
			name:     "with null",
			input:    []any{nil},
			expected: `[null]`,
		},
		{
			name:     "special characters",
			input:    []any{"hello\"world", "tab\there"},
			expected: `["hello\"world","tab\there"]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k := WrapKey(tt.input)
			result := k.String()
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestKey_String_Precomputed(t *testing.T) {
	k := WrapKey([]any{"us", 12345})

	// String is precomputed at construction time; multiple calls return same value
	first := k.String()
	second := k.String()

	if first != second {
		t.Error("expected precomputed result to be identical")
	}
	if first != `["us",12345]` {
		t.Errorf("expected canonical JSON, got %q", first)
	}
}

func TestKey_String_ZeroAndEmptyKey(t *testing.T) {
	// All zero/empty key forms report "[]". This is where String and
	// graph.FormatKey(key.Clone()...) diverge rather than agree: Clone returns
	// nil for a nil-constructed key, and FormatKey renders a spread nil slice as
	// "null". The divergence is documented on Key.String.

	// Zero key via WrapKey(nil)
	zeroKey := WrapKey(nil)
	if zeroKey.String() != "[]" {
		t.Errorf("expected WrapKey(nil).String() to be \"[]\", got %q", zeroKey.String())
	}

	// Literal zero Key{}
	var literalZero Key
	if literalZero.String() != "[]" {
		t.Errorf("expected literal Key{}.String() to be \"[]\", got %q", literalZero.String())
	}

	// Empty key (empty slice input)
	emptyKey := WrapKey([]any{})
	if emptyKey.String() != "[]" {
		t.Errorf("expected WrapKey([]any{}).String() to be \"[]\", got %q", emptyKey.String())
	}

	// All should have Len() == 0
	if zeroKey.Len() != 0 {
		t.Errorf("expected WrapKey(nil).Len() to be 0, got %d", zeroKey.Len())
	}
	if literalZero.Len() != 0 {
		t.Errorf("expected literal Key{}.Len() to be 0, got %d", literalZero.Len())
	}
	if emptyKey.Len() != 0 {
		t.Errorf("expected WrapKey([]any{}).Len() to be 0, got %d", emptyKey.Len())
	}
}

func TestKey_WrapKey_PanicsOnUnmarshalableValue(t *testing.T) {
	tests := []struct {
		name  string
		input []any
	}{
		{
			name:  "channel",
			input: []any{make(chan int)},
		},
		{
			name:  "function",
			input: []any{func() {}},
		},
		{
			name:  "channel in nested slice",
			input: []any{[]any{"ok", make(chan string)}},
		},
		{
			name:  "function in nested map",
			input: []any{map[string]any{"fn": func() {}}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				r := recover()
				if r == nil {
					t.Error("expected panic for unmarshalable value")
					return
				}
				msg, ok := r.(string)
				if !ok {
					t.Errorf("expected panic message to be string, got %T", r)
					return
				}
				if !strings.Contains(msg, "immutable: key component is not JSON-marshalable") {
					t.Errorf("unexpected panic message: %s", msg)
				}
			}()

			_ = WrapKey(tt.input)
		})
	}
}

func TestKey_WrapKey_WithClone_PanicsOnUnmarshalableValue(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Error("expected panic for unmarshalable value")
			return
		}
		msg, ok := r.(string)
		if !ok {
			t.Errorf("expected panic message to be string, got %T", r)
			return
		}
		if !strings.Contains(msg, "immutable: key component is not JSON-marshalable") {
			t.Errorf("unexpected panic message: %s", msg)
		}
	}()

	_ = WrapKey([]any{make(chan int)}, WithClone())
}

func TestKey_Iter(t *testing.T) {
	input := []any{"a", 1, true}

	k := WrapKey(input)

	var values []any
	for v := range k.Iter() {
		values = append(values, v.Unwrap())
	}

	if len(values) != 3 {
		t.Errorf("expected 3 values, got %d", len(values))
	}
	if values[0] != "a" {
		t.Errorf("expected first value 'a', got %v", values[0])
	}
	if values[1] != 1 {
		t.Errorf("expected second value 1, got %v", values[1])
	}
	if values[2] != true {
		t.Errorf("expected third value true, got %v", values[2])
	}
}

func TestKey_Clone(t *testing.T) {
	input := []any{"us", 12345}

	k := WrapKey(input)
	cloned := k.Clone()

	if cloned == nil {
		t.Fatal("expected Clone() to return non-nil")
	}

	if len(cloned) != 2 {
		t.Errorf("expected 2 components in clone, got %d", len(cloned))
	}

	if cloned[0] != "us" {
		t.Errorf("expected first component 'us', got %v", cloned[0])
	}
	if cloned[1] != 12345 {
		t.Errorf("expected second component 12345, got %v", cloned[1])
	}

	// Verify clone is independent
	cloned[0] = "eu"

	first := k.Get(0)
	wantString(t, first, "us")
}

func TestKey_CloneNil(t *testing.T) {
	k := WrapKey(nil)
	cloned := k.Clone()

	if cloned != nil {
		t.Error("expected Clone() of nil key to return nil")
	}
}

func TestKey_WrapKey_WithClone_Isolation(t *testing.T) {
	input := []any{"original", 100}

	k := WrapKey(input, WithClone())

	// Mutate original
	input[0] = "mutated"

	// Wrapped key should be isolated
	first := k.Get(0)
	wantString(t, first, "original")
}

func TestKey_IteratorEarlyExit(t *testing.T) {
	input := []any{1, 2, 3, 4, 5}

	k := WrapKey(input)

	count := 0
	for range k.Iter() {
		count++
		if count == 2 {
			break
		}
	}
	if count != 2 {
		t.Errorf("expected early exit after 2, got %d", count)
	}
}

func TestKey_IteratorRepeatability(t *testing.T) {
	input := []any{"a", "b", "c"}

	k := WrapKey(input)

	// First iteration
	var first []string
	for v := range k.Iter() {
		s, _ := v.String()
		first = append(first, s)
	}

	// Second iteration
	var second []string
	for v := range k.Iter() {
		s, _ := v.String()
		second = append(second, s)
	}

	if !slices.Equal(first, second) {
		t.Errorf("expected same results, got %v and %v", first, second)
	}
}

func TestKey_NestedValues(t *testing.T) {
	// Keys typically don't have nested values, but the type should handle it
	input := []any{
		map[string]any{"nested": "value"},
	}

	k := WrapKey(input)

	first := k.Get(0)
	m, ok := first.Map()
	if !ok {
		t.Fatal("expected first component to be Map")
	}

	nested, ok := m.Get("nested")
	if !ok {
		t.Fatal("expected nested key")
	}
	wantString(t, nested, "value")
}

func TestKey_String_WithNestedStructures(t *testing.T) {
	// Test Key.String() with nested maps and slices to cover unwrapForJSON paths
	tests := []struct {
		name     string
		input    []any
		expected string
	}{
		{
			name:     "with nested map",
			input:    []any{map[string]any{"a": 1}},
			expected: `[{"a":1}]`,
		},
		{
			name:     "with nested slice",
			input:    []any{[]any{1, 2}},
			expected: `[[1,2]]`,
		},
		{
			name:     "with deeply nested",
			input:    []any{map[string]any{"list": []any{"x", "y"}}},
			expected: `[{"list":["x","y"]}]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k := WrapKey(tt.input)
			result := k.String()
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestKey_Iter_ZeroValue(t *testing.T) {
	// Iterating over a zero-value Key{} must not panic.
	var k Key

	count := 0
	for range k.Iter() {
		count++
	}

	if count != 0 {
		t.Errorf("expected 0 iterations for zero-value Key{}, got %d", count)
	}
}

func TestKey_WrapKey_PanicsOnNaNInf(t *testing.T) {
	// Tests that NaN and Inf values cause panics since they cannot be JSON-marshaled.
	tests := []struct {
		name  string
		input []any
	}{
		{
			name:  "NaN",
			input: []any{math.NaN()},
		},
		{
			name:  "positive infinity",
			input: []any{math.Inf(1)},
		},
		{
			name:  "negative infinity",
			input: []any{math.Inf(-1)},
		},
		{
			name:  "NaN in nested slice",
			input: []any{[]any{"ok", math.NaN()}},
		},
		{
			name:  "Inf in nested map",
			input: []any{map[string]any{"value": math.Inf(1)}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				r := recover()
				if r == nil {
					t.Error("expected panic for non-JSON-marshalable value")
					return
				}
				msg, ok := r.(string)
				if !ok {
					t.Errorf("expected panic message to be string, got %T", r)
					return
				}
				if !strings.Contains(msg, "immutable: key component is not JSON-marshalable") {
					t.Errorf("unexpected panic message: %s", msg)
				}
			}()

			_ = WrapKey(tt.input)
		})
	}
}

func TestKey_WrapKey_PanicsOnNonStringKeyedMap(t *testing.T) {
	// Tests that maps with non-string/non-int keys cause panics since they cannot be JSON-marshaled.
	// Note: map[int]string works because json.Marshal converts int keys to strings.
	// But map[struct{}]string fails because struct keys cannot be converted.
	type customKey struct{ x int }

	defer func() {
		r := recover()
		if r == nil {
			t.Error("expected panic for non-string-keyed map")
			return
		}
		msg, ok := r.(string)
		if !ok {
			t.Errorf("expected panic message to be string, got %T", r)
			return
		}
		if !strings.Contains(msg, "immutable: key component is not JSON-marshalable") {
			t.Errorf("unexpected panic message: %s", msg)
		}
	}()

	// Map with struct keys cannot be JSON-marshaled
	_ = WrapKey([]any{map[customKey]string{{1}: "one", {2}: "two"}})
}

func TestKey_String_MatchesJSONMarshal(t *testing.T) {
	// This test defends the architecture spec's invariant:
	// "Key.String() returns the same canonical JSON array format as graph.FormatKey()"
	// We verify Key.String() == json.Marshal(key.Clone())
	// which is the same underlying mechanism FormatKey will use.
	//
	// Note: for nil input Clone() returns nil and json.Marshal(nil) is "null",
	// which is what graph.FormatKey renders for it too. String reports "[]"
	// instead; TestKey_String_ZeroAndEmptyKey pins that side. Here we test
	// non-nil inputs only.
	tests := [][]any{
		{"us", 12345},
		{"id123"},
		{42},
		{},
		{"region", 100, "subkey"},
		{3.14},
		{true},
		{nil}, // []any{nil} - a key with one nil component, not WrapKey(nil)
		{"hello\"world", "tab\there"},
		{map[string]any{"a": 1}},
		{[]any{1, 2}},
		{map[string]any{"list": []any{"x", "y"}}},
	}

	for _, input := range tests {
		k := WrapKey(input)
		clone := k.Clone()
		expected, err := json.Marshal(clone)
		if err != nil {
			t.Fatalf("json.Marshal failed for %v: %v", input, err)
		}
		if got := k.String(); got != string(expected) {
			t.Errorf("input %v: Key.String() = %q, json.Marshal(Clone()) = %q", input, got, string(expected))
		}
	}
}

// A key component renders through json.Marshal, which hands a time.Time to
// time.Time.MarshalJSON. The two representations of one instant therefore agree
// only where the string is already in Go's canonical spelling: a zero offset
// renders "Z", and fractional seconds drop their trailing zeros.
func TestKey_String_TimeAgreesWithItsStringFormOnlyWhenCanonical(t *testing.T) {
	tests := []struct {
		spelling string
		agrees   bool
	}{
		{"2020-01-02T03:04:05Z", true},
		{"2020-01-02T03:04:05+02:00", true},
		{"2020-01-02T03:04:05+00:00", false},
		{"2020-01-02T03:04:05.500Z", false},
		{"2020-01-02T03:04:05.000Z", false},
	}

	for _, tt := range tests {
		t.Run(tt.spelling, func(t *testing.T) {
			parsed, err := time.Parse(time.RFC3339, tt.spelling)
			if err != nil {
				t.Fatalf("parse %q: %v", tt.spelling, err)
			}
			fromString := WrapKey([]any{tt.spelling}).String()
			fromTime := WrapKey([]any{parsed}).String()
			if agreed := fromString == fromTime; agreed != tt.agrees {
				t.Errorf("string key %s, time.Time key %s: agreed=%v, want %v",
					fromString, fromTime, agreed, tt.agrees)
			}
		})
	}
}

// The rendering a time.Time component takes is exactly RFC3339Nano, which is
// what makes it predictable rather than merely observed.
func TestKey_String_TimeRendersAsRFC3339Nano(t *testing.T) {
	ts := time.Date(2020, 1, 2, 3, 4, 5, 500000000, time.UTC)
	got := WrapKey([]any{ts}).String()
	want := `["` + ts.Format(time.RFC3339Nano) + `"]`
	if got != want {
		t.Errorf("Key.String() = %s, want %s", got, want)
	}
}
