package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRun_UsageIsPrintedOnce pins B53: flag's own parser already calls Usage on
// a bad flag, and run called it a second time, so an operator read the whole
// option list twice and -help printed it once — the two paths disagreeing about
// what a usage error looks like.
func TestRun_UsageIsPrintedOnce(t *testing.T) {
	t.Parallel()

	var bad, help bytes.Buffer
	require.Error(t, run(io.Discard, &bad, []string{"--invalid-flag-xyz"}))
	require.NoError(t, run(io.Discard, &help, []string{"-help"}))

	const marker = "Usage: yammm-lsp"
	assert.Equal(t, 1, strings.Count(bad.String(), marker),
		"a bad flag must print one usage block, not two:\n%s", bad.String())
	assert.Equal(t, 1, strings.Count(help.String(), marker),
		"-help must print one usage block:\n%s", help.String())
	assert.Contains(t, bad.String(), "-log-level",
		"the option list must reach the injected writer, not os.Stderr")
}

func TestRun_VersionFlag(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	err := run(&buf, io.Discard, []string{"--version"})

	require.NoError(t, err)
	assert.Contains(t, buf.String(), "yammm-lsp")
}

func TestRun_HelpFlag(t *testing.T) {
	t.Parallel()

	err := run(io.Discard, io.Discard, []string{"-help"})
	require.NoError(t, err)
}

func TestRun_InvalidFlag(t *testing.T) {
	t.Parallel()

	err := run(io.Discard, io.Discard, []string{"--invalid-flag-xyz"})
	assert.Error(t, err)
}

func TestRun_InvalidLogLevel(t *testing.T) {
	t.Parallel()

	err := run(io.Discard, io.Discard, []string{"--log-level", "invalid"})
	require.Error(t, err)
	assert.ErrorContains(t, err, "invalid log level")
}

func TestRun_DebounceDelayFlag(t *testing.T) {
	t.Parallel()

	// --version short-circuits before the server starts, so success here
	// proves the duration flag parses and is accepted.
	var buf bytes.Buffer
	err := run(&buf, io.Discard, []string{"--debounce-delay", "5ms", "--version"})
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "yammm-lsp")

	err = run(io.Discard, io.Discard, []string{"--debounce-delay", "not-a-duration"})
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
