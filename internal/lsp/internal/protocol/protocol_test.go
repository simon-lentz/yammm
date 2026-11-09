package protocol

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntegerOrString_MarshalInteger(t *testing.T) {
	t.Parallel()
	s := IntegerOrString{Value: Integer(42)}
	data, err := json.Marshal(s)
	require.NoError(t, err)
	assert.JSONEq(t, `42`, string(data))
}

func TestIntegerOrString_MarshalString(t *testing.T) {
	t.Parallel()
	s := IntegerOrString{Value: "E_PARSE_ERROR"}
	data, err := json.Marshal(s)
	require.NoError(t, err)
	assert.JSONEq(t, `"E_PARSE_ERROR"`, string(data))
}

func TestIntegerOrString_UnmarshalInteger(t *testing.T) {
	t.Parallel()
	var s IntegerOrString
	require.NoError(t, json.Unmarshal([]byte(`42`), &s))
	assert.Equal(t, Integer(42), s.Value)
}

func TestIntegerOrString_UnmarshalString(t *testing.T) {
	t.Parallel()
	var s IntegerOrString
	require.NoError(t, json.Unmarshal([]byte(`"E_PARSE_ERROR"`), &s))
	assert.Equal(t, "E_PARSE_ERROR", s.Value)
}

func TestIntegerOrString_RoundTrip(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		value IntegerOrString
	}{
		{"integer", IntegerOrString{Value: Integer(0)}},
		{"negative integer", IntegerOrString{Value: Integer(-1)}},
		{"string", IntegerOrString{Value: "some_code"}},
		{"empty string", IntegerOrString{Value: ""}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			data, err := json.Marshal(tt.value)
			require.NoError(t, err)
			var got IntegerOrString
			require.NoError(t, json.Unmarshal(data, &got))
			assert.Equal(t, tt.value.Value, got.Value)
		})
	}
}

func TestBoolOrString_MarshalBool(t *testing.T) {
	t.Parallel()
	s := BoolOrString{Value: true}
	data, err := json.Marshal(s)
	require.NoError(t, err)
	assert.JSONEq(t, `true`, string(data))
}

func TestBoolOrString_MarshalString(t *testing.T) {
	t.Parallel()
	s := BoolOrString{Value: "workspace-id"}
	data, err := json.Marshal(s)
	require.NoError(t, err)
	assert.JSONEq(t, `"workspace-id"`, string(data))
}

func TestBoolOrString_UnmarshalBool(t *testing.T) {
	t.Parallel()
	var s BoolOrString
	require.NoError(t, json.Unmarshal([]byte(`false`), &s))
	assert.Equal(t, false, s.Value)
}

func TestBoolOrString_UnmarshalString(t *testing.T) {
	t.Parallel()
	var s BoolOrString
	require.NoError(t, json.Unmarshal([]byte(`"workspace-id"`), &s))
	assert.Equal(t, "workspace-id", s.Value)
}

func TestBoolOrString_RoundTrip(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		value BoolOrString
	}{
		{"true", BoolOrString{Value: true}},
		{"false", BoolOrString{Value: false}},
		{"string", BoolOrString{Value: "notifications"}},
		{"empty string", BoolOrString{Value: ""}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			data, err := json.Marshal(tt.value)
			require.NoError(t, err)
			var got BoolOrString
			require.NoError(t, json.Unmarshal(data, &got))
			assert.Equal(t, tt.value.Value, got.Value)
		})
	}
}

func TestBoolOrString_String(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		value    BoolOrString
		expected string
	}{
		{"true", BoolOrString{Value: true}, "true"},
		{"false", BoolOrString{Value: false}, "false"},
		{"string", BoolOrString{Value: "hello"}, "hello"},
		{"nil", BoolOrString{Value: nil}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, tt.value.String())
		})
	}
}

func TestContentChange_IsFullSync(t *testing.T) {
	t.Parallel()

	fullSync := ContentChange{Text: "full content"}
	assert.True(t, fullSync.IsFullSync())

	rng := Range{Start: Position{Line: 0, Character: 0}, End: Position{Line: 0, Character: 5}}
	incremental := ContentChange{Range: &rng, Text: "partial"}
	assert.False(t, incremental.IsFullSync())
}

func TestExtractFullSyncText_FullSync(t *testing.T) {
	t.Parallel()
	changes := []ContentChange{{Text: "new content"}}
	text, ok := ExtractFullSyncText(changes)
	assert.True(t, ok)
	assert.Equal(t, "new content", text)
}

func TestExtractFullSyncText_Incremental(t *testing.T) {
	t.Parallel()
	rng := Range{Start: Position{Line: 0, Character: 0}, End: Position{Line: 0, Character: 5}}
	changes := []ContentChange{{Range: &rng, Text: "partial"}}
	_, ok := ExtractFullSyncText(changes)
	assert.False(t, ok)
}

func TestExtractFullSyncText_Empty(t *testing.T) {
	t.Parallel()
	_, ok := ExtractFullSyncText(nil)
	assert.False(t, ok)
}

func TestExtractFullSyncText_LastFullSyncWins(t *testing.T) {
	t.Parallel()
	changes := []ContentChange{
		{Text: "first"},
		{Text: "second"},
		{Text: "third"},
	}
	text, ok := ExtractFullSyncText(changes)
	assert.True(t, ok)
	assert.Equal(t, "third", text)
}

func TestExtractFullSyncText_MixedChanges(t *testing.T) {
	t.Parallel()
	rng := Range{Start: Position{Line: 0, Character: 0}, End: Position{Line: 0, Character: 5}}
	changes := []ContentChange{
		{Text: "full sync"},
		{Range: &rng, Text: "incremental"},
	}
	text, ok := ExtractFullSyncText(changes)
	assert.True(t, ok)
	assert.Equal(t, "full sync", text)
}

func TestContentChange_JSONRoundTrip_FullSync(t *testing.T) {
	t.Parallel()
	original := ContentChange{Text: "hello world"}
	data, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded ContentChange
	require.NoError(t, json.Unmarshal(data, &decoded))
	assert.Nil(t, decoded.Range)
	assert.Equal(t, "hello world", decoded.Text)
}

func TestContentChange_JSONRoundTrip_Incremental(t *testing.T) {
	t.Parallel()
	rng := Range{Start: Position{Line: 1, Character: 5}, End: Position{Line: 1, Character: 10}}
	original := ContentChange{Range: &rng, Text: "replacement"}
	data, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded ContentChange
	require.NoError(t, json.Unmarshal(data, &decoded))
	require.NotNil(t, decoded.Range)
	assert.Equal(t, UInteger(1), decoded.Range.Start.Line)
	assert.Equal(t, UInteger(5), decoded.Range.Start.Character)
	assert.Equal(t, "replacement", decoded.Text)
}
