package lsp

import (
	"context"
	"log/slog"

	"github.com/creachadair/jrpc2"
	"github.com/creachadair/jrpc2/handler"

	"github.com/simon-lentz/yammm/lsp/internal/protocol"
)

// handleDidOpen returns a handler for textDocument/didOpen notifications.
func handleDidOpen(ws DocumentManager, logger *slog.Logger) jrpc2.Handler {
	return handler.New(func(_ context.Context, params *protocol.DidOpenTextDocumentParams) error {
		uri := params.TextDocument.URI
		logger.Debug("textDocument/didOpen",
			slog.String("uri", uri),
			slog.Int("version", int(params.TextDocument.Version)),
		)
		ws.OpenDocument(uri, int(params.TextDocument.Version), params.TextDocument.Text)
		return nil
	})
}

// handleDidChange returns a handler for textDocument/didChange notifications.
// Full-sync text extraction happens here at the protocol boundary — the workspace
// receives plain text, not protocol types.
func handleDidChange(ws DocumentManager, logger *slog.Logger) jrpc2.Handler {
	return handler.New(func(_ context.Context, params *protocol.DidChangeTextDocumentParams) error {
		uri := params.TextDocument.URI
		logger.Debug("textDocument/didChange",
			slog.String("uri", uri),
			slog.Int("version", int(params.TextDocument.Version)),
		)
		text, ok := protocol.ExtractFullSyncText(params.ContentChanges)
		if !ok {
			logger.Warn("non-full-sync change ignored", slog.String("uri", uri))
			return nil
		}
		ws.ChangeDocument(uri, int(params.TextDocument.Version), text)
		return nil
	})
}

// handleDidClose returns a handler for textDocument/didClose notifications.
func handleDidClose(ws DocumentManager, _ *slog.Logger) jrpc2.Handler {
	return handler.New(func(_ context.Context, params *protocol.DidCloseTextDocumentParams) error {
		ws.CloseDocument(params.TextDocument.URI)
		return nil
	})
}

// handleDidChangeWatchedFiles returns a handler for workspace/didChangeWatchedFiles notifications.
func handleDidChangeWatchedFiles(ws DocumentManager, logger *slog.Logger) jrpc2.Handler {
	return handler.New(func(_ context.Context, params *protocol.DidChangeWatchedFilesParams) error {
		for _, change := range params.Changes {
			logger.Debug("watched file changed",
				slog.String("uri", change.URI),
				slog.Int("type", int(change.Type)),
			)
			ws.FileChanged(change.URI, change.Type)
		}
		return nil
	})
}

// handleDidChangeWorkspaceFolders returns a handler for workspace/didChangeWorkspaceFolders notifications.
func handleDidChangeWorkspaceFolders(ws DocumentManager, logger *slog.Logger) jrpc2.Handler {
	return handler.New(func(_ context.Context, params *protocol.DidChangeWorkspaceFoldersParams) error {
		for _, folder := range params.Event.Removed {
			logger.Debug("workspace folder removed", slog.String("uri", folder.URI))
			ws.RemoveRoot(folder.URI)
		}
		for _, folder := range params.Event.Added {
			logger.Debug("workspace folder added", slog.String("uri", folder.URI))
			ws.AddRoot(folder.URI)
		}
		ws.ReanalyzeOpenDocuments()
		return nil
	})
}
