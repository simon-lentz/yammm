package json

import (
	"cmp"
	"slices"
	"unicode/utf8"

	"github.com/simon-lentz/yammm/location"
)

// positionTable converts a byte offset in one document to a [location.Position].
//
// Build it over the document's own bytes, never over the buffer jsonc returns:
// jsonc replaces each comment byte with one space, so a multibyte rune inside a
// comment moves every later rune column on that line.
//
// A column is counted from the nearest checkpoint at or before the offset, not
// from the line start, so a document written on one line costs a bounded scan
// per position rather than one proportional to the line.
type positionTable struct {
	content     []byte
	lineOffsets []int

	// marks holds, per 0-based line longer than markStride, the checkpoints
	// marksFor records; a line is decomposed once, on its first long lookup.
	marks map[int][]columnMark
}

// columnMark is a rune start on a line and that rune's column.
type columnMark struct {
	at, column int
}

// markStride is the byte distance between a line's checkpoints.
const markStride = 256

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
	i, column := t.lineOffsets[line-1], 1
	if byteOffset-i > markStride {
		marks := t.marksFor(line - 1)
		// The last checkpoint at or before byteOffset; the line start is one.
		n, _ := slices.BinarySearchFunc(marks, byteOffset, func(m columnMark, off int) int {
			return cmp.Compare(m.at, off+1)
		})
		if n > 0 {
			i, column = marks[n-1].at, marks[n-1].column
		}
	}
	for i < byteOffset {
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

// marksFor returns the checkpoints of a 0-based line: the first rune start at
// or after every markStride bytes from the line start, found by the same
// decomposition positionAt applies, so counting on from a checkpoint gives the
// column counting from the line start gives.
func (t *positionTable) marksFor(line int) []columnMark {
	if marks, ok := t.marks[line]; ok {
		return marks
	}
	end := len(t.content)
	if line+1 < len(t.lineOffsets) {
		end = t.lineOffsets[line+1]
	}
	var marks []columnMark
	start := t.lineOffsets[line]
	next := start + markStride
	for i, column := start, 1; i < end; column++ {
		if i >= next {
			marks = append(marks, columnMark{at: i, column: column})
			next = i - (i-start)%markStride + markStride
		}
		_, size := utf8.DecodeRune(t.content[i:])
		i += size
	}
	if t.marks == nil {
		t.marks = make(map[int][]columnMark)
	}
	t.marks[line] = marks
	return marks
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
