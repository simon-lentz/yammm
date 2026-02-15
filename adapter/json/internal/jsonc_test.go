// Package internal contains tests that verify external dependency behavior.
//
// The jsonc_test.go file verifies that tidwall/jsonc preserves byte offsets
// during preprocessing. These tests serve as a regression guard for dependency
// upgrades — if jsonc changes behavior, these tests fail immediately rather
// than causing subtle diagnostic accuracy issues.
//
// Upgrade policy: When tidwall/jsonc is upgraded, run:
//
//	go test ./adapter/json/internal/... -run "Jsonc|Offset|Preservation"
//
// to verify offset semantics are preserved before releasing.
package internal

import (
	"bytes"
	"testing"

	"github.com/tidwall/jsonc"
)

// TestJsoncLengthPreservation verifies that jsonc.ToJSON always produces
// output of exactly the same length as the input. This is foundational to
// the JSON adapter's location tracking — byte offsets from json.Decoder.InputOffset()
// must map directly to original source positions.
func TestJsoncLengthPreservation(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		// Line comments
		{
			name:  "line comment only",
			input: "// comment\n{}",
		},
		{
			name:  "line comment with content",
			input: "// this is a comment\n{\"x\": 1}",
		},
		{
			name:  "multiple line comments",
			input: "// first\n// second\n{\"x\": 1}",
		},
		{
			name:  "line comment at end",
			input: "{\"x\": 1}\n// trailing",
		},

		// Block comments
		{
			name:  "block comment only",
			input: "/* c */{}",
		},
		{
			name:  "block comment with content",
			input: "/* comment */{\"x\": 1}",
		},
		{
			name:  "multi-line block comment",
			input: "/* line1\nline2\nline3 */{\"x\": 1}",
		},
		{
			name:  "inline block comment",
			input: "{\"x\": /* note */ 1}",
		},
		{
			name:  "block comment with asterisks",
			input: "/* * * * */{\"x\": 1}",
		},

		// Trailing commas
		{
			name:  "trailing comma in object",
			input: "{\"x\": 1,}",
		},
		{
			name:  "trailing comma in array",
			input: "[1, 2, 3,]",
		},
		{
			name:  "nested trailing commas",
			input: "{\"a\": [1, 2,], \"b\": {\"c\": 3,},}",
		},
		{
			name:  "trailing comma with whitespace",
			input: "{\"x\": 1 , }",
		},

		// Combined
		{
			name:  "comment and trailing comma",
			input: "// header\n{\"x\": 1, /* inline */ \"y\": 2,}",
		},
		{
			name:  "complex nested structure",
			input: "/* top */\n{\"arr\": [1, /* mid */ 2,], \"obj\": {\"k\": \"v\",},}\n// end",
		},

		// Edge cases
		{
			name:  "empty input",
			input: "",
		},
		{
			name:  "whitespace only with comment",
			input: "  // comment\n  ",
		},
		{
			name:  "unicode in comment",
			input: "// 日本語コメント\n{\"x\": 1}",
		},
		{
			name:  "unicode in block comment",
			input: "/* 日本語 */ {\"x\": 1}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := []byte(tt.input)
			output := jsonc.ToJSON(input)

			if len(output) != len(input) {
				t.Errorf("length not preserved:\n  input:  %d bytes %q\n  output: %d bytes %q",
					len(input), input, len(output), output)
			}
		})
	}
}

// TestJsoncNewlinePreservation verifies that newlines within block comments
// are preserved at the same byte offsets. This ensures line number calculations
// remain accurate after preprocessing.
func TestJsoncNewlinePreservation(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "single newline in block comment",
			input: "/* a\nb */{\"x\": 1}",
		},
		{
			name:  "multiple newlines in block comment",
			input: "/* line1\nline2\nline3 */{\"x\": 1}",
		},
		{
			name:  "CRLF in block comment",
			input: "/* a\r\nb */{\"x\": 1}",
		},
		{
			name:  "mixed line endings",
			input: "/* unix\nwindows\r\nold-mac\r */{\"x\": 1}",
		},
		{
			name:  "newline at comment boundaries",
			input: "/*\ncontent\n*/{\"x\": 1}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := []byte(tt.input)
			output := jsonc.ToJSON(input)

			// Find all newline positions in input
			inputNewlines := findNewlineOffsets(input)
			outputNewlines := findNewlineOffsets(output)

			if len(inputNewlines) != len(outputNewlines) {
				t.Errorf("newline count changed: input has %d, output has %d",
					len(inputNewlines), len(outputNewlines))
				return
			}

			for i := range inputNewlines {
				if inputNewlines[i] != outputNewlines[i] {
					t.Errorf("newline %d moved: input offset %d, output offset %d",
						i, inputNewlines[i], outputNewlines[i])
				}
			}
		})
	}
}

// TestJsoncOffsetMapping verifies that byte offsets of JSON tokens remain
// unchanged after preprocessing. This is critical for json.Decoder.InputOffset()
// to map correctly back to original source positions.
func TestJsoncOffsetMapping(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		markerByte   byte   // byte to find in the result
		markerString string // context for error messages
	}{
		{
			name:         "object after line comment",
			input:        "// comment\n{\"x\": 1}",
			markerByte:   '{',
			markerString: "opening brace",
		},
		{
			name:         "object after block comment",
			input:        "/* c */{\"x\": 1}",
			markerByte:   '{',
			markerString: "opening brace",
		},
		{
			name:         "value after inline comment",
			input:        "{\"x\": /* note */ 1}",
			markerByte:   '1',
			markerString: "value 1",
		},
		{
			name:         "key after comment",
			input:        "{/* k */\"x\": 1}",
			markerByte:   '"',
			markerString: "key quote",
		},
		{
			name:         "array element after comment",
			input:        "[/* first */1, 2]",
			markerByte:   '1',
			markerString: "array element 1",
		},
		{
			name:         "nested object after comment",
			input:        "{\"a\": /* nested */ {\"b\": 2}}",
			markerByte:   'b',
			markerString: "nested key b",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := []byte(tt.input)
			output := jsonc.ToJSON(input)

			// Find first occurrence of marker in output (which should be JSON)
			outputIdx := bytes.IndexByte(output, tt.markerByte)
			if outputIdx == -1 {
				t.Fatalf("marker byte %q not found in output", tt.markerByte)
			}

			// The same offset in input should have the same byte (since length is preserved
			// and content before JSON tokens is replaced with spaces)
			if outputIdx >= len(input) {
				t.Fatalf("marker offset %d exceeds input length %d", outputIdx, len(input))
			}

			inputByte := input[outputIdx]
			if inputByte != tt.markerByte {
				t.Errorf("%s offset mismatch: output[%d]=%q but input[%d]=%q",
					tt.markerString, outputIdx, tt.markerByte, outputIdx, inputByte)
			}
		})
	}
}

// TestJsoncCommentReplacement verifies the specific replacement behavior:
// - Line comments (// ...) → spaces up to newline
// - Block comments (/* ... */) → spaces preserving internal newlines
// - Trailing commas → single space
func TestJsoncCommentReplacement(t *testing.T) {
	t.Run("line comment becomes spaces", func(t *testing.T) {
		input := []byte("// abc\n{}")
		output := jsonc.ToJSON(input)

		// First 6 bytes should be spaces (replacing "// abc")
		for i := range 6 {
			if output[i] != ' ' {
				t.Errorf("expected space at offset %d, got %q", i, output[i])
			}
		}
		// Newline preserved
		if output[6] != '\n' {
			t.Errorf("expected newline at offset 6, got %q", output[6])
		}
	})

	t.Run("block comment becomes spaces with preserved newlines", func(t *testing.T) {
		// Input: "/* a\nb */{}" has newline at offset 4
		// Offsets: 0:'/' 1:'*' 2:' ' 3:'a' 4:'\n' 5:'b' 6:' ' 7:'*' 8:'/' 9:'{' 10:'}'
		input := []byte("/* a\nb */{}")
		output := jsonc.ToJSON(input)

		// Check that newline at offset 4 is preserved
		if output[4] != '\n' {
			t.Errorf("expected newline preserved at offset 4, got %q", output[4])
		}

		// Expected: 4 spaces + newline + 4 spaces + "{}"
		for i, expected := range []byte("    \n    {}") {
			if output[i] != expected {
				t.Errorf("offset %d: expected %q, got %q", i, expected, output[i])
			}
		}
	})

	t.Run("trailing comma becomes space", func(t *testing.T) {
		input := []byte("{\"x\": 1,}")
		output := jsonc.ToJSON(input)

		// The comma at offset 7 should become a space
		if output[7] != ' ' {
			t.Errorf("expected trailing comma replaced with space at offset 7, got %q", output[7])
		}
	})
}

// TestJsoncToJSONInPlace verifies that ToJSONInPlace has identical semantics
// to ToJSON (same output for same input, just modifies in place).
func TestJsoncToJSONInPlace(t *testing.T) {
	inputs := []string{
		"// comment\n{}",
		"/* block */{\"x\": 1}",
		"{\"x\": 1,}",
		"/* a\nb */{\"arr\": [1,]}",
	}

	for _, input := range inputs {
		t.Run(input, func(t *testing.T) {
			// ToJSON (returns new slice)
			toJSONResult := jsonc.ToJSON([]byte(input))

			// ToJSONInPlace (modifies in place)
			inPlaceInput := []byte(input)
			inPlaceResult := jsonc.ToJSONInPlace(inPlaceInput)

			if !bytes.Equal(toJSONResult, inPlaceResult) {
				t.Errorf("ToJSON and ToJSONInPlace differ:\n  ToJSON:      %q\n  ToJSONInPlace: %q",
					toJSONResult, inPlaceResult)
			}
		})
	}
}

// findNewlineOffsets returns the byte offsets of all newline characters (\n, \r\n, \r).
func findNewlineOffsets(data []byte) []int {
	var offsets []int
	for i, b := range data {
		if b == '\n' || b == '\r' {
			// For \r\n, only count the \n position
			if b == '\r' && i+1 < len(data) && data[i+1] == '\n' {
				continue // skip \r, will count \n next
			}
			offsets = append(offsets, i)
		}
	}
	return offsets
}
