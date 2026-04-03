package e2e_test

import (
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	lsp "github.com/simon-lentz/yammm/lsp"
)

func TestNewServer(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := lsp.NewServer(logger, lsp.Config{ModuleRoot: "/test/root"})

	require.NotNil(t, server)
	assert.NotNil(t, server.Mux())
	assert.NotNil(t, server.Workspace())
}

func TestConfig_ModuleRoot(t *testing.T) {
	t.Parallel()

	cfg := lsp.Config{ModuleRoot: "/custom/path"}
	assert.Equal(t, "/custom/path", cfg.ModuleRoot)
}

func TestConfig_Empty(t *testing.T) {
	t.Parallel()

	cfg := lsp.Config{}
	assert.Empty(t, cfg.ModuleRoot)
}

func TestServer_Close_BeforeRunStdio(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := lsp.NewServer(logger, lsp.Config{})

	// Close before RunStdio should not panic
	_ = server.Close()
}

func TestServer_Close(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := lsp.NewServer(logger, lsp.Config{})

	// Close before RunStdio should be safe (GetStdio returns nil)
	assert.NoError(t, server.Close())

	// Close is idempotent: subsequent calls return the same result
	assert.NoError(t, server.Close())

	// Third close for good measure
	assert.NoError(t, server.Close())
}

func TestServer_WorkspaceCreated(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := lsp.NewServer(logger, lsp.Config{ModuleRoot: "/test"})

	require.NotNil(t, server.Workspace())

	// The workspace should inherit the config's module root
	assert.Equal(t, "/test", server.Workspace().FindModuleRoot("/any/path/file.yammm"))
}
