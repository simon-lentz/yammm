package diag

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/location"
)

func TestFormatIssueJSON_Basic(t *testing.T) {
	issue := NewIssue(Error, E_SYNTAX, "syntax error").Build()

	r := NewRenderer()
	data := r.FormatIssueJSON(issue)

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

	r := NewRenderer()
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			issue := NewIssue(tt.severity, E_SYNTAX, "msg").Build()
			data := r.FormatIssueJSON(issue)

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
	source := location.MustNewSourceID("test://schema.yammm")
	issue := NewIssue(Error, E_SYNTAX, "error").
		WithSpan(location.Span{
			Source: source,
			Start:  location.NewPosition(10, 5, 150),
			End:    location.NewPosition(10, 15, 160),
		}).
		Build()

	r := NewRenderer()
	data := r.FormatIssueJSON(issue)

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	span, ok := parsed["span"].(map[string]any)
	if !ok {
		t.Fatal("span should be present")
	}

	if span["source"] != "test://schema.yammm" {
		t.Errorf("span.source = %v; want 'test://schema.yammm'", span["source"])
	}

	start := span["start"].(map[string]any)
	if start["line"] != float64(10) {
		t.Errorf("start.line = %v; want 10", start["line"])
	}
	if start["column"] != float64(5) {
		t.Errorf("start.column = %v; want 5", start["column"])
	}
	if start["byte"] != float64(150) {
		t.Errorf("start.byte = %v; want 150", start["byte"])
	}

	end := span["end"].(map[string]any)
	if end["line"] != float64(10) {
		t.Errorf("end.line = %v; want 10", end["line"])
	}
	if end["column"] != float64(15) {
		t.Errorf("end.column = %v; want 15", end["column"])
	}
	if end["byte"] != float64(160) {
		t.Errorf("end.byte = %v; want 160", end["byte"])
	}
}

// TestFormatIssueJSON_ByteOffsetEncoding verifies's three-case table
// for byte offset encoding.
//
// Each test case uses consistent byte offsets for both start and end positions
// to ensure clean test vectors. The end byte is computed to stay in the same
// domain as start (unknown stays unknown, known stays known with offset).
func TestFormatIssueJSON_ByteOffsetEncoding(t *testing.T) {
	source := location.MustNewSourceID("test://file.yammm")

	tests := []struct {
		name        string
		startByte   int
		endByte     int
		wantByte    any // nil for omitted, float64 for present (start position)
		wantEndByte any // nil for omitted, float64 for present (end position)
	}{
		{
			name:        "unknown (-1) → omitted",
			startByte:   -1,
			endByte:     -1, // Both unknown for consistent test vector
			wantByte:    nil,
			wantEndByte: nil,
		},
		{
			name:        "zero (0) → present as 0",
			startByte:   0,
			endByte:     4, // Known offset from start
			wantByte:    float64(0),
			wantEndByte: float64(4),
		},
		{
			name:        "positive (100) → present as 100",
			startByte:   100,
			endByte:     104, // Known offset from start
			wantByte:    float64(100),
			wantEndByte: float64(104),
		},
	}

	r := NewRenderer()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			issue := NewIssue(Error, E_SYNTAX, "msg").
				WithSpan(location.Span{
					Source: source,
					Start:  location.NewPosition(1, 1, tt.startByte),
					End:    location.NewPosition(1, 5, tt.endByte),
				}).
				Build()

			data := r.FormatIssueJSON(issue)

			var parsed map[string]any
			if err := json.Unmarshal(data, &parsed); err != nil {
				t.Fatalf("invalid JSON: %v", err)
			}

			span := parsed["span"].(map[string]any)

			// Verify start position byte offset
			start := span["start"].(map[string]any)
			byteVal, exists := start["byte"]
			if tt.wantByte == nil {
				if exists {
					t.Errorf("start.byte should be omitted, got %v", byteVal)
				}
			} else {
				if !exists {
					t.Error("start.byte should be present")
				} else if byteVal != tt.wantByte {
					t.Errorf("start.byte = %v; want %v", byteVal, tt.wantByte)
				}
			}

			// Verify end position byte offset (same domain as start)
			end := span["end"].(map[string]any)
			endByteVal, endExists := end["byte"]
			if tt.wantEndByte == nil {
				if endExists {
					t.Errorf("end.byte should be omitted, got %v", endByteVal)
				}
			} else {
				if !endExists {
					t.Error("end.byte should be present")
				} else if endByteVal != tt.wantEndByte {
					t.Errorf("end.byte = %v; want %v", endByteVal, tt.wantEndByte)
				}
			}
		})
	}
}

// TestFormatIssueJSON_UnknownPosition verifies behavior when a span
// has a known source but unknown positions.
//
// This scenario occurs with TrackLocations=false in JSON adapters. Unknown
// positions use UnknownPosition() which sets Byte=-1, causing the byte field
// to be correctly omitted from JSON output
func TestFormatIssueJSON_UnknownPosition(t *testing.T) {
	source := location.MustNewSourceID("test://file.json")

	// Span with known source but unknown positions.
	// Use UnknownPosition() which sets Line=0, Column=0, Byte=-1.
	issue := NewIssue(Error, E_TYPE_MISMATCH, "type mismatch").
		WithSpan(location.Span{
			Source: source,
			Start:  location.UnknownPosition(),
			End:    location.UnknownPosition(),
		}).
		Build()

	r := NewRenderer()
	data := r.FormatIssueJSON(issue)

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	// Verify span is included (source is known, so span is not zero)
	span := parsed["span"]
	if span == nil {
		t.Fatal("span should be present when source is known")
	}

	spanMap := span.(map[string]any)

	// Verify source is present
	if spanMap["source"] != "test://file.json" {
		t.Errorf("source = %v; want 'test://file.json'", spanMap["source"])
	}

	// Unknown positions emit line=0, column=0
	start := spanMap["start"].(map[string]any)
	if start["line"] != float64(0) {
		t.Errorf("start.line = %v; want 0 (unknown position)", start["line"])
	}
	if start["column"] != float64(0) {
		t.Errorf("start.column = %v; want 0 (unknown position)", start["column"])
	}

	// Byte should be OMITTED for unknown positions
	// UnknownPosition() sets Byte=-1, which correctly omits the byte field.
	if _, exists := start["byte"]; exists {
		t.Errorf("start.byte should be omitted for unknown position, got %v", start["byte"])
	}

	// Verify end position has same behavior
	end := spanMap["end"].(map[string]any)
	if end["line"] != float64(0) || end["column"] != float64(0) {
		t.Errorf("end position = %v; want line=0, column=0", end)
	}
	if _, exists := end["byte"]; exists {
		t.Errorf("end.byte should be omitted for unknown position, got %v", end["byte"])
	}
}

// TestFormatIssueJSON_PositionZeroValueFootgun verifies that Position{} (Go zero
// value with Byte=0) is safely handled by the wire conversion.
//
// This test documents that HasByte() correctly prevents Position{} from emitting
// "byte": 0 even though Position{}.Byte == 0. The wire conversion checks
// HasByte() which returns false when IsZero() is true, avoiding the footgun.
func TestFormatIssueJSON_PositionZeroValueFootgun(t *testing.T) {
	source := location.MustNewSourceID("test://file.json")

	// Position{} has Byte=0 (Go zero value), but IsZero() returns true.
	// The wire conversion uses HasByte() which returns false for zero positions,
	// so the byte field should be omitted even though Byte==0.
	issue := NewIssue(Error, E_TYPE_MISMATCH, "type mismatch").
		WithSpan(location.Span{
			Source: source,
			Start:  location.Position{}, // Line=0, Column=0, Byte=0 (Go zero value)
			End:    location.Position{},
		}).
		Build()

	r := NewRenderer()
	data := r.FormatIssueJSON(issue)

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	spanMap := parsed["span"].(map[string]any)
	start := spanMap["start"].(map[string]any)

	// Despite Position{}.Byte == 0, the byte field should be OMITTED because
	// HasByte() returns false when IsZero() is true. This prevents the footgun
	// where accidental Position{} usage would emit "byte": 0.
	if _, exists := start["byte"]; exists {
		t.Errorf("start.byte should be omitted for zero Position (footgun prevention), got %v", start["byte"])
	}
}

func TestFormatIssueJSON_WithHint(t *testing.T) {
	issue := NewIssue(Error, E_SYNTAX, "error").
		WithHint("try this instead").
		Build()

	r := NewRenderer()
	data := r.FormatIssueJSON(issue)

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if parsed["hint"] != "try this instead" {
		t.Errorf("hint = %v; want 'try this instead'", parsed["hint"])
	}
}

func TestFormatIssueJSON_WithRelated(t *testing.T) {
	source := location.MustNewSourceID("test://file.yammm")
	issue := NewIssue(Error, E_TYPE_COLLISION, "collision").
		WithRelated(
			location.RelatedInfo{
				Message: "first definition here",
				Span:    location.Point(source, 5, 1),
			},
			location.RelatedInfo{
				Message: "second definition here",
				Span:    location.Point(source, 10, 1),
			},
		).
		Build()

	r := NewRenderer()
	data := r.FormatIssueJSON(issue)

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

	r := NewRenderer()
	data := r.FormatIssueJSON(issue)

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

	r := NewRenderer()
	data := r.FormatIssueJSON(issue)

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
	c := NewCollector(0)
	c.Collect(NewIssue(Error, E_SYNTAX, "first error").Build())
	c.Collect(NewIssue(Warning, E_INVALID_NAME, "second warning").Build())

	r := NewRenderer()
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
	c := NewCollector(2)
	c.Collect(NewIssue(Error, E_SYNTAX, "first").Build())
	c.Collect(NewIssue(Error, E_SYNTAX, "second").Build())
	c.Collect(NewIssue(Error, E_SYNTAX, "third").Build())  // Dropped
	c.Collect(NewIssue(Error, E_SYNTAX, "fourth").Build()) // Dropped

	r := NewRenderer()
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
}

func TestFormatIssueJSON_CompleteIssue(t *testing.T) {
	source := location.MustNewSourceID("test://complete.yammm")
	issue := NewIssue(Error, E_TYPE_COLLISION, "complete test").
		WithSpan(location.Span{
			Source: source,
			Start:  location.NewPosition(10, 5, 100),
			End:    location.NewPosition(10, 15, 110),
		}).
		WithHint("try this").
		WithRelated(location.RelatedInfo{
			Message: "related note",
			Span:    location.Point(source, 5, 1),
		}).
		WithDetails(Detail{Key: "key", Value: "value"}).
		Build()

	r := NewRenderer()
	data := r.FormatIssueJSON(issue)

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
		WithRelated(location.RelatedInfo{
			Message: "note without location",
			// Span is zero
		}).
		Build()

	r := NewRenderer()
	data := r.FormatIssueJSON(issue)

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
	source := location.MustNewSourceID("test://roundtrip.yammm")
	original := NewIssue(Error, E_SYNTAX, "test message").
		WithSpan(location.Span{
			Source: source,
			Start:  location.NewPosition(1, 1, 0),
			End:    location.NewPosition(1, 10, 9),
		}).
		Build()

	r := NewRenderer()
	data := r.FormatIssueJSON(original)

	// Re-marshal should produce identical output
	var parsed issueWire
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
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
