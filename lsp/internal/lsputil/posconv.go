package lsputil

import (
	"unicode/utf8"

	"github.com/simon-lentz/yammm/location"

	"github.com/simon-lentz/yammm/lsp/internal/protocol"
)

// ToUInteger safely converts an int to protocol.UInteger (uint32).
// Negative values are clamped to 0.
func ToUInteger(n int) protocol.UInteger {
	if n < 0 {
		return 0
	}
	return protocol.UInteger(n) //nolint:gosec // clamped to non-negative
}

// ByteOffsetFromLSP converts an LSP position, whose character counts UTF-16
// code units, to a byte offset.
//
// Mid-surrogate positions: If char points to the second code unit of a
// surrogate pair, we floor to the start of that rune.
//
// The character is counted in the text the client holds of the source, which
// [Texts] defines.
//
// Returns (offset, false) if the source is not found or the line is invalid.
// Callers should bail out when ok is false to avoid incorrect navigation.
func ByteOffsetFromLSP(sources *Texts, id location.SourceID, line, char int) (int, bool) {
	// Handle nil registry
	if sources == nil {
		return 0, false
	}

	// LSP line is 0-based; registry API is 1-based
	lineStart, ok := sources.characterOrigin(id, line+1)
	if !ok {
		return 0, false // line not found or source unknown
	}

	// Create a span covering the entire source for content lookup
	content, ok := sources.ContentBySource(id)
	if !ok {
		return 0, false // content unavailable
	}

	return UTF16CharToByteOffset(content, lineStart, char), true
}

// UTF16CharToByteOffset converts a UTF-16 character offset to a byte offset.
func UTF16CharToByteOffset(content []byte, lineStart, charOffset int) int {
	if charOffset <= 0 {
		return lineStart
	}

	pos := lineStart
	utf16Units := 0

	for pos < len(content) && utf16Units < charOffset {
		r, size := utf8.DecodeRune(content[pos:])
		if r == utf8.RuneError && size <= 1 {
			// Invalid UTF-8 byte: count as 1 UTF-16 unit
			utf16Units++
			pos++
			continue
		}

		// Stop at newline (line boundary)
		if r == '\n' {
			break
		}

		// Runes in the BMP (U+0000 to U+FFFF) take 1 UTF-16 code unit
		// Runes above BMP (U+10000+) require 2 UTF-16 code units (surrogate pair)
		if r > 0xFFFF {
			// If we're asked for the second half of a surrogate pair,
			// floor to the start of the rune
			if utf16Units+2 > charOffset && utf16Units+1 == charOffset {
				// Requesting mid-surrogate: return start of this rune
				return pos
			}
			utf16Units += 2
		} else {
			utf16Units++
		}
		pos += size
	}

	return pos
}

// PositionFromLSP converts an LSP position to an internal location.Position.
// It converts the client's column, in UTF-16 units, to a rune column through
// the text registered for the source, counting line 1 of a file read from disk
// from after a leading byte order mark.
//
// This is the primary entry point for inbound LSP position conversion.
// The hover and definition providers use it instead of naive column
// arithmetic; completion takes a byte offset from [ByteOffsetFromLSP].
//
// Returns (position, false) if the source is not found or the line is invalid.
// Callers should bail out when ok is false to avoid incorrect navigation.
func PositionFromLSP(
	sources *Texts,
	sourceID location.SourceID,
	lspLine, lspChar int,
) (location.Position, bool) {
	// Convert LSP position to byte offset
	byteOffset, ok := ByteOffsetFromLSP(sources, sourceID, lspLine, lspChar)
	if !ok {
		return location.Position{}, false
	}

	// Use source registry's PositionAt for accurate line/column computation
	return sources.PositionAt(sourceID, byteOffset), true
}

// ByteToUTF16Offset converts a byte offset on a line to UTF-16 code units.
// This is the inverse of UTF16CharToByteOffset, used for outbound conversion.
//
// Parameters:
//   - content: the full source content
//   - lineStart: byte offset of the start of the line
//   - targetByte: byte offset to convert (must be >= lineStart)
//
// Returns the number of UTF-16 code units from lineStart to targetByte.
func ByteToUTF16Offset(content []byte, lineStart, targetByte int) int {
	if targetByte <= lineStart {
		return 0
	}

	utf16Units := 0
	pos := lineStart

	for pos < targetByte && pos < len(content) {
		r, size := utf8.DecodeRune(content[pos:])
		if r == utf8.RuneError && size <= 1 {
			// Invalid UTF-8 byte: count as 1 UTF-16 unit
			utf16Units++
			pos++
			continue
		}

		// Stop at newline (shouldn't happen if targetByte is on same line)
		if r == '\n' {
			break
		}

		// Check if we would go past targetByte
		if pos+size > targetByte {
			break
		}

		// Runes above BMP require 2 UTF-16 code units (surrogate pair)
		if r > 0xFFFF {
			utf16Units += 2
		} else {
			utf16Units++
		}
		pos += size
	}

	return utf16Units
}

// SpanToLSPRange converts a location.Span to an LSP Range. For a source that
// sources holds, it converts each byte offset to UTF-16 units and counts each
// character in the text the client holds of the source, which [Texts] defines.
// For a source that sources does not hold, for a position with no byte offset,
// and for a line the held text does not reach, it returns the rune column less
// one; an end that is not known takes the start's position.
// It returns zero positions and false for a nil sources, a zero span and a
// span whose start is not known.
func SpanToLSPRange(sources *Texts, span location.Span) (start, end [2]int, ok bool) {
	if span.IsZero() || !span.Start.IsKnown() {
		return [2]int{}, [2]int{}, false
	}

	// Handle nil sources registry - return false to trigger fallback path
	if sources == nil {
		return [2]int{}, [2]int{}, false
	}

	// Get content for position conversion
	content, hasContent := sources.ContentBySource(span.Source)

	// Convert start position
	startLine := max(span.Start.Line-1, 0)
	var startChar int
	if hasContent && span.Start.Byte >= 0 {
		lineStartByte, lineOk := sources.characterOrigin(span.Source, span.Start.Line)
		if lineOk {
			startChar = ByteToUTF16Offset(content, lineStartByte, span.Start.Byte)
		} else {
			startChar = span.Start.Column - 1 // fallback to rune column
		}
	} else {
		// No content or unknown byte offset: use rune column as-is
		startChar = span.Start.Column - 1
	}

	// Convert end position
	endLine := startLine
	endChar := startChar
	if span.End.IsKnown() {
		endLine = max(span.End.Line-1, 0)
		if hasContent && span.End.Byte >= 0 {
			lineStartByte, lineOk := sources.characterOrigin(span.Source, span.End.Line)
			if lineOk {
				endChar = ByteToUTF16Offset(content, lineStartByte, span.End.Byte)
			} else {
				endChar = span.End.Column - 1 // fallback to rune column
			}
		} else {
			// No content or unknown byte offset: use rune column
			endChar = span.End.Column - 1
		}
	}

	return [2]int{startLine, startChar}, [2]int{endLine, endChar}, true
}

// SpanToProtocolRange converts a yammm Span to an LSP protocol.Range through
// [SpanToLSPRange]. When that gives no range (a zero span, an unknown start or
// nil sources), the range is the span's own lines and columns, each less one
// and at least zero.
func SpanToProtocolRange(sources *Texts, span location.Span) protocol.Range {
	start, end, ok := SpanToLSPRange(sources, span)
	if ok {
		return protocol.Range{
			Start: protocol.Position{
				Line:      ToUInteger(start[0]),
				Character: ToUInteger(start[1]),
			},
			End: protocol.Position{
				Line:      ToUInteger(end[0]),
				Character: ToUInteger(end[1]),
			},
		}
	}
	// Fallback: column-based approximation (1-based to 0-based)
	return protocol.Range{
		Start: protocol.Position{
			Line:      ToUInteger(max(span.Start.Line-1, 0)),
			Character: ToUInteger(max(span.Start.Column-1, 0)),
		},
		End: protocol.Position{
			Line:      ToUInteger(max(span.End.Line-1, 0)),
			Character: ToUInteger(max(span.End.Column-1, 0)),
		},
	}
}
