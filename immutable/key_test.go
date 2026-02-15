package immutable

import (
	"encoding/json"
	"math"
	"slices"
	"strings"
	"testing"
)

func TestKey_WrapKey(t *testing.T) {
	input := []any{"us", 12345}

	k := WrapKey(input)

	if k.Len() != 2 {
		t.Errorf("expected Len() to be 2, got %d", k.Len())
	}

	first := k.Get(0)
	if s, ok := first.String(); !ok || s != "us" {
		t.Errorf("expected first component 'us', got %v", first.Unwrap())
	}

	second := k.Get(1)
	if n, ok := second.Int(); !ok || n != 12345 {
		t.Errorf("expected second component 12345, got %v", second.Unwrap())
	}
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
	// All zero/empty key forms should return "[]" to satisfy the invariant:
	// key.String() == graph.FormatKey(key.Clone()...)

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

func TestKey_WrapKeyClone_PanicsOnUnmarshalableValue(t *testing.T) {
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

	_ = WrapKeyClone([]any{make(chan int)})
}

func TestKey_SingleString(t *testing.T) {
	tests := []struct {
		name     string
		input    []any
		expected string
		ok       bool
	}{
		{
			name:     "single string",
			input:    []any{"hello"},
			expected: "hello",
			ok:       true,
		},
		{
			name:     "empty string",
			input:    []any{""},
			expected: "",
			ok:       true,
		},
		{
			name:     "not a string",
			input:    []any{42},
			expected: "",
			ok:       false,
		},
		{
			name:     "multiple components",
			input:    []any{"a", "b"},
			expected: "",
			ok:       false,
		},
		{
			name:     "empty key",
			input:    []any{},
			expected: "",
			ok:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k := WrapKey(tt.input)
			result, ok := k.SingleString()
			if ok != tt.ok {
				t.Errorf("expected ok=%v, got ok=%v", tt.ok, ok)
			}
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestKey_SingleInt(t *testing.T) {
	tests := []struct {
		name     string
		input    []any
		expected int64
		ok       bool
	}{
		{
			name:     "single int",
			input:    []any{42},
			expected: 42,
			ok:       true,
		},
		{
			name:     "single int64",
			input:    []any{int64(9999999999)},
			expected: 9999999999,
			ok:       true,
		},
		{
			name:     "whole float64",
			input:    []any{float64(42)},
			expected: 42,
			ok:       true,
		},
		{
			name:     "not an int",
			input:    []any{"hello"},
			expected: 0,
			ok:       false,
		},
		{
			name:     "multiple components",
			input:    []any{1, 2},
			expected: 0,
			ok:       false,
		},
		{
			name:     "empty key",
			input:    []any{},
			expected: 0,
			ok:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k := WrapKey(tt.input)
			result, ok := k.SingleInt()
			if ok != tt.ok {
				t.Errorf("expected ok=%v, got ok=%v", tt.ok, ok)
			}
			if result != tt.expected {
				t.Errorf("expected %d, got %d", tt.expected, result)
			}
		})
	}
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
	if s, ok := first.String(); !ok || s != "us" {
		t.Error("clone modification affected original")
	}
}

func TestKey_CloneNil(t *testing.T) {
	k := WrapKey(nil)
	cloned := k.Clone()

	if cloned != nil {
		t.Error("expected Clone() of nil key to return nil")
	}
}

func TestKey_WrapKeyClone_Isolation(t *testing.T) {
	input := []any{"original", 100}

	k := WrapKeyClone(input)

	// Mutate original
	input[0] = "mutated"

	// Wrapped key should be isolated
	first := k.Get(0)
	if s, ok := first.String(); !ok || s != "original" {
		t.Errorf("expected 'original', got %v", first.Unwrap())
	}
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
	if s, ok := nested.String(); !ok || s != "value" {
		t.Errorf("expected 'value', got %v", nested.Unwrap())
	}
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
	// A4: Verify iterating over literal zero-value Key{} handles gracefully
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
	// Note: For nil input, Clone() returns nil and json.Marshal(nil) = "null",
	// but FormatKey() with zero args should return "[]". This is tested separately
	// in TestKey_String_ZeroAndEmptyKey. Here we test non-nil inputs only.
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
