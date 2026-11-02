package testutil

import (
	"cmp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/simon-lentz/yammm/lsp/internal/protocol"
)

// ApplyEdits applies LSP TextEdits, whose characters count UTF-16 code units,
// to a document string.
// Edits are applied in reverse document order to preserve earlier offsets.
func ApplyEdits(text string, edits []protocol.TextEdit) string {
	if len(edits) == 0 {
		return text
	}

	// Copy and sort edits in reverse document order (highest position first).
	sorted := make([]protocol.TextEdit, len(edits))
	copy(sorted, edits)
	slices.SortFunc(sorted, func(x, y protocol.TextEdit) int {
		a, b := x.Range, y.Range
		// Reverse order (highest position first)
		return cmp.Or(
			cmp.Compare(b.Start.Line, a.Start.Line),
			cmp.Compare(b.Start.Character, a.Start.Character),
		)
	})

	result := text
	for _, edit := range sorted {
		startOff := positionToByteOffset(result, int(edit.Range.Start.Line), int(edit.Range.Start.Character))
		endOff := positionToByteOffset(result, int(edit.Range.End.Line), int(edit.Range.End.Character))
		result = result[:startOff] + edit.NewText + result[endOff:]
	}
	return result
}

// positionToByteOffset converts an LSP (line, character) position to a byte offset.
func positionToByteOffset(text string, line, char int) int {
	offset := 0

	// Advance to the target line.
	for range line {
		idx := strings.IndexByte(text[offset:], '\n')
		if idx < 0 {
			// Past the end of the document; clamp to end.
			return len(text)
		}
		offset += idx + 1
	}

	// Now offset points to the start of the target line.
	lineStart := offset

	// Walk runes counting UTF-16 code units.
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
