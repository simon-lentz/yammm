package lsp

import (
	"context"
	"strings"

	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema/load"
)

// textDocumentFormatting handles textDocument/formatting requests.
// params.Options (FormattingOptions) is intentionally ignored: yammm formatting
// is canonical (like gofmt) — tabs for indentation, trailing whitespace trimmed,
// final newline enforced. All style decisions are hardcoded.
func (s *Server) textDocumentFormatting(_ *glsp.Context, params *protocol.DocumentFormattingParams) ([]protocol.TextEdit, error) {
	uri := params.TextDocument.URI

	if isMarkdownURI(uri) {
		return []protocol.TextEdit{}, nil
	}

	s.logger.Debug("formatting request",
		"uri", uri,
	)

	// Get the document snapshot
	doc := s.workspace.GetDocumentSnapshot(uri)
	if doc == nil {
		return nil, nil
	}

	// Check for syntax errors only - semantic errors like unresolved imports
	// should not prevent formatting. This ensures we don't corrupt files with
	// syntax errors while still allowing formatting for files with imports.
	ctx := context.Background()
	_, result, err := load.LoadString(ctx, doc.Text, "format-check")
	if err != nil {
		s.logger.Debug("formatting skipped due to load error",
			"uri", uri,
			"error", err,
		)
		return []protocol.TextEdit{}, nil
	}

	// Only skip formatting if there are syntax errors (not semantic errors)
	if hasSyntaxErrors(result) {
		s.logger.Debug("formatting skipped due to syntax errors",
			"uri", uri,
		)
		return []protocol.TextEdit{}, nil
	}

	// Format the document with parse-aware token spacing. Fall back to the
	// conservative line-by-line formatter if internal formatting fails.
	formatted, formatErr := formatTokenStream(doc.Text)
	if formatErr != nil {
		s.logger.Debug("token-stream formatting failed, falling back",
			"uri", uri,
			"error", formatErr,
		)
		formatted = formatDocument(doc.Text)
	}

	// If no changes, return empty edits
	if formatted == doc.Text {
		return []protocol.TextEdit{}, nil
	}

	// Return a single edit that replaces the entire document
	lines := strings.Split(doc.Text, "\n")
	lastLine := len(lines) - 1
	lastLineContent := []byte(lines[lastLine])

	// Convert byte length based on negotiated position encoding.
	// UTF-8: character offset IS byte offset (no conversion needed)
	// UTF-16: convert byte offset to UTF-16 code units for non-ASCII safety
	var lastChar int
	switch s.workspace.PositionEncoding() {
	case PositionEncodingUTF8:
		// UTF-8: character offset is byte offset
		lastChar = len(lastLineContent)
	case PositionEncodingUTF16:
		fallthrough
	default:
		// UTF-16 (default): convert byte offset to UTF-16 code units
		// ByteToUTF16Offset(content, lineStart, targetByte) - pass 0 as lineStart
		// since we're converting just the line content
		lastChar = ByteToUTF16Offset(lastLineContent, 0, len(lastLineContent))
	}

	return []protocol.TextEdit{
		{
			Range: protocol.Range{
				Start: protocol.Position{Line: 0, Character: 0},
				End: protocol.Position{
					Line:      toUInteger(lastLine),
					Character: toUInteger(lastChar),
				},
			},
			NewText: formatted,
		},
	}, nil
}

// formatDocument applies canonical formatting rules to a YAMMM document.
//
// Implementation Note: This formatter uses line-by-line string processing rather
// than AST walking. This approach correctly normalizes whitespace and preserves
// comments positionally, but cannot safely reorder declarations while maintaining
// comment associations. This is acceptable since the current formatting rules do
// not require semantic reordering. If declaration reordering is needed in the
// future, an AST-based formatter should be implemented.
//
// Rules:
// - Tabs for indentation (spaces converted to tabs: 4 spaces = 1 tab)
// - LF line endings
// - No trailing whitespace
// - Preserve blank lines (conservative: maintains visual structure between declarations)
// - Preserve comment text and line positions (indentation is normalized)
func formatDocument(text string) string {
	// Normalize line endings to LF
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")

	lines := strings.Split(text, "\n")
	result := make([]string, 0, len(lines))

	for _, line := range lines {
		// Remove trailing whitespace but preserve leading whitespace (indentation)
		trimmedRight := strings.TrimRight(line, " \t")

		// Normalize indentation: convert spaces to tabs (canonical format)
		normalized := normalizeIndentation(trimmedRight)

		// Check if line is blank (only whitespace)
		isBlank := strings.TrimSpace(line) == ""

		if isBlank {
			// Preserve all blank lines - maintains visual structure between declarations
			result = append(result, "")
		} else {
			result = append(result, normalized)
		}
	}

	// Remove trailing blank lines
	for len(result) > 0 && result[len(result)-1] == "" {
		result = result[:len(result)-1]
	}

	// Ensure file ends with newline
	formatted := strings.Join(result, "\n")
	if formatted != "" && !strings.HasSuffix(formatted, "\n") {
		formatted += "\n"
	}

	return formatted
}

// normalizeIndentation converts spaces to tabs for indentation.
// Each 4 spaces at the start of a line becomes 1 tab.
func normalizeIndentation(line string) string {
	if line == "" {
		return line
	}

	// Count leading whitespace
	leadingWS := 0
	for _, r := range line {
		if r == ' ' || r == '\t' {
			leadingWS++
		} else {
			break
		}
	}

	if leadingWS == 0 {
		return line
	}

	// Extract leading whitespace and content
	leading := line[:leadingWS]
	content := line[leadingWS:]

	// Convert to tabs: count equivalent spaces (tab = 4 spaces)
	spaceCount := 0
	for _, r := range leading {
		if r == '\t' {
			spaceCount += 4
		} else {
			spaceCount++
		}
	}

	// Convert to tabs
	tabs := spaceCount / 4
	remaining := spaceCount % 4

	return strings.Repeat("\t", tabs) + strings.Repeat(" ", remaining) + content
}

// hasSyntaxErrors checks if the result contains any syntax parsing errors.
// This is used by formatting to distinguish between:
//   - Syntax errors (unparseable file - don't format)
//   - Semantic errors like unresolved imports (formattable file)
//
// We iterate Issues() rather than Errors() and explicitly check severity to be
// robust against future syntax diagnostics that might use different severities.
// This blocks formatting for Fatal/Error syntax issues but allows formatting
// for files with Warning-level syntax diagnostics (e.g., deprecation warnings).
func hasSyntaxErrors(result diag.Result) bool {
	for issue := range result.Issues() {
		if issue.Code().Category() == diag.CategorySyntax &&
			issue.Severity().IsAtLeastAsSevereAs(diag.Error) {
			return true
		}
	}
	return false
}
