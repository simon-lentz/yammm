package lsp

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/creachadair/jrpc2"
	"github.com/creachadair/jrpc2/handler"

	"github.com/simon-lentz/yammm/format"
	"github.com/simon-lentz/yammm/lsp/internal/lsputil"
	"github.com/simon-lentz/yammm/lsp/internal/protocol"
)

// handleFormatting returns a handler for textDocument/formatting requests.
// params.Options (FormattingOptions) is intentionally ignored: yammm formatting
// is canonical (like gofmt) — tabs for indentation, trailing whitespace trimmed,
// final newline enforced. All style decisions are hardcoded.
func handleFormatting(ws Resolver, logger *slog.Logger) jrpc2.Handler {
	return formattingHandler(ws, logger, format.TokenStream)
}

// formattingHandler is [handleFormatting] with the formatter as a parameter, so
// a refusal is testable without an input the formatter mishandles.
func formattingHandler(ws Resolver, logger *slog.Logger, tokenStream func(string) (string, error)) jrpc2.Handler {
	return handler.New(func(_ context.Context, params *protocol.DocumentFormattingParams) ([]protocol.TextEdit, error) {
		uri := params.TextDocument.URI

		if lsputil.IsMarkdownURI(uri) {
			return []protocol.TextEdit{}, nil
		}

		logger.Debug("formatting request", "uri", uri)

		doc := ws.GetDocumentSnapshot(uri)
		if doc == nil {
			return nil, nil
		}

		formatted, formatErr := tokenStream(doc.Text)
		if formatErr != nil {
			if errors.Is(formatErr, format.ErrNotPreserved) {
				logger.Warn("formatting refused: the formatter would change the document's tokens or comments",
					"uri", uri, "error", formatErr)
			} else {
				logger.Debug("formatting skipped due to parse error", "uri", uri, "error", formatErr)
			}
			return []protocol.TextEdit{}, nil
		}

		if formatted == doc.Text {
			return []protocol.TextEdit{}, nil
		}

		lines := strings.Split(doc.Text, "\n")
		lastLine := len(lines) - 1
		lastLineContent := []byte(lines[lastLine])
		lastChar := lsputil.ByteLenToCharLen(lastLineContent, len(lastLineContent), ws.PositionEncoding())

		return []protocol.TextEdit{
			{
				Range: protocol.Range{
					Start: protocol.Position{Line: 0, Character: 0},
					End: protocol.Position{
						Line:      lsputil.ToUInteger(lastLine),
						Character: lsputil.ToUInteger(lastChar),
					},
				},
				NewText: formatted,
			},
		}, nil
	})
}
