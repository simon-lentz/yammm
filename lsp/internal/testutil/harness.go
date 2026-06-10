// Package testutil provides integration testing utilities for the Yammm LSP.
package testutil

import (
	"path/filepath"
	"sync"
	"testing"

	"github.com/creachadair/jrpc2"
	"github.com/creachadair/jrpc2/handler"
	jrpc2server "github.com/creachadair/jrpc2/server"

	"github.com/simon-lentz/yammm/lsp/internal/protocol"

	"github.com/simon-lentz/yammm/lsp/internal/lsputil"
)

// PathToURI converts a filesystem path to a file:// URI.
func PathToURI(path string) string {
	return lsputil.PathToURI(path)
}

// Harness provides an in-process LSP server for integration testing.
// It uses jrpc2's NewLocal to create a real client/server pair connected
// by an in-memory pipe, exercising full JSON-RPC serialization.
type Harness struct {
	t      *testing.T
	local  jrpc2server.Local
	client *jrpc2.Client

	// Root path for the test workspace
	Root string

	// Captured diagnostics from server notifications
	diagMu      sync.Mutex
	diagnostics map[string][]protocol.Diagnostic // URI -> diagnostics
}

// NewHarness creates a new test harness with the given handler map.
func NewHarness(t *testing.T, mux handler.Map, root string) *Harness {
	t.Helper()

	h := &Harness{
		t:           t,
		Root:        root,
		diagnostics: make(map[string][]protocol.Diagnostic),
	}

	opts := &jrpc2server.LocalOptions{
		Server: &jrpc2.ServerOptions{AllowPush: true},
		Client: &jrpc2.ClientOptions{
			OnNotify: func(req *jrpc2.Request) {
				if req.Method() == "textDocument/publishDiagnostics" {
					var params protocol.PublishDiagnosticsParams
					if err := req.UnmarshalParams(&params); err == nil {
						h.diagMu.Lock()
						h.diagnostics[params.URI] = params.Diagnostics
						h.diagMu.Unlock()
					}
				}
			},
		},
	}

	h.local = jrpc2server.NewLocal(mux, opts)
	h.client = h.local.Client

	return h
}

// Initialize performs LSP initialization handshake with a single root.
func (h *Harness) Initialize() error {
	return h.InitializeWithFolders(nil)
}

// InitializeWithFolders performs LSP initialization handshake with multiple workspace folders.
// If folders is nil or empty, uses h.Root as the single workspace folder.
func (h *Harness) InitializeWithFolders(folders []string) error {
	h.t.Helper()

	// Default to h.Root if no folders specified
	if len(folders) == 0 {
		folders = []string{h.Root}
	}

	rootURI := PathToURI(folders[0])

	// Build workspace folders
	workspaceFolders := make([]protocol.WorkspaceFolder, len(folders))
	for i, folder := range folders {
		uri := PathToURI(folder)
		workspaceFolders[i] = protocol.WorkspaceFolder{
			URI:  uri,
			Name: filepath.Base(folder),
		}
	}

	params := &protocol.InitializeParams{
		RootURI:          &rootURI,
		WorkspaceFolders: workspaceFolders,
		Capabilities: protocol.ClientCapabilities{
			TextDocument: &protocol.TextDocumentClientCapabilities{
				Synchronization: &protocol.TextDocumentSyncClientCapabilities{},
				Hover:           &protocol.HoverClientCapabilities{},
				Completion:      &protocol.CompletionClientCapabilities{},
				Definition:      &protocol.DefinitionClientCapabilities{},
				DocumentSymbol:  &protocol.DocumentSymbolClientCapabilities{},
				Formatting:      &protocol.DocumentFormattingClientCapabilities{},
			},
		},
	}

	ctx := h.t.Context()
	var result protocol.InitializeResult
	if err := h.client.CallResult(ctx, "initialize", params, &result); err != nil {
		return err //nolint:wrapcheck // test utility
	}

	return h.client.Notify(ctx, "initialized", &protocol.InitializedParams{}) //nolint:wrapcheck // test utility
}

// OpenDocument opens a document with the given content.
func (h *Harness) OpenDocument(path, content string) error {
	h.t.Helper()

	absPath := path
	if !filepath.IsAbs(path) {
		absPath = filepath.Join(h.Root, path)
	}

	uri := PathToURI(absPath)
	ctx := h.t.Context()
	return h.client.Notify(ctx, "textDocument/didOpen", &protocol.DidOpenTextDocumentParams{ //nolint:wrapcheck // test utility
		TextDocument: protocol.TextDocumentItem{
			URI:        uri,
			LanguageID: "yammm",
			Version:    1,
			Text:       content,
		},
	})
}

// OpenMarkdownDocument opens a markdown document with the given content.
// Sends languageID "markdown" for protocol fidelity. The server dispatches
// by URI extension (.md/.markdown), not languageID.
func (h *Harness) OpenMarkdownDocument(path, content string) error {
	h.t.Helper()

	absPath := path
	if !filepath.IsAbs(path) {
		absPath = filepath.Join(h.Root, path)
	}

	uri := PathToURI(absPath)
	ctx := h.t.Context()
	return h.client.Notify(ctx, "textDocument/didOpen", &protocol.DidOpenTextDocumentParams{ //nolint:wrapcheck // test utility
		TextDocument: protocol.TextDocumentItem{
			URI:        uri,
			LanguageID: "markdown",
			Version:    1,
			Text:       content,
		},
	})
}

// ChangeDocument sends a document change notification.
func (h *Harness) ChangeDocument(path, content string, version int) error {
	h.t.Helper()

	absPath := path
	if !filepath.IsAbs(path) {
		absPath = filepath.Join(h.Root, path)
	}

	uri := PathToURI(absPath)
	ctx := h.t.Context()
	return h.client.Notify(ctx, "textDocument/didChange", &protocol.DidChangeTextDocumentParams{ //nolint:wrapcheck // test utility
		TextDocument: protocol.VersionedTextDocumentIdentifier{
			TextDocumentIdentifier: protocol.TextDocumentIdentifier{
				URI: uri,
			},
			Version: protocol.Integer(version), //nolint:gosec // test utility, version is always small
		},
		ContentChanges: []protocol.ContentChange{
			{Text: content},
		},
	})
}

// CloseDocument closes a document.
func (h *Harness) CloseDocument(path string) error {
	h.t.Helper()

	absPath := path
	if !filepath.IsAbs(path) {
		absPath = filepath.Join(h.Root, path)
	}

	uri := PathToURI(absPath)
	ctx := h.t.Context()
	return h.client.Notify(ctx, "textDocument/didClose", &protocol.DidCloseTextDocumentParams{ //nolint:wrapcheck // test utility
		TextDocument: protocol.TextDocumentIdentifier{
			URI: uri,
		},
	})
}

// Hover requests hover information at the given position.
func (h *Harness) Hover(path string, line, char int) (*protocol.Hover, error) {
	h.t.Helper()

	absPath := path
	if !filepath.IsAbs(path) {
		absPath = filepath.Join(h.Root, path)
	}

	uri := PathToURI(absPath)
	ctx := h.t.Context()
	var result *protocol.Hover
	err := h.client.CallResult(ctx, "textDocument/hover", &protocol.HoverParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: uri,
			},
			Position: protocol.Position{
				Line:      protocol.UInteger(line), //nolint:gosec // test utility, line is always small
				Character: protocol.UInteger(char), //nolint:gosec // test utility, char is always small
			},
		},
	}, &result)
	if err != nil {
		// jrpc2 returns an error for null results; treat as nil hover
		return nil, nil //nolint:nilerr,nilnil // LSP protocol: null result = no hover
	}
	return result, nil
}

// Definition requests go-to-definition at the given position. A nil
// Location with nil error means the server returned null (no definition).
func (h *Harness) Definition(path string, line, char int) (*protocol.Location, error) {
	h.t.Helper()

	absPath := path
	if !filepath.IsAbs(path) {
		absPath = filepath.Join(h.Root, path)
	}

	uri := PathToURI(absPath)
	ctx := h.t.Context()
	var result *protocol.Location
	err := h.client.CallResult(ctx, "textDocument/definition", &protocol.DefinitionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: uri,
			},
			Position: protocol.Position{
				Line:      protocol.UInteger(line), //nolint:gosec // test utility, line is always small
				Character: protocol.UInteger(char), //nolint:gosec // test utility, char is always small
			},
		},
	}, &result)
	if err != nil {
		return nil, nil //nolint:nilerr,nilnil // LSP protocol: null result = no definition
	}
	if result == nil {
		return nil, nil //nolint:nilnil // LSP protocol: null result = no definition
	}
	return result, nil
}

// Completion requests completion items at the given position. A nil slice
// with nil error means the server returned null (no completions).
func (h *Harness) Completion(path string, line, char int) ([]protocol.CompletionItem, error) {
	h.t.Helper()

	absPath := path
	if !filepath.IsAbs(path) {
		absPath = filepath.Join(h.Root, path)
	}

	uri := PathToURI(absPath)
	ctx := h.t.Context()
	var result []protocol.CompletionItem
	err := h.client.CallResult(ctx, "textDocument/completion", &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: uri,
			},
			Position: protocol.Position{
				Line:      protocol.UInteger(line), //nolint:gosec // test utility, line is always small
				Character: protocol.UInteger(char), //nolint:gosec // test utility, char is always small
			},
		},
	}, &result)
	if err != nil {
		return nil, nil //nolint:nilerr // LSP protocol: null result = no completions
	}
	return result, nil
}

// DocumentSymbols requests document symbols. A nil slice with nil error
// means the server returned null (no symbols).
func (h *Harness) DocumentSymbols(path string) ([]protocol.DocumentSymbol, error) {
	h.t.Helper()

	absPath := path
	if !filepath.IsAbs(path) {
		absPath = filepath.Join(h.Root, path)
	}

	uri := PathToURI(absPath)
	ctx := h.t.Context()
	var result []protocol.DocumentSymbol
	err := h.client.CallResult(ctx, "textDocument/documentSymbol", &protocol.DocumentSymbolParams{
		TextDocument: protocol.TextDocumentIdentifier{
			URI: uri,
		},
	}, &result)
	if err != nil {
		return nil, nil //nolint:nilerr // LSP protocol: null result = no symbols
	}
	return result, nil
}

// Formatting requests document formatting.
func (h *Harness) Formatting(path string) ([]protocol.TextEdit, error) {
	h.t.Helper()

	absPath := path
	if !filepath.IsAbs(path) {
		absPath = filepath.Join(h.Root, path)
	}

	uri := PathToURI(absPath)
	ctx := h.t.Context()
	var result []protocol.TextEdit
	err := h.client.CallResult(ctx, "textDocument/formatting", &protocol.DocumentFormattingParams{
		TextDocument: protocol.TextDocumentIdentifier{
			URI: uri,
		},
		Options: protocol.FormattingOptions{
			"tabSize":      4,
			"insertSpaces": false,
		},
	}, &result)
	if err != nil {
		return nil, err //nolint:wrapcheck // test utility
	}
	return result, nil
}

// Sync ensures all previously sent notifications have been processed by the
// server. It does this by sending a request (which is processed in order after
// any pending notifications) and waiting for the response.
func (h *Harness) Sync() {
	h.t.Helper()
	ctx := h.t.Context()
	// Send a hover request on a non-existent URI. The server will process it
	// after all queued notifications, ensuring they've completed.
	var result *protocol.Hover
	_ = h.client.CallResult(ctx, "textDocument/hover", &protocol.HoverParams{ //nolint:errcheck // sync barrier; result is intentionally discarded
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: "file:///nonexistent"},
			Position:     protocol.Position{Line: 0, Character: 0},
		},
	}, &result)
}

// Diagnostics returns captured diagnostics for the given URI.
func (h *Harness) Diagnostics(uri string) []protocol.Diagnostic {
	h.diagMu.Lock()
	defer h.diagMu.Unlock()
	return h.diagnostics[uri]
}

// OpenDocumentRaw opens a document with a custom languageID. This is useful
// for testing that non-yammm/markdown files are handled correctly.
func (h *Harness) OpenDocumentRaw(uri, languageID, content string) error {
	h.t.Helper()
	ctx := h.t.Context()
	return h.client.Notify(ctx, "textDocument/didOpen", &protocol.DidOpenTextDocumentParams{ //nolint:wrapcheck // test utility
		TextDocument: protocol.TextDocumentItem{
			URI:        uri,
			LanguageID: languageID,
			Version:    1,
			Text:       content,
		},
	})
}

// JRPCServer returns the underlying jrpc2 server for notification wiring.
func (h *Harness) JRPCServer() *jrpc2.Server {
	return h.local.Server
}

// Close shuts down the harness.
func (h *Harness) Close() {
	_ = h.local.Close()
}
