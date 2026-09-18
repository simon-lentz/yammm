package json

import (
	"unicode/utf8"

	"github.com/simon-lentz/yammm/location"
)

// positionTable converts a byte offset in one document to a [location.Position].
//
// Build it over the document's own bytes, never over the buffer jsonc returns:
// jsonc replaces each comment byte with one space, so a multibyte rune inside a
// comment moves every later rune column on that line.
type positionTable struct {
	content     []byte
	lineOffsets []int
}

// newPositionTable indexes content's line starts. "\r\n" is one break, and so
// is a bare "\r".
func newPositionTable(content []byte) *positionTable {
	offsets := []int{0}
	for i := 0; i < len(content); i++ {
		switch content[i] {
		case '\n':
			offsets = append(offsets, i+1)
		case '\r':
			if i+1 < len(content) && content[i+1] == '\n' {
				offsets = append(offsets, i+2)
				i++
			} else {
				offsets = append(offsets, i+1)
			}
		}
	}
	return &positionTable{content: content, lineOffsets: offsets}
}

// positionAt returns the 1-based line and rune column of byteOffset.
//
// An offset outside [0, len(content)] gives [location.UnknownPosition]. An
// offset inside a rune reports that rune's column, and len(content) is the
// end-of-input position.
//
// Columns decompose the line with [utf8.DecodeRune], which counts each byte of
// an invalid sequence as its own rune. Content is not guaranteed valid UTF-8 —
// a decoder offset can land inside a malformed sequence — and any other
// decomposition disagrees with the converter the rest of the module reads
// through.
func (t *positionTable) positionAt(byteOffset int) location.Position {
	if byteOffset < 0 || byteOffset > len(t.content) {
		return location.UnknownPosition()
	}
	line := t.lineAt(byteOffset)
	lineStart := t.lineOffsets[line-1]

	column := 1
	for i := lineStart; i < byteOffset; {
		_, size := utf8.DecodeRune(t.content[i:])
		next := i + size
		if next > byteOffset {
			break // byteOffset lies inside this rune, which is its column
		}
		i = next
		column++
	}
	return location.NewPosition(line, column, byteOffset)
}

// lineAt returns the 1-based line holding byteOffset.
func (t *positionTable) lineAt(byteOffset int) int {
	lo, hi := 0, len(t.lineOffsets)-1
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if t.lineOffsets[mid] <= byteOffset {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return lo + 1
}
