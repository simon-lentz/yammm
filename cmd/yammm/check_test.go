package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
)

func TestCheck_ValidJSON(t *testing.T) {
	t.Parallel()

	cmd := newRootCmd("test")
	cmd.SetArgs([]string{"check", "testdata/valid.yammm", "testdata/data.json"})

	err := cmd.Execute()
	require.NoError(t, err)
}

func TestCheck_ValidCSV(t *testing.T) {
	t.Parallel()

	cmd := newRootCmd("test")
	cmd.SetArgs([]string{"check", "--type", "Person", "testdata/valid.yammm", "testdata/data.csv"})

	err := cmd.Execute()
	require.NoError(t, err)
}

func TestCheck_CSVRequiresType(t *testing.T) {
	t.Parallel()

	cmd := newRootCmd("test")
	cmd.SetArgs([]string{"check", "testdata/valid.yammm", "testdata/data.csv"})

	err := cmd.Execute()
	require.Error(t, err)
}

func TestCheck_MissingDataFile(t *testing.T) {
	t.Parallel()

	cmd := newRootCmd("test")
	cmd.SetArgs([]string{"check", "testdata/valid.yammm", "testdata/nonexistent.json"})

	err := cmd.Execute()
	require.Error(t, err)
}

func TestCheck_AutoDetectJSON(t *testing.T) {
	t.Parallel()

	cmd := newRootCmd("test")
	cmd.SetArgs([]string{"check", "testdata/valid.yammm", "testdata/data.json"})

	err := cmd.Execute()
	require.NoError(t, err)
}

func TestCheck_InvalidData_ExitCode(t *testing.T) {
	t.Parallel()

	// Person missing required "name" field
	tmpDir := t.TempDir()
	dataPath := filepath.Join(tmpDir, "bad.json")
	require.NoError(t, os.WriteFile(dataPath, []byte(`{"Person": [{"id": "x"}]}`), 0o600))

	code := executeCmd(t, "check", "testdata/valid.yammm", dataPath)
	assert.Equal(t, cli.ExitValidation, code)
}

func TestCheck_MissingDataFile_ExitCode(t *testing.T) {
	t.Parallel()

	code := executeCmd(t, "check", "testdata/valid.yammm", "testdata/nonexistent.json")
	assert.Equal(t, cli.ExitUsage, code)
}

func TestCheck_CSVRequiresType_ExitCode(t *testing.T) {
	t.Parallel()

	code := executeCmd(t, "check", "testdata/valid.yammm", "testdata/data.csv")
	assert.Equal(t, cli.ExitUsage, code)
}

func TestCheck_JSONFormatOutput(t *testing.T) {
	t.Parallel()

	// Person missing required "name" field
	tmpDir := t.TempDir()
	dataPath := filepath.Join(tmpDir, "bad.json")
	require.NoError(t, os.WriteFile(dataPath, []byte(`{"Person": [{"id": "x"}]}`), 0o600))

	cmd := newRootCmd("test")
	var errBuf bytes.Buffer
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"check", "--format", "json", "testdata/valid.yammm", dataPath})

	err := cmd.Execute()
	require.Error(t, err)

	output := errBuf.String()
	assert.Contains(t, output, `"severity"`)
	assert.Contains(t, output, `"error"`)
}
