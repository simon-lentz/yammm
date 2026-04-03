package lsp

import (
	"context"
	"log/slog"
	"path/filepath"

	"github.com/creachadair/jrpc2"
	"github.com/creachadair/jrpc2/handler"

	"github.com/simon-lentz/yammm/lsp/internal/definition"
	"github.com/simon-lentz/yammm/lsp/internal/lsputil"
	"github.com/simon-lentz/yammm/lsp/internal/protocol"
)

// handleDefinition returns a handler for textDocument/definition requests.
func handleDefinition(ws Resolver, logger *slog.Logger) jrpc2.Handler {
	return handler.New(func(_ context.Context, params *protocol.DefinitionParams) (*protocol.Location, error) {
		unit := ws.ResolveUnit(params.TextDocument.URI,
			int(params.Position.Line), int(params.Position.Character), true)
		if unit == nil {
			return nil, nil
		}

		loc := definition.AtPosition(unit.Snapshot, unit.Doc, unit.LocalLine, unit.LocalChar,
			ws.PositionEncoding(), ws.RemapPathToURI, logger)
		if loc == nil {
			return nil, nil
		}

		// Markdown block remap
		if unit.Remap != nil {
			block := unit.Remap.Block()
			locPath, pathErr := lsputil.URIToPath(loc.URI)
			if pathErr == nil && filepath.ToSlash(locPath) == block.SourceID.String() {
				loc.URI = unit.Remap.DocumentURI()
				loc.Range = unit.Remap.RemapRange(loc.Range)
			}
		}

		return loc, nil
	})
}
