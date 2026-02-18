package testutil

import (
	"sort"
	"unicode/utf8"

	protocol "github.com/tliron/glsp/protocol_3_16"
)

// ApplyEdits applies LSP TextEdits to a document string, respecting the
// given position encoding ("utf-16" or "utf-8").
// Edits are applied in reverse document order to preserve earlier offsets.
func ApplyEdits(text string, edits []protocol.TextEdit, encoding string) string {
	if len(edits) == 0 {
		return text
	}

	// Copy and sort edits in reverse document order (highest position first).
	sorted := make([]protocol.TextEdit, len(edits))
	copy(sorted, edits)
	sort.Slice(sorted, func(i, j int) bool {
		a, b := sorted[i].Range, sorted[j].Range
		if a.Start.Line != b.Start.Line {
			return a.Start.Line > b.Start.Line
		}
		return a.Start.Character > b.Start.Character
	})

	result := text
	for _, edit := range sorted {
		startOff := positionToByteOffset(result, int(edit.Range.Start.Line), int(edit.Range.Start.Character), encoding)
		endOff := positionToByteOffset(result, int(edit.Range.End.Line), int(edit.Range.End.Character), encoding)
		result = result[:startOff] + edit.NewText + result[endOff:]
	}
	return result
}

// positionToByteOffset converts an LSP (line, character) position to a byte offset.
func positionToByteOffset(text string, line, char int, encoding string) int {
	offset := 0

	// Advance to the target line.
	for range line {
		idx := indexNewline(text[offset:])
		if idx < 0 {
			// Past the end of the document; clamp to end.
			return len(text)
		}
		offset += idx + 1
	}

	// Now offset points to the start of the target line.
	lineStart := offset

	if encoding == "utf-8" {
		// UTF-8 mode: character offset IS byte offset from line start.
		byteOff := lineStart + char
		if byteOff > len(text) {
			return len(text)
		}
		return byteOff
	}

	// UTF-16 mode: walk runes counting UTF-16 code units.
	utf16Units := 0
	pos := lineStart
	for pos < len(text) && text[pos] != '\n' {
		if utf16Units >= char {
			break
		}
		r, size := utf8.DecodeRuneInString(text[pos:])
		if r > 0xFFFF {
			// Supplementary plane: surrogate pair = 2 UTF-16 code units.
			utf16Units += 2
		} else {
			utf16Units++
		}
		pos += size
	}
	return pos
}

// indexNewline returns the index of the first '\n' in s, or -1.
func indexNewline(s string) int {
	for i := range len(s) {
		if s[i] == '\n' {
			return i
		}
	}
	return -1
}
