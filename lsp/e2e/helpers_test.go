package e2e_test

import (
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	lsp "github.com/simon-lentz/yammm/lsp"
	"github.com/simon-lentz/yammm/lsp/internal/testutil"
)

// newTestHarness creates a harness for integration testing with a real LSP server.
func newTestHarness(t *testing.T, root string) *testutil.Harness {
	t.Helper()

	// Use silent logging for tests
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// Create server with test configuration
	server := lsp.NewServer(logger, lsp.Config{
		ModuleRoot: root,
	})

	ctx := t.Context()
	h := testutil.NewHarness(t, server.Mux(), root)
	server.Workspace().SetNotifier(func(method string, params any) {
		_ = h.JRPCServer().Notify(ctx, method, params)
	})
	return h
}

// newTestHarnessWithServer creates a harness with an initialized LSP server,
// returning both the harness and server for tests that need direct workspace access.
func newTestHarnessWithServer(t *testing.T, root string) (*testutil.Harness, *lsp.Server) {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := lsp.NewServer(logger, lsp.Config{ModuleRoot: root})
	ctx := t.Context()
	h := testutil.NewHarness(t, server.Mux(), root)
	server.Workspace().SetNotifier(func(method string, params any) {
		_ = h.JRPCServer().Notify(ctx, method, params)
	})
	require.NoError(t, h.Initialize(), "harness initialization failed")
	return h, server
}

// newMarkdownTestHarness creates a harness for markdown integration testing.
// Initializes the server with the given root directory.
func newMarkdownTestHarness(t *testing.T, root string) *testutil.Harness {
	t.Helper()
	h := newTestHarness(t, root)
	err := h.Initialize()
	require.NoError(t, err, "harness initialization failed")
	return h
}
