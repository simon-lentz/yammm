package spec_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	jsonadapter "github.com/simon-lentz/yammm/adapter/json"
	"github.com/simon-lentz/yammm/internal/source"
	"github.com/simon-lentz/yammm/location"
)

// =============================================================================
// JSON Adapter — SPEC.md §JSON Adapter (10 claims)
//
// These tests verify that the JSON adapter creates correctly, respects parse
// options, supports JSONC by default, and produces the expected output shape.
// =============================================================================

// ---------------------------------------------------------------------------
// Adapter Creation (2 claims)
// ---------------------------------------------------------------------------

// TestAdapter_NilRegistry verifies that NewAdapter succeeds with a nil registry
// when location tracking is not requested.
// Source: SPEC.md, "adapter, err := json.NewAdapter(registry, opts...)"
func TestAdapter_NilRegistry(t *testing.T) {
	t.Parallel()
	a, err := jsonadapter.NewAdapter(nil)
	require.NoError(t, err)
	require.NotNil(t, a)
}

// TestAdapter_NonNilRegistry verifies that NewAdapter succeeds with a real
// PositionRegistry implementation (source.Registry).
// Source: SPEC.md, "adapter, err := json.NewAdapter(registry, opts...)"
func TestAdapter_NonNilRegistry(t *testing.T) {
	t.Parallel()
	reg := source.NewRegistry()
	a, err := jsonadapter.NewAdapter(reg)
	require.NoError(t, err)
	require.NotNil(t, a)
}

// ---------------------------------------------------------------------------
// Parse Options (3 claims)
// ---------------------------------------------------------------------------

// TestAdapter_StrictJSON_RejectsComments verifies that WithStrictJSON(true)
// causes the adapter to reject input containing line comments.
// Source: SPEC.md, "WithStrictJSON — Use stdlib JSON only (no comments/trailing commas)"
func TestAdapter_StrictJSON_RejectsComments(t *testing.T) {
	t.Parallel()
	a, err := jsonadapter.NewAdapter(nil, jsonadapter.WithStrictJSON(true))
	require.NoError(t, err)

	jsonc := []byte(`{
		// comment should cause failure
		"Person": [{ "name": "Alice" }]
	}`)
	sourceID := location.NewSourceID("test://strict-json-comments")
	_, result := a.ParseObject(sourceID, jsonc)
	assert.False(t, result.OK(), "strict JSON should reject line comments")
}

// TestAdapter_StrictJSON_RejectsTrailingCommas verifies that WithStrictJSON(true)
// causes the adapter to reject input containing trailing commas.
// Source: SPEC.md, "WithStrictJSON — Use stdlib JSON only (no comments/trailing commas)"
func TestAdapter_StrictJSON_RejectsTrailingCommas(t *testing.T) {
	t.Parallel()
	a, err := jsonadapter.NewAdapter(nil, jsonadapter.WithStrictJSON(true))
	require.NoError(t, err)

	data := []byte(`{
		"Person": [{ "name": "Alice", }]
	}`)
	sourceID := location.NewSourceID("test://strict-json-trailing")
	_, result := a.ParseObject(sourceID, data)
	assert.False(t, result.OK(), "strict JSON should reject trailing commas")
}

// TestAdapter_TrackLocations verifies that WithTrackLocations(true) enables
// provenance tracking on parsed instances when a valid registry is provided.
// Source: SPEC.md, "WithTrackLocations — Enable source position tracking"
func TestAdapter_TrackLocations(t *testing.T) {
	t.Parallel()

	// Create a source registry and register the JSON content so PositionAt
	// can resolve byte offsets to line/column positions.
	reg := source.NewRegistry()
	sourceID := location.NewSourceID("test://track-locations")
	data := []byte(`{"name": "Alice", "age": 30}`)
	require.NoError(t, reg.Register(sourceID, data))

	a, err := jsonadapter.NewAdapter(reg, jsonadapter.WithTrackLocations(true))
	require.NoError(t, err)

	raw, result := a.ParseOne(sourceID, "Person", data)
	require.True(t, result.OK(), "parse should succeed: %v", result.Messages())
	require.NotNil(t, raw.Provenance, "Provenance should be set when location tracking is enabled")
}

// TestAdapter_TrackLocations_NilRegistryError verifies that requesting location
// tracking without a registry returns an error.
// Source: SPEC.md (implied), adapter.go: "WithTrackLocations(true) requires a non-nil PositionRegistry"
func TestAdapter_TrackLocations_NilRegistryError(t *testing.T) {
	t.Parallel()
	_, err := jsonadapter.NewAdapter(nil, jsonadapter.WithTrackLocations(true))
	require.Error(t, err, "WithTrackLocations(true) with nil registry should error")
}

// TestAdapter_CustomTypeField verifies that WithTypeField changes the field name
// used for type discrimination in ParseArray.
// Source: SPEC.md, "WithTypeField — Field name for type tagging (default: $type)"
func TestAdapter_CustomTypeField(t *testing.T) {
	t.Parallel()
	a, err := jsonadapter.NewAdapter(nil, jsonadapter.WithTypeField("_type"))
	require.NoError(t, err)

	data := []byte(`[{"_type": "Person", "name": "Alice"}]`)
	sourceID := location.NewSourceID("test://custom-type-field")
	parsed, result := a.ParseArray(sourceID, data)
	require.True(t, result.OK(), "ParseArray with custom type field should succeed: %v", result.Messages())
	require.Len(t, parsed["Person"], 1)
	assert.Equal(t, "Alice", parsed["Person"][0].Properties["name"])

	// The custom type field should be removed from properties
	_, hasType := parsed["Person"][0].Properties["_type"]
	assert.False(t, hasType, "custom type field should be removed from properties")
}

// ---------------------------------------------------------------------------
// JSONC Support — default mode (3 claims)
// ---------------------------------------------------------------------------

// TestAdapter_JSONC_LineComments verifies that line comments (//) are stripped
// by default (JSONC preprocessing).
// Source: SPEC.md, "Strips // and /* */ comments"
func TestAdapter_JSONC_LineComments(t *testing.T) {
	t.Parallel()
	a, err := jsonadapter.NewAdapter(nil)
	require.NoError(t, err)

	jsonc := []byte(`{
		// This is a line comment
		"Person": [
			{ "name": "Alice" }
		]
	}`)
	sourceID := location.NewSourceID("test://jsonc-line-comments")
	parsed, result := a.ParseObject(sourceID, jsonc)
	require.True(t, result.OK(), "JSONC with line comments should parse: %v", result.Messages())
	require.Len(t, parsed["Person"], 1)
	assert.Equal(t, "Alice", parsed["Person"][0].Properties["name"])
}

// TestAdapter_JSONC_BlockComments verifies that block comments (/* */) are
// stripped by default (JSONC preprocessing).
// Source: SPEC.md, "Strips // and /* */ comments"
func TestAdapter_JSONC_BlockComments(t *testing.T) {
	t.Parallel()
	a, err := jsonadapter.NewAdapter(nil)
	require.NoError(t, err)

	jsonc := []byte(`{
		/* block comment spanning
		   multiple lines */
		"Person": [
			{ "name": "Alice" }
		]
	}`)
	sourceID := location.NewSourceID("test://jsonc-block-comments")
	parsed, result := a.ParseObject(sourceID, jsonc)
	require.True(t, result.OK(), "JSONC with block comments should parse: %v", result.Messages())
	require.Len(t, parsed["Person"], 1)
	assert.Equal(t, "Alice", parsed["Person"][0].Properties["name"])
}

// TestAdapter_JSONC_TrailingCommas verifies that trailing commas are removed
// by default (JSONC preprocessing).
// Source: SPEC.md, "Removes trailing commas"
func TestAdapter_JSONC_TrailingCommas(t *testing.T) {
	t.Parallel()
	a, err := jsonadapter.NewAdapter(nil)
	require.NoError(t, err)

	jsonc := []byte(`{
		"Person": [
			{ "name": "Alice", },
		],
	}`)
	sourceID := location.NewSourceID("test://jsonc-trailing-commas")
	parsed, result := a.ParseObject(sourceID, jsonc)
	require.True(t, result.OK(), "JSONC with trailing commas should parse: %v", result.Messages())
	require.Len(t, parsed["Person"], 1)
	assert.Equal(t, "Alice", parsed["Person"][0].Properties["name"])
}

// ---------------------------------------------------------------------------
// Parse Output Shape (2 claims)
// ---------------------------------------------------------------------------

// TestAdapter_ParseObject_OutputShape verifies that ParseObject returns a
// map[string][]RawInstance keyed by type name, with records containing the
// expected properties.
// Source: SPEC.md, "ParseObject returns map[string][]RawInstance keyed by type"
func TestAdapter_ParseObject_OutputShape(t *testing.T) {
	t.Parallel()
	a, err := jsonadapter.NewAdapter(nil)
	require.NoError(t, err)

	data := []byte(`{
		"Person": [
			{"name": "Alice", "age": 30},
			{"name": "Bob", "age": 25}
		],
		"Company": [
			{"name": "Acme"}
		]
	}`)
	sourceID := location.NewSourceID("test://output-shape")
	parsed, result := a.ParseObject(sourceID, data)
	require.True(t, result.OK(), "ParseObject should succeed: %v", result.Messages())

	// Keyed by type name
	assert.Len(t, parsed["Person"], 2, "should have 2 Person instances")
	assert.Len(t, parsed["Company"], 1, "should have 1 Company instance")

	// Verify non-existent type key returns nil slice
	assert.Nil(t, parsed["Unknown"], "non-existent type key should return nil")
}

// TestAdapter_ParseObject_RecordProperties verifies that parsed records contain
// the expected property values in RawInstance.Properties.
// Source: SPEC.md, parsed records contain expected properties
func TestAdapter_ParseObject_RecordProperties(t *testing.T) {
	t.Parallel()
	a, err := jsonadapter.NewAdapter(nil)
	require.NoError(t, err)

	data := []byte(`{
		"Person": [
			{"name": "Alice", "age": 30, "active": true}
		]
	}`)
	sourceID := location.NewSourceID("test://record-properties")
	parsed, result := a.ParseObject(sourceID, data)
	require.True(t, result.OK(), "ParseObject should succeed: %v", result.Messages())
	require.Len(t, parsed["Person"], 1)

	alice := parsed["Person"][0]
	assert.Equal(t, "Alice", alice.Properties["name"], "string property should match")
	assert.Equal(t, int64(30), alice.Properties["age"], "integer property should be int64")
	assert.Equal(t, true, alice.Properties["active"], "boolean property should match")
}
