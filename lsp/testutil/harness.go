// Package testutil provides integration testing utilities for the Yammm LSP.
package testutil

import (
	"net/url"
	"path/filepath"
	"runtime"
	"testing"

	protocol "github.com/tliron/glsp/protocol_3_16"
)

// PathToURI converts a filesystem path to a file:// URI.
// This is a local copy to avoid import cycles with the lsp package.
// It matches the behavior of lsp.PathToURI including Windows support.
// Exported for equivalence testing with lsp.PathToURI.
func PathToURI(path string) string {
	// Ensure absolute path
	if !filepath.IsAbs(path) {
		absPath, err := filepath.Abs(path)
		if err == nil {
			path = absPath
		}
	}

	// Normalize to forward slashes for URI path
	uriPath := filepath.ToSlash(path)

	// Windows: prepend leading slash for drive letters (C:/path → /C:/path)
	if runtime.GOOS == "windows" && len(uriPath) >= 2 && uriPath[1] == ':' && isWindowsDriveLetter(uriPath[0]) {
		uriPath = "/" + uriPath
	}

	// Use url.URL to properly escape the path
	u := url.URL{
		Scheme: "file",
		Path:   uriPath,
	}
	return u.String()
}

// isWindowsDriveLetter reports whether c is a valid Windows drive letter (A-Z or a-z).
func isWindowsDriveLetter(c byte) bool {
	return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
}

// Harness provides an in-process LSP server for integration testing.
// It sets up a full LSP server connected to an in-memory client transport.
type Harness struct {
	t       *testing.T
	handler *protocol.Handler

	// Root path for the test workspace
	Root string
}

// NewHarness creates a new test harness with the given handler.
func NewHarness(t *testing.T, handler *protocol.Handler, root string) *Harness {
	t.Helper()

	return &Harness{
		t:       t,
		handler: handler,
		Root:    root,
	}
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

	_, err := h.handler.Initialize(nil, params)
	if err != nil {
		return err //nolint:wrapcheck // test utility
	}

	return h.handler.Initialized(nil, &protocol.InitializedParams{}) //nolint:wrapcheck // test utility
}

// OpenDocument opens a document with the given content.
func (h *Harness) OpenDocument(path, content string) error {
	h.t.Helper()

	absPath := path
	if !filepath.IsAbs(path) {
		absPath = filepath.Join(h.Root, path)
	}

	uri := PathToURI(absPath)
	return h.handler.TextDocumentDidOpen(nil, &protocol.DidOpenTextDocumentParams{ //nolint:wrapcheck // test utility
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
	return h.handler.TextDocumentDidOpen(nil, &protocol.DidOpenTextDocumentParams{ //nolint:wrapcheck // test utility
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
	return h.handler.TextDocumentDidChange(nil, &protocol.DidChangeTextDocumentParams{ //nolint:wrapcheck // test utility
		TextDocument: protocol.VersionedTextDocumentIdentifier{
			TextDocumentIdentifier: protocol.TextDocumentIdentifier{
				URI: uri,
			},
			Version: protocol.Integer(version), //nolint:gosec // test utility, version is always small
		},
		ContentChanges: []any{
			protocol.TextDocumentContentChangeEventWhole{
				Text: content,
			},
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
	return h.handler.TextDocumentDidClose(nil, &protocol.DidCloseTextDocumentParams{ //nolint:wrapcheck // test utility
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
	return h.handler.TextDocumentHover(nil, &protocol.HoverParams{ //nolint:wrapcheck // test utility
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: uri,
			},
			Position: protocol.Position{
				Line:      protocol.UInteger(line), //nolint:gosec // test utility, line is always small
				Character: protocol.UInteger(char), //nolint:gosec // test utility, char is always small
			},
		},
	})
}

// Definition requests go-to-definition at the given position.
func (h *Harness) Definition(path string, line, char int) (any, error) {
	h.t.Helper()

	absPath := path
	if !filepath.IsAbs(path) {
		absPath = filepath.Join(h.Root, path)
	}

	uri := PathToURI(absPath)
	return h.handler.TextDocumentDefinition(nil, &protocol.DefinitionParams{ //nolint:wrapcheck // test utility
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: uri,
			},
			Position: protocol.Position{
				Line:      protocol.UInteger(line), //nolint:gosec // test utility, line is always small
				Character: protocol.UInteger(char), //nolint:gosec // test utility, char is always small
			},
		},
	})
}

// Completion requests completion items at the given position.
func (h *Harness) Completion(path string, line, char int) (any, error) {
	h.t.Helper()

	absPath := path
	if !filepath.IsAbs(path) {
		absPath = filepath.Join(h.Root, path)
	}

	uri := PathToURI(absPath)
	return h.handler.TextDocumentCompletion(nil, &protocol.CompletionParams{ //nolint:wrapcheck // test utility
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: uri,
			},
			Position: protocol.Position{
				Line:      protocol.UInteger(line), //nolint:gosec // test utility, line is always small
				Character: protocol.UInteger(char), //nolint:gosec // test utility, char is always small
			},
		},
	})
}

// DocumentSymbols requests document symbols.
func (h *Harness) DocumentSymbols(path string) (any, error) {
	h.t.Helper()

	absPath := path
	if !filepath.IsAbs(path) {
		absPath = filepath.Join(h.Root, path)
	}

	uri := PathToURI(absPath)
	return h.handler.TextDocumentDocumentSymbol(nil, &protocol.DocumentSymbolParams{ //nolint:wrapcheck // test utility
		TextDocument: protocol.TextDocumentIdentifier{
			URI: uri,
		},
	})
}

// Formatting requests document formatting.
func (h *Harness) Formatting(path string) ([]protocol.TextEdit, error) {
	h.t.Helper()

	absPath := path
	if !filepath.IsAbs(path) {
		absPath = filepath.Join(h.Root, path)
	}

	uri := PathToURI(absPath)
	return h.handler.TextDocumentFormatting(nil, &protocol.DocumentFormattingParams{ //nolint:wrapcheck // test utility
		TextDocument: protocol.TextDocumentIdentifier{
			URI: uri,
		},
		// Options are sent per the LSP protocol but intentionally ignored by
		// the formatter — yammm formatting is canonical (like gofmt). These
		// values match the hardcoded behavior for documentation purposes only.
		Options: protocol.FormattingOptions{
			"tabSize":      4,
			"insertSpaces": false,
		},
	})
}

// Handler returns the protocol handler for low-level test access.
func (h *Harness) Handler() *protocol.Handler {
	return h.handler
}

// Close shuts down the harness.
func (h *Harness) Close() {
	// No-op for now - the harness doesn't own any resources
}
