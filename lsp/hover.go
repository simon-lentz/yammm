package lsp

import (
	"context"
	"log/slog"

	"github.com/creachadair/jrpc2"
	"github.com/creachadair/jrpc2/handler"

	"github.com/simon-lentz/yammm/lsp/internal/hover"
	"github.com/simon-lentz/yammm/lsp/internal/protocol"
)

// handleHover returns a handler for textDocument/hover requests.
func handleHover(ws Resolver, logger *slog.Logger) jrpc2.Handler {
	return handler.New(func(_ context.Context, params *protocol.HoverParams) (*protocol.Hover, error) {
		unit := ws.ResolveUnit(params.TextDocument.URI,
			int(params.Position.Line), int(params.Position.Character), true)
		if unit == nil {
			return nil, nil
		}

		result, err := hover.HoverAtPosition(unit.Snapshot, unit.Doc, unit.LocalLine, unit.LocalChar,
			ws.PositionEncoding(), logger)
		if err != nil || result == nil {
			return result, err //nolint:wrapcheck // thin dispatch to internal package
		}

		if unit.Remap != nil {
			result.Range = unit.Remap.RemapRangePtr(result.Range)
		}
		return result, nil
	})
}
