// Package lsp implements a Language Server Protocol server for YAMMM schema files.
package lsp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/creachadair/jrpc2"
	"github.com/creachadair/jrpc2/channel"
	"github.com/creachadair/jrpc2/handler"

	"github.com/simon-lentz/yammm/lsp/internal/docstate"
	"github.com/simon-lentz/yammm/lsp/internal/lsputil"
	"github.com/simon-lentz/yammm/lsp/internal/protocol"
	"github.com/simon-lentz/yammm/lsp/internal/workspace"
)

// PositionEncoding is an alias for lsputil.PositionEncoding within the lsp package.
type PositionEncoding = lsputil.PositionEncoding

const (
	// PositionEncodingUTF16 counts positions in UTF-16 code units (default).
	PositionEncodingUTF16 = lsputil.PositionEncodingUTF16

	// PositionEncodingUTF8 counts positions in UTF-8 bytes.
	PositionEncodingUTF8 = lsputil.PositionEncodingUTF8
)

const serverName = "yammm-lsp"

// Config holds the server configuration.
type Config struct {
	// ModuleRoot overrides the computed module root for import resolution.
	ModuleRoot string

	// Version is the server version reported during LSP initialization.
	// Injected from -ldflags at build time. Falls back to "dev" when empty.
	Version string

	// DebounceDelay is the delay between a document change and the analysis
	// it triggers. Zero or negative means the workspace default
	// (150ms). Tests use a small value to make temporal behavior
	// deterministic.
	DebounceDelay time.Duration
}

// Validate canonicalizes and validates the Config. It applies defaults
// (e.g., Version falls back to "dev") and resolves the ModuleRoot path.
// Hard errors are returned for unrecoverable failures (e.g., filepath.Abs error).
// Warnings are logged for conditions that might resolve later (e.g., module root
// does not exist yet — the server has fallback mechanisms for import resolution).
func (c *Config) Validate(logger *slog.Logger) error {
	if c.Version == "" {
		c.Version = "dev"
	}
	if c.ModuleRoot != "" {
		absRoot, err := filepath.Abs(c.ModuleRoot)
		if err != nil {
			return fmt.Errorf("resolve module root: %w", err)
		}
		if resolved, err := filepath.EvalSymlinks(absRoot); err == nil {
			absRoot = resolved
		}
		c.ModuleRoot = filepath.Clean(absRoot)

		// Warn (not error) — path may be created later, and the server
		// has fallback mechanisms for import resolution.
		if info, err := os.Stat(c.ModuleRoot); err != nil {
			logger.Warn("module root does not exist; import resolution may fail",
				slog.String("path", c.ModuleRoot), slog.Any("error", err))
		} else if !info.IsDir() {
			logger.Warn("module root is not a directory; import resolution may fail",
				slog.String("path", c.ModuleRoot))
		}
	}
	return nil
}

// Option is a functional option for NewServer. Options are for dependency
// injection in tests, not for configuration (Config handles that).
type Option func(*Server)

// WithLogger overrides the server's logger. Useful for injecting a test
// logger that captures records for assertions.
func WithLogger(logger *slog.Logger) Option {
	return func(s *Server) {
		s.logger = logger.With(slog.String("component", "server"))
	}
}

// Resolver is consumed by feature handlers (hover, completion, definition, symbols, formatting).
// It provides read-only access to workspace state for resolving analysis units.
type Resolver interface {
	ResolveUnit(uri string, line, char int, snapshotRequired bool) *workspace.Unit
	ResolveAllUnits(uri string) []workspace.Unit
	PositionEncoding() lsputil.PositionEncoding
	GetDocumentSnapshot(uri string) *docstate.Snapshot
	RemapPathToURI(input string) string
}

// DocumentManager is consumed by sync/workspace handlers (didOpen, didChange, didClose, etc.).
// It provides mutating access to workspace state for document lifecycle management.
type DocumentManager interface {
	OpenDocument(uri string, version int, text string)
	ChangeDocument(uri string, version int, text string)
	CloseDocument(uri string)
	FileChanged(uri string, changeType protocol.UInteger)
	AddRoot(uri string)
	RemoveRoot(uri string)
	ReanalyzeOpenDocuments()
}

// Server is the YAMMM language server. It handles both standalone .yammm
// files and YAMMM code blocks embedded in Markdown documents (.md, .markdown).
type Server struct {
	logger    *slog.Logger
	config    Config
	mux       handler.Map
	jrpcSrv   *jrpc2.Server
	workspace *workspace.Workspace

	// posEncoding is the negotiated position encoding, set once during initialize.
	posEncoding lsputil.PositionEncoding

	// traceValue stores the current trace level (replaces protocol.SetTraceValue global).
	// Accessed atomically because jrpc2 may dispatch setTrace and shutdown on different goroutines.
	traceValue atomic.Value // stores protocol.TraceValue (string)

	// shutdownCalled tracks whether shutdown was called before exit (LSP lifecycle).
	// Atomic because jrpc2 may dispatch shutdown (request) and exit (notification)
	// on different goroutines.
	shutdownCalled atomic.Bool

	// closeOnce ensures Close is idempotent
	closeOnce sync.Once
}

// NewServer creates a new YAMMM language server.
// If logger is nil, slog.Default() is used.
// Options are applied after default construction, allowing test-only overrides
// (e.g., WithLogger). The variadic opts parameter is backward-compatible —
// existing callers that pass no options continue to work unchanged.
func NewServer(logger *slog.Logger, cfg Config, opts ...Option) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	s := &Server{
		logger:      logger.With(slog.String("component", "server")),
		config:      cfg,
		posEncoding: PositionEncodingUTF16, // default; overridden by initialize
		workspace: workspace.NewWorkspace(logger, workspace.Config{
			ModuleRoot:    cfg.ModuleRoot,
			DebounceDelay: cfg.DebounceDelay,
		}),
	}

	for _, opt := range opts {
		opt(s)
	}

	s.mux = handler.Map{
		// Lifecycle — stay as methods (mutate Server state)
		"initialize":      handler.New(s.initialize),
		"initialized":     handler.New(s.initialized),
		"shutdown":        handler.New(s.shutdown),
		"exit":            handler.New(s.exit),
		"$/setTrace":      handler.New(s.setTrace),
		"$/cancelRequest": handler.New(s.cancelRequest),

		// Features — accept Resolver (read-only workspace access)
		"textDocument/completion":     handleCompletion(s.workspace, s.logger),
		"textDocument/hover":          handleHover(s.workspace, s.logger),
		"textDocument/definition":     handleDefinition(s.workspace, s.logger),
		"textDocument/documentSymbol": handleDocumentSymbol(s.workspace, s.logger),
		"textDocument/formatting":     handleFormatting(s.workspace, s.logger),

		// Document sync — accept DocumentManager (mutating workspace access)
		"textDocument/didOpen":   handleDidOpen(s.workspace, s.logger),
		"textDocument/didChange": handleDidChange(s.workspace, s.logger),
		"textDocument/didClose":  handleDidClose(s.workspace, s.logger),

		// Workspace
		"workspace/didChangeWatchedFiles":     handleDidChangeWatchedFiles(s.workspace, s.logger),
		"workspace/didChangeWorkspaceFolders": handleDidChangeWorkspaceFolders(s.workspace, s.logger),
	}

	// Apply middleware to all handlers for latency-based log escalation.
	for method, h := range s.mux {
		s.mux[method] = s.withMiddleware(method, h)
	}

	return s
}

// Mux returns the handler map for testing purposes.
func (s *Server) Mux() handler.Map { return s.mux }

// Workspace returns the workspace for testing purposes.
func (s *Server) Workspace() *workspace.Workspace { return s.workspace }

// RunStdio runs the server using stdio transport with LSP framing.
// It wires the workspace notifier to use the jrpc2 server for push
// notifications (diagnostics). Uses context.Background() so notifications
// are not tied to any individual request's lifecycle.
func (s *Server) RunStdio() error {
	ch := channel.LSP(os.Stdin, os.Stdout)
	s.jrpcSrv = jrpc2.NewServer(s.mux, &jrpc2.ServerOptions{
		AllowPush: true, // required for server-to-client notifications
	})
	s.workspace.SetNotifier(func(method string, params any) {
		_ = s.jrpcSrv.Notify(context.Background(), method, params) //nolint:errcheck // best-effort notification
	})
	s.jrpcSrv.Start(ch)
	return s.jrpcSrv.Wait() //nolint:wrapcheck // top-level server entrypoint
}

// Close performs an ordered shutdown:
//  1. workspace.Shutdown():
//     a. set stopping flag — refuse new analysis goroutines
//     b. bgCancel() — cancel in-flight analysis contexts
//     c. stop pending debounce timers
//     d. wg.Wait() with 3s timeout — wait for tracked goroutines
//  2. jrpcSrv.Stop() — close connection, unblock RunStdio
//
// Close is idempotent. jrpc2.Stop() returns immediately; RunStdio
// unblocks because the underlying channel is closed.
// Returns nil unconditionally — jrpc2.Stop() is void.
func (s *Server) Close() error {
	s.closeOnce.Do(func() {
		s.logger.Info("initiating shutdown")
		s.workspace.Shutdown()
		if s.jrpcSrv != nil {
			s.jrpcSrv.Stop()
		}
	})
	return nil
}

// slowRequestThreshold is the latency above which requests are logged at warn level.
const slowRequestThreshold = 500 * time.Millisecond

// withMiddleware wraps a jrpc2.Handler with timing and latency-based log escalation.
// Requests that error or exceed slowRequestThreshold are logged at warn level;
// all others are logged at debug level.
func (s *Server) withMiddleware(method string, h jrpc2.Handler) jrpc2.Handler {
	return func(ctx context.Context, req *jrpc2.Request) (any, error) {
		start := time.Now()
		result, err := h(ctx, req)
		elapsed := time.Since(start)

		level := slog.LevelDebug
		if err != nil {
			if errors.Is(err, context.Canceled) {
				level = slog.LevelDebug // cancellation is normal (editor closed, new edit)
			} else {
				level = slog.LevelWarn
			}
		} else if elapsed > slowRequestThreshold {
			level = slog.LevelWarn
		}
		s.logger.Log(
			ctx, level, "request completed",
			slog.String("method", method),
			slog.Duration("elapsed", elapsed),
			slog.Any("error", err),
		)
		return result, err
	}
}

// initialize handles the initialize request.
func (s *Server) initialize(_ context.Context, params *protocol.InitializeParams) (any, error) {
	s.logger.Info(
		"initialize request received",
		slog.String("client_name", clientName(params)),
		slog.String("root_uri", rootURI(params)),
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
		s.workspace.AddRoot(lsputil.PathToURI(*params.RootPath))
	}

	// Use UTF-16 encoding (default for VS Code compatibility)
	posEncoding := PositionEncodingUTF16
	s.posEncoding = posEncoding
	s.workspace.SetPositionEncoding(posEncoding)
	s.logger.Info("using position encoding", slog.String("encoding", string(posEncoding)))

	// Build capabilities manually (replaces CreateServerCapabilities)
	syncKind := protocol.TextDocumentSyncKindFull
	capabilities := protocol.ServerCapabilities{
		TextDocumentSync: &protocol.TextDocumentSyncOptions{
			OpenClose: new(true),
			Change:    &syncKind,
		},
		CompletionProvider: &protocol.CompletionOptions{
			TriggerCharacters: []string{".", " "},
		},
		HoverProvider:              true,
		DefinitionProvider:         true,
		DocumentSymbolProvider:     true,
		DocumentFormattingProvider: true,
		Workspace: &protocol.ServerCapabilitiesWorkspace{
			WorkspaceFolders: &protocol.WorkspaceFoldersServerCapabilities{
				Supported:           new(true),
				ChangeNotifications: &protocol.BoolOrString{Value: true},
			},
		},
	}

	version := s.config.Version
	return protocol.InitializeResult{
		Capabilities: capabilities,
		ServerInfo: &protocol.InitializeResultServerInfo{
			Name:    serverName,
			Version: &version,
		},
	}, nil
}

// initialized handles the initialized notification.
func (s *Server) initialized(_ context.Context, _ *protocol.InitializedParams) error {
	s.logger.Info("server initialized")
	return nil
}

// shutdown handles the shutdown request.
func (s *Server) shutdown(_ context.Context) (any, error) {
	s.logger.Info("shutdown request received")
	s.shutdownCalled.Store(true)
	s.traceValue.Store(protocol.TraceValueOff)
	return nil, nil //nolint:nilnil // LSP shutdown response is always null
}

// exit handles the exit notification per LSP spec.
func (s *Server) exit(_ context.Context) error {
	exitCode := 0
	if !s.shutdownCalled.Load() {
		s.logger.Warn("exit called without shutdown")
		exitCode = 1
	}
	s.logger.Info("exit notification received", slog.Int("exit_code", exitCode))
	os.Exit(exitCode)
	return nil // unreachable
}

// setTrace handles the $/setTrace notification.
func (s *Server) setTrace(_ context.Context, params *protocol.SetTraceParams) error {
	s.logger.Debug("setTrace", slog.String("value", params.Value))
	s.traceValue.Store(params.Value)
	return nil
}

// cancelRequest handles the $/cancelRequest notification.
// NOTE: jrpc2 handles request cancellation natively. This handler exists only
// for logging; we do not need to perform any cancellation ourselves.
func (s *Server) cancelRequest(_ context.Context, params *protocol.CancelParams) error {
	s.logger.Debug("cancelRequest", slog.Any("id", params.ID))
	return nil
}

// Helper functions

func clientName(params *protocol.InitializeParams) string {
	if params.ClientInfo != nil {
		if params.ClientInfo.Version != nil {
			return params.ClientInfo.Name + " " + *params.ClientInfo.Version
		}
		return params.ClientInfo.Name
	}
	return "unknown"
}

func rootURI(params *protocol.InitializeParams) string {
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
