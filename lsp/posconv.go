package lsp

import (
	"bytes"
	"unicode/utf8"

	"github.com/simon-lentz/yammm/internal/source"
	"github.com/simon-lentz/yammm/location"
)

// ByteOffsetFromLSP converts an LSP position to a byte offset.
//
// This is the inverse of the UTF-16 conversion in diag/lsp.go.
// It handles:
//   - UTF-16 encoding: character offset is in UTF-16 code units
//   - UTF-8 encoding: character offset IS byte offset from line start
//
// Mid-surrogate positions (UTF-16): If char points to the second code unit
// of a surrogate pair, we floor to the start of that rune.
//
// Returns (offset, false) if the source is not found or the line is invalid.
// Callers should bail out when ok is false to avoid incorrect navigation.
func ByteOffsetFromLSP(sources *source.Registry, id location.SourceID, line, char int, enc PositionEncoding) (int, bool) {
	// Handle nil registry
	if sources == nil {
		return 0, false
	}

	// LSP line is 0-based; registry API is 1-based
	lineStart, ok := sources.LineStartByte(id, line+1)
	if !ok {
		return 0, false // line not found or source unknown
	}

	// Create a span covering the entire source for content lookup
	content, ok := sources.ContentBySource(id)
	if !ok {
		return 0, false // content unavailable
	}

	switch enc {
	case PositionEncodingUTF16:
		return utf16CharToByteOffset(content, lineStart, char), true
	case PositionEncodingUTF8:
		// UTF-8 encoding: character offset IS byte offset from line start
		// Clamp to end-of-line (not end-of-file) to match utf16CharToByteOffset behavior
		return clampToLineEnd(content, lineStart, lineStart+char), true
	default:
		return utf16CharToByteOffset(content, lineStart, char), true // default UTF-16
	}
}

// utf16CharToByteOffset converts a UTF-16 character offset to a byte offset.
func utf16CharToByteOffset(content []byte, lineStart, charOffset int) int {
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

// clampToLineEnd ensures offset doesn't exceed the end of the current line.
// Returns the lesser of: offset, position of next newline, or content length.
// This is used for UTF-8 encoding mode to match the behavior of utf16CharToByteOffset
// which stops at newline boundaries.
//
// For conceptual correctness, offsets before lineStart are clamped to lineStart
// (though LSP inputs should never be negative).
func clampToLineEnd(content []byte, lineStart, offset int) int {
	if offset < lineStart {
		return lineStart
	}
	// Use slice-based scanning for efficiency
	lineContent := content[lineStart:]
	if idx := bytes.IndexByte(lineContent, '\n'); idx >= 0 {
		lineEnd := lineStart + idx
		if offset > lineEnd {
			return lineEnd
		}
	} else if offset > len(content) {
		return len(content)
	}
	return offset
}

// PositionFromLSP converts an LSP position to an internal location.Position.
// Uses the source registry for accurate UTF-16 → rune column conversion.
//
// This is the primary entry point for inbound LSP position conversion.
// Use this in all providers (definition, hover, completion) instead of
// naive column arithmetic.
//
// Returns (position, false) if the source is not found or the line is invalid.
// Callers should bail out when ok is false to avoid incorrect navigation.
func PositionFromLSP(
	sources *source.Registry,
	sourceID location.SourceID,
	lspLine, lspChar int,
	enc PositionEncoding,
) (location.Position, bool) {
	// Convert LSP position to byte offset
	byteOffset, ok := ByteOffsetFromLSP(sources, sourceID, lspLine, lspChar, enc)
	if !ok {
		return location.Position{}, false
	}

	// Use source registry's PositionAt for accurate line/column computation
	return sources.PositionAt(sourceID, byteOffset), true
}

// ByteToUTF16Offset converts a byte offset on a line to UTF-16 code units.
// This is the inverse of utf16CharToByteOffset, used for outbound conversion.
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

// SpanToLSPRange converts a location.Span to an LSP Range.
// Uses the source registry for accurate UTF-16 or UTF-8 conversion.
//
// The encoding parameter should be the negotiated position encoding from
// the client (default UTF-16).
func SpanToLSPRange(sources *source.Registry, span location.Span, enc PositionEncoding) (start, end [2]int, ok bool) {
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
		lineStartByte, lineOk := sources.LineStartByte(span.Source, span.Start.Line)
		if lineOk {
			switch enc {
			case PositionEncodingUTF16:
				startChar = ByteToUTF16Offset(content, lineStartByte, span.Start.Byte)
			case PositionEncodingUTF8:
				// UTF-8 mode: character offset IS byte offset from line start
				startChar = span.Start.Byte - lineStartByte
			default:
				startChar = ByteToUTF16Offset(content, lineStartByte, span.Start.Byte)
			}
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
			lineStartByte, lineOk := sources.LineStartByte(span.Source, span.End.Line)
			if lineOk {
				switch enc {
				case PositionEncodingUTF16:
					endChar = ByteToUTF16Offset(content, lineStartByte, span.End.Byte)
				case PositionEncodingUTF8:
					// UTF-8 mode: character offset IS byte offset from line start
					endChar = span.End.Byte - lineStartByte
				default:
					endChar = ByteToUTF16Offset(content, lineStartByte, span.End.Byte)
				}
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
