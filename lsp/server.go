// Package lsp implements a Language Server Protocol server for YAMMM schema files.
package lsp

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strings"
	"sync"

	// commonlog is a required dependency of github.com/tliron/glsp.
	// We silence it in NewServer() via commonlog.Configure(0, nil) because
	// this server uses slog for all logging. The blank import of the "simple"
	// backend is required by glsp at runtime.
	"github.com/tliron/commonlog"
	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
	"github.com/tliron/glsp/server"

	_ "github.com/tliron/commonlog/simple" // required backend for glsp
)

const (
	serverName = "yammm-lsp"
)

// Config holds the server configuration.
type Config struct {
	// ModuleRoot overrides the computed module root for import resolution.
	ModuleRoot string
}

// Server is the YAMMM language server.
type Server struct {
	logger    *slog.Logger
	config    Config
	handler   protocol.Handler
	server    *server.Server
	workspace *Workspace

	// shutdownCalled tracks whether shutdown was called before exit (LSP lifecycle)
	shutdownCalled bool

	// closeOnce ensures Close is idempotent
	closeOnce sync.Once
	closeErr  error
}

// NewServer creates a new YAMMM language server.
// If logger is nil, slog.Default() is used.
func NewServer(logger *slog.Logger, cfg Config) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	s := &Server{
		logger:    logger.With(slog.String("component", "server")),
		config:    cfg,
		workspace: NewWorkspace(logger, cfg), // Pass base logger; workspace adds its own component
	}

	// Silence commonlog - glsp uses it internally but we use slog for all logging.
	commonlog.Configure(0, nil)

	s.handler = protocol.Handler{
		// Lifecycle
		Initialize:    s.initialize,
		Initialized:   s.initialized,
		Shutdown:      s.shutdown,
		Exit:          s.exit,
		SetTrace:      s.setTrace,
		CancelRequest: s.cancelRequest,

		// Text Document Synchronization (Phase 1)
		TextDocumentDidOpen:   s.textDocumentDidOpen,
		TextDocumentDidChange: s.textDocumentDidChange,
		TextDocumentDidClose:  s.textDocumentDidClose,

		// Language Features (Phases 2-6) - stubs for now
		TextDocumentDefinition:     s.textDocumentDefinition,
		TextDocumentHover:          s.textDocumentHover,
		TextDocumentCompletion:     s.textDocumentCompletion,
		TextDocumentDocumentSymbol: s.textDocumentDocumentSymbol,
		TextDocumentFormatting:     s.textDocumentFormatting,

		// Workspace
		WorkspaceDidChangeWatchedFiles:     s.workspaceDidChangeWatchedFiles,
		WorkspaceDidChangeWorkspaceFolders: s.workspaceDidChangeWorkspaceFolders,
	}

	s.server = server.NewServer(&s.handler, serverName, false)

	return s
}

// Handler returns the protocol handler for testing purposes.
func (s *Server) Handler() *protocol.Handler {
	return &s.handler
}

// RunStdio runs the server using stdio transport.
func (s *Server) RunStdio() error {
	if err := s.server.RunStdio(); err != nil {
		return fmt.Errorf("run stdio: %w", err)
	}
	return nil
}

// Shutdown initiates graceful server shutdown.
// It cancels pending workspace operations to ensure clean termination.
func (s *Server) Shutdown() {
	s.logger.Info("initiating shutdown")
	s.workspace.Shutdown()
}

// Close closes the JSON-RPC connection, causing RunStdio to return.
// This enables graceful shutdown when a signal is received.
//
// Close is idempotent: multiple calls return the same result and do not panic.
// It is safe to call before RunStdio (returns nil if connection not initialized).
//
// Note: The nil check is intentionally outside closeOnce.Do() to avoid consuming
// the Once when the connection is not yet ready. This allows callers to retry
// Close() if called before RunStdio() has initialized the connection.
func (s *Server) Close() error {
	conn := s.server.GetStdio()
	if conn == nil {
		return nil // Connection not ready, caller can retry
	}
	s.closeOnce.Do(func() {
		if err := conn.Close(); err != nil {
			s.closeErr = fmt.Errorf("close connection: %w", err)
		}
	})
	return s.closeErr
}

// initialize handles the initialize request.
func (s *Server) initialize(ctx *glsp.Context, params *protocol.InitializeParams) (any, error) {
	s.logger.Info("initialize request received",
		slog.String("client_name", s.clientName(params)),
		slog.String("root_uri", s.rootURI(params)),
	)

	// Log client capabilities summary
	s.logClientCapabilities(params.Capabilities)

	// Extract workspace folders
	switch {
	case params.WorkspaceFolders != nil:
		for _, folder := range params.WorkspaceFolders {
			s.workspace.AddRoot(folder.URI)
			s.logger.Debug("workspace folder", slog.String("uri", folder.URI))
		}
	case params.RootURI != nil:
		s.workspace.AddRoot(*params.RootURI)
	case params.RootPath != nil:
		// Fallback for older LSP clients that only provide RootPath
		s.workspace.AddRoot(PathToURI(*params.RootPath))
	}

	// Use UTF-16 encoding (default for VS Code compatibility)
	// Note: position encoding negotiation requires LSP 3.17, glsp only supports 3.16
	posEncoding := PositionEncodingUTF16
	s.workspace.SetPositionEncoding(posEncoding)
	s.logger.Info("using position encoding", slog.String("encoding", string(posEncoding)))

	// Build capabilities
	capabilities := s.handler.CreateServerCapabilities()

	// Override to use full text sync (Phase 1 - simpler and safer)
	syncKind := protocol.TextDocumentSyncKindFull
	if syncOpts, ok := capabilities.TextDocumentSync.(*protocol.TextDocumentSyncOptions); ok {
		syncOpts.Change = &syncKind
	}

	// Configure completion trigger characters:
	// - "." for qualified access (e.g., "parts.Wheel")
	// - " " for context after keywords (extends, import, as, -->, *->)
	capabilities.CompletionProvider = &protocol.CompletionOptions{
		TriggerCharacters: []string{".", " "},
	}

	version := "dev"
	return protocol.InitializeResult{
		Capabilities: capabilities,
		ServerInfo: &protocol.InitializeResultServerInfo{
			Name:    serverName,
			Version: &version,
		},
	}, nil
}

// initialized handles the initialized notification.
func (s *Server) initialized(ctx *glsp.Context, params *protocol.InitializedParams) error {
	s.logger.Info("server initialized")
	return nil
}

// shutdown handles the shutdown request.
func (s *Server) shutdown(ctx *glsp.Context) error {
	s.logger.Info("shutdown request received")
	s.shutdownCalled = true
	protocol.SetTraceValue(protocol.TraceValueOff)
	return nil
}

// exit handles the exit notification per LSP spec.
// Exit code is 0 if shutdown was called first, 1 otherwise.
func (s *Server) exit(_ *glsp.Context) error {
	exitCode := 0
	if !s.shutdownCalled {
		s.logger.Warn("exit called without shutdown")
		exitCode = 1
	}
	s.logger.Info("exit notification received", slog.Int("exit_code", exitCode))
	os.Exit(exitCode)
	return nil // unreachable
}

// setTrace handles the $/setTrace notification.
func (s *Server) setTrace(ctx *glsp.Context, params *protocol.SetTraceParams) error {
	s.logger.Debug("setTrace", slog.String("value", string(params.Value)))
	protocol.SetTraceValue(params.Value)
	return nil
}

// cancelRequest handles the $/cancelRequest notification.
//
// This method logs cancellation requests for debugging. The current implementation
// relies on context-based cancellation in ScheduleAnalysis for debounced operations.
// Full request cancellation (e.g., for long-running completion requests) would
// require tracking request IDs and their associated contexts.
func (s *Server) cancelRequest(ctx *glsp.Context, params *protocol.CancelParams) error {
	s.logger.Debug("cancelRequest", slog.Any("id", params.ID))
	// Note: The glsp library handles JSON-RPC level request cancellation.
	// This handler provides a hook for additional cancellation logic if needed.
	return nil
}

// textDocumentDidOpen handles textDocument/didOpen.
func (s *Server) textDocumentDidOpen(ctx *glsp.Context, params *protocol.DidOpenTextDocumentParams) error {
	s.logger.Debug("textDocument/didOpen",
		slog.String("uri", params.TextDocument.URI),
		slog.Int("version", int(params.TextDocument.Version)),
	)

	s.workspace.DocumentOpened(
		params.TextDocument.URI,
		int(params.TextDocument.Version),
		params.TextDocument.Text,
	)

	// Trigger immediate analysis for didOpen to provide instant feedback.
	// Note: This runs synchronously on the handler goroutine, which can block
	// other LSP requests on large files. Converting to async would require
	// integration test infrastructure changes to wait for analysis completion.
	// Version gating in AnalyzeAndPublish prevents stale results.
	var notify Notifier
	if ctx != nil {
		notify = func(method string, params any) { ctx.Notify(method, params) }
	}
	s.workspace.AnalyzeAndPublish(notify, context.Background(), params.TextDocument.URI)

	return nil
}

// textDocumentDidChange handles textDocument/didChange.
func (s *Server) textDocumentDidChange(ctx *glsp.Context, params *protocol.DidChangeTextDocumentParams) error {
	s.logger.Debug("textDocument/didChange",
		slog.String("uri", params.TextDocument.URI),
		slog.Int("version", int(params.TextDocument.Version)),
	)

	// Full sync: apply the last full content change (some clients may send multiple)
	if len(params.ContentChanges) > 0 {
		// Find the last full change event in the list
		var lastFullChange *protocol.TextDocumentContentChangeEventWhole
		for _, rawChange := range params.ContentChanges {
			if change, ok := rawChange.(protocol.TextDocumentContentChangeEventWhole); ok {
				lastFullChange = &change
			}
		}

		if lastFullChange != nil {
			// Apply the last full change
			s.workspace.DocumentChanged(
				params.TextDocument.URI,
				int(params.TextDocument.Version),
				lastFullChange.Text,
			)
		} else if _, ok := params.ContentChanges[0].(protocol.TextDocumentContentChangeEvent); ok {
			// Server advertises full sync but received only incremental changes.
			// Apply incrementally to prevent stale content issues.
			s.logger.Warn("received incremental change but server advertises full sync",
				slog.String("uri", params.TextDocument.URI),
				slog.Int("version", int(params.TextDocument.Version)),
			)
			s.applyIncrementalChanges(params)
		}
	}

	// Trigger debounced analysis
	s.workspace.ScheduleAnalysis(ctx, params.TextDocument.URI)

	return nil
}

// applyIncrementalChanges applies incremental text changes to a document.
// This handles misbehaving clients that send incremental changes despite
// the server advertising full sync mode.
func (s *Server) applyIncrementalChanges(params *protocol.DidChangeTextDocumentParams) {
	doc := s.workspace.GetDocumentSnapshot(params.TextDocument.URI)
	if doc == nil {
		s.logger.Warn("incremental change for unknown document",
			slog.String("uri", params.TextDocument.URI),
		)
		return
	}

	// Normalize line endings to LF for consistent offset calculation.
	// Windows clients may send CRLF (\r\n) which would cause incorrect
	// byte offset calculations since rangeToByteOffset assumes LF-only.
	// We normalize once and work with normalized text throughout.
	text := normalizeLineEndings(doc.Text)
	enc := s.workspace.PositionEncoding()

	// Apply each change in order
	for _, rawChange := range params.ContentChanges {
		change, ok := rawChange.(protocol.TextDocumentContentChangeEvent)
		if !ok {
			continue
		}
		if change.Range == nil {
			// No range means full replacement - normalize the new text too
			text = normalizeLineEndings(change.Text)
			continue
		}

		// Convert LSP range to byte offsets using the negotiated encoding
		lines := strings.Split(text, "\n")

		startOffset := s.rangeToByteOffset(lines, int(change.Range.Start.Line), int(change.Range.Start.Character), enc)
		endOffset := s.rangeToByteOffset(lines, int(change.Range.End.Line), int(change.Range.End.Character), enc)

		// Splice in the new text (normalize incoming change text too)
		if startOffset <= len(text) && endOffset <= len(text) && startOffset <= endOffset {
			text = text[:startOffset] + normalizeLineEndings(change.Text) + text[endOffset:]
		} else {
			// Invalid range: log warning and fall back to full-text replace.
			// This prevents silent document desync when clients send bad ranges.
			s.logger.Warn("incremental change has invalid range, using full-text fallback",
				slog.String("uri", params.TextDocument.URI),
				slog.Int("start_offset", startOffset),
				slog.Int("end_offset", endOffset),
				slog.Int("text_len", len(text)),
			)
			text = normalizeLineEndings(change.Text)
		}
	}

	s.workspace.DocumentChanged(
		params.TextDocument.URI,
		int(params.TextDocument.Version),
		text,
	)
}

// rangeToByteOffset converts an LSP position to a byte offset in the document.
// The encoding parameter specifies how character positions are counted (UTF-16 or UTF-8).
func (s *Server) rangeToByteOffset(lines []string, line, char int, enc PositionEncoding) int {
	offset := 0

	// Sum lengths of preceding lines (including newlines)
	for i := 0; i < line && i < len(lines); i++ {
		offset += len(lines[i]) + 1 // +1 for newline
	}

	// Add character offset within line using the negotiated encoding
	if line < len(lines) {
		lineContent := []byte(lines[line])
		var charOffset int
		switch enc {
		case PositionEncodingUTF8:
			// UTF-8 encoding: character offset IS byte offset
			charOffset = min(char, len(lineContent))
		default:
			// UTF-16 encoding (default): convert from UTF-16 code units to bytes
			charOffset = utf16CharToByteOffset(lineContent, 0, char)
		}
		offset += charOffset
	}

	return offset
}

// normalizeLineEndings converts CRLF and CR line endings to LF.
// This ensures consistent line ending handling across platforms.
// Windows clients may send CRLF (\r\n), which would cause incorrect
// byte offset calculations in rangeToByteOffset.
func normalizeLineEndings(text string) string {
	// First replace CRLF with LF, then replace any remaining CR with LF
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	return text
}

// textDocumentDidClose handles textDocument/didClose.
func (s *Server) textDocumentDidClose(ctx *glsp.Context, params *protocol.DidCloseTextDocumentParams) error {
	s.logger.Debug("textDocument/didClose",
		slog.String("uri", params.TextDocument.URI),
	)

	var notify Notifier
	if ctx != nil {
		notify = func(method string, params any) { ctx.Notify(method, params) }
	}
	s.workspace.DocumentClosed(notify, params.TextDocument.URI)

	return nil
}

// workspaceDidChangeWatchedFiles handles workspace/didChangeWatchedFiles.
func (s *Server) workspaceDidChangeWatchedFiles(ctx *glsp.Context, params *protocol.DidChangeWatchedFilesParams) error {
	for _, change := range params.Changes {
		s.logger.Debug("watched file changed",
			slog.String("uri", change.URI),
			slog.Int("type", int(change.Type)),
		)
		s.workspace.FileChanged(ctx, change.URI, change.Type)
	}
	return nil
}

// workspaceDidChangeWorkspaceFolders handles workspace/didChangeWorkspaceFolders.
// This is called when the user adds or removes workspace folders in VS Code.
func (s *Server) workspaceDidChangeWorkspaceFolders(ctx *glsp.Context, params *protocol.DidChangeWorkspaceFoldersParams) error {
	// Process removed folders first to ensure clean state
	for _, folder := range params.Event.Removed {
		s.logger.Debug("workspace folder removed", slog.String("uri", folder.URI))
		s.workspace.RemoveRoot(folder.URI)
	}

	// Process added folders
	for _, folder := range params.Event.Added {
		s.logger.Debug("workspace folder added", slog.String("uri", folder.URI))
		s.workspace.AddRoot(folder.URI)
	}

	// Trigger re-analysis of open documents whose module root may have changed
	s.workspace.ReanalyzeOpenDocuments(ctx)

	return nil
}

// Helper functions

func (s *Server) clientName(params *protocol.InitializeParams) string {
	if params.ClientInfo != nil {
		if params.ClientInfo.Version != nil {
			return params.ClientInfo.Name + " " + *params.ClientInfo.Version
		}
		return params.ClientInfo.Name
	}
	return "unknown"
}

func (s *Server) rootURI(params *protocol.InitializeParams) string {
	if params.RootURI != nil {
		return *params.RootURI
	}
	return ""
}

func (s *Server) logClientCapabilities(caps protocol.ClientCapabilities) {
	var features []string

	if caps.TextDocument != nil {
		if caps.TextDocument.Completion != nil {
			features = append(features, "completion")
			if caps.TextDocument.Completion.CompletionItem != nil {
				if caps.TextDocument.Completion.CompletionItem.SnippetSupport != nil &&
					*caps.TextDocument.Completion.CompletionItem.SnippetSupport {
					features = append(features, "snippets")
				}
			}
		}
		if caps.TextDocument.Hover != nil {
			features = append(features, "hover")
			if caps.TextDocument.Hover.ContentFormat != nil &&
				slices.Contains(caps.TextDocument.Hover.ContentFormat, protocol.MarkupKindMarkdown) {
				features = append(features, "hover-markdown")
			}
		}
		if caps.TextDocument.Definition != nil {
			features = append(features, "definition")
		}
		if caps.TextDocument.DocumentSymbol != nil {
			features = append(features, "document-symbol")
		}
		if caps.TextDocument.Formatting != nil {
			features = append(features, "formatting")
		}
	}

	s.logger.Info("client capabilities", slog.Any("features", features))
}
