package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRun_VersionFlag(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	err := run(&buf, []string{"--version"})

	require.NoError(t, err)
	assert.Contains(t, buf.String(), "yammm-lsp")
}

func TestRun_HelpFlag(t *testing.T) {
	t.Parallel()

	err := run(io.Discard, []string{"-help"})
	require.NoError(t, err)
}

func TestRun_InvalidFlag(t *testing.T) {
	t.Parallel()

	err := run(io.Discard, []string{"--invalid-flag-xyz"})
	assert.Error(t, err)
}

func TestRun_InvalidLogLevel(t *testing.T) {
	t.Parallel()

	err := run(io.Discard, []string{"--log-level", "invalid"})
	require.Error(t, err)
	assert.ErrorContains(t, err, "invalid log level")
}

func TestRun_DebounceDelayFlag(t *testing.T) {
	t.Parallel()

	// --version short-circuits before the server starts, so success here
	// proves the duration flag parses and is accepted.
	var buf bytes.Buffer
	err := run(&buf, []string{"--debounce-delay", "5ms", "--version"})
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "yammm-lsp")

	err = run(io.Discard, []string{"--debounce-delay", "not-a-duration"})
	assert.Error(t, err, "a malformed duration must fail flag parsing")
}

func TestSetupLogger_ValidLevels(t *testing.T) {
	t.Parallel()

	levels := []string{"error", "warn", "info", "debug", "trace"}
	for _, level := range levels {
		t.Run(level, func(t *testing.T) {
			t.Parallel()

			logger, cleanup, err := setupLogger(level, "")
			require.NoError(t, err)
			require.NotNil(t, logger)
			require.NotNil(t, cleanup)
			cleanup()
		})
	}
}

func TestSetupLogger_InvalidLevel(t *testing.T) {
	t.Parallel()

	_, _, err := setupLogger("invalid", "")
	require.Error(t, err)
	assert.ErrorContains(t, err, "invalid log level")
}

func TestSetupLogger_FileCreation(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	logPath := filepath.Join(dir, "test.log")

	logger, cleanup, err := setupLogger("info", logPath)
	require.NoError(t, err)
	require.NotNil(t, logger)

	logger.Info("test message")
	cleanup()

	data, err := os.ReadFile(logPath)
	require.NoError(t, err)
	assert.NotEmpty(t, data)
	assert.Contains(t, string(data), "test message")
}

func TestSetupLogger_FileAppends(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	logPath := filepath.Join(dir, "test.log")

	require.NoError(t, os.WriteFile(logPath, []byte("existing\n"), 0o600))

	logger, cleanup, err := setupLogger("info", logPath)
	require.NoError(t, err)

	logger.Info("appended message")
	cleanup()

	data, err := os.ReadFile(logPath)
	require.NoError(t, err)

	content := string(data)
	assert.Contains(t, content, "existing")
	assert.Contains(t, content, "appended message")
}
