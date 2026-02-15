package diag

import (
	"net/url"
	"unicode/utf8"

	"github.com/simon-lentz/yammm/location"
)

// LSP Diagnostic Severity values per LSP specification.
const (
	LSPSeverityError       = 1
	LSPSeverityWarning     = 2
	LSPSeverityInformation = 3
	LSPSeverityHint        = 4
)

// LSPDiagnostic is the LSP Diagnostic structure.
//
// See: https://microsoft.github.io/language-server-protocol/specifications/lsp/3.17/specification/#diagnostic
type LSPDiagnostic struct {
	Range              LSPRange         `json:"range"`
	Severity           int              `json:"severity"`
	Code               string           `json:"code,omitzero"`
	Source             string           `json:"source"`
	Message            string           `json:"message"`
	RelatedInformation []LSPRelatedInfo `json:"relatedInformation,omitzero"`
}

// LSPRange is the LSP Range structure with 0-based positions.
type LSPRange struct {
	Start LSPPosition `json:"start"`
	End   LSPPosition `json:"end"`
}

// LSPPosition is the LSP Position structure.
//
// Line is 0-based (unlike our 1-based positions).
// Character is the UTF-16 code unit offset (not byte offset, not rune offset).
type LSPPosition struct {
	Line      int `json:"line"`      // 0-based line number
	Character int `json:"character"` // UTF-16 code unit offset from line start
}

// LSPRelatedInfo is the LSP DiagnosticRelatedInformation structure.
type LSPRelatedInfo struct {
	Location LSPLocation `json:"location"`
	Message  string      `json:"message"`
}

// LSPLocation is the LSP Location structure.
type LSPLocation struct {
	URI   string   `json:"uri"`
	Range LSPRange `json:"range"`
}

// LSPDiagnostic converts an Issue to an LSP Diagnostic.
//
// Returns nil if the issue doesn't have a valid span or if byte offset
// conversion fails and the renderer is configured with LSPByteFallbackOmit.
//
// The Source field is set to "yammm" to identify the diagnostic source.
func (r *Renderer) LSPDiagnostic(issue Issue) *LSPDiagnostic {
	if !issue.HasSpan() {
		return nil
	}

	span := issue.Span()
	if !span.Start.IsKnown() {
		return nil
	}

	// Convert start position
	startPos, ok := r.toLSPPosition(span, span.Start)
	if !ok {
		return nil
	}

	// Convert end position (use start if end is invalid)
	var endPos LSPPosition
	if span.End.IsKnown() {
		endPos, ok = r.toLSPPosition(span, span.End)
		if !ok {
			// If end conversion fails but start succeeded, use start
			endPos = startPos
		}
	} else {
		endPos = startPos
	}

	diag := &LSPDiagnostic{
		Range: LSPRange{
			Start: startPos,
			End:   endPos,
		},
		Severity: SeverityToLSP(issue.Severity()),
		Code:     issue.Code().String(),
		Source:   "yammm",
		Message:  issue.Message(),
	}

	// Add related information
	related := issue.Related()
	if len(related) > 0 {
		diag.RelatedInformation = make([]LSPRelatedInfo, 0, len(related))
		for _, rel := range related {
			if lspRel := r.toLSPRelatedInfo(rel); lspRel != nil {
				diag.RelatedInformation = append(diag.RelatedInformation, *lspRel)
			}
		}
		if len(diag.RelatedInformation) == 0 {
			diag.RelatedInformation = nil
		}
	}

	return diag
}

// LSPDiagnostics converts all issues in a Result to LSP Diagnostics.
//
// Issues without valid spans are skipped. See [Renderer.LSPDiagnostic] for
// conversion details.
//
// Returns an empty slice (not nil) when there are no diagnostics, for
// consistent JSON serialization as "[]" rather than "null".
func (r *Renderer) LSPDiagnostics(res Result) []LSPDiagnostic {
	diagnostics := make([]LSPDiagnostic, 0)

	for issue := range res.Issues() {
		if lspDiag := r.LSPDiagnostic(issue); lspDiag != nil {
			diagnostics = append(diagnostics, *lspDiag)
		}
	}

	return diagnostics
}

// sourceIDToURI converts a SourceID to an LSP-compatible URI.
//
// File-backed sources become file:// URIs; synthetic sources (test://, inline:, etc.)
// pass through as-is since they already have URI-like schemes.
func sourceIDToURI(source location.SourceID) string {
	if cp, ok := source.CanonicalPath(); ok {
		// File-backed: convert to file:// URI with proper percent-encoding.
		// Use url.URL to correctly encode special characters like spaces.
		u := url.URL{
			Scheme: "file",
			Path:   cp.String(),
		}
		return u.String()
	}
	// Synthetic: return as-is (already URI-like)
	return source.String()
}

// SeverityToLSP converts our Severity to LSP severity value.
func SeverityToLSP(sev Severity) int {
	switch sev {
	case Fatal, Error:
		return LSPSeverityError
	case Warning:
		return LSPSeverityWarning
	case Info:
		return LSPSeverityInformation
	case Hint:
		return LSPSeverityHint
	default:
		return LSPSeverityError
	}
}

// toLSPPosition converts a location.Position to an LSPPosition.
//
// Returns (LSPPosition, false) if conversion fails.
func (r *Renderer) toLSPPosition(span location.Span, pos location.Position) (LSPPosition, bool) {
	// Line: 1-based → 0-based
	line := max(pos.Line-1, 0)

	// Character: need UTF-16 code unit offset from line start
	character, ok := r.computeUTF16Character(span, pos)
	if !ok {
		return LSPPosition{}, false
	}

	return LSPPosition{
		Line:      line,
		Character: character,
	}, true
}

// computeUTF16Character computes the UTF-16 code unit offset for a position.
//
// This handles the conversion from our rune-based column (or byte offset) to
// UTF-16 code units as required by LSP.
//
// Strategy:
//  1. If we have a LineIndexProvider and byte offset is known, compute exact UTF-16 offset
//  2. If we have content but no line index, scan content to find line start (slow path)
//  3. If byte offset is unknown and LSPByteFallbackApproximate is set, use Column-1
//  4. Otherwise (LSPByteFallbackOmit), return false
func (r *Renderer) computeUTF16Character(span location.Span, pos location.Position) (int, bool) {
	// Check if we have byte offset and can compute exact UTF-16 offset
	if pos.Byte >= 0 && r.provider != nil {
		// Fast path: LineIndexProvider available
		if lineProvider, ok := r.provider.(LineIndexProvider); ok {
			lineStart, hasLineStart := lineProvider.LineStartByte(span.Source, pos.Line)
			if hasLineStart {
				content, hasContent := r.provider.Content(span)
				if hasContent {
					// Compute exact UTF-16 offset
					return utf16OffsetFromByte(content, lineStart, pos.Byte), true
				}
			}
		}

		// Slow path: content available but no LineIndexProvider
		// Scan content to find line start byte offset
		if content, ok := r.provider.Content(span); ok {
			lineStart := findLineStartByte(content, pos.Line)
			if lineStart >= 0 && pos.Byte >= lineStart {
				return utf16OffsetFromByte(content, lineStart, pos.Byte), true
			}
		}
	}

	// Byte offset unknown or provider unavailable
	switch r.lspByteFallback {
	case LSPByteFallbackApproximate:
		// Use Column-1 as approximation (correct for ASCII/BMP text)
		return pos.Column - 1, true
	case LSPByteFallbackOmit:
		fallthrough
	default:
		return 0, false
	}
}

// findLineStartByte scans content to find the byte offset of a line's start.
// Returns -1 if the line is not found.
func findLineStartByte(content []byte, lineNum int) int {
	if lineNum < 1 {
		return -1
	}
	if lineNum == 1 {
		return 0
	}

	currentLine := 1
	for i := range content {
		if content[i] == '\n' {
			currentLine++
			if currentLine == lineNum {
				return i + 1
			}
		}
	}
	return -1
}

// utf16OffsetFromByte computes the UTF-16 code unit offset within a line.
//
// Given the byte offset of the line start and the target byte offset,
// returns the number of UTF-16 code units from line start to target.
//
// Mid-rune semantics: If targetByte falls in the middle of a multi-byte rune,
// the function returns the offset of that rune's start (floor semantics), not
// the offset after it. This ensures diagnostics point to the containing
// character rather than the next character.
//
// This correctly handles:
//   - ASCII (1 byte, 1 UTF-16 code unit)
//   - BMP characters (1-3 bytes, 1 UTF-16 code unit)
//   - Non-BMP characters like emoji (4 bytes, 2 UTF-16 code units / surrogate pair)
func utf16OffsetFromByte(content []byte, lineStart, targetByte int) int {
	if targetByte <= lineStart {
		return 0
	}

	// Limit to content bounds
	end := min(targetByte, len(content))

	// Count UTF-16 code units
	utf16Offset := 0
	for pos := lineStart; pos < end; {
		r, size := utf8.DecodeRune(content[pos:])
		if r == utf8.RuneError && size <= 1 {
			// Invalid UTF-8 byte: count only if fully before target
			if pos+1 > end {
				break
			}
			utf16Offset++
			pos++
			continue
		}

		// Only count this rune if targetByte is at or after the rune's end byte.
		// If targetByte falls within the rune (mid-rune), stop without counting it.
		if pos+size > end {
			break
		}

		// Runes in the BMP (U+0000 to U+FFFF) take 1 UTF-16 code unit
		// Runes above BMP (U+10000+) require 2 UTF-16 code units (surrogate pair)
		if r > 0xFFFF {
			utf16Offset += 2
		} else {
			utf16Offset++
		}
		pos += size
	}

	return utf16Offset
}

// toLSPRelatedInfo converts a location.RelatedInfo to LSPRelatedInfo.
func (r *Renderer) toLSPRelatedInfo(rel location.RelatedInfo) *LSPRelatedInfo {
	if rel.Span.IsZero() || !rel.Span.Start.IsKnown() {
		return nil
	}

	startPos, ok := r.toLSPPosition(rel.Span, rel.Span.Start)
	if !ok {
		return nil
	}

	endPos := startPos
	if rel.Span.End.IsKnown() {
		if ep, ok := r.toLSPPosition(rel.Span, rel.Span.End); ok {
			endPos = ep
		}
	}

	return &LSPRelatedInfo{
		Location: LSPLocation{
			URI: sourceIDToURI(rel.Span.Source),
			Range: LSPRange{
				Start: startPos,
				End:   endPos,
			},
		},
		Message: rel.Message,
	}
}
