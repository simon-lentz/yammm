package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFmt_Stdout(t *testing.T) {
	t.Parallel()

	cmd := newRootCmd("test")
	cmd.SetArgs([]string{"fmt", "testdata/unformatted.yammm"})

	err := cmd.Execute()
	require.NoError(t, err)
}

func TestFmt_WriteInPlace(t *testing.T) {
	t.Parallel()

	// Copy the unformatted file to a temp location
	content, err := os.ReadFile("testdata/unformatted.yammm")
	require.NoError(t, err)

	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test.yammm")
	require.NoError(t, os.WriteFile(tmpFile, content, 0o644)) //nolint:gosec // test file

	cmd := newRootCmd("test")
	cmd.SetArgs([]string{"fmt", "--write", tmpFile})

	err = cmd.Execute()
	require.NoError(t, err)

	// Read the formatted file
	formatted, err := os.ReadFile(tmpFile)
	require.NoError(t, err)

	// Should contain canonical formatting
	assert.Contains(t, string(formatted), "schema \"test\"")
	assert.Contains(t, string(formatted), "\tid")
}

func TestFmt_Idempotent(t *testing.T) {
	t.Parallel()

	// Format a valid file — already canonical
	cmd := newRootCmd("test")
	cmd.SetArgs([]string{"fmt", "testdata/valid.yammm"})

	err := cmd.Execute()
	require.NoError(t, err)
}

func TestFmt_WriteIdempotent(t *testing.T) {
	t.Parallel()

	// Copy the valid (already formatted) file
	content, err := os.ReadFile("testdata/valid.yammm")
	require.NoError(t, err)

	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test.yammm")
	require.NoError(t, os.WriteFile(tmpFile, content, 0o644)) //nolint:gosec // test file

	cmd := newRootCmd("test")
	cmd.SetArgs([]string{"fmt", "--write", tmpFile})
	require.NoError(t, cmd.Execute())

	// Content should be unchanged
	after, err := os.ReadFile(tmpFile)
	require.NoError(t, err)
	assert.Equal(t, string(content), string(after))
}

func TestFmt_MissingFile(t *testing.T) {
	t.Parallel()

	cmd := newRootCmd("test")
	cmd.SetArgs([]string{"fmt", "testdata/nonexistent.yammm"})

	err := cmd.Execute()
	require.Error(t, err)
}
