package json

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/simon-lentz/yammm/location"
)

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

func TestNewAdapter(t *testing.T) {
	t.Run("nil registry without tracking", func(t *testing.T) {
		adapter, err := NewAdapter(nil)
		require.NoError(t, err)
		assert.NotNil(t, adapter)
	})

	t.Run("nil registry with tracking returns error", func(t *testing.T) {
		_, err := NewAdapter(nil, WithTrackLocations(true))
		require.Error(t, err)
		assert.Equal(t, ErrNilRegistry, err)
	})

	t.Run("valid registry with tracking", func(t *testing.T) {
		reg := newMockRegistry()
		adapter, err := NewAdapter(reg, WithTrackLocations(true))
		require.NoError(t, err)
		assert.NotNil(t, adapter)
	})

	t.Run("custom type field", func(t *testing.T) {
		adapter, err := NewAdapter(nil, WithTypeField("_type"))
		require.NoError(t, err)
		assert.NotNil(t, adapter)
	})

	t.Run("empty type field returns error", func(t *testing.T) {
		_, err := NewAdapter(nil, WithTypeField(""))
		require.Error(t, err)
		assert.Equal(t, ErrEmptyTypeField, err)
	})
}

func TestParseObject(t *testing.T) {
	source := location.NewSourceID("test://object")

	t.Run("single type with multiple instances", func(t *testing.T) {
		adapter, _ := NewAdapter(nil)
		data := []byte(`{
			"Person": [
				{"name": "Alice", "age": 30},
				{"name": "Bob", "age": 25}
			]
		}`)

		result, diags := adapter.ParseObject(source, data)
		require.True(t, diags.OK(), "expected no errors: %v", diags)
		require.Len(t, result, 1)
		require.Len(t, result["Person"], 2)

		assert.Equal(t, "Alice", result["Person"][0].Properties["name"])
		assert.Equal(t, int64(30), result["Person"][0].Properties["age"])
		assert.Equal(t, "Bob", result["Person"][1].Properties["name"])
	})

	t.Run("multiple types", func(t *testing.T) {
		adapter, _ := NewAdapter(nil)
		data := []byte(`{
			"Person": [{"name": "Alice"}],
			"Company": [{"title": "Acme Inc"}]
		}`)

		result, diags := adapter.ParseObject(source, data)
		require.True(t, diags.OK())
		require.Len(t, result, 2)
		require.Len(t, result["Person"], 1)
		require.Len(t, result["Company"], 1)
	})

	t.Run("empty object", func(t *testing.T) {
		adapter, _ := NewAdapter(nil)
		data := []byte(`{}`)

		result, diags := adapter.ParseObject(source, data)
		require.True(t, diags.OK())
		assert.Len(t, result, 0)
	})

	t.Run("invalid JSON", func(t *testing.T) {
		adapter, _ := NewAdapter(nil)
		data := []byte(`{invalid}`)

		_, diags := adapter.ParseObject(source, data)
		require.False(t, diags.OK())
	})

	t.Run("expected object at root", func(t *testing.T) {
		adapter, _ := NewAdapter(nil)
		data := []byte(`[1, 2, 3]`)

		_, diags := adapter.ParseObject(source, data)
		require.False(t, diags.OK())
	})

	t.Run("invalid type name", func(t *testing.T) {
		adapter, _ := NewAdapter(nil)
		data := []byte(`{"person": [{"name": "Alice"}]}`) // lowercase type name

		result, diags := adapter.ParseObject(source, data)
		require.False(t, diags.OK())
		assert.Len(t, result, 0) // Invalid type should be skipped
	})

	t.Run("qualified type name", func(t *testing.T) {
		adapter, _ := NewAdapter(nil)
		data := []byte(`{"common.Person": [{"name": "Alice"}]}`)

		result, diags := adapter.ParseObject(source, data)
		require.True(t, diags.OK())
		require.Len(t, result["common.Person"], 1)
	})

	t.Run("with jsonc comments", func(t *testing.T) {
		adapter, _ := NewAdapter(nil) // jsonc enabled by default
		data := []byte(`{
			// This is a comment
			"Person": [
				{"name": "Alice"}, // trailing comment
			]
		}`)

		result, diags := adapter.ParseObject(source, data)
		require.True(t, diags.OK())
		require.Len(t, result["Person"], 1)
	})

	t.Run("strict JSON rejects comments", func(t *testing.T) {
		adapter, _ := NewAdapter(nil, WithStrictJSON(true))
		data := []byte(`{
			// This is a comment
			"Person": [{"name": "Alice"}]
		}`)

		_, diags := adapter.ParseObject(source, data)
		require.False(t, diags.OK())
	})
}

func TestParseArray(t *testing.T) {
	source := location.NewSourceID("test://array")

	t.Run("mixed types", func(t *testing.T) {
		adapter, _ := NewAdapter(nil)
		data := []byte(`[
			{"$type": "Person", "name": "Alice"},
			{"$type": "Company", "title": "Acme Inc"},
			{"$type": "Person", "name": "Bob"}
		]`)

		result, diags := adapter.ParseArray(source, data)
		require.True(t, diags.OK())
		require.Len(t, result["Person"], 2)
		require.Len(t, result["Company"], 1)

		// $type field should be removed from properties
		_, hasType := result["Person"][0].Properties["$type"]
		assert.False(t, hasType, "$type should be removed from properties")
	})

	t.Run("custom type field", func(t *testing.T) {
		adapter, _ := NewAdapter(nil, WithTypeField("_type"))
		data := []byte(`[{"_type": "Person", "name": "Alice"}]`)

		result, diags := adapter.ParseArray(source, data)
		require.True(t, diags.OK())
		require.Len(t, result["Person"], 1)

		// Custom type field should be removed
		_, hasType := result["Person"][0].Properties["_type"]
		assert.False(t, hasType)
	})

	t.Run("missing type field", func(t *testing.T) {
		adapter, _ := NewAdapter(nil)
		data := []byte(`[{"name": "Alice"}]`)

		_, diags := adapter.ParseArray(source, data)
		require.False(t, diags.OK())
	})

	t.Run("non-string type field", func(t *testing.T) {
		adapter, _ := NewAdapter(nil)
		data := []byte(`[{"$type": 123, "name": "Alice"}]`)

		_, diags := adapter.ParseArray(source, data)
		require.False(t, diags.OK())
	})

	t.Run("invalid type name syntax", func(t *testing.T) {
		adapter, _ := NewAdapter(nil)
		data := []byte(`[{"$type": "person", "name": "Alice"}]`) // lowercase

		_, diags := adapter.ParseArray(source, data)
		require.False(t, diags.OK())
	})

	t.Run("empty array", func(t *testing.T) {
		adapter, _ := NewAdapter(nil)
		data := []byte(`[]`)

		result, diags := adapter.ParseArray(source, data)
		require.True(t, diags.OK())
		assert.Len(t, result, 0)
	})

	t.Run("expected array at root", func(t *testing.T) {
		adapter, _ := NewAdapter(nil)
		data := []byte(`{"Person": []}`)

		_, diags := adapter.ParseArray(source, data)
		require.False(t, diags.OK())
	})
}

func TestParseTypedArray(t *testing.T) {
	source := location.NewSourceID("test://typed")

	t.Run("basic usage", func(t *testing.T) {
		adapter, _ := NewAdapter(nil)
		data := []byte(`[
			{"name": "Alice", "age": 30},
			{"name": "Bob", "age": 25}
		]`)

		result, diags := adapter.ParseTypedArray(source, "Person", data)
		require.True(t, diags.OK())
		require.Len(t, result, 2)

		assert.Equal(t, "Alice", result[0].Properties["name"])
		assert.Equal(t, int64(30), result[0].Properties["age"])
	})

	t.Run("invalid type name", func(t *testing.T) {
		adapter, _ := NewAdapter(nil)
		data := []byte(`[{"name": "Alice"}]`)

		_, diags := adapter.ParseTypedArray(source, "person", data) // lowercase
		require.False(t, diags.OK())
	})

	t.Run("empty array", func(t *testing.T) {
		adapter, _ := NewAdapter(nil)
		data := []byte(`[]`)

		result, diags := adapter.ParseTypedArray(source, "Person", data)
		require.True(t, diags.OK())
		assert.Len(t, result, 0)
	})
}

func TestParseOne(t *testing.T) {
	source := location.NewSourceID("test://one")

	t.Run("basic usage", func(t *testing.T) {
		adapter, _ := NewAdapter(nil)
		data := []byte(`{"name": "Alice", "age": 30}`)

		result, diags := adapter.ParseOne(source, "Person", data)
		require.True(t, diags.OK())

		assert.Equal(t, "Alice", result.Properties["name"])
		assert.Equal(t, int64(30), result.Properties["age"])
	})

	t.Run("nested objects", func(t *testing.T) {
		adapter, _ := NewAdapter(nil)
		data := []byte(`{
			"name": "Alice",
			"address": {"city": "NYC", "zip": "10001"}
		}`)

		result, diags := adapter.ParseOne(source, "Person", data)
		require.True(t, diags.OK())

		address, ok := result.Properties["address"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "NYC", address["city"])
	})

	t.Run("invalid type name", func(t *testing.T) {
		adapter, _ := NewAdapter(nil)
		data := []byte(`{"name": "Alice"}`)

		_, diags := adapter.ParseOne(source, "person", data) // lowercase
		require.False(t, diags.OK())
	})

	t.Run("invalid JSON", func(t *testing.T) {
		adapter, _ := NewAdapter(nil)
		data := []byte(`{invalid}`)

		_, diags := adapter.ParseOne(source, "Person", data)
		require.False(t, diags.OK())
	})
}

func TestNumericConversion(t *testing.T) {
	source := location.NewSourceID("test://numbers")
	adapter, _ := NewAdapter(nil)

	t.Run("integers preserved as int64", func(t *testing.T) {
		data := []byte(`{"value": 42}`)
		result, diags := adapter.ParseOne(source, "Test", data)
		require.True(t, diags.OK())

		val := result.Properties["value"]
		assert.IsType(t, int64(0), val)
		assert.Equal(t, int64(42), val)
	})

	t.Run("floats preserved as float64", func(t *testing.T) {
		data := []byte(`{"value": 3.14}`)
		result, diags := adapter.ParseOne(source, "Test", data)
		require.True(t, diags.OK())

		val := result.Properties["value"]
		assert.IsType(t, float64(0), val)
		assert.Equal(t, 3.14, val)
	})

	t.Run("large integers", func(t *testing.T) {
		data := []byte(`{"value": 9223372036854775807}`) // MaxInt64
		result, diags := adapter.ParseOne(source, "Test", data)
		require.True(t, diags.OK())

		val := result.Properties["value"]
		assert.Equal(t, int64(9223372036854775807), val)
	})

	t.Run("nested numeric values", func(t *testing.T) {
		data := []byte(`{
			"obj": {"count": 5},
			"arr": [1, 2.5, 3]
		}`)
		result, diags := adapter.ParseOne(source, "Test", data)
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
		// Register positions for the test data
		reg.register(source, 0, location.Position{Line: 1, Column: 1, Byte: 0})
		reg.register(source, 28, location.Position{Line: 1, Column: 29, Byte: 28})

		adapter, err := NewAdapter(reg, WithTrackLocations(true))
		require.NoError(t, err)

		data := []byte(`{"name": "Alice", "age": 30}`)
		result, diags := adapter.ParseOne(source, "Person", data)
		require.True(t, diags.OK())

		assert.NotNil(t, result.Provenance, "Provenance should be set when tracking")
	})

	t.Run("no provenance when tracking disabled", func(t *testing.T) {
		adapter, _ := NewAdapter(nil)
		data := []byte(`{"name": "Alice"}`)

		result, diags := adapter.ParseOne(source, "Person", data)
		require.True(t, diags.OK())

		assert.Nil(t, result.Provenance, "Provenance should be nil when not tracking")
	})
}

func TestEdgeCases(t *testing.T) {
	source := location.NewSourceID("test://edge")

	t.Run("unicode in values", func(t *testing.T) {
		adapter, _ := NewAdapter(nil)
		data := []byte(`{"name": "日本語", "emoji": "🎉"}`)

		result, diags := adapter.ParseOne(source, "Test", data)
		require.True(t, diags.OK())
		assert.Equal(t, "日本語", result.Properties["name"])
		assert.Equal(t, "🎉", result.Properties["emoji"])
	})

	t.Run("null values", func(t *testing.T) {
		adapter, _ := NewAdapter(nil)
		data := []byte(`{"name": null}`)

		result, diags := adapter.ParseOne(source, "Test", data)
		require.True(t, diags.OK())
		assert.Nil(t, result.Properties["name"])
	})

	t.Run("boolean values", func(t *testing.T) {
		adapter, _ := NewAdapter(nil)
		data := []byte(`{"active": true, "deleted": false}`)

		result, diags := adapter.ParseOne(source, "Test", data)
		require.True(t, diags.OK())
		assert.Equal(t, true, result.Properties["active"])
		assert.Equal(t, false, result.Properties["deleted"])
	})

	t.Run("empty string", func(t *testing.T) {
		adapter, _ := NewAdapter(nil)
		data := []byte(`{"name": ""}`)

		result, diags := adapter.ParseOne(source, "Test", data)
		require.True(t, diags.OK())
		assert.Equal(t, "", result.Properties["name"])
	})

	t.Run("empty object as value", func(t *testing.T) {
		adapter, _ := NewAdapter(nil)
		data := []byte(`{"data": {}}`)

		result, diags := adapter.ParseOne(source, "Test", data)
		require.True(t, diags.OK())

		obj, ok := result.Properties["data"].(map[string]any)
		require.True(t, ok)
		assert.Len(t, obj, 0)
	})

	t.Run("empty array as value", func(t *testing.T) {
		adapter, _ := NewAdapter(nil)
		data := []byte(`{"items": []}`)

		result, diags := adapter.ParseOne(source, "Test", data)
		require.True(t, diags.OK())

		arr, ok := result.Properties["items"].([]any)
		require.True(t, ok)
		assert.Len(t, arr, 0)
	})
}

func TestContinuesAfterErrors(t *testing.T) {
	source := location.NewSourceID("test://continues")

	t.Run("ParseArray continues after invalid type", func(t *testing.T) {
		adapter, _ := NewAdapter(nil)
		data := []byte(`[
			{"$type": "person", "name": "Alice"},
			{"$type": "Person", "name": "Bob"},
			{"$type": "Person", "name": "Charlie"}
		]`)

		result, diags := adapter.ParseArray(source, data)
		// Should have an error for the first element
		require.False(t, diags.OK())
		// But should still have parsed the valid elements
		require.Len(t, result["Person"], 2)
	})

	t.Run("ParseObject continues after invalid type", func(t *testing.T) {
		adapter, _ := NewAdapter(nil)
		data := []byte(`{
			"person": [{"name": "Alice"}],
			"Person": [{"name": "Bob"}]
		}`)

		result, diags := adapter.ParseObject(source, data)
		require.False(t, diags.OK())
		// Should have parsed the valid type
		require.Len(t, result["Person"], 1)
	})
}

func TestParseErrors_WithLocationTracking(t *testing.T) {
	// These tests exercise error paths with location tracking enabled.
	reg := newMockRegistry()
	source := location.NewSourceID("test://tracked")

	// Register positions for the source
	for i := range 101 {
		reg.register(source, i, location.Position{Line: 1, Column: i + 1})
	}

	t.Run("parseError with tracking", func(t *testing.T) {
		adapter, err := NewAdapter(reg, WithTrackLocations(true))
		require.NoError(t, err)

		// Invalid JSON to trigger parse error
		data := []byte(`{invalid json}`)
		_, diags := adapter.ParseOne(source, "Test", data)

		require.False(t, diags.OK())
		issues := diags.IssuesSlice()
		require.NotEmpty(t, issues)

		// Issue should have span when tracking is enabled
		issue := issues[0]
		assert.NotEqual(t, location.Span{}, issue.Span())
	})

	t.Run("missingTypeTagError with tracking", func(t *testing.T) {
		adapter, err := NewAdapter(reg, WithTrackLocations(true))
		require.NoError(t, err)

		// Array element missing type tag
		data := []byte(`[{"name": "Test"}]`)
		_, diags := adapter.ParseArray(source, data)

		require.False(t, diags.OK())
		issues := diags.IssuesSlice()
		require.NotEmpty(t, issues)

		// Should have span from location tracking
		issue := issues[0]
		assert.NotEqual(t, location.Span{}, issue.Span())
	})

	t.Run("invalidTypeTagError with tracking", func(t *testing.T) {
		adapter, err := NewAdapter(reg, WithTrackLocations(true))
		require.NoError(t, err)

		// Array element with invalid (lowercase) type tag
		data := []byte(`[{"$type": "person", "name": "Test"}]`)
		_, diags := adapter.ParseArray(source, data)

		require.False(t, diags.OK())
		issues := diags.IssuesSlice()
		require.NotEmpty(t, issues)

		issue := issues[0]
		assert.NotEqual(t, location.Span{}, issue.Span())
	})

	t.Run("typeTagError with tracking - reserved keyword", func(t *testing.T) {
		adapter, err := NewAdapter(reg, WithTrackLocations(true))
		require.NoError(t, err)

		// Type name that is a reserved keyword
		data := []byte(`[{"$type": "String", "name": "Test"}]`)
		_, diags := adapter.ParseArray(source, data)

		require.False(t, diags.OK())
		issues := diags.IssuesSlice()
		require.NotEmpty(t, issues)

		issue := issues[0]
		assert.NotEqual(t, location.Span{}, issue.Span())
	})
}

func TestParseArray_ErrorPaths(t *testing.T) {
	source := location.NewSourceID("test://errors")

	t.Run("malformed JSON syntax error", func(t *testing.T) {
		adapter, _ := NewAdapter(nil)
		// Malformed JSON
		data := []byte(`[{"name": }]`)

		result, diags := adapter.ParseArray(source, data)

		require.False(t, diags.OK())
		// Should return partial results up to error
		require.NotNil(t, result)
	})

	t.Run("truncated JSON", func(t *testing.T) {
		adapter, _ := NewAdapter(nil)
		// Truncated JSON
		data := []byte(`[{"$type": "Person"`)

		result, diags := adapter.ParseArray(source, data)

		require.False(t, diags.OK())
		require.NotNil(t, result)
	})

	t.Run("not an array", func(t *testing.T) {
		adapter, _ := NewAdapter(nil)
		// Object instead of array
		data := []byte(`{"$type": "Person"}`)

		result, diags := adapter.ParseArray(source, data)

		require.False(t, diags.OK())
		// Should have error but empty result
		require.Empty(t, result)
	})

	t.Run("empty array", func(t *testing.T) {
		adapter, _ := NewAdapter(nil)
		data := []byte(`[]`)

		result, diags := adapter.ParseArray(source, data)

		require.True(t, diags.OK())
		require.Empty(t, result)
	})

	t.Run("non-string type tag", func(t *testing.T) {
		adapter, _ := NewAdapter(nil)
		// Type tag is a number, not string
		data := []byte(`[{"$type": 123, "name": "Test"}]`)

		result, diags := adapter.ParseArray(source, data)

		require.False(t, diags.OK())
		// Should still have empty map since element failed
		require.NotNil(t, result)
	})
}

func TestParseObject_ErrorPaths(t *testing.T) {
	source := location.NewSourceID("test://object-errors")

	t.Run("malformed JSON", func(t *testing.T) {
		adapter, _ := NewAdapter(nil)
		data := []byte(`{invalid}`)

		_, diags := adapter.ParseObject(source, data)

		require.False(t, diags.OK())
	})

	t.Run("not an object", func(t *testing.T) {
		adapter, _ := NewAdapter(nil)
		// Array instead of object
		data := []byte(`[{"$type": "Person"}]`)

		_, diags := adapter.ParseObject(source, data)

		require.False(t, diags.OK())
	})

	t.Run("truncated JSON", func(t *testing.T) {
		adapter, _ := NewAdapter(nil)
		data := []byte(`{"Person": [{"name":`)

		_, diags := adapter.ParseObject(source, data)

		require.False(t, diags.OK())
	})

	t.Run("nested array with invalid element", func(t *testing.T) {
		adapter, _ := NewAdapter(nil)
		// Person array with one valid, one invalid element
		data := []byte(`{
			"Person": [
				{"name": "Alice"},
				{"name": "broken}
			]
		}`)

		result, diags := adapter.ParseObject(source, data)

		require.False(t, diags.OK())
		// Should have partial results
		require.NotNil(t, result)
	})
}

func TestParseTypedArray_ErrorPaths(t *testing.T) {
	source := location.NewSourceID("test://typed-errors")

	t.Run("malformed JSON", func(t *testing.T) {
		adapter, _ := NewAdapter(nil)
		data := []byte(`[{"name": }]`)

		_, diags := adapter.ParseTypedArray(source, "Person", data)

		require.False(t, diags.OK())
		// Result may be nil for syntax errors
	})

	t.Run("not an array", func(t *testing.T) {
		adapter, _ := NewAdapter(nil)
		data := []byte(`{"name": "Alice"}`)

		result, diags := adapter.ParseTypedArray(source, "Person", data)

		require.False(t, diags.OK())
		require.Empty(t, result)
	})

	t.Run("truncated JSON", func(t *testing.T) {
		adapter, _ := NewAdapter(nil)
		data := []byte(`[{"name": "Alice"`)

		_, diags := adapter.ParseTypedArray(source, "Person", data)

		require.False(t, diags.OK())
		// Result may be nil for syntax errors
	})
}

func TestParseArray_ErrorDetails_WithoutTracking(t *testing.T) {
	// Tests error path code coverage without location tracking enabled.
	// This exercises the non-tracking branches of error helper functions.
	source := location.NewSourceID("test://details")
	adapter, _ := NewAdapter(nil) // No registry, no tracking

	t.Run("invalid_type_tag_details", func(t *testing.T) {
		// Type tag is present but not a string - exercises invalidTypeTagError without tracking
		data := []byte(`[{"$type": 123, "name": "Test"}]`)

		_, diags := adapter.ParseArray(source, data)

		require.False(t, diags.OK())
		issues := diags.IssuesSlice()
		require.NotEmpty(t, issues)

		// Verify the issue has expected details but no span (no tracking)
		issue := issues[0]
		assert.Equal(t, location.Span{}, issue.Span(), "Should have empty span without tracking")

		// Should have detail keys
		details := issue.Details()
		hasDetail := false
		hasGot := false
		for _, d := range details {
			if d.Key == "detail" {
				hasDetail = true
			}
			if d.Key == "got" {
				hasGot = true
			}
		}
		assert.True(t, hasDetail, "Should have 'detail' key")
		assert.True(t, hasGot, "Should have 'got' key")
	})

	t.Run("empty_type_tag", func(t *testing.T) {
		// Empty string type tag
		data := []byte(`[{"$type": "", "name": "Test"}]`)

		_, diags := adapter.ParseArray(source, data)

		require.False(t, diags.OK())
	})

	t.Run("type_tag_null", func(t *testing.T) {
		// Null type tag
		data := []byte(`[{"$type": null, "name": "Test"}]`)

		_, diags := adapter.ParseArray(source, data)

		require.False(t, diags.OK())
	})

	t.Run("type_tag_object", func(t *testing.T) {
		// Object as type tag
		data := []byte(`[{"$type": {"x": 1}, "name": "Test"}]`)

		_, diags := adapter.ParseArray(source, data)

		require.False(t, diags.OK())
	})

	t.Run("type_tag_array", func(t *testing.T) {
		// Array as type tag
		data := []byte(`[{"$type": ["Person"], "name": "Test"}]`)

		_, diags := adapter.ParseArray(source, data)

		require.False(t, diags.OK())
	})

	t.Run("type_tag_boolean", func(t *testing.T) {
		// Boolean as type tag
		data := []byte(`[{"$type": true, "name": "Test"}]`)

		_, diags := adapter.ParseArray(source, data)

		require.False(t, diags.OK())
	})
}

func TestParseObject_NestedArrayErrors(t *testing.T) {
	// Tests error recovery in nested arrays within ParseObject.
	source := location.NewSourceID("test://nested-errors")
	adapter, _ := NewAdapter(nil)

	t.Run("array_with_non_object_element", func(t *testing.T) {
		// Array contains non-object (string) - should report error
		data := []byte(`{"Person": ["not an object"]}`)

		result, diags := adapter.ParseObject(source, data)

		require.False(t, diags.OK())
		require.NotNil(t, result)
	})

	t.Run("array_with_number_element", func(t *testing.T) {
		// Array contains number instead of object
		data := []byte(`{"Person": [123]}`)

		result, diags := adapter.ParseObject(source, data)

		require.False(t, diags.OK())
		require.NotNil(t, result)
	})
}

// Regression tests for Issue 1: ParseObject desync when type value is not an array.
// When a type maps to a non-array value, the decoder must skip the entire value
// to stay synchronized for subsequent type names.
func TestParseObject_NonArrayValues(t *testing.T) {
	source := location.NewSourceID("test://non-array")
	adapter, _ := NewAdapter(nil)

	tests := []struct {
		name         string
		input        string
		expectError  bool
		validTypeKey string
		validCount   int
	}{
		{
			name:         "object instead of array",
			input:        `{"Person": {"nested": "obj"}, "Company": [{"title": "Acme"}]}`,
			expectError:  true,
			validTypeKey: "Company",
			validCount:   1,
		},
		{
			name:         "string instead of array",
			input:        `{"Person": "not an array", "Company": [{"title": "Acme"}]}`,
			expectError:  true,
			validTypeKey: "Company",
			validCount:   1,
		},
		{
			name:         "number instead of array",
			input:        `{"Person": 123, "Company": [{"title": "Acme"}]}`,
			expectError:  true,
			validTypeKey: "Company",
			validCount:   1,
		},
		{
			name:         "null instead of array",
			input:        `{"Person": null, "Company": [{"title": "Acme"}]}`,
			expectError:  true,
			validTypeKey: "Company",
			validCount:   1,
		},
		{
			name:         "boolean instead of array",
			input:        `{"Person": true, "Company": [{"title": "Acme"}]}`,
			expectError:  true,
			validTypeKey: "Company",
			validCount:   1,
		},
		{
			name:         "deeply nested object instead of array",
			input:        `{"Person": {"a": {"b": {"c": "deep"}}}, "Company": [{"title": "Acme"}]}`,
			expectError:  true,
			validTypeKey: "Company",
			validCount:   1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, diags := adapter.ParseObject(source, []byte(tt.input))

			if tt.expectError {
				require.False(t, diags.OK(), "expected error for non-array value")
			}

			// Verify decoder stayed synchronized - should have parsed the valid type
			if tt.validTypeKey != "" {
				require.Len(t, result[tt.validTypeKey], tt.validCount,
					"should have parsed valid type after skipping invalid value")
			}
		})
	}
}

// Regression tests for Issue 2: Null values should be rejected as invalid objects.
func TestNullRejection(t *testing.T) {
	source := location.NewSourceID("test://null")
	adapter, _ := NewAdapter(nil)

	t.Run("ParseOne rejects null root", func(t *testing.T) {
		data := []byte(`null`)
		_, diags := adapter.ParseOne(source, "Person", data)

		require.False(t, diags.OK(), "null root should be rejected")
		issues := diags.IssuesSlice()
		require.NotEmpty(t, issues)
		assert.Contains(t, issues[0].Message(), "expected object")
	})

	t.Run("ParseTypedArray rejects null elements", func(t *testing.T) {
		data := []byte(`[null, {"name": "Alice"}, null]`)
		result, diags := adapter.ParseTypedArray(source, "Person", data)

		require.False(t, diags.OK(), "null array elements should be rejected")
		// Should have parsed the valid element
		require.Len(t, result, 1)
		assert.Equal(t, "Alice", result[0].Properties["name"])
	})

	t.Run("ParseArray rejects null elements", func(t *testing.T) {
		data := []byte(`[null, {"$type": "Person", "name": "Alice"}]`)
		result, diags := adapter.ParseArray(source, data)

		require.False(t, diags.OK(), "null array elements should be rejected")
		// Should have parsed the valid element
		require.Len(t, result["Person"], 1)
	})

	t.Run("ParseObject array with null elements", func(t *testing.T) {
		data := []byte(`{"Person": [null, {"name": "Alice"}, null]}`)
		result, diags := adapter.ParseObject(source, data)

		require.False(t, diags.OK(), "null elements in type array should be rejected")
		// Should have parsed the valid element
		require.Len(t, result["Person"], 1)
	})
}

// Regression tests for Issue 3: Trailing content after root value should be rejected.
func TestTrailingContent(t *testing.T) {
	source := location.NewSourceID("test://trailing")
	adapter, _ := NewAdapter(nil)

	t.Run("ParseObject rejects trailing content", func(t *testing.T) {
		tests := []struct {
			name  string
			input string
		}{
			{"trailing array", `{"Person": []}[]`},
			{"trailing object", `{"Person": []} {"extra": 1}`},
			{"trailing string", `{"Person": []} "extra"`},
			{"trailing number", `{"Person": []} 123`},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				_, diags := adapter.ParseObject(source, []byte(tt.input))
				require.False(t, diags.OK(), "trailing content should be rejected")
				issues := diags.IssuesSlice()
				require.NotEmpty(t, issues)
				assert.Contains(t, issues[len(issues)-1].Message(), "unexpected content")
			})
		}
	})

	t.Run("ParseArray rejects trailing content", func(t *testing.T) {
		tests := []struct {
			name  string
			input string
		}{
			{"trailing array", `[{"$type": "Person"}][]`},
			{"trailing object", `[{"$type": "Person"}] {"extra": 1}`},
			{"trailing string", `[] "extra"`},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				_, diags := adapter.ParseArray(source, []byte(tt.input))
				require.False(t, diags.OK(), "trailing content should be rejected")
			})
		}
	})

	t.Run("ParseTypedArray rejects trailing content", func(t *testing.T) {
		tests := []struct {
			name  string
			input string
		}{
			{"trailing array", `[{"name": "Alice"}][]`},
			{"trailing object", `[] {"extra": 1}`},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				_, diags := adapter.ParseTypedArray(source, "Person", []byte(tt.input))
				require.False(t, diags.OK(), "trailing content should be rejected")
			})
		}
	})

	t.Run("ParseOne rejects trailing content", func(t *testing.T) {
		tests := []struct {
			name  string
			input string
		}{
			{"trailing object", `{"name": "Alice"} {"extra": 1}`},
			{"trailing array", `{"name": "Alice"} []`},
			{"trailing string", `{"name": "Alice"} "extra"`},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				_, diags := adapter.ParseOne(source, "Person", []byte(tt.input))
				require.False(t, diags.OK(), "trailing content should be rejected")
			})
		}
	})
}
