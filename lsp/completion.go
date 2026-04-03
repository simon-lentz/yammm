package lsp

import (
	"context"
	"log/slog"

	"github.com/creachadair/jrpc2"
	"github.com/creachadair/jrpc2/handler"

	"github.com/simon-lentz/yammm/lsp/internal/completion"
	"github.com/simon-lentz/yammm/lsp/internal/protocol"
)

// handleCompletion returns a handler for textDocument/completion requests.
func handleCompletion(ws Resolver, logger *slog.Logger) jrpc2.Handler {
	return handler.New(func(_ context.Context, params *protocol.CompletionParams) ([]protocol.CompletionItem, error) {
		unit := ws.ResolveUnit(params.TextDocument.URI,
			int(params.Position.Line), int(params.Position.Character), false)
		if unit == nil {
			return nil, nil
		}

		items := completion.Complete(unit.Snapshot, unit.Doc, unit.LocalLine, unit.LocalChar,
			ws.PositionEncoding(), logger)
		return items, nil
	})
}
