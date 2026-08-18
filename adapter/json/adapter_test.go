package json

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/location"
)

// parseOne parses one well-formed object through ParseObject, the surviving
// entry point, and returns its RawInstance.
func parseOne(t *testing.T, a *Adapter, source location.SourceID, data []byte) (instance.RawInstance, diag.Result) {
	t.Helper()
	const typeName = "Test"
	wrapped := fmt.Appendf(nil, `{%q: [%s]}`, typeName, data)
	result, diags := a.ParseObject(context.Background(), source, wrapped)
	insts := result[typeName]
	if len(insts) == 0 {
		return instance.RawInstance{}, diags
	}
	return insts[0], diags
}

// newAdapter constructs an adapter for a test.
func newAdapter(t *testing.T, opts ...Option) *Adapter {
	t.Helper()
	return New(opts...)
}

// mockRegistry implements location.PositionRegistry for testing.
func TestParseObject(t *testing.T) {
	source := location.NewSourceID("test://object")

	t.Run("single type with multiple instances", func(t *testing.T) {
		adapter := newAdapter(t)
		data := []byte(`{
			"Person": [
				{"name": "Alice", "age": 30},
				{"name": "Bob", "age": 25}
			]
		}`)

		result, diags := adapter.ParseObject(context.Background(), source, data)
		require.True(t, diags.OK(), "expected no errors: %v", diags)
		require.Len(t, result, 1)
		require.Len(t, result["Person"], 2)

		assert.Equal(t, "Alice", result["Person"][0].Properties["name"])
		assert.Equal(t, int64(30), result["Person"][0].Properties["age"])
		assert.Equal(t, "Bob", result["Person"][1].Properties["name"])
	})

	t.Run("multiple types", func(t *testing.T) {
		adapter := newAdapter(t)
		data := []byte(`{
			"Person": [{"name": "Alice"}],
			"Company": [{"title": "Acme Inc"}]
		}`)

		result, diags := adapter.ParseObject(context.Background(), source, data)
		require.True(t, diags.OK())
		require.Len(t, result, 2)
		require.Len(t, result["Person"], 1)
		require.Len(t, result["Company"], 1)
	})

	t.Run("empty object", func(t *testing.T) {
		adapter := newAdapter(t)
		result, diags := adapter.ParseObject(context.Background(), source, []byte(`{}`))
		require.True(t, diags.OK())
		assert.Empty(t, result)
	})

	t.Run("qualified type name", func(t *testing.T) {
		adapter := newAdapter(t)
		data := []byte(`{"common.Person": [{"name": "Alice"}]}`)

		result, diags := adapter.ParseObject(context.Background(), source, data)
		require.True(t, diags.OK())
		require.Len(t, result["common.Person"], 1)
	})

	t.Run("with jsonc comments", func(t *testing.T) {
		adapter := newAdapter(t) // jsonc enabled by default
		data := []byte(`{
			// This is a comment
			"Person": [
				{"name": "Alice"}, // trailing comment
			]
		}`)

		result, diags := adapter.ParseObject(context.Background(), source, data)
		require.True(t, diags.OK())
		require.Len(t, result["Person"], 1)
	})
}

// TestParseObject_Errors is the consolidated ParseObject error table. The
// wantTypes column pins the parse-continues-after-error contract: valid types
// must still be parsed after an invalid entry, and the decoder must stay
// synchronized after skipping a non-array value.
func TestParseObject_Errors(t *testing.T) {
	source := location.NewSourceID("test://object-errors")

	tests := []struct {
		name      string
		opts      []Option
		data      string
		wantTypes map[string]int // nil = no parsed-content requirement
	}{
		{name: "invalid JSON", data: `{invalid}`},
		{name: "array at root", data: `[1, 2, 3]`},
		{name: "truncated JSON", data: `{"Person": [{"name":`},
		{name: "invalid type name skipped", data: `{"person": [{"name": "Alice"}]}`, wantTypes: map[string]int{}},
		{
			name: "continues after invalid type", data: `{"person": [{"name": "Alice"}], "Person": [{"name": "Bob"}]}`,
			wantTypes: map[string]int{"Person": 1},
		},
		{name: "nested array with invalid element", data: `{"Person": [{"name": "Alice"}, {"name": "broken}]}`},
		{name: "array with non-object element", data: `{"Person": ["not an object"]}`},
		{name: "array with number element", data: `{"Person": [123]}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapter := newAdapter(t, tt.opts...)
			result, diags := adapter.ParseObject(context.Background(), source, []byte(tt.data))
			require.False(t, diags.OK(), "expected parse errors")
			for typeName, count := range tt.wantTypes {
				require.Len(t, result[typeName], count, "type %q", typeName)
			}
			if tt.wantTypes != nil && len(tt.wantTypes) == 0 {
				assert.Empty(t, result)
			}
		})
	}
}

// Regression tests for ParseObject desync when a type value is not an array:
// the decoder must skip the entire value to stay synchronized for subsequent
// type names.
func TestParseObject_NonArrayValues(t *testing.T) {
	source := location.NewSourceID("test://non-array")
	adapter := newAdapter(t)

	tests := []struct {
		name  string
		input string
	}{
		{"object instead of array", `{"Person": {"nested": "obj"}, "Company": [{"title": "Acme"}]}`},
		{"string instead of array", `{"Person": "not an array", "Company": [{"title": "Acme"}]}`},
		{"number instead of array", `{"Person": 123, "Company": [{"title": "Acme"}]}`},
		{"null instead of array", `{"Person": null, "Company": [{"title": "Acme"}]}`},
		{"boolean instead of array", `{"Person": true, "Company": [{"title": "Acme"}]}`},
		{"deeply nested object instead of array", `{"Person": {"a": {"b": {"c": "deep"}}}, "Company": [{"title": "Acme"}]}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, diags := adapter.ParseObject(context.Background(), source, []byte(tt.input))
			require.False(t, diags.OK(), "expected error for non-array value")
			require.Len(t, result["Company"], 1,
				"should have parsed valid type after skipping invalid value")
		})
	}
}

// Regression test: null array elements must be rejected as invalid objects
// while sibling valid elements still parse.
func TestNullRejection(t *testing.T) {
	source := location.NewSourceID("test://null")
	adapter := newAdapter(t)

	t.Run("ParseObject array with null elements", func(t *testing.T) {
		data := []byte(`{"Person": [null, {"name": "Alice"}, null]}`)
		result, diags := adapter.ParseObject(context.Background(), source, data)

		require.False(t, diags.OK(), "null elements in type array should be rejected")
		require.Len(t, result["Person"], 1)
	})
}

// Regression tests: trailing content after the root value must be rejected.
func TestTrailingContent(t *testing.T) {
	source := location.NewSourceID("test://trailing")
	adapter := newAdapter(t)

	cases := []struct {
		name  string
		parse func(data []byte) diag.Result
		data  string
	}{
		{"ParseObject trailing array", func(d []byte) diag.Result {
			_, r := adapter.ParseObject(context.Background(), source, d)
			return r
		}, `{"Person": []}[]`},
		{"ParseObject trailing object", func(d []byte) diag.Result {
			_, r := adapter.ParseObject(context.Background(), source, d)
			return r
		}, `{"Person": []} {"extra": 1}`},
		{"ParseObject trailing string", func(d []byte) diag.Result {
			_, r := adapter.ParseObject(context.Background(), source, d)
			return r
		}, `{"Person": []} "extra"`},
		{"ParseObject trailing number", func(d []byte) diag.Result {
			_, r := adapter.ParseObject(context.Background(), source, d)
			return r
		}, `{"Person": []} 123`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			diags := tc.parse([]byte(tc.data))
			require.False(t, diags.OK(), "trailing content should be rejected")
		})
	}
}

func TestNumericConversion(t *testing.T) {
	source := location.NewSourceID("test://numbers")
	adapter := newAdapter(t)

	t.Run("integers preserved as int64", func(t *testing.T) {
		data := []byte(`{"value": 42}`)
		result, diags := parseOne(t, adapter, source, data)
		require.True(t, diags.OK())

		val := result.Properties["value"]
		assert.IsType(t, int64(0), val)
		assert.Equal(t, int64(42), val)
	})

	t.Run("floats preserved as float64", func(t *testing.T) {
		data := []byte(`{"value": 3.14}`)
		result, diags := parseOne(t, adapter, source, data)
		require.True(t, diags.OK())

		val := result.Properties["value"]
		assert.IsType(t, float64(0), val)
		assert.Equal(t, 3.14, val)
	})

	t.Run("large integers", func(t *testing.T) {
		data := []byte(`{"value": 9223372036854775807}`) // MaxInt64
		result, diags := parseOne(t, adapter, source, data)
		require.True(t, diags.OK())

		assert.Equal(t, int64(9223372036854775807), result.Properties["value"])
	})

	t.Run("nested numeric values", func(t *testing.T) {
		data := []byte(`{
			"obj": {"count": 5},
			"arr": [1, 2.5, 3]
		}`)
		result, diags := parseOne(t, adapter, source, data)
		require.True(t, diags.OK())

		obj := result.Properties["obj"].(map[string]any)
		assert.Equal(t, int64(5), obj["count"])

		arr := result.Properties["arr"].([]any)
		assert.Equal(t, int64(1), arr[0])
		assert.Equal(t, 2.5, arr[1])
		assert.Equal(t, int64(3), arr[2])
	})
}

func TestEdgeCases(t *testing.T) {
	source := location.NewSourceID("test://edge")
	adapter := newAdapter(t)

	t.Run("unicode in values", func(t *testing.T) {
		data := []byte(`{"name": "日本語", "emoji": "🎉"}`)

		result, diags := parseOne(t, adapter, source, data)
		require.True(t, diags.OK())
		assert.Equal(t, "日本語", result.Properties["name"])
		assert.Equal(t, "🎉", result.Properties["emoji"])
	})

	t.Run("null values", func(t *testing.T) {
		data := []byte(`{"name": null}`)

		result, diags := parseOne(t, adapter, source, data)
		require.True(t, diags.OK())
		assert.Nil(t, result.Properties["name"])
	})

	t.Run("boolean values", func(t *testing.T) {
		data := []byte(`{"active": true, "deleted": false}`)

		result, diags := parseOne(t, adapter, source, data)
		require.True(t, diags.OK())
		assert.Equal(t, true, result.Properties["active"])
		assert.Equal(t, false, result.Properties["deleted"])
	})

	t.Run("empty string", func(t *testing.T) {
		data := []byte(`{"name": ""}`)

		result, diags := parseOne(t, adapter, source, data)
		require.True(t, diags.OK())
		assert.Empty(t, result.Properties["name"])
	})

	t.Run("empty object as value", func(t *testing.T) {
		data := []byte(`{"data": {}}`)

		result, diags := parseOne(t, adapter, source, data)
		require.True(t, diags.OK())

		obj, ok := result.Properties["data"].(map[string]any)
		require.True(t, ok)
		assert.Empty(t, obj)
	})

	t.Run("empty array as value", func(t *testing.T) {
		data := []byte(`{"items": []}`)

		result, diags := parseOne(t, adapter, source, data)
		require.True(t, diags.OK())

		arr, ok := result.Properties["items"].([]any)
		require.True(t, ok)
		assert.Empty(t, arr)
	})
}
