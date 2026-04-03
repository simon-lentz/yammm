package lsp

import (
	"context"
	"log/slog"

	"github.com/creachadair/jrpc2"
	"github.com/creachadair/jrpc2/handler"

	"github.com/simon-lentz/yammm/lsp/internal/protocol"
	"github.com/simon-lentz/yammm/lsp/internal/symbols"
	"github.com/simon-lentz/yammm/lsp/internal/workspace"
)

// handleDocumentSymbol returns a handler for textDocument/documentSymbol requests.
func handleDocumentSymbol(ws Resolver, logger *slog.Logger) jrpc2.Handler {
	return handler.New(func(_ context.Context, params *protocol.DocumentSymbolParams) ([]protocol.DocumentSymbol, error) {
		uri := params.TextDocument.URI

		logger.Debug("documentSymbol request", "uri", uri)

		units := ws.ResolveAllUnits(uri)
		if units == nil {
			return nil, nil
		}

		var allSymbols []protocol.DocumentSymbol
		for _, unit := range units {
			syms := symbols.DocumentSymbolsFor(unit.Snapshot.ToSymbolView(), unit.Doc.URI, unit.Doc.SourceID, unit.Doc.Version,
				ws.PositionEncoding(), logger)
			if len(syms) > 0 && unit.Remap != nil {
				syms = workspace.RemapDocumentSymbolRanges(syms, unit.Remap)
			}
			allSymbols = append(allSymbols, syms...)
		}
		return allSymbols, nil
	})
}
