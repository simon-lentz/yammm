package json

import (
	"context"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/location"
)

// newAdapter constructs an adapter, failing the test on any option error —
// a silently-discarded constructor error cannot masquerade as a parse result.
func newAdapter(t *testing.T, opts ...Option) *Adapter {
	t.Helper()
	adapter, err := New(nil, opts...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return adapter
}

// mockRegistry implements location.PositionRegistry for testing.
type mockRegistry struct {
	positions map[location.SourceID]map[int]location.Position
}

func newMockRegistry() *mockRegistry {
	return &mockRegistry{
		positions: make(map[location.SourceID]map[int]location.Position),
	}
}

func (m *mockRegistry) register(source location.SourceID, byteOffset int, pos location.Position) {
	if m.positions[source] == nil {
		m.positions[source] = make(map[int]location.Position)
	}
	m.positions[source][byteOffset] = pos
}

func (m *mockRegistry) PositionAt(source location.SourceID, byteOffset int) location.Position {
	if byteOffset < 0 {
		return location.Position{}
	}
	positions, ok := m.positions[source]
	if !ok {
		return location.Position{}
	}
	pos, ok := positions[byteOffset]
	if !ok {
		return location.Position{}
	}
	return pos
}

func TestNew(t *testing.T) {
	t.Run("nil registry without tracking", func(t *testing.T) {
		adapter, err := New(nil)
		require.NoError(t, err)
		assert.NotNil(t, adapter)
	})

	t.Run("nil registry with tracking returns error", func(t *testing.T) {
		_, err := New(nil, WithTrackLocations(true))
		require.Error(t, err)
		assert.Equal(t, ErrNilRegistry, err)
	})

	t.Run("valid registry with tracking", func(t *testing.T) {
		reg := newMockRegistry()
		adapter, err := New(reg, WithTrackLocations(true))
		require.NoError(t, err)
		assert.NotNil(t, adapter)
	})

	t.Run("custom type field", func(t *testing.T) {
		adapter, err := New(nil, WithTypeField("_type"))
		require.NoError(t, err)
		assert.NotNil(t, adapter)
	})

	t.Run("empty type field returns error", func(t *testing.T) {
		_, err := New(nil, WithTypeField(""))
		require.Error(t, err)
		assert.Equal(t, ErrEmptyTypeField, err)
	})
}

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
			name: "strict JSON rejects comments", opts: []Option{WithStrictJSON(true)},
			data: "{\n// comment\n\"Person\": [{\"name\": \"Alice\"}]}",
		},
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

func TestParseArray(t *testing.T) {
	source := location.NewSourceID("test://array")

	t.Run("mixed types", func(t *testing.T) {
		adapter := newAdapter(t)
		data := []byte(`[
			{"$type": "Person", "name": "Alice"},
			{"$type": "Company", "title": "Acme Inc"},
			{"$type": "Person", "name": "Bob"}
		]`)

		result, diags := adapter.ParseArray(context.Background(), source, data)
		require.True(t, diags.OK())
		require.Len(t, result["Person"], 2)
		require.Len(t, result["Company"], 1)

		// $type field should be removed from properties
		_, hasType := result["Person"][0].Properties["$type"]
		assert.False(t, hasType, "$type should be removed from properties")
	})

	t.Run("custom type field", func(t *testing.T) {
		adapter := newAdapter(t, WithTypeField("_type"))
		data := []byte(`[{"_type": "Person", "name": "Alice"}]`)

		result, diags := adapter.ParseArray(context.Background(), source, data)
		require.True(t, diags.OK())
		require.Len(t, result["Person"], 1)

		_, hasType := result["Person"][0].Properties["_type"]
		assert.False(t, hasType)
	})

	t.Run("empty array", func(t *testing.T) {
		adapter := newAdapter(t)
		result, diags := adapter.ParseArray(context.Background(), source, []byte(`[]`))
		require.True(t, diags.OK())
		assert.Empty(t, result)
	})
}

// TestParseArray_Errors is the consolidated ParseArray error table, including
// the type-tag zoo: every non-string JSON value in the type-tag position must
// be rejected while remaining elements still parse.
func TestParseArray_Errors(t *testing.T) {
	source := location.NewSourceID("test://array-errors")

	tests := []struct {
		name      string
		data      string
		wantTypes map[string]int
	}{
		{name: "object at root", data: `{"Person": []}`},
		{name: "malformed JSON", data: `[{"name": }]`},
		{name: "truncated JSON", data: `[{"$type": "Person"`},
		{name: "missing type field", data: `[{"name": "Alice"}]`},
		{name: "invalid type name syntax", data: `[{"$type": "person", "name": "Alice"}]`},
		{name: "empty type tag", data: `[{"$type": "", "name": "Test"}]`},
		{name: "null type tag", data: `[{"$type": null, "name": "Test"}]`},
		{name: "number type tag", data: `[{"$type": 123, "name": "Test"}]`},
		{name: "object type tag", data: `[{"$type": {"x": 1}, "name": "Test"}]`},
		{name: "array type tag", data: `[{"$type": ["Person"], "name": "Test"}]`},
		{name: "boolean type tag", data: `[{"$type": true, "name": "Test"}]`},
		{name: "continues after invalid type", data: `[
			{"$type": "person", "name": "Alice"},
			{"$type": "Person", "name": "Bob"},
			{"$type": "Person", "name": "Charlie"}
		]`, wantTypes: map[string]int{"Person": 2}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapter := newAdapter(t)
			result, diags := adapter.ParseArray(context.Background(), source, []byte(tt.data))
			require.False(t, diags.OK(), "expected parse errors")
			for typeName, count := range tt.wantTypes {
				require.Len(t, result[typeName], count, "type %q", typeName)
			}
		})
	}
}

// TestParseArray_ErrorDetails verifies the structured detail keys on a
// type-tag error when tracking is disabled (the non-tracking branch of the
// error helpers).
func TestParseArray_ErrorDetails(t *testing.T) {
	source := location.NewSourceID("test://details")
	adapter := newAdapter(t)

	data := []byte(`[{"$type": 123, "name": "Test"}]`)
	_, diags := adapter.ParseArray(context.Background(), source, data)

	require.False(t, diags.OK())
	issues := slices.Collect(diags.Issues())
	require.NotEmpty(t, issues)

	issue := issues[0]
	assert.Equal(t, location.Span{}, issue.Span(), "Should have empty span without tracking")

	var hasDetail, hasGot bool
	for _, d := range issue.Details() {
		if d.Key == "detail" {
			hasDetail = true
		}
		if d.Key == "got" {
			hasGot = true
		}
	}
	assert.True(t, hasDetail, "Should have 'detail' key")
	assert.True(t, hasGot, "Should have 'got' key")
}

func TestParseTypedArray(t *testing.T) {
	source := location.NewSourceID("test://typed")

	t.Run("basic usage", func(t *testing.T) {
		adapter := newAdapter(t)
		data := []byte(`[
			{"name": "Alice", "age": 30},
			{"name": "Bob", "age": 25}
		]`)

		result, diags := adapter.ParseTypedArray(context.Background(), source, "Person", data)
		require.True(t, diags.OK())
		require.Len(t, result, 2)

		assert.Equal(t, "Alice", result[0].Properties["name"])
		assert.Equal(t, int64(30), result[0].Properties["age"])
	})

	t.Run("empty array", func(t *testing.T) {
		adapter := newAdapter(t)
		result, diags := adapter.ParseTypedArray(context.Background(), source, "Person", []byte(`[]`))
		require.True(t, diags.OK())
		assert.Empty(t, result)
	})
}

// TestParseTypedArray_Errors is the consolidated ParseTypedArray error table.
func TestParseTypedArray_Errors(t *testing.T) {
	source := location.NewSourceID("test://typed-errors")

	tests := []struct {
		name     string
		typeName string
		data     string
	}{
		{name: "invalid type name", typeName: "person", data: `[{"name": "Alice"}]`},
		{name: "malformed JSON", typeName: "Person", data: `[{"name": }]`},
		{name: "truncated JSON", typeName: "Person", data: `[{"name": "Alice"`},
		{name: "object at root", typeName: "Person", data: `{"name": "Alice"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapter := newAdapter(t)
			_, diags := adapter.ParseTypedArray(context.Background(), source, tt.typeName, []byte(tt.data))
			require.False(t, diags.OK(), "expected parse errors")
		})
	}
}

func TestParseOne(t *testing.T) {
	source := location.NewSourceID("test://one")

	t.Run("basic usage", func(t *testing.T) {
		adapter := newAdapter(t)
		data := []byte(`{"name": "Alice", "age": 30}`)

		result, diags := adapter.ParseOne(context.Background(), source, "Person", data)
		require.True(t, diags.OK())

		assert.Equal(t, "Alice", result.Properties["name"])
		assert.Equal(t, int64(30), result.Properties["age"])
	})

	t.Run("nested objects", func(t *testing.T) {
		adapter := newAdapter(t)
		data := []byte(`{
			"name": "Alice",
			"address": {"city": "NYC", "zip": "10001"}
		}`)

		result, diags := adapter.ParseOne(context.Background(), source, "Person", data)
		require.True(t, diags.OK())

		address, ok := result.Properties["address"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "NYC", address["city"])
	})
}

// TestParseOne_Errors is the consolidated ParseOne error table.
func TestParseOne_Errors(t *testing.T) {
	source := location.NewSourceID("test://one-errors")

	tests := []struct {
		name     string
		typeName string
		data     string
	}{
		{name: "invalid type name", typeName: "person", data: `{"name": "Alice"}`},
		{name: "invalid JSON", typeName: "Person", data: `{invalid}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapter := newAdapter(t)
			_, diags := adapter.ParseOne(context.Background(), source, tt.typeName, []byte(tt.data))
			require.False(t, diags.OK(), "expected parse errors")
		})
	}
}

// Regression tests: null values must be rejected as invalid objects on every
// entry point, while sibling valid elements still parse.
func TestNullRejection(t *testing.T) {
	source := location.NewSourceID("test://null")
	adapter := newAdapter(t)

	t.Run("ParseOne rejects null root", func(t *testing.T) {
		_, diags := adapter.ParseOne(context.Background(), source, "Person", []byte(`null`))

		require.False(t, diags.OK(), "null root should be rejected")
		issues := slices.Collect(diags.Issues())
		require.NotEmpty(t, issues)
		assert.Contains(t, issues[0].Message(), "expected object")
	})

	t.Run("ParseTypedArray rejects null elements", func(t *testing.T) {
		data := []byte(`[null, {"name": "Alice"}, null]`)
		result, diags := adapter.ParseTypedArray(context.Background(), source, "Person", data)

		require.False(t, diags.OK(), "null array elements should be rejected")
		require.Len(t, result, 1)
		assert.Equal(t, "Alice", result[0].Properties["name"])
	})

	t.Run("ParseArray rejects null elements", func(t *testing.T) {
		data := []byte(`[null, {"$type": "Person", "name": "Alice"}]`)
		result, diags := adapter.ParseArray(context.Background(), source, data)

		require.False(t, diags.OK(), "null array elements should be rejected")
		require.Len(t, result["Person"], 1)
	})

	t.Run("ParseObject array with null elements", func(t *testing.T) {
		data := []byte(`{"Person": [null, {"name": "Alice"}, null]}`)
		result, diags := adapter.ParseObject(context.Background(), source, data)

		require.False(t, diags.OK(), "null elements in type array should be rejected")
		require.Len(t, result["Person"], 1)
	})
}

// Regression tests: trailing content after the root value must be rejected on
// every entry point.
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
		{"ParseArray trailing array", func(d []byte) diag.Result {
			_, r := adapter.ParseArray(context.Background(), source, d)
			return r
		}, `[{"$type": "Person"}][]`},
		{"ParseArray trailing object", func(d []byte) diag.Result {
			_, r := adapter.ParseArray(context.Background(), source, d)
			return r
		}, `[{"$type": "Person"}] {"extra": 1}`},
		{"ParseArray trailing string", func(d []byte) diag.Result {
			_, r := adapter.ParseArray(context.Background(), source, d)
			return r
		}, `[] "extra"`},
		{"ParseTypedArray trailing array", func(d []byte) diag.Result {
			_, r := adapter.ParseTypedArray(context.Background(), source, "Person", d)
			return r
		}, `[{"name": "Alice"}][]`},
		{"ParseTypedArray trailing object", func(d []byte) diag.Result {
			_, r := adapter.ParseTypedArray(context.Background(), source, "Person", d)
			return r
		}, `[] {"extra": 1}`},
		{"ParseOne trailing object", func(d []byte) diag.Result {
			_, r := adapter.ParseOne(context.Background(), source, "Person", d)
			return r
		}, `{"name": "Alice"} {"extra": 1}`},
		{"ParseOne trailing array", func(d []byte) diag.Result {
			_, r := adapter.ParseOne(context.Background(), source, "Person", d)
			return r
		}, `{"name": "Alice"} []`},
		{"ParseOne trailing string", func(d []byte) diag.Result {
			_, r := adapter.ParseOne(context.Background(), source, "Person", d)
			return r
		}, `{"name": "Alice"} "extra"`},
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
		result, diags := adapter.ParseOne(context.Background(), source, "Test", data)
		require.True(t, diags.OK())

		val := result.Properties["value"]
		assert.IsType(t, int64(0), val)
		assert.Equal(t, int64(42), val)
	})

	t.Run("floats preserved as float64", func(t *testing.T) {
		data := []byte(`{"value": 3.14}`)
		result, diags := adapter.ParseOne(context.Background(), source, "Test", data)
		require.True(t, diags.OK())

		val := result.Properties["value"]
		assert.IsType(t, float64(0), val)
		assert.Equal(t, 3.14, val)
	})

	t.Run("large integers", func(t *testing.T) {
		data := []byte(`{"value": 9223372036854775807}`) // MaxInt64
		result, diags := adapter.ParseOne(context.Background(), source, "Test", data)
		require.True(t, diags.OK())

		assert.Equal(t, int64(9223372036854775807), result.Properties["value"])
	})

	t.Run("nested numeric values", func(t *testing.T) {
		data := []byte(`{
			"obj": {"count": 5},
			"arr": [1, 2.5, 3]
		}`)
		result, diags := adapter.ParseOne(context.Background(), source, "Test", data)
		require.True(t, diags.OK())

		obj := result.Properties["obj"].(map[string]any)
		assert.Equal(t, int64(5), obj["count"])

		arr := result.Properties["arr"].([]any)
		assert.Equal(t, int64(1), arr[0])
		assert.Equal(t, 2.5, arr[1])
		assert.Equal(t, int64(3), arr[2])
	})
}

func TestLocationTracking(t *testing.T) {
	source := location.NewSourceID("test://locations")

	t.Run("provenance set when tracking enabled", func(t *testing.T) {
		reg := newMockRegistry()
		reg.register(source, 0, location.Position{Line: 1, Column: 1, Byte: 0})
		reg.register(source, 28, location.Position{Line: 1, Column: 29, Byte: 28})

		adapter, err := New(reg, WithTrackLocations(true))
		require.NoError(t, err)

		data := []byte(`{"name": "Alice", "age": 30}`)
		result, diags := adapter.ParseOne(context.Background(), source, "Person", data)
		require.True(t, diags.OK())

		assert.NotNil(t, result.Provenance, "Provenance should be set when tracking")
	})

	t.Run("no provenance when tracking disabled", func(t *testing.T) {
		adapter := newAdapter(t)
		data := []byte(`{"name": "Alice"}`)

		result, diags := adapter.ParseOne(context.Background(), source, "Person", data)
		require.True(t, diags.OK())

		assert.Nil(t, result.Provenance, "Provenance should be nil when not tracking")
	})
}

func TestEdgeCases(t *testing.T) {
	source := location.NewSourceID("test://edge")
	adapter := newAdapter(t)

	t.Run("unicode in values", func(t *testing.T) {
		data := []byte(`{"name": "日本語", "emoji": "🎉"}`)

		result, diags := adapter.ParseOne(context.Background(), source, "Test", data)
		require.True(t, diags.OK())
		assert.Equal(t, "日本語", result.Properties["name"])
		assert.Equal(t, "🎉", result.Properties["emoji"])
	})

	t.Run("null values", func(t *testing.T) {
		data := []byte(`{"name": null}`)

		result, diags := adapter.ParseOne(context.Background(), source, "Test", data)
		require.True(t, diags.OK())
		assert.Nil(t, result.Properties["name"])
	})

	t.Run("boolean values", func(t *testing.T) {
		data := []byte(`{"active": true, "deleted": false}`)

		result, diags := adapter.ParseOne(context.Background(), source, "Test", data)
		require.True(t, diags.OK())
		assert.Equal(t, true, result.Properties["active"])
		assert.Equal(t, false, result.Properties["deleted"])
	})

	t.Run("empty string", func(t *testing.T) {
		data := []byte(`{"name": ""}`)

		result, diags := adapter.ParseOne(context.Background(), source, "Test", data)
		require.True(t, diags.OK())
		assert.Empty(t, result.Properties["name"])
	})

	t.Run("empty object as value", func(t *testing.T) {
		data := []byte(`{"data": {}}`)

		result, diags := adapter.ParseOne(context.Background(), source, "Test", data)
		require.True(t, diags.OK())

		obj, ok := result.Properties["data"].(map[string]any)
		require.True(t, ok)
		assert.Empty(t, obj)
	})

	t.Run("empty array as value", func(t *testing.T) {
		data := []byte(`{"items": []}`)

		result, diags := adapter.ParseOne(context.Background(), source, "Test", data)
		require.True(t, diags.OK())

		arr, ok := result.Properties["items"].([]any)
		require.True(t, ok)
		assert.Empty(t, arr)
	})
}

func TestParseErrors_WithLocationTracking(t *testing.T) {
	// These tests exercise error paths with location tracking enabled: every
	// error class must carry a span when a registry is configured.
	reg := newMockRegistry()
	source := location.NewSourceID("test://tracked")
	for i := range 101 {
		reg.register(source, i, location.Position{Line: 1, Column: i + 1})
	}

	tests := []struct {
		name string
		data string
		one  bool // parse via ParseOne instead of ParseArray
	}{
		{name: "parse error", data: `{invalid json}`, one: true},
		{name: "missing type tag", data: `[{"name": "Test"}]`},
		{name: "invalid type tag", data: `[{"$type": "person", "name": "Test"}]`},
		{name: "reserved keyword type tag", data: `[{"$type": "String", "name": "Test"}]`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapter, err := New(reg, WithTrackLocations(true))
			require.NoError(t, err)

			var diags diag.Result
			if tt.one {
				_, diags = adapter.ParseOne(context.Background(), source, "Test", []byte(tt.data))
			} else {
				_, diags = adapter.ParseArray(context.Background(), source, []byte(tt.data))
			}

			require.False(t, diags.OK())
			issues := slices.Collect(diags.Issues())
			require.NotEmpty(t, issues)
			assert.NotEqual(t, location.Span{}, issues[0].Span(),
				"issue should carry a span when tracking is enabled")
		})
	}
}
