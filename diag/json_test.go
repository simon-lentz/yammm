package diag

import (
	"encoding/json"
	"maps"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/internal/yammmtest/sourcetest"

	"github.com/simon-lentz/yammm/internal/yammmtest"
	"github.com/simon-lentz/yammm/location"
)

// marshalIssueWire renders one issue's wire object — the per-issue shape
// [Renderer.FormatResultJSON] emits inside its envelope.
func marshalIssueWire(issue Issue) json.RawMessage {
	data, err := json.Marshal(toIssueWire(issue))
	if err != nil {
		panic("diag: unexpected JSON marshal error: " + err.Error())
	}
	return data
}

func TestFormatIssueJSON_Basic(t *testing.T) {
	issue := NewIssue(Error, E_SYNTAX, "syntax error").Build()

	data := marshalIssueWire(issue)

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	// Required fields
	if parsed["severity"] != "error" {
		t.Errorf("severity = %v; want 'error'", parsed["severity"])
	}
	if parsed["code"] != "E_SYNTAX" {
		t.Errorf("code = %v; want 'E_SYNTAX'", parsed["code"])
	}
	if parsed["message"] != "syntax error" {
		t.Errorf("message = %v; want 'syntax error'", parsed["message"])
	}

	// Optional fields should be omitted
	if _, exists := parsed["span"]; exists {
		t.Error("span should be omitted when not set")
	}
	if _, exists := parsed["hint"]; exists {
		t.Error("hint should be omitted when not set")
	}
	if _, exists := parsed["related"]; exists {
		t.Error("related should be omitted when not set")
	}
	if _, exists := parsed["details"]; exists {
		t.Error("details should be omitted when not set")
	}
}

func TestFormatIssueJSON_AllSeverities(t *testing.T) {
	tests := []struct {
		severity Severity
		want     string
	}{
		{Fatal, "fatal"},
		{Error, "error"},
		{Warning, "warning"},
		{Info, "info"},
		{Hint, "hint"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			issue := NewIssue(tt.severity, E_SYNTAX, "msg").Build()
			data := marshalIssueWire(issue)

			var parsed map[string]any
			if err := json.Unmarshal(data, &parsed); err != nil {
				t.Fatalf("invalid JSON: %v", err)
			}

			if parsed["severity"] != tt.want {
				t.Errorf("severity = %v; want %q", parsed["severity"], tt.want)
			}
		})
	}
}

func TestFormatIssueJSON_WithSpan(t *testing.T) {
	source := sourcetest.ID("test://schema.yammm")
	issue := NewIssue(Error, E_SYNTAX, "error").
		WithSpan(location.Span{
			Source: source,
			Start:  location.Position{Line: 10, Column: 5, Offset: 150},
			End:    location.Position{Line: 10, Column: 15, Offset: 160},
		}).
		Build()

	data := marshalIssueWire(issue)

	// The golden pins the complete JSON wire format — the span object with
	// line/column/offset for both endpoints is the behavior under test, and
	// the message field is part of this format's contract.
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	yammmtest.GoldenJSON(t, "format_issue_json_with_span", parsed)
}

// TestFormatIssueJSON_WritesTheOffsetOfAValidPosition holds the position
// member to go/token's model: a position with a line is valid and writes its
// offset, 0 included, and a position with no line writes line and column 0
// and no offset, whatever its Offset field holds.
func TestFormatIssueJSON_WritesTheOffsetOfAValidPosition(t *testing.T) {
	source := sourcetest.ID("test://file.yammm")

	tests := []struct {
		name string
		pos  location.Position
		want map[string]any
	}{
		{
			name: "the first byte",
			pos:  location.Position{Line: 1, Column: 1, Offset: 0},
			want: map[string]any{"line": float64(1), "column": float64(1), "offset": float64(0)},
		},
		{
			name: "a later byte",
			pos:  location.Position{Line: 3, Column: 5, Offset: 100},
			want: map[string]any{"line": float64(3), "column": float64(5), "offset": float64(100)},
		},
		{
			name: "the zero position",
			pos:  location.Position{},
			want: map[string]any{"line": float64(0), "column": float64(0)},
		},
		{
			name: "a valid line with no column",
			pos:  location.Position{Line: 2, Column: 0, Offset: 5},
			want: map[string]any{"line": float64(2), "column": float64(0), "offset": float64(5)},
		},
		{
			name: "an offset with no line",
			pos:  location.Position{Offset: 7},
			want: map[string]any{"line": float64(0), "column": float64(0)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			issue := NewIssue(Error, E_SYNTAX, "msg").
				WithSpan(location.Span{Source: source, Start: tt.pos, End: tt.pos}).
				Build()

			var parsed struct {
				Span struct {
					Source     string
					Start, End map[string]any
				}
			}
			if err := json.Unmarshal(marshalIssueWire(issue), &parsed); err != nil {
				t.Fatalf("invalid JSON: %v", err)
			}
			if parsed.Span.Source != "test://file.yammm" {
				t.Errorf("source = %q; want test://file.yammm", parsed.Span.Source)
			}
			if !maps.Equal(parsed.Span.Start, tt.want) {
				t.Errorf("start = %v; want %v", parsed.Span.Start, tt.want)
			}
			if !maps.Equal(parsed.Span.End, tt.want) {
				t.Errorf("end = %v; want %v", parsed.Span.End, tt.want)
			}
		})
	}
}

func TestFormatIssueJSON_WithHint(t *testing.T) {
	issue := NewIssue(Error, E_SYNTAX, "error").
		WithHint("try this instead").
		Build()

	data := marshalIssueWire(issue)

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if parsed["hint"] != "try this instead" {
		t.Errorf("hint = %v; want 'try this instead'", parsed["hint"])
	}
}

func TestFormatIssueJSON_WithRelated(t *testing.T) {
	source := sourcetest.ID("test://file.yammm")
	issue := NewIssue(Error, E_DUPLICATE_TYPE, "collision").
		WithRelated(
			RelatedInfo{
				Message: "first definition here",
				Span:    location.Span{Source: source, Start: location.Position{Line: 5, Column: 1}, End: location.Position{Line: 5, Column: 1}},
			},
			RelatedInfo{
				Message: "second definition here",
				Span:    location.Span{Source: source, Start: location.Position{Line: 10, Column: 1}, End: location.Position{Line: 10, Column: 1}},
			},
		).
		Build()

	data := marshalIssueWire(issue)

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	related, ok := parsed["related"].([]any)
	if !ok {
		t.Fatal("related should be an array")
	}
	if len(related) != 2 {
		t.Fatalf("len(related) = %d; want 2", len(related))
	}

	first := related[0].(map[string]any)
	if first["message"] != "first definition here" {
		t.Errorf("related[0].message = %v", first["message"])
	}
	if _, exists := first["span"]; !exists {
		t.Error("related[0].span should be present")
	}
}

func TestFormatIssueJSON_WithDetails(t *testing.T) {
	issue := NewIssue(Error, E_SYNTAX, "error").
		WithDetails(
			Detail{Key: DetailKeyExpected, Value: "String"},
			Detail{Key: DetailKeyGot, Value: "Int"},
		).
		Build()

	data := marshalIssueWire(issue)

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	details, ok := parsed["details"].([]any)
	if !ok {
		t.Fatal("details should be an array")
	}
	if len(details) != 2 {
		t.Fatalf("len(details) = %d; want 2", len(details))
	}

	first := details[0].(map[string]any)
	if first["key"] != DetailKeyExpected {
		t.Errorf("details[0].key = %v; want %q", first["key"], DetailKeyExpected)
	}
	if first["value"] != "String" {
		t.Errorf("details[0].value = %v; want 'String'", first["value"])
	}
}

func TestFormatIssueJSON_WithPath(t *testing.T) {
	// WithPath(sourceName, path) - sourceName is the file, path is the JSON path
	issue := NewIssue(Error, E_SYNTAX, "error").
		WithPath("data.json", "$.items[0]").
		Build()

	data := marshalIssueWire(issue)

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	// sourceName is the file name (first parameter)
	if parsed["sourceName"] != "data.json" {
		t.Errorf("sourceName = %v; want 'data.json'", parsed["sourceName"])
	}
	// path is the JSON path (second parameter)
	if parsed["path"] != "$.items[0]" {
		t.Errorf("path = %v; want '$.items[0]'", parsed["path"])
	}
}

func TestFormatResultJSON_Empty(t *testing.T) {
	r := NewRenderer()
	data := r.FormatResultJSON(OK())

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	issues, ok := parsed["issues"].([]any)
	if !ok {
		t.Fatal("issues should be an array")
	}
	if len(issues) != 0 {
		t.Errorf("len(issues) = %d; want 0", len(issues))
	}

	// Limit fields should be omitted for empty result
	if _, exists := parsed["limitReached"]; exists {
		t.Error("limitReached should be omitted for empty result")
	}
	if _, exists := parsed["droppedCount"]; exists {
		t.Error("droppedCount should be omitted for empty result")
	}
}

func TestFormatResultJSON_WithIssues(t *testing.T) {
	r := NewRenderer()
	c := NewCollector(0)
	c.Collect(NewIssue(Error, E_SYNTAX, "first error").Build())
	c.Collect(NewIssue(Warning, E_INVALID_NAME, "second warning").Build())

	data := r.FormatResultJSON(c.Result())

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	issues, ok := parsed["issues"].([]any)
	if !ok {
		t.Fatal("issues should be an array")
	}
	if len(issues) != 2 {
		t.Fatalf("len(issues) = %d; want 2", len(issues))
	}

	// Result sorts issues by source, position, then code.
	// Both issues have no span so they sort by code: E_INVALID_NAME < E_SYNTAX
	// Verify both messages are present (order depends on sorting)
	messages := make(map[string]bool)
	for _, issue := range issues {
		m := issue.(map[string]any)["message"].(string)
		messages[m] = true
	}
	if !messages["first error"] {
		t.Error("'first error' message not found in issues")
	}
	if !messages["second warning"] {
		t.Error("'second warning' message not found in issues")
	}
}

func TestFormatResultJSON_WithLimit(t *testing.T) {
	r := NewRenderer()
	c := NewCollector(2)
	c.Collect(NewIssue(Error, E_SYNTAX, "first").Build())
	c.Collect(NewIssue(Error, E_SYNTAX, "second").Build())
	c.Collect(NewIssue(Error, E_SYNTAX, "third").Build())  // Dropped
	c.Collect(NewIssue(Error, E_SYNTAX, "fourth").Build()) // Dropped

	data := r.FormatResultJSON(c.Result())

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	issues := parsed["issues"].([]any)
	if len(issues) != 2 {
		t.Fatalf("len(issues) = %d; want 2", len(issues))
	}

	if parsed["limitReached"] != true {
		t.Errorf("limitReached = %v; want true", parsed["limitReached"])
	}
	if parsed["droppedCount"] != float64(2) {
		t.Errorf("droppedCount = %v; want 2", parsed["droppedCount"])
	}
	// A cap is the collector's setting, not a fact the result carries.
	if limit, exists := parsed["limit"]; exists {
		t.Errorf("the wire carries limit = %v; want no limit key", limit)
	}
}

func TestFormatIssueJSON_CompleteIssue(t *testing.T) {
	source := sourcetest.ID("test://complete.yammm")
	issue := NewIssue(Error, E_DUPLICATE_TYPE, "complete test").
		WithSpan(location.Span{
			Source: source,
			Start:  location.Position{Line: 10, Column: 5, Offset: 100},
			End:    location.Position{Line: 10, Column: 15, Offset: 110},
		}).
		WithHint("try this").
		WithRelated(RelatedInfo{
			Message: "related note",
			Span:    location.Span{Source: source, Start: location.Position{Line: 5, Column: 1}, End: location.Position{Line: 5, Column: 1}},
		}).
		WithDetails(Detail{Key: "key", Value: "value"}).
		Build()

	data := marshalIssueWire(issue)

	// Verify it's valid JSON
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	// Verify all fields are present
	expected := []string{"span", "severity", "code", "message", "hint", "related", "details"}
	for _, field := range expected {
		if _, exists := parsed[field]; !exists {
			t.Errorf("field %q should be present", field)
		}
	}
}

func TestFormatIssueJSON_RelatedWithoutSpan(t *testing.T) {
	issue := NewIssue(Error, E_SYNTAX, "error").
		WithRelated(RelatedInfo{
			Message: "note without location",
			// Span is zero
		}).
		Build()

	data := marshalIssueWire(issue)

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	related := parsed["related"].([]any)
	first := related[0].(map[string]any)

	if first["message"] != "note without location" {
		t.Errorf("related message wrong")
	}
	if _, exists := first["span"]; exists {
		t.Error("related span should be omitted when zero")
	}
}

// TestJSON_RoundTrip verifies that the JSON structure is stable.
func TestJSON_RoundTrip(t *testing.T) {
	source := sourcetest.ID("test://roundtrip.yammm")
	original := NewIssue(Error, E_SYNTAX, "test message").
		WithSpan(location.Span{
			Source: source,
			Start:  location.Position{Line: 1, Column: 1, Offset: 0},
			End:    location.Position{Line: 1, Column: 10, Offset: 9},
		}).
		Build()

	data := marshalIssueWire(original)

	// Re-marshal should produce identical output
	var parsed issueWire
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	// A wire that lost the span would still re-marshal to itself.
	if s := parsed.Span; s == nil || s.Source != source.String() || s.Start.Line != 1 || s.Start.Column != 1 ||
		s.End.Column != 10 || s.End.Offset == nil || *s.End.Offset != 9 {
		t.Errorf("the wire does not carry the issue's span: %s", data)
	}

	data2, err := json.Marshal(parsed)
	if err != nil {
		t.Fatalf("re-marshal failed: %v", err)
	}

	if string(data) != string(data2) {
		t.Errorf("round-trip changed output:\n  original: %s\n  roundtrip: %s", data, data2)
	}
}

// TestJSON_EmptyArrayNotNull verifies issues array is [] not null.
func TestJSON_EmptyArrayNotNull(t *testing.T) {
	r := NewRenderer()
	data := r.FormatResultJSON(OK())

	// Should contain [] not null
	expected := `"issues":[]`
	if !strings.Contains(string(data), expected) {
		t.Errorf("empty result should have issues:[], got: %s", data)
	}
}
